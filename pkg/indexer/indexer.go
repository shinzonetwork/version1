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
	config      *config.Config
	lastBlock   int
	client      *http.Client
	transformer *lens.Transformer
}

type Response struct {
	Data map[string][]struct {
		DocID string `json:"_docID"`
	} `json:"data"`
}

func NewIndexer(cfg *config.Config) (*Indexer, error) {
	logConfig := zap.NewDevelopmentConfig()
	logConfig.Level = zap.NewAtomicLevelAt(zap.ErrorLevel)
	logger, err := logConfig.Build()
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	client := &http.Client{
		Timeout: time.Second * 30,
	}

	transformConfig := &lens.TransformConfig{
		Pipelines: map[string]string{
			"block":       "config/pipelines/block.yaml",
			"transaction": "config/pipelines/transaction.yaml",
			"log":         "config/pipelines/log.yaml",
			"event":       "config/pipelines/event.yaml",
		},
		DefaultPipeline: "config/pipelines/block.yaml",
		Options: struct {
			MaxConcurrency int  `yaml:"maxConcurrency"`
			BufferSize     int  `yaml:"bufferSize"`
			EnableMetrics  bool `yaml:"enableMetrics"`
		}{
			MaxConcurrency: 10,
			BufferSize:     1000,
			EnableMetrics:  true,
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
		config:      cfg,
		lastBlock:   cfg.Indexer.StartHeight - 1,
		client:      client,
		transformer: transformer,
	}

	return i, nil
}

func (i *Indexer) Start(ctx context.Context) error {
	i.logger.Info("Starting indexer",
		zap.Int("start_height", i.config.Indexer.StartHeight),
		zap.Int("batch_size", i.config.Indexer.BatchSize),
	)

	// Get the highest block number from the database
	highestBlock, err := i.getHighestBlockNumber(ctx)
	if err != nil {
		return fmt.Errorf("failed to get highest block: %w", err)
	}

	i.lastBlock = highestBlock
	i.logger.Info("starting from block", zap.Int("block", i.lastBlock+1))

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		ticker := time.NewTicker(time.Duration(i.config.Indexer.BlockPollingInterval * float64(time.Second)))
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				i.logger.Info("Indexer shutting down gracefully",
					zap.Int("last_processed_block", i.lastBlock))
				return nil
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

func (i *Indexer) getHighestBlockNumber(ctx context.Context) (int, error) {
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
		return i.config.Indexer.StartHeight - 1, nil
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

func (i *Indexer) postToCollection(ctx context.Context, collection string, data map[string]interface{}) (string, error) {
	// Get primary key based on collection type
	var primaryKey string
	switch collection {
	case "Block", "Transaction":
		if hash, ok := data["hash"].(string); ok {
			primaryKey = hash
		} else {
			return "", fmt.Errorf("missing hash field for %s", collection)
		}
	case "Log", "Event":
		if logIndex, ok := data["logIndex"].(string); ok {
			primaryKey = logIndex
		} else {
			return "", fmt.Errorf("missing logIndex field for %s", collection)
		}
	default:
		return "", fmt.Errorf("unknown collection: %s", collection)
	}

	// Prepare request URL
	url := fmt.Sprintf("%s/api/v0/collections/%s", i.defraURL, collection)

	// For Block collection, we no longer need to extract from block field
	// since the pipeline now outputs flattened data
	if collection == "Block" {
		// Ensure hash field exists and is a string
		if _, ok := data["hash"].(string); !ok {
			return "", fmt.Errorf("invalid or missing hash field in block data")
		}
	}

	// Convert data to JSON
	jsonData, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("failed to marshal data: %w", err)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
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

	// Check response status
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		var errorResp struct {
			Error interface{} `json:"error"`
		}
		body, _ := io.ReadAll(resp.Body)
		if err := json.Unmarshal(body, &errorResp); err == nil {
			return "", fmt.Errorf("failed to create document: %v", errorResp.Error)
		}
		return "", fmt.Errorf("failed to create document: status=%d body=%s", resp.StatusCode, string(body))
	}

	return primaryKey, nil
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
	// Get block data from Alchemy
	blockData, err := i.alchemy.GetBlockByNumber(ctx, i.lastBlock+1)
	if err != nil {
		return fmt.Errorf("failed to get block: %w", err)
	}

	i.logger.Info("Processing block",
		zap.Int("block_number", i.lastBlock+1),
		zap.String("block_hash", blockData["hash"].(string)))

	// Check if block already exists
	exists, err := i.blockExists(ctx, blockData["hash"].(string))
	if err != nil {
		return fmt.Errorf("failed to check if block exists: %w", err)
	}
	if exists {
		i.logger.Info("Block already exists, skipping",
			zap.Int("block_number", i.lastBlock+1),
			zap.String("block_hash", blockData["hash"].(string)))
		i.lastBlock++
		return nil
	}

	// Transform and store block
	transformedBlock, err := i.transformer.Transform(ctx, "block", blockData)
	if err != nil {
		return fmt.Errorf("failed to transform block: %w", err)
	}

	// Remove relationship fields before storing
	delete(transformedBlock, "transactions")

	// Store block
	blockID, err := i.postToCollection(ctx, "Block", transformedBlock)
	if err != nil {
		return fmt.Errorf("failed to store block: %w", err)
	}
	i.logger.Info("Stored block",
		zap.Int("block_number", i.lastBlock+1),
		zap.String("block_hash", blockData["hash"].(string)),
		zap.String("block_id", blockID))

	// Process transactions
	transactions := blockData["transactions"].([]interface{})
	for _, tx := range transactions {
		txData := tx.(map[string]interface{})
		i.logger.Info("Processing transaction",
			zap.String("tx_hash", txData["hash"].(string)),
			zap.String("block_hash", blockData["hash"].(string)))

		// Transform and store transaction
		transformedTx, err := i.transformer.Transform(ctx, "transaction", txData)
		if err != nil {
			return fmt.Errorf("failed to transform transaction: %w", err)
		}

		// Remove relationship fields before storing
		delete(transformedTx, "block")
		delete(transformedTx, "logs")

		txID, err := i.postToCollection(ctx, "Transaction", transformedTx)
		if err != nil {
			return fmt.Errorf("failed to store transaction: %w", err)
		}
		i.logger.Info("Stored transaction",
			zap.String("tx_hash", txData["hash"].(string)),
			zap.String("tx_id", txID))

		// Update block-transaction relationship from Block side
		if err := i.updateBlockTransactionRelation(ctx, blockID, txID); err != nil {
			return fmt.Errorf("failed to update block-transaction relation: %w", err)
		}

		// Process logs
		logs := txData["logs"].([]interface{})
		for _, log := range logs {
			logData := log.(map[string]interface{})
			i.logger.Info("Processing log",
				zap.String("tx_hash", txData["hash"].(string)),
				zap.String("log_index", logData["logIndex"].(string)))

			// Transform and store log
			transformedLog, err := i.transformer.Transform(ctx, "log", logData)
			if err != nil {
				return fmt.Errorf("failed to transform log: %w", err)
			}

			// Remove relationship fields before storing
			delete(transformedLog, "transaction")
			delete(transformedLog, "events")

			logID, err := i.postToCollection(ctx, "Log", transformedLog)
			if err != nil {
				return fmt.Errorf("failed to store log: %w", err)
			}
			i.logger.Info("Stored log",
				zap.String("tx_hash", txData["hash"].(string)),
				zap.String("log_index", logData["logIndex"].(string)),
				zap.String("log_id", logID))

			// Update transaction-log relationship from Transaction side
			if err := i.updateTransactionLogRelation(ctx, txID, logID); err != nil {
				return fmt.Errorf("failed to update transaction-log relation: %w", err)
			}

			// Process events
			events := logData["events"].([]interface{})
			for _, event := range events {
				eventData := event.(map[string]interface{})
				i.logger.Info("Processing event",
					zap.String("tx_hash", txData["hash"].(string)),
					zap.String("log_index", logData["logIndex"].(string)),
					zap.String("event_name", eventData["name"].(string)))

				// Transform and store event
				transformedEvent, err := i.transformer.Transform(ctx, "event", eventData)
				if err != nil {
					return fmt.Errorf("failed to transform event: %w", err)
				}

				// Remove relationship fields before storing
				delete(transformedEvent, "log")

				eventID, err := i.postToCollection(ctx, "Event", transformedEvent)
				if err != nil {
					return fmt.Errorf("failed to store event: %w", err)
				}
				i.logger.Info("Stored event",
					zap.String("tx_hash", txData["hash"].(string)),
					zap.String("log_index", logData["logIndex"].(string)),
					zap.String("event_name", eventData["name"].(string)),
					zap.String("event_id", eventID))

				// Update log-event relationship from Log side
				if err := i.updateLogEventRelation(ctx, logID, eventID); err != nil {
					return fmt.Errorf("failed to update log-event relation: %w", err)
				}
			}
		}
	}

	i.lastBlock++
	return nil
}

func (i *Indexer) updateBlockTransactionRelation(ctx context.Context, blockID, txID string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Only update from Block side since it's the primary side of the relationship
	mutation := fmt.Sprintf(`mutation {
		update_Block(docID: %q, input: {
			transactions: {
				connect: [%q]
			}
		}) {
			_docID
			transactions {
				_docID
			}
		}
	}`, blockID, txID)

	i.logger.Info("Updating block-transaction relation",
		zap.String("block_id", blockID),
		zap.String("tx_id", txID))

	req, err := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("%s/api/v0/graphql", i.defraURL), bytes.NewBuffer([]byte(fmt.Sprintf(`{"query": %q}`, mutation))))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := i.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send GraphQL request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		var errorResp struct {
			Error interface{} `json:"error"`
		}
		if err := json.Unmarshal(body, &errorResp); err == nil {
			return fmt.Errorf("GraphQL request failed: %v", errorResp.Error)
		}
		return fmt.Errorf("GraphQL request failed: status=%d body=%s", resp.StatusCode, string(body))
	}

	i.logger.Info("Updated block-transaction relation",
		zap.String("block_id", blockID),
		zap.String("tx_id", txID),
		zap.String("response", string(body)))

	return nil
}

