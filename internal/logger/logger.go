package logger

import (
	"fmt"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func New(debug bool) (*zap.Logger, error) {
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.EncodeTime = zapcore.RFC3339TimeEncoder

	var (
		options []zap.Option
		lvl     zapcore.Level
	)

	if debug {
		lvl = zap.DebugLevel
	} else {
		options = append(options, zap.AddStacktrace(zapcore.PanicLevel))
		lvl = zap.InfoLevel
	}

	config := zap.Config{
		Level:            zap.NewAtomicLevelAt(lvl),
		Development:      debug,
		Encoding:         "json",
		EncoderConfig:    encoderConfig,
		OutputPaths:      []string{"stdout"},
		ErrorOutputPaths: []string{"stderr"},
	}

	log, err := config.Build(options...)
	if err != nil {
		return nil, fmt.Errorf("build logger: %w", err)
	}

	return log, nil
}
