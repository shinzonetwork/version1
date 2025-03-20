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
	logConfig.Level = zap.NewAtomicLevelAt(zap.DebugLevel)
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

	resp, err := i.client.Post(
		fmt.Sprintf("%s/api/v0/graphql", i.defraURL),
		"application/json",
		bytes.NewReader([]byte(fmt.Sprintf(`{"query": %q}`, query))),
	)
	if err != nil {
		return 0, fmt.Errorf("failed to get highest block: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("failed to get highest block: status=%d body=%s", resp.StatusCode, string(body))
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

	resp, err := i.client.Post(
		fmt.Sprintf("%s/api/v0/graphql", i.defraURL),
		"application/json",
		bytes.NewReader([]byte(fmt.Sprintf(`{"query": %q}`, query))),
	)
	if err != nil {
		return false, fmt.Errorf("failed to check if block exists: %w", err)
	}
	defer resp.Body.Close()

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

	// Log the request for debugging
	// if i.logger.Core().Enabled(zap.DebugLevel) {
	// 	i.logger.Debug("Sending collection request",
	// 		zap.String("collection", collection),
	// 		zap.String("url", url))
	// }

	// Send request
	resp, err := i.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("failed to create document: status=%d body=%s", resp.StatusCode, string(body))
	}

	return primaryKey, nil
}

func (i *Indexer) processNextBlock(ctx context.Context) error {
	// Get block data from Alchemy
	blockData, err := i.alchemy.GetBlockByNumber(ctx, i.lastBlock+1)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("failed to get block: %w", err)
	}

	// Log raw block data structure for debugging
	if i.logger.Core().Enabled(zap.DebugLevel) {
		i.logger.Debug("Raw block data structure",
			zap.Any("block_data", blockData))
	}

	i.logger.Info("Retrieved block",
		zap.Int("block_number", i.lastBlock+1),
		zap.String("hash", blockData["hash"].(string)))

	// Transform block data using LensVM pipeline
	i.logger.Info("Transforming block data",
		zap.Int("block_number", i.lastBlock+1))
	transformedData, err := i.transformer.Transform(ctx, "block", blockData)
	if err != nil {
		if ctx.Err() != nil {
			i.logger.Info("Context canceled, stopping block processing")
			return nil
		}
		return fmt.Errorf("failed to transform block data: %w", err)
	}

	// Log transformed data structure for debugging
	if i.logger.Core().Enabled(zap.DebugLevel) {
		i.logger.Debug("Transformed block data structure",
			zap.Any("block_data", transformedData))
	}

	// Block data is now at the root level
	blockInfo := transformedData

	// Extract transactions before storing block
	transactions := []interface{}{}
	if txs, ok := blockInfo["transactions"].([]interface{}); ok {
		transactions = txs
		delete(blockInfo, "transactions") // Remove transactions from block data
	}

	// Validate required block fields
	hash, ok := blockInfo["hash"].(string)
	if !ok {
		return fmt.Errorf("invalid or missing hash field in block data")
	}

	// Check if block already exists
	exists, err := i.blockExists(ctx, hash)
	if err != nil {
		return fmt.Errorf("failed to check if block exists: %w", err)
	}

	if exists {
		i.logger.Info("Block already exists, skipping",
			zap.Int("block_number", i.lastBlock+1),
			zap.String("hash", hash))
		i.lastBlock++
		return nil
	}

	// Store block in DefraDB
	blockID, err := i.postToCollection(ctx, "Block", blockInfo)
	if err != nil {
		// Check if error is due to document already existing
		if strings.Contains(err.Error(), "document with the given ID already exists") {
			i.logger.Info("Block already exists (concurrent indexing), skipping",
				zap.Int("block_number", i.lastBlock+1),
				zap.String("hash", hash))
			i.lastBlock++
			return nil
		}
		return fmt.Errorf("failed to store block: %w", err)
	}

	// Process transactions separately
	for _, tx := range transactions {
		txData, ok := tx.(map[string]interface{})
		if !ok {
			i.logger.Warn("Invalid transaction data format", zap.Any("tx", tx))
			continue
		}

		// Ensure logs field exists, even if null
		if _, exists := txData["logs"]; !exists {
			txData["logs"] = nil
		}

		// Ensure events array exists, even if empty
		if _, exists := txData["events"]; !exists {
			txData["events"] = []interface{}{}
		}

		// Extract logs before storing transaction
		logs := []interface{}{}
		if txLogs, ok := txData["logs"].([]interface{}); ok {
			logs = txLogs
			delete(txData, "logs") // Remove logs from transaction data
		}

		// Store transaction in DefraDB with block relationship
		txData["block_transactions"] = map[string]interface{}{
			"connect": blockID,
		}
		delete(txData, "block_id")

		// Store transaction in DefraDB
		txID, err := i.postToCollection(ctx, "Transaction", txData)
		if err != nil {
			return fmt.Errorf("failed to store transaction: %w", err)
		}

		// Process logs if they exist
		for _, log := range logs {
			logData, ok := log.(map[string]interface{})
			if !ok {
				i.logger.Warn("Invalid log data format", zap.Any("log", log))
				continue
			}

			// Store log in DefraDB
			logID, err := i.postToCollection(ctx, "Log", logData)
			if err != nil {
				return fmt.Errorf("failed to store log: %w", err)
			}

			// Update transaction to connect with the log
			err = i.updateTransactionLogRelation(ctx, txID, logID)
			if err != nil {
				return fmt.Errorf("failed to update transaction-log relation: %w", err)
			}
		}
	}

	i.logger.Info("Successfully processed block",
		zap.Int("block_number", i.lastBlock+1),
		zap.String("hash", hash))

	i.lastBlock++
	return nil
}

func (i *Indexer) postGraphQL(ctx context.Context, mutation string) ([]byte, error) {
	// i.logger.Debug("sending mutation", zap.String("mutation", mutation))

	resp, err := i.client.Post(
		fmt.Sprintf("%s/api/v0/graphql", i.defraURL),
		"application/json",
		bytes.NewReader([]byte(fmt.Sprintf(`{"query": %q}`, mutation))),
	)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// i.logger.Debug("store response", zap.ByteString("body", body))
	return body, nil
}

func (i *Indexer) updateTransactionLogRelation(ctx context.Context, txID, logID string) error {
	mutation := fmt.Sprintf(`mutation {
		update_Transaction(docID: %q, input: {
			transaction_logs: {
				connect: [%q]
			}
		}) {
			_docID
		}
	}`, txID, logID)

	_, err := i.postGraphQL(ctx, mutation)
	if err != nil {
		return fmt.Errorf("failed to update transaction-log relation: %w", err)
	}

	return nil
}
