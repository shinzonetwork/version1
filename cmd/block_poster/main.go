package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"shinzo/version1/config"
	"shinzo/version1/pkg/defra"
	"shinzo/version1/pkg/logger"
	"shinzo/version1/pkg/rpc"
	"shinzo/version1/pkg/types"

	"go.uber.org/zap"
)

func main() {
	// Load config
	cfg, err := config.LoadConfig("config/config.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	logger.Init(cfg.Logger.Development)
	sugar := logger.Sugar

	// Create Alchemy client
	alchemy := rpc.NewAlchemyClient(cfg.Alchemy.APIKey)

	// Create DefraDB block handler
	blockHandler := defra.NewBlockHandler(cfg.DefraDB.Host, cfg.DefraDB.Port)

	// Starting block number (in decimal)
	// Get the highest block number from DefraDB
	startBlock := blockHandler.GetHighestBlockNumber(context.Background(), sugar)
	if startBlock == 0 {
		startBlock = int64(cfg.Indexer.StartHeight)
	}

	endBlock := startBlock + 10

	//go routines start here

	workerPool := make(chan struct{}, 1)

	for blockNum := startBlock; blockNum <= endBlock; blockNum++ {

		workerPool <- struct{}{}

		go func(blockNum int64) {

			// Convert blocknumber to hex for Alchemy API
			blockHex := fmt.Sprintf("0x%x", blockNum)

			sugar.Info("Processing block: ", blockNum, ", hex: ", blockHex)
			// Get block with retry logic
			var block *types.Block
			for retries := 0; retries < 3; retries++ {
				block, err = alchemy.GetBlock(context.Background(), blockHex)
				if block != nil && err == nil {
					sugar.Info("Received block from Alechemy")
					break
				}
				sugar.Error("Failed to get block ", blockNum, " retries: ", retries+1, " error: ", err)
				time.Sleep(time.Second * 1)
				if err != nil {
					sugar.Error("Skipping block ", blockNum, " after all retries failed: ", err)
					return
				}

			}
			// Process all transactions first
			var transactions []types.Transaction
			var allLogs []types.Log
			var allEvents []types.Event
			var count int
			// First pass: collect all data
			for _, tx := range block.Transactions {
				sugar.Debug("count: ", count+1)
				receipt, err := getTransactionReceipt(context.Background(), alchemy, tx.Hash, sugar)

				if err != nil {
					sugar.Error("Skipping transaction ", tx.Hash, ": ", err)
					return
				}

				// Build logs and events
				logs, events := processLogsAndEvents(receipt, sugar)
				sugar.Debug("Transaction contains ", len(logs), " logs & ", len(events), " events")
				allLogs = append(allLogs, logs...)
				allEvents = append(allEvents, events...)

				transactions = append(transactions, buildTransaction(tx, receipt, logs))
				count++
			}
			// Create block once
			sugar.Debug("...Build a Block")
			block = buildBlock(block, transactions)
			sugar.Debug("done...CreateBlock")
			blockDocID := blockHandler.CreateBlock(context.Background(), block, sugar)
			if blockDocID == "" {
				sugar.Error("Failed to create block, skipping relationships")
				return
			}
			// Create all transactions
			txIDMap := make(map[string]string) // hash -> docID
			for _, tx := range transactions {
				txID := blockHandler.CreateTransaction(context.Background(), &tx, blockDocID, sugar)
				if txID != "" {
					txIDMap[tx.Hash] = txID
				}
			}

			// Create all logs and update relationships
			logIDMap := make(map[string]string) // logIndex -> docID
			for _, log := range allLogs {
				if logID := blockHandler.CreateLog(context.Background(), &log, blockDocID, txIDMap[log.TransactionHash], sugar); logID != "" {
					logIDMap[log.LogIndex] = logID
				}
			}

			// Create all events and update relationships
			for _, event := range allEvents {
				_ = blockHandler.CreateEvent(context.Background(), &event, logIDMap[event.LogIndex], sugar)
			}

			sugar.Info("Successfully processed block - ", blockNum, " with DocID - ", blockDocID, " (#", len(transactions), " transactions)")

			<-workerPool
		}(blockNum)

	}
}

// Helper functions to move complexity out of main loop
func getTransactionReceipt(ctx context.Context, alchemy *rpc.AlchemyClient, hash string, sugar *zap.SugaredLogger) (*types.TransactionReceipt, error) {
	for retries := 0; retries < 3; retries++ {
		receipt, err := alchemy.GetTransactionReceipt(ctx, hash)
		if err == nil {
			return receipt, nil
		}
		sugar.Error("Failed to get receipt for tx %s, retry %d: %v", hash, retries+1, err)
		time.Sleep(time.Second * 2)
	}
	return nil, fmt.Errorf("failed after 3 retries")
}

func processLogsAndEvents(receipt *types.TransactionReceipt, sugar *zap.SugaredLogger) ([]types.Log, []types.Event) {
	var processedLogs []types.Log
	var processedEvents []types.Event
	sugar.Debug("Processing Logs: ")
	for _, rcptLog := range receipt.Logs {
		// Create events from log
		var events []types.Event
		if len(rcptLog.Topics) > 0 {
			sugar.Debug("Single log: ", rcptLog.LogIndex, " with topics: ", rcptLog.Topics)

			// First topic is always the event signature
			eventSig := rcptLog.Topics[0]
			// Create event
			event := types.Event{
				ContractAddress:  rcptLog.Address,
				EventName:        eventSig,     // We could decode this to human-readable name if we had ABI
				Parameters:       rcptLog.Data, // Raw data, could be decoded with ABI
				TransactionHash:  rcptLog.TransactionHash,
				BlockHash:        rcptLog.BlockHash,
				BlockNumber:      receipt.BlockNumber,
				TransactionIndex: rcptLog.TransactionIndex,
				LogIndex:         rcptLog.LogIndex,
			}
			sugar.Debug("Gathered event: " + event.EventName)
			events = append(events, event)
			sugar.Debug("...Appeneded")
		}
		// Build log with events
		sugar.Debug("... Adding log: ", rcptLog.LogIndex, " with topics: ", rcptLog.Topics)
		processedLogs = append(processedLogs, types.Log{
			Address:          rcptLog.Address,
			Topics:           rcptLog.Topics,
			Data:             rcptLog.Data,
			BlockNumber:      rcptLog.BlockNumber,
			TransactionHash:  rcptLog.TransactionHash,
			TransactionIndex: rcptLog.TransactionIndex,
			BlockHash:        rcptLog.BlockHash,
			LogIndex:         rcptLog.LogIndex,
			Removed:          rcptLog.Removed,
			Events:           events,
		})

		processedEvents = append(processedEvents, events...)
		sugar.Debug("done...")
	}
	sugar.Debug("Processed ", len(processedLogs), " logs and ", len(processedEvents), " events")
	return processedLogs, processedEvents
}

func buildTransaction(tx types.Transaction, receipt *types.TransactionReceipt, logs []types.Log) types.Transaction {
	return types.Transaction{
		Hash:             tx.Hash,
		BlockHash:        tx.BlockHash,
		BlockNumber:      tx.BlockNumber,
		From:             tx.From,
		To:               tx.To,
		Value:            tx.Value,
		Gas:              tx.Gas,
		GasPrice:         tx.GasPrice,
		Input:            tx.Input,
		Nonce:            tx.Nonce,
		TransactionIndex: tx.TransactionIndex,
		Status:           receipt.Status == "0x1",
		Logs:             logs,
	}
}

func buildBlock(block *types.Block, transactions []types.Transaction) *types.Block {
	return &types.Block{
		Hash:             block.Hash,
		Number:           block.Number,
		Timestamp:        block.Timestamp,
		ParentHash:       block.ParentHash,
		Difficulty:       block.Difficulty,
		GasUsed:          block.GasUsed,
		GasLimit:         block.GasLimit,
		Nonce:            block.Nonce,
		Miner:            block.Miner,
		Size:             block.Size,
		StateRoot:        block.StateRoot,
		Sha3Uncles:       block.Sha3Uncles,
		TransactionsRoot: block.TransactionsRoot,
		ReceiptsRoot:     block.ReceiptsRoot,
		ExtraData:        block.ExtraData,
		Transactions:     transactions,
	}
}
