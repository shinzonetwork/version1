package utils

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
)

type RetryConfig struct {
	MaxRetries int
	Delay      time.Duration
	Logger     *zap.SugaredLogger
}

func DefaultRetryConfig(logger *zap.SugaredLogger) RetryConfig {
	return RetryConfig{
		MaxRetries: 3,
		Delay:      time.Second,
		Logger:     logger,
	}
}

// RequestResourceWithRetries executes a function with retry logic
// The function should return a result and an error
// Config is optional - if not provided, uses DefaultRetryConfig
func RequestResourceWithRetries[T any](
	ctx context.Context,
	logger *zap.SugaredLogger,
	operation func() (T, error),
	operationName string,
	configs ...RetryConfig,
) (T, error) {
	var config RetryConfig
	if len(configs) > 0 {
		config = configs[0]
	} else {
		config = DefaultRetryConfig(logger)
	}

	var result T
	var lastErr error

	for retries := 0; retries <= config.MaxRetries; retries++ {
		if retries > 0 {
			config.Logger.Error("Retry %d/%d for %s: %v", retries, config.MaxRetries, operationName, lastErr)
			time.Sleep(config.Delay)
		}

		result, lastErr = operation()
		if lastErr == nil {
			if retries > 0 {
				config.Logger.Info("Successfully completed %s after %d retries", operationName, retries)
			}
			return result, nil
		}

		config.Logger.Error("Failed %s, attempt %d/%d: %v", operationName, retries+1, config.MaxRetries+1, lastErr)
	}

	return result, fmt.Errorf("failed %s after %d attempts: %w", operationName, config.MaxRetries+1, lastErr)
}
