package logger

import (
	"fmt"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Config holds logger configuration
type Config struct {
	Level      string // debug, info, warn, error
	Format     string // json, console
	OutputPath string // stdout, stderr, file path
}

var globalLogger *zap.Logger

// Initialize creates and configures the global logger
func Initialize(cfg Config) error {
	// Apply defaults for empty values
	if cfg.Level == "" {
		cfg.Level = "info"
	}
	if cfg.Format == "" {
		cfg.Format = "console"
	}
	if cfg.OutputPath == "" {
		cfg.OutputPath = "stdout"
	}

	var zapConfig zap.Config

	// Base config by format
	if cfg.Format == "json" {
		zapConfig = zap.NewProductionConfig()
		zapConfig.EncoderConfig.TimeKey = "timestamp"
		zapConfig.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	} else {
		zapConfig = zap.NewDevelopmentConfig()
		zapConfig.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	}

	// Parse log level
	level, err := zapcore.ParseLevel(cfg.Level)
	if err != nil {
		return fmt.Errorf("invalid log level %q: %w", cfg.Level, err)
	}
	zapConfig.Level = zap.NewAtomicLevelAt(level)

	// Set output paths
	zapConfig.OutputPaths = []string{cfg.OutputPath}
	zapConfig.ErrorOutputPaths = []string{cfg.OutputPath}

	// Build logger
	globalLogger, err = zapConfig.Build(
		zap.AddCallerSkip(1), // Skip wrapper functions in stack trace
	)
	if err != nil {
		return fmt.Errorf("failed to build logger: %w", err)
	}

	return nil
}

// Get returns the global logger (sugar for easier use)
func Get() *zap.SugaredLogger {
	if globalLogger == nil {
		// Fallback to development logger if Initialize wasn't called
		globalLogger, _ = zap.NewDevelopment()
	}
	return globalLogger.Sugar()
}

// Sync flushes buffered logs
func Sync() error {
	if globalLogger != nil {
		return globalLogger.Sync()
	}
	return nil
}
