package logger

import (
	"log/slog"
	"os"
)

func main() {
	// Create a new logger with a JSON handler
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	logger.Info("JSONHandler Example", "Content", "Logging in JSON format")
	logger.Info("TextHandler Example", "Content", "Logging in text format")
	slog.SetDefault(logger)

}
