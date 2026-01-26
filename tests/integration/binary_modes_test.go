// +build integration

package integration

import (
	"context"
	"net/http"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestBinaryModeAll tests that the unified binary works in "all" mode
func TestBinaryModeAll(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping binary mode test in short mode")
	}

	// Build the binary first
	buildCmd := exec.Command("go", "build", "-o", "/tmp/knowledge-agent-test", "./cmd/knowledge-agent")
	if output, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build binary: %v\n%s", err, output)
	}
	defer exec.Command("rm", "/tmp/knowledge-agent-test").Run()

	// Start binary in "all" mode
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "/tmp/knowledge-agent-test", "--mode", "all")

	// Capture output
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start binary: %v", err)
	}

	// Wait for services to start
	time.Sleep(5 * time.Second)

	// Test that both ports are listening
	t.Run("AgentPortListening", func(t *testing.T) {
		resp, err := http.Get("http://localhost:8081/health")
		if err != nil {
			t.Errorf("Agent port not listening: %v", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Agent health check failed: status %d", resp.StatusCode)
		}
	})

	t.Run("SlackBotPortListening", func(t *testing.T) {
		resp, err := http.Get("http://localhost:8080/health")
		if err != nil {
			t.Errorf("Slack bot port not listening: %v", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Slack bot health check failed: status %d", resp.StatusCode)
		}
	})

	// Stop the process
	if err := cmd.Process.Kill(); err != nil {
		t.Logf("Failed to kill process: %v", err)
	}

	// Wait for process to exit
	cmd.Wait()

	t.Logf("Stdout: %s", stdout.String())
	t.Logf("Stderr: %s", stderr.String())
}

// TestBinaryModeAgent tests that the unified binary works in "agent" mode
func TestBinaryModeAgent(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping binary mode test in short mode")
	}

	buildCmd := exec.Command("go", "build", "-o", "/tmp/knowledge-agent-test", "./cmd/knowledge-agent")
	if output, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build binary: %v\n%s", err, output)
	}
	defer exec.Command("rm", "/tmp/knowledge-agent-test").Run()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "/tmp/knowledge-agent-test", "--mode", "agent")

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start binary: %v", err)
	}

	time.Sleep(5 * time.Second)

	// Test that only agent port is listening
	t.Run("AgentPortListening", func(t *testing.T) {
		resp, err := http.Get("http://localhost:8081/health")
		if err != nil {
			t.Errorf("Agent port not listening: %v", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Agent health check failed: status %d", resp.StatusCode)
		}
	})

	t.Run("SlackBotPortNotListening", func(t *testing.T) {
		// In agent-only mode, port 8080 should not be listening
		client := &http.Client{Timeout: 2 * time.Second}
		_, err := client.Get("http://localhost:8080/health")
		if err == nil {
			t.Error("Slack bot port should not be listening in agent-only mode")
		}
	})

	if err := cmd.Process.Kill(); err != nil {
		t.Logf("Failed to kill process: %v", err)
	}
	cmd.Wait()
}

// TestBinaryModeSlackBot tests that the unified binary works in "slack-bot" mode
func TestBinaryModeSlackBot(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping binary mode test in short mode")
	}

	buildCmd := exec.Command("go", "build", "-o", "/tmp/knowledge-agent-test", "./cmd/knowledge-agent")
	if output, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build binary: %v\n%s", err, output)
	}
	defer exec.Command("rm", "/tmp/knowledge-agent-test").Run()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "/tmp/knowledge-agent-test", "--mode", "slack-bot")

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start binary: %v", err)
	}

	time.Sleep(5 * time.Second)

	// Test that only slack-bot port is listening
	t.Run("SlackBotPortListening", func(t *testing.T) {
		resp, err := http.Get("http://localhost:8080/health")
		if err != nil {
			t.Errorf("Slack bot port not listening: %v", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Slack bot health check failed: status %d", resp.StatusCode)
		}
	})

	t.Run("AgentPortNotListening", func(t *testing.T) {
		// In slack-bot-only mode, port 8081 should not be listening
		client := &http.Client{Timeout: 2 * time.Second}
		_, err := client.Get("http://localhost:8081/health")
		if err == nil {
			t.Error("Agent port should not be listening in slack-bot-only mode")
		}
	})

	if err := cmd.Process.Kill(); err != nil {
		t.Logf("Failed to kill process: %v", err)
	}
	cmd.Wait()
}

// TestBinaryGracefulShutdown verifies graceful shutdown works
func TestBinaryGracefulShutdown(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping graceful shutdown test in short mode")
	}

	buildCmd := exec.Command("go", "build", "-o", "/tmp/knowledge-agent-test", "./cmd/knowledge-agent")
	if output, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build binary: %v\n%s", err, output)
	}
	defer exec.Command("rm", "/tmp/knowledge-agent-test").Run()

	cmd := exec.Command("/tmp/knowledge-agent-test", "--mode", "all")

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start binary: %v", err)
	}

	time.Sleep(5 * time.Second)

	// Send SIGTERM for graceful shutdown
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Logf("Failed to send SIGTERM: %v", err)
	}

	// Wait for process to exit gracefully
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-time.After(15 * time.Second):
		t.Error("Process did not shutdown gracefully within 15 seconds")
		cmd.Process.Kill()
	case err := <-done:
		if err != nil {
			t.Logf("Process exited with error: %v", err)
		}
	}

	output := stdout.String() + stderr.String()

	// Verify graceful shutdown messages
	if !strings.Contains(output, "Shutting down") && !strings.Contains(output, "shutdown") {
		t.Error("Expected graceful shutdown messages in output")
	}

	t.Logf("Shutdown output: %s", output)
}
