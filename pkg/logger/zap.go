package logger

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var Sugar *zap.SugaredLogger

func Init(development bool) {
	var zapLevel zap.AtomicLevel
	if development {
		zapLevel = zap.NewAtomicLevelAt(zap.DebugLevel)
	} else {
		zapLevel = zap.NewAtomicLevelAt(zap.InfoLevel)
	}

	encoderConfig := zap.NewDevelopmentEncoderConfig()
	encoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder

	config := zap.Config{
		Level:            zapLevel,
		Development:      development,
		Encoding:         "console",
		EncoderConfig:    encoderConfig,
		OutputPaths:      []string{"stdout", "logs/logfile"},
		ErrorOutputPaths: []string{"stderr"},
	}
	logger, err := config.Build()
	if err != nil {
		panic(err)
	}
	Sugar = logger.Sugar()
}