func (i *Indexer) updateTransactionLogRelation(ctx context.Context, txID, logID string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Only update from Transaction side since it's the primary side of the relationship
	mutation := fmt.Sprintf(`mutation {
		update_Transaction(docID: %q, input: {
			logs: {
				connect: [%q]
			}
		}) {
			_docID
			logs {
				_docID
			}
		}
	}`, txID, logID)

	i.logger.Info("Updating transaction-log relation",
		zap.String("tx_id", txID),
		zap.String("log_id", logID))

	req, err := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("%s/api/v0/graphql", i.defraURL), bytes.NewBuffer([]byte(fmt.Sprintf(`{"query": %q}`, mutation))))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := i.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send GraphQL request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		var errorResp struct {
			Error any `json:"error"`
		}
		if err := json.Unmarshal(body, &errorResp); err == nil {
			return fmt.Errorf("GraphQL request failed: %v", errorResp.Error)
		}
		return fmt.Errorf("GraphQL request failed: status=%d body=%s", resp.StatusCode, string(body))
	}

	i.logger.Info("Updated transaction-log relation",
		zap.String("tx_id", txID),
		zap.String("log_id", logID),
		zap.String("response", string(body)))

	return nil
}

func (i *Indexer) updateLogEventRelation(ctx context.Context, logID, eventID string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Only update from Log side since it's the primary side of the relationship
	mutation := fmt.Sprintf(`mutation {
		update_Log(docID: %q, input: {
			events: {
				connect: [%q]
			}
		}) {
			_docID
			events {
				_docID
			}
		}
	}`, logID, eventID)

	i.logger.Info("Updating log-event relation",
		zap.String("log_id", logID),
		zap.String("event_id", eventID))

	req, err := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("%s/api/v0/graphql", i.defraURL), bytes.NewBuffer([]byte(fmt.Sprintf(`{"query": %q}`, mutation))))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := i.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send GraphQL request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		var errorResp struct {
			Error any `json:"error"`
		}
		if err := json.Unmarshal(body, &errorResp); err == nil {
			return fmt.Errorf("GraphQL request failed: %v", errorResp.Error)
		}
		return fmt.Errorf("GraphQL request failed: status=%d body=%s", resp.StatusCode, string(body))
	}

	i.logger.Info("Updated log-event relation",
		zap.String("log_id", logID),
		zap.String("event_id", eventID),
		zap.String("response", string(body)))

	return nil
}
