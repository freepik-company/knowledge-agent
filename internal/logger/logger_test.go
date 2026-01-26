package logger

import (
	"testing"
)

func TestInitializeWithEmptyValues(t *testing.T) {
	// Test that empty values are handled with defaults
	err := Initialize(Config{
		Level:      "",
		Format:     "",
		OutputPath: "",
	})
	if err != nil {
		t.Fatalf("Initialize should not fail with empty values: %v", err)
	}

	log := Get()
	if log == nil {
		t.Fatal("Get() returned nil logger")
	}

	// Should not panic
	log.Info("Test log message")
}

func TestInitializeWithValidValues(t *testing.T) {
	err := Initialize(Config{
		Level:      "debug",
		Format:     "console",
		OutputPath: "stdout",
	})
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	log := Get()
	log.Debugw("Test debug message", "key", "value")
	log.Infow("Test info message", "level", "info")
}

func TestInitializeWithInvalidLevel(t *testing.T) {
	err := Initialize(Config{
		Level:      "invalid",
		Format:     "console",
		OutputPath: "stdout",
	})
	if err == nil {
		t.Fatal("Initialize should fail with invalid log level")
	}
}
