package indexer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"bytes"
	"shinzo/version1/config"
	"shinzo/version1/pkg/lens"
	"shinzo/version1/pkg/rpc"
)

type Indexer struct {
	defraURL    string
	alchemy     *rpc.AlchemyClient
	logger      *zap.Logger
	lastBlock   int
	client      *http.Client
	transformer *lens.Transformer
}

type Response struct {
	Data map[string][]struct {
		DocID string `json:"_docID"`
	} `json:"data"`
}

func NewIndexer(cfg *config.Config, logger *zap.Logger) (*Indexer, error) {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	transformConfig := &lens.TransformConfig{
		Pipelines: map[string]string{
			"block":       "config/pipelines/block.yaml",
			"transaction": "config/pipelines/transaction.yaml",
			"log":         "config/pipelines/log.yaml",
			"event":       "config/pipelines/event.yaml",
		},
	}

	transformer, err := lens.NewTransformer(transformConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create transformer: %w", err)
	}

	i := &Indexer{
		defraURL:    fmt.Sprintf("http://%s:%d", cfg.DefraDB.Host, cfg.DefraDB.Port),
		alchemy:     rpc.NewAlchemyClient(cfg.Alchemy.APIKey),
		logger:      logger,
		lastBlock:   cfg.Indexer.StartHeight - 1,
		client:      client,
		transformer: transformer,
	}

	return i, nil
}

func (i *Indexer) Start(ctx context.Context) error {
	i.logger.Info("Starting indexer",
		zap.Int("start_height", i.lastBlock+1))

	// Get the highest block number from the database
	highestBlock, err := i.getHighestBlock(ctx)
	if err != nil {
		return fmt.Errorf("failed to get highest block: %w", err)
	}
	i.lastBlock = highestBlock

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
				if err := i.processNextBlock(ctx); err != nil {
					i.logger.Error("Failed to process block",
						zap.Int("block_number", i.lastBlock+1),
						zap.Error(err))
					return err
				}
			}
		}
	})

	return g.Wait()
}

func (i *Indexer) getHighestBlock(ctx context.Context) (int, error) {
	query := `{
		Block(orderBy: {number: DESC}, limit: 1) {
			number
		}
	}`

	req, err := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("%s/api/v0/graphql", i.defraURL), bytes.NewBuffer([]byte(fmt.Sprintf(`{"query": %q}`, query))))
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := i.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("failed to send GraphQL request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errorResp struct {
			Error interface{} `json:"error"`
		}
		body, _ := io.ReadAll(resp.Body)
		if err := json.Unmarshal(body, &errorResp); err == nil {
			return 0, fmt.Errorf("GraphQL request failed: %v", errorResp.Error)
		}
		return 0, fmt.Errorf("GraphQL request failed: status=%d body=%s", resp.StatusCode, string(body))
	}

	var result struct {
		Data struct {
			Block []struct {
				Number string `json:"number"`
			} `json:"Block"`
		}
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(result.Data.Block) == 0 {
		return i.lastBlock + 1, nil
	}

	// Convert hex string to int
	highestBlock := result.Data.Block[0].Number
	if !strings.HasPrefix(highestBlock, "0x") {
		return 0, fmt.Errorf("invalid block number format: %s", highestBlock)
	}

	blockNum, err := strconv.ParseInt(highestBlock[2:], 16, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse block number: %w", err)
	}

	return int(blockNum), nil
}

