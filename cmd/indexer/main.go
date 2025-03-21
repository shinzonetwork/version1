package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"shinzo/version1/config"
	"shinzo/version1/pkg/indexer"

	"go.uber.org/zap"
)

func main() {
	// Create logger
	logger, err := zap.NewDevelopment()
	if err != nil {
		zap.L().Fatal("Failed to create logger", zap.Error(err))
	}
	defer logger.Sync()

	// Load config
	cfg, err := config.LoadConfig("config/config.yaml")
	if err != nil {
		logger.Fatal("Failed to load config", zap.Error(err))
	}

	// Create indexer
	idx, err := indexer.NewIndexer(cfg, logger)
	if err != nil {
		logger.Fatal("Failed to create indexer", zap.Error(err))
	}

	// Create context that can be canceled
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		logger.Info("Received shutdown signal", zap.String("signal", sig.String()))
		cancel()
	}()

	logger.Info("Starting indexer",
		zap.String("defra_host", cfg.DefraDB.Host),
		zap.Int("defra_port", cfg.DefraDB.Port))

	// Start indexer
	if err := idx.Start(ctx); err != nil {
		logger.Fatal("Indexer failed", zap.Error(err))
	}
}
