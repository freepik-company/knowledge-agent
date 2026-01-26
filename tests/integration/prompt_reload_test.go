// +build integration

package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"knowledge-agent/internal/config"
	"knowledge-agent/internal/prompt"
)

// TestPromptHotReload verifies that prompt hot reload works in development mode
func TestPromptHotReload(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping prompt reload test in short mode")
	}

	// Create temporary prompt file
	tmpDir := t.TempDir()
	promptFile := filepath.Join(tmpDir, "test_prompt.txt")

	initialPrompt := "You are a test assistant. Version 1."
	if err := os.WriteFile(promptFile, []byte(initialPrompt), 0644); err != nil {
		t.Fatalf("Failed to write initial prompt: %v", err)
	}

	// Create config with hot reload enabled
	cfg := &config.PromptConfig{
		TemplatePath:    promptFile,
		EnableHotReload: true,
	}

	// Create prompt manager
	manager, err := prompt.NewManager(cfg)
	if err != nil {
		t.Fatalf("Failed to create prompt manager: %v", err)
	}
	defer manager.Close()

	// Verify initial prompt
	currentPrompt := manager.GetPrompt()
	if currentPrompt != initialPrompt {
		t.Errorf("Initial prompt mismatch.\nExpected: %s\nGot: %s", initialPrompt, currentPrompt)
	}

	// Modify the prompt file
	updatedPrompt := "You are a test assistant. Version 2 - UPDATED!"
	if err := os.WriteFile(promptFile, []byte(updatedPrompt), 0644); err != nil {
		t.Fatalf("Failed to write updated prompt: %v", err)
	}

	// Wait for file watcher to detect change (fsnotify should trigger within 1-2 seconds)
	time.Sleep(3 * time.Second)

	// Verify prompt was reloaded
	currentPrompt = manager.GetPrompt()
	if currentPrompt != updatedPrompt {
		t.Errorf("Prompt not reloaded.\nExpected: %s\nGot: %s", updatedPrompt, currentPrompt)
	}

	t.Log("Prompt hot reload successful")
}

// TestPromptManagerWithoutHotReload verifies normal operation without hot reload
func TestPromptManagerWithoutHotReload(t *testing.T) {
	tmpDir := t.TempDir()
	promptFile := filepath.Join(tmpDir, "test_prompt.txt")

	initialPrompt := "You are a static assistant."
	if err := os.WriteFile(promptFile, []byte(initialPrompt), 0644); err != nil {
		t.Fatalf("Failed to write prompt: %v", err)
	}

	cfg := &config.PromptConfig{
		TemplatePath:    promptFile,
		EnableHotReload: false, // Hot reload disabled
	}

	manager, err := prompt.NewManager(cfg)
	if err != nil {
		t.Fatalf("Failed to create prompt manager: %v", err)
	}
	defer manager.Close()

	// Verify initial prompt
	currentPrompt := manager.GetPrompt()
	if currentPrompt != initialPrompt {
		t.Errorf("Initial prompt mismatch.\nExpected: %s\nGot: %s", initialPrompt, currentPrompt)
	}

	// Modify file
	updatedPrompt := "You are a static assistant. UPDATED!"
	if err := os.WriteFile(promptFile, []byte(updatedPrompt), 0644); err != nil {
		t.Fatalf("Failed to write updated prompt: %v", err)
	}

	// Wait a bit
	time.Sleep(2 * time.Second)

	// Verify prompt was NOT reloaded (hot reload disabled)
	currentPrompt = manager.GetPrompt()
	if currentPrompt != initialPrompt {
		t.Errorf("Prompt should not have reloaded.\nExpected: %s\nGot: %s", initialPrompt, currentPrompt)
	}

	t.Log("Prompt manager correctly ignoring file changes with hot reload disabled")
}

// TestPromptManagerBasePrompt verifies loading from base_prompt in config
func TestPromptManagerBasePrompt(t *testing.T) {
	basePrompt := "You are an assistant configured via base_prompt."

	cfg := &config.PromptConfig{
		BasePrompt:      basePrompt,
		TemplatePath:    "", // No template file
		EnableHotReload: false,
	}

	manager, err := prompt.NewManager(cfg)
	if err != nil {
		t.Fatalf("Failed to create prompt manager: %v", err)
	}
	defer manager.Close()

	currentPrompt := manager.GetPrompt()
	if currentPrompt != basePrompt {
		t.Errorf("Base prompt mismatch.\nExpected: %s\nGot: %s", basePrompt, currentPrompt)
	}

	t.Log("Base prompt loaded successfully")
}

// TestPromptManagerPriority verifies template_path takes priority over base_prompt
func TestPromptManagerPriority(t *testing.T) {
	tmpDir := t.TempDir()
	promptFile := filepath.Join(tmpDir, "priority_test.txt")

	filePrompt := "Prompt from file"
	if err := os.WriteFile(promptFile, []byte(filePrompt), 0644); err != nil {
		t.Fatalf("Failed to write prompt file: %v", err)
	}

	cfg := &config.PromptConfig{
		BasePrompt:      "Prompt from base_prompt (should be ignored)",
		TemplatePath:    promptFile,
		EnableHotReload: false,
	}

	manager, err := prompt.NewManager(cfg)
	if err != nil {
		t.Fatalf("Failed to create prompt manager: %v", err)
	}
	defer manager.Close()

	currentPrompt := manager.GetPrompt()
	if currentPrompt != filePrompt {
		t.Errorf("File prompt should take priority.\nExpected: %s\nGot: %s", filePrompt, currentPrompt)
	}

	t.Log("Template path correctly takes priority over base_prompt")
}

// TestPromptManagerConcurrentAccess verifies thread-safe prompt access
func TestPromptManagerConcurrentAccess(t *testing.T) {
	cfg := &config.PromptConfig{
		BasePrompt:      "Thread-safe prompt",
		EnableHotReload: false,
	}

	manager, err := prompt.NewManager(cfg)
	if err != nil {
		t.Fatalf("Failed to create prompt manager: %v", err)
	}
	defer manager.Close()

	// Spawn multiple goroutines reading the prompt concurrently
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan bool, 100)
	for i := 0; i < 100; i++ {
		go func() {
			for {
				select {
				case <-ctx.Done():
					done <- true
					return
				default:
					prompt := manager.GetPrompt()
					if prompt == "" {
						t.Error("Prompt should not be empty")
					}
					time.Sleep(10 * time.Millisecond)
				}
			}
		}()
	}

	// Wait for all goroutines
	<-ctx.Done()
	for i := 0; i < 100; i++ {
		<-done
	}

	t.Log("Concurrent access test passed")
}