func (i *Indexer) blockExists(ctx context.Context, blockHash string) (bool, error) {
	query := fmt.Sprintf(`{
		Block(filter: { hash: %q }) {
			_docID
			hash
		}
	}`, blockHash)

	req, err := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("%s/api/v0/graphql", i.defraURL), bytes.NewBuffer([]byte(fmt.Sprintf(`{"query": %q}`, query))))
	if err != nil {
		return false, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := i.client.Do(req)
	if err != nil {
		return false, fmt.Errorf("failed to send GraphQL request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errorResp struct {
			Error interface{} `json:"error"`
		}
		body, _ := io.ReadAll(resp.Body)
		if err := json.Unmarshal(body, &errorResp); err == nil {
			return false, fmt.Errorf("GraphQL request failed: %v", errorResp.Error)
		}
		return false, fmt.Errorf("GraphQL request failed: status=%d body=%s", resp.StatusCode, string(body))
	}

	var result struct {
		Data struct {
			Block []struct {
				DocID string `json:"_docID"`
				Hash  string `json:"hash"`
			} `json:"Block"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, fmt.Errorf("failed to decode response: %w", err)
	}

	return len(result.Data.Block) > 0, nil
}

func (i *Indexer) postToCollection(ctx context.Context, collectionName string, data map[string]interface{}) (string, error) {
	// Build the mutation query with explicit variable types
	var variables []string
	var variableTypes []string

	// Create a copy of data to modify
	dataCopy := make(map[string]interface{})

	// Only include fields that DefraDB expects
	for k, v := range data {
		switch k {
		case "baseFeePerGas", "logsBloom", "mixHash", "transactions", "uncles", "withdrawals", "withdrawalsRoot":
			// Skip these fields as they're not in our schema
			continue
		case "block_id":
			// Special handling for block_id as it's an ID type
			dataCopy[k] = v
			variables = append(variables, fmt.Sprintf("$%s", k))
			variableTypes = append(variableTypes, fmt.Sprintf("$%s: ID!", k))
		default:
			dataCopy[k] = v
			variables = append(variables, fmt.Sprintf("$%s", k))
			variableTypes = append(variableTypes, fmt.Sprintf("$%s: String!", k))
		}
	}

	query := fmt.Sprintf(`
		mutation Create%s(%s) {
			create_%s(input: [{
				%s
			}]) {
				_docID
			}
		}
	`, collectionName, strings.Join(variableTypes, ", "),
		collectionName,
		strings.Join(mapToFields(dataCopy), ",\n"))

	// Convert input data to JSON for variables
	jsonData, err := json.Marshal(map[string]interface{}{
		"query":     query,
		"variables": dataCopy,
	})
	if err != nil {
		return "", fmt.Errorf("failed to marshal query: %w", err)
	}

	i.logger.Info("Sending GraphQL mutation",
		zap.String("collection", collectionName),
		zap.String("query", query),
		zap.Any("variables", dataCopy))

	// Create request
	req, err := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("%s/api/v0/graphql", i.defraURL), bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Send request
	resp, err := i.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Read response body for error handling
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	// Parse response
	var result struct {
		Data map[string][]struct {
			DocID string `json:"_docID"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	// Log the full response for debugging
	i.logger.Info("DefraDB create response",
		zap.String("collection", collectionName),
		zap.String("response", string(body)))

	// Check for GraphQL errors
	if len(result.Errors) > 0 {
		return "", fmt.Errorf("GraphQL error: %s", result.Errors[0].Message)
	}

	// Extract DocID from the response
	createKey := fmt.Sprintf("create_%s", collectionName)
	createResult, ok := result.Data[createKey]
	if !ok || len(createResult) == 0 {
		return "", fmt.Errorf("no document created")
	}

	docID := createResult[0].DocID
	i.logger.Info("Created document",
		zap.String("collection", collectionName),
		zap.String("doc_id", docID))

	return docID, nil
}

// mapToFields converts a map to GraphQL field assignments
func mapToFields(data map[string]interface{}) []string {
	var fields []string
	for k := range data {
		fields = append(fields, fmt.Sprintf("%s: $%s", k, k))
	}
	sort.Strings(fields) // Sort for consistent order
	return fields
}

func (i *Indexer) postCollection(ctx context.Context, collection string, docID string, data map[string]interface{}) ([]byte, error) {
	// First get the document to get its version
	getURL := fmt.Sprintf("%s/api/v0/collections/%s/%s", i.defraURL, collection, docID)
	req, err := http.NewRequestWithContext(ctx, "GET", getURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create GET request: %w", err)
	}

	resp, err := i.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get document: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errorResp struct {
			Error interface{} `json:"error"`
		}
		body, _ := io.ReadAll(resp.Body)
		if err := json.Unmarshal(body, &errorResp); err == nil {
			return nil, fmt.Errorf("failed to get document: %v", errorResp.Error)
		}
		return nil, fmt.Errorf("failed to get document: status=%d body=%s", resp.StatusCode, string(body))
	}

	var docResponse struct {
		Version string `json:"_version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&docResponse); err != nil {
		return nil, fmt.Errorf("failed to decode document response: %w", err)
	}

	// Now update the document with its version
	patchURL := fmt.Sprintf("%s/api/v0/collections/%s/%s/%s", i.defraURL, collection, docID, docResponse.Version)

	// Convert data to JSON
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal data: %w", err)
	}

	// Create PATCH request
	patchReq, err := http.NewRequestWithContext(ctx, "PATCH", patchURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create PATCH request: %w", err)
	}
	patchReq.Header.Set("Content-Type", "application/json")

	// Send request
	patchResp, err := i.client.Do(patchReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer patchResp.Body.Close()

	// Check response status
	if patchResp.StatusCode != http.StatusOK {
		var errorResp struct {
			Error interface{} `json:"error"`
		}
		body, _ := io.ReadAll(patchResp.Body)
		if err := json.Unmarshal(body, &errorResp); err == nil {
			return nil, fmt.Errorf("failed to update document: %v", errorResp.Error)
		}
		return nil, fmt.Errorf("failed to update document: status=%d body=%s", patchResp.StatusCode, string(body))
	}

	// Read response body
	body, err := io.ReadAll(patchResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	return body, nil
}

func (i *Indexer) processNextBlock(ctx context.Context) error {
	// Get next block
	blockData, err := i.alchemy.GetBlockByNumber(ctx, i.lastBlock+1)
	if err != nil {
		return fmt.Errorf("failed to get block %d: %w", i.lastBlock+1, err)
	}

	i.logger.Info("Processing block",
		zap.Int("number", i.lastBlock+1),
		zap.String("hash", blockData["hash"].(string)))

	// Store the block first to get its DocID
	blockDataCopy := make(map[string]interface{})
	for k, v := range blockData {
		switch k {
		case "timestamp":
			blockDataCopy["timestamp"] = v
		case "stateRoot":
			blockDataCopy["stateRoot"] = v
		case "sha3Uncles":
			blockDataCopy["sha3Uncles"] = v
		case "transactionsRoot":
			blockDataCopy["transactionsRoot"] = v
		case "receiptsRoot":
			blockDataCopy["receiptsRoot"] = v
		case "baseFeePerGas", "logsBloom", "mixHash", "transactions", "uncles", "withdrawals", "withdrawalsRoot":
			// Skip these fields as they're not in our schema
			continue
		default:
			blockDataCopy[k] = v
		}
	}

	blockDocID, err := i.postToCollection(ctx, "Block", blockDataCopy)
	if err != nil {
		return fmt.Errorf("failed to store block %d: %w", i.lastBlock+1, err)
	}

	i.logger.Info("Stored block",
		zap.Int("number", i.lastBlock+1),
		zap.String("block_id", blockData["hash"].(string)),
		zap.String("doc_id", blockDocID))

	// Process transactions
	transactions, ok := blockData["transactions"].([]interface{})
	if !ok {
		return fmt.Errorf("invalid transactions data in block %d", i.lastBlock+1)
	}

	txHashToID := make(map[string]string) // txHash -> stored DocID
	for _, txData := range transactions {
		tx := txData.(map[string]interface{})
		hash := tx["hash"].(string)

		// Create a copy of tx to avoid modifying the original
		txCopy := make(map[string]interface{})
		for k, v := range tx {
			txCopy[k] = v
		}

		// Set block_id to the block's DocID
		txCopy["block_id"] = blockDocID

		txID, err := i.postToCollection(ctx, "Transaction", txCopy)
		if err != nil {
			return fmt.Errorf("failed to store transaction: %w", err)
		}

		txHashToID[hash] = txID
		i.logger.Info("Stored transaction",
			zap.String("hash", hash),
			zap.String("doc_id", txID))

		// Process logs for this transaction
		logs, ok := tx["logs"].([]interface{})
		if !ok {
			continue // Skip if no logs
		}

		for _, logData := range logs {
			log := logData.(map[string]interface{})

			// Create a copy of log to avoid modifying the original
			logCopy := make(map[string]interface{})
			for k, v := range log {
				logCopy[k] = v
			}

			// Set transaction_id for relationship
			logCopy["transaction_id"] = txID

			logID, err := i.postToCollection(ctx, "Log", logCopy)
			if err != nil {
				return fmt.Errorf("failed to store log: %w", err)
			}

			i.logger.Info("Stored log",
				zap.String("transaction_hash", hash),
				zap.String("doc_id", logID))

			// Process events for this log
			events, ok := log["events"].([]interface{})
			if !ok {
				continue // Skip if no events
			}

			for _, eventData := range events {
				event := eventData.(map[string]interface{})

				// Create a copy of event to avoid modifying the original
				eventCopy := make(map[string]interface{})
				for k, v := range event {
					eventCopy[k] = v
				}

				// Set log_id for relationship
				eventCopy["log_id"] = logID

				eventID, err := i.postToCollection(ctx, "Event", eventCopy)
				if err != nil {
					return fmt.Errorf("failed to store event: %w", err)
				}

				i.logger.Info("Stored event",
					zap.String("transaction_hash", hash),
					zap.String("doc_id", eventID))
			}
		}
	}

	i.logger.Info("Processed block",
		zap.Int("block_number", i.lastBlock+1),
		zap.String("block_hash", blockData["hash"].(string)),
		zap.String("block_id", blockDocID))

	i.lastBlock++
	return nil
}
