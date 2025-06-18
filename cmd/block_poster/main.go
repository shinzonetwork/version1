package main

import (
	"context"
	"log"
	"time"

	"shinzo/version1/config"
	"shinzo/version1/pkg/defra"
	"shinzo/version1/pkg/logger"
	"shinzo/version1/pkg/rpc"
	"shinzo/version1/pkg/types"

	"math/big"
)

func main() {
	// Load config
	cfg, err := config.LoadConfig("config.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	logger.Init(cfg.Logger.Development)
	sugar := logger.Sugar

	// Connect to Geth RPC node (with gRPC support and HTTP fallback)
	client, err := rpc.NewGRPCEthereumClient("", cfg.Geth.NodeURL) // Empty gRPC addr for now, will use HTTP fallback
	if err != nil {
		log.Fatalf("Failed to connect to Geth node: %v", err)
	}
	defer client.Close()

	// Create DefraDB block handler
	blockHandler := defra.NewBlockHandler(cfg.DefraDB.Host, cfg.DefraDB.Port)

	// Instead of looping over a range, always query the latest block from Geth
	for {
		// Fetch the latest block
		gethBlock, err := client.GetLatestBlock(context.Background())
		if err != nil {
			sugar.Error("Failed to get latest block: ", err)
			time.Sleep(time.Second * 3)
			continue
		}

		blockNum := gethBlock.Number
		sugar.Info("Processing latest block: ", blockNum)

		// Get network ID for transaction conversion (skip if it fails)
		networkID, err := client.GetNetworkID(context.Background())
		if err != nil {
			sugar.Warn("Failed to get network ID (continuing anyway): ", err)
			networkID = big.NewInt(1) // Default to mainnet
		}
		_ = networkID // Use networkID if needed for transaction processing

		// Convert Geth transactions to local transactions (already done in convertGethBlock)
		transactions := gethBlock.Transactions

		// Build the complete block
		block := buildBlock(gethBlock, transactions)

		// Create block in DefraDB
		blockDocId := blockHandler.CreateBlock(context.Background(), block, sugar)
		sugar.Info("Created block with DocID: ", blockDocId)

		// Process transactions
		for _, tx := range transactions {
			// Note: Transaction receipts would need to be fetched separately if needed
			// For now, we'll skip receipt processing to get the basic indexing working
			
			// Create transaction in DefraDB
			txDocId := blockHandler.CreateTransaction(context.Background(), &tx, blockDocId, sugar)
			sugar.Info("Created transaction with DocID: ", txDocId)

			// Update transaction relationships
			blockHandler.UpdateTransactionRelationships(context.Background(), blockDocId, tx.Hash, sugar)
		}

		sugar.Info("Successfully processed block: ", blockNum)
		time.Sleep(time.Duration(cfg.Indexer.BlockPollingInterval) * time.Second)
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
