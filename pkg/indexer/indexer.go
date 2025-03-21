package indexer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"shinzo/version1/config"
	"shinzo/version1/pkg/lens"
	"shinzo/version1/pkg/rpc"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
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
			"log":        "config/pipelines/log.yaml",
			"event":      "config/pipelines/event.yaml",
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
	// Build the mutation query
	query := fmt.Sprintf(`
		mutation Create%s($input: %sInput!) {
			create_%s(input: $input) {
				_docID
			}
		}
	`, collectionName, collectionName, collectionName)

	// Convert input data to JSON for variables
	jsonData, err := json.Marshal(map[string]interface{}{
		"query": query,
		"variables": map[string]interface{}{
			"input": data,
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to marshal query: %w", err)
	}

	i.logger.Info("Sending GraphQL mutation",
		zap.String("collection", collectionName),
		zap.Any("input", data))

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
	// Get the next block
	blockData, err := i.alchemy.GetBlockByNumber(ctx, i.lastBlock+1)
	if err != nil {
		return fmt.Errorf("failed to get block %d: %w", i.lastBlock+1, err)
	}

	// Transform and store block first
	i.logger.Info("Processing block",
		zap.Int("number", i.lastBlock+1),
		zap.String("hash", blockData["hash"].(string)))

	transformedBlock, err := i.transformer.Transform(ctx, "block", blockData)
	if err != nil {
		return fmt.Errorf("failed to transform block %d: %w", i.lastBlock+1, err)
	}

	// 1. Store block first to get its DocID
	blockDocID, err := i.postToCollection(ctx, "Block", transformedBlock)
	if err != nil {
		return fmt.Errorf("failed to store block %d: %w", i.lastBlock+1, err)
	}
	i.logger.Info("Stored block",
		zap.Int("number", i.lastBlock+1),
		zap.String("doc_id", blockDocID),
		zap.String("hash", blockData["hash"].(string)))

	// Extract transactions
	transactions, ok := blockData["transactions"].([]interface{})
	if !ok {
		return fmt.Errorf("invalid transactions data in block %d", i.lastBlock+1)
	}

	// 2. Store all transactions with block relationship
	txHashToID := make(map[string]string) // txHash -> stored DocID
	for _, txData := range transactions {
		tx := txData.(map[string]interface{})
		txHash := tx["hash"].(string)

		// Create a copy of tx to avoid modifying the original
		txCopy := make(map[string]interface{})
		for k, v := range tx {
			txCopy[k] = v
		}

		// Set both relationship fields
		txCopy["block_id"] = blockDocID // For DefraDB relationship
		txCopy["block"] = blockDocID    // For GraphQL relationship

		i.logger.Info("Processing transaction",
			zap.String("tx_hash", txHash),
			zap.String("block_doc_id", blockDocID))

		// Transform and store transaction
		transformedTx, err := i.transformer.Transform(ctx, "transaction", txCopy)
		if err != nil {
			i.logger.Error("Failed to transform transaction",
				zap.String("hash", txHash),
				zap.String("block_id", blockDocID),
				zap.Error(err))
			return fmt.Errorf("failed to transform transaction %s: %w", txHash, err)
		}

		i.logger.Info("Transaction after transform",
			zap.String("hash", txHash),
			zap.String("block_id", transformedTx["block_id"].(string)))

		txID, err := i.postToCollection(ctx, "Transaction", transformedTx)
		if err != nil {
			i.logger.Error("Failed to store transaction",
				zap.String("hash", txHash),
				zap.String("block_id", blockDocID),
				zap.Error(err))
			return fmt.Errorf("failed to store transaction: %w", err)
		}
		txHashToID[txHash] = txID
		i.logger.Info("Stored transaction",
			zap.String("hash", txHash),
			zap.String("tx_id", txID),
			zap.String("block_id", blockDocID))
	}

	// 3. Store all logs with transaction_id
	for _, txData := range transactions {
		tx := txData.(map[string]interface{})
		txHash := tx["hash"].(string)
		txID := txHashToID[txHash]

		logs, ok := tx["logs"].([]interface{})
		if !ok {
			continue // Skip if no logs
		}

		for _, logData := range logs {
			log := logData.(map[string]interface{})
			logHash := fmt.Sprintf("%s-%d", txHash, int(log["logIndex"].(float64)))

			// Set transaction_id for relationship
			log["transaction_id"] = txID

			// Transform and store log
			transformedLog, err := i.transformer.Transform(ctx, "log", log)
			if err != nil {
				i.logger.Error("Failed to transform log",
					zap.String("tx_hash", txHash),
					zap.String("log_hash", logHash),
					zap.Error(err))
				return fmt.Errorf("failed to transform log %s: %w", logHash, err)
			}

			logID, err := i.postToCollection(ctx, "Log", transformedLog)
			if err != nil {
				i.logger.Error("Failed to store log",
					zap.String("tx_hash", txHash),
					zap.String("log_hash", logHash),
					zap.Error(err))
				return fmt.Errorf("failed to store log: %w", err)
			}
			i.logger.Info("Stored log",
				zap.String("tx_hash", txHash),
				zap.String("log_id", logID),
				zap.String("transaction_id", txID))

			// 4. Store events with log_id
			if events, ok := log["events"].([]interface{}); ok {
				for _, eventData := range events {
					event := eventData.(map[string]interface{})

					// Set log_id for relationship
					event["log_id"] = logID

					// Transform and store event
					transformedEvent, err := i.transformer.Transform(ctx, "event", event)
					if err != nil {
						i.logger.Error("Failed to transform event",
							zap.String("log_hash", logHash),
							zap.Error(err))
						return fmt.Errorf("failed to transform event in log %s: %w", logHash, err)
					}

					eventID, err := i.postToCollection(ctx, "Event", transformedEvent)
					if err != nil {
						i.logger.Error("Failed to store event",
							zap.String("log_hash", logHash),
							zap.Error(err))
						return fmt.Errorf("failed to store event: %w", err)
					}
					i.logger.Info("Stored event",
						zap.String("log_hash", logHash),
						zap.String("event_id", eventID),
						zap.String("log_id", logID))
				}
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
