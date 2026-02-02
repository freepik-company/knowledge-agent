package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"

	"knowledge-agent/internal/config"
	"knowledge-agent/internal/logger"
)

const (
	// MaxConcurrentAsyncTasks limits the number of concurrent async tasks to prevent resource exhaustion
	MaxConcurrentAsyncTasks = 100
)

// SlackPoster interface for posting messages to Slack
type SlackPoster interface {
	PostMessage(channelID, threadTS, text string) error
}

// SelfInvoker interface for callback to the agent itself
type SelfInvoker interface {
	SelfInvoke(ctx context.Context, req CallbackRequest) error
}

// CallbackRequest represents a request to callback the agent with results
type CallbackRequest struct {
	Question     string `json:"question"`
	ChannelID    string `json:"channel_id"`
	ThreadTS     string `json:"thread_ts"`
	SessionID    string `json:"session_id,omitempty"`
	IsCallback   bool   `json:"is_callback"`
	AgentName    string `json:"agent_name"`
	OriginalTask string `json:"original_task"`
}

// A2AInvoker interface for invoking sub-agents
type A2AInvoker interface {
	Invoke(ctx context.Context, agentName, task string) (string, error)
}

// AsyncSubAgentToolset provides tools for async sub-agent invocation
type AsyncSubAgentToolset struct {
	tools      []tool.Tool
	config     *config.A2AAsyncConfig
	subAgents  map[string]config.A2ASubAgentConfig
	a2aInvoker A2AInvoker
	agentPort  int
	httpClient *http.Client // Reusable HTTP client

	// Callbacks - protected by callbacksMu
	slackPoster SlackPoster
	selfInvoker SelfInvoker
	callbacksMu sync.RWMutex

	// Track pending tasks for observability
	pendingTasks   map[string]*PendingTask
	pendingTasksMu sync.RWMutex

	// Graceful shutdown
	wg       sync.WaitGroup
	shutdown chan struct{}
	closed   bool
}

// PendingTask tracks an async task in progress
type PendingTask struct {
	ID        string
	AgentName string
	Task      string
	ChannelID string
	ThreadTS  string
	SessionID string
	StartedAt time.Time
}

// AsyncSubAgentConfig holds configuration for creating the toolset
type AsyncSubAgentConfig struct {
	Config      *config.A2AAsyncConfig
	SubAgents   []config.A2ASubAgentConfig
	A2AInvoker  A2AInvoker
	SlackPoster SlackPoster
	SelfInvoker SelfInvoker
	AgentPort   int
}

// NewAsyncSubAgentToolset creates a new async sub-agent toolset
func NewAsyncSubAgentToolset(cfg AsyncSubAgentConfig) (*AsyncSubAgentToolset, error) {
	if cfg.Config == nil {
		return nil, fmt.Errorf("async config is required")
	}

	// Build map of sub-agents for quick lookup
	subAgentMap := make(map[string]config.A2ASubAgentConfig)
	for _, sa := range cfg.SubAgents {
		subAgentMap[sa.Name] = sa
	}

	// Create reusable HTTP client with sensible defaults
	httpClient := &http.Client{
		Timeout: 0, // No client timeout - context handles it
		Transport: &http.Transport{
			MaxIdleConns:        10,
			MaxIdleConnsPerHost: 5,
			IdleConnTimeout:     90 * time.Second,
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
		},
	}

	ts := &AsyncSubAgentToolset{
		config:       cfg.Config,
		subAgents:    subAgentMap,
		a2aInvoker:   cfg.A2AInvoker,
		slackPoster:  cfg.SlackPoster,
		selfInvoker:  cfg.SelfInvoker,
		agentPort:    cfg.AgentPort,
		pendingTasks: make(map[string]*PendingTask),
		httpClient:   httpClient,
		shutdown:     make(chan struct{}),
	}

	// Build description with available agents
	var agentList string
	for name, sa := range subAgentMap {
		agentList += fmt.Sprintf("\n  - %s: %s", name, sa.Description)
	}

	description := fmt.Sprintf(`Invoke a sub-agent asynchronously for long-running tasks (5-15 minutes).
The task is launched in the background and results are posted to the Slack thread when complete.
Use this for tasks that take a long time (code generation, analysis, etc.).

Available agents:%s

IMPORTANT: This tool returns immediately. The actual work happens in the background.`, agentList)

	// Create the async invoke tool
	asyncInvokeTool, err := functiontool.New(
		functiontool.Config{
			Name:        "async_invoke_agent",
			Description: description,
		},
		ts.asyncInvokeAgent,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create async_invoke_agent tool: %w", err)
	}

	ts.tools = []tool.Tool{asyncInvokeTool}
	return ts, nil
}

// Name returns the name of the toolset
func (ts *AsyncSubAgentToolset) Name() string {
	return "async_subagent_toolset"
}

// Tools returns the list of tools
func (ts *AsyncSubAgentToolset) Tools(ctx agent.ReadonlyContext) ([]tool.Tool, error) {
	return ts.tools, nil
}

// AsyncInvokeArgs are the arguments for the async_invoke_agent tool
type AsyncInvokeArgs struct {
	// AgentName is the name of the sub-agent to invoke
	AgentName string `json:"agent_name"`
	// Task is the task description to send to the sub-agent
	Task string `json:"task"`
	// ChannelID is the Slack channel to post results to
	ChannelID string `json:"channel_id"`
	// ThreadTS is the Slack thread to post results to
	ThreadTS string `json:"thread_ts"`
	// SessionID is optional session ID for callback context
	SessionID string `json:"session_id,omitempty"`
}

// AsyncInvokeResult is the result of the async_invoke_agent tool
type AsyncInvokeResult struct {
	Success bool   `json:"success"`
	TaskID  string `json:"task_id,omitempty"`
	Message string `json:"message"`
	Error   string `json:"error,omitempty"`
}

// asyncInvokeAgent handles async invocation of a sub-agent
func (ts *AsyncSubAgentToolset) asyncInvokeAgent(ctx tool.Context, args AsyncInvokeArgs) (AsyncInvokeResult, error) {
	log := logger.Get()

	// Check if shutdown is in progress
	select {
	case <-ts.shutdown:
		return AsyncInvokeResult{
			Success: false,
			Error:   "service is shutting down, cannot accept new tasks",
		}, nil
	default:
	}

	// Validate agent name
	if args.AgentName == "" {
		return AsyncInvokeResult{
			Success: false,
			Error:   "agent_name is required",
		}, nil
	}

	subAgent, exists := ts.subAgents[args.AgentName]
	if !exists {
		var available []string
		for name := range ts.subAgents {
			available = append(available, name)
		}
		return AsyncInvokeResult{
			Success: false,
			Error:   fmt.Sprintf("unknown agent '%s'. Available agents: %v", args.AgentName, available),
		}, nil
	}

	if args.Task == "" {
		return AsyncInvokeResult{
			Success: false,
			Error:   "task is required",
		}, nil
	}

	if args.ChannelID == "" || args.ThreadTS == "" {
		return AsyncInvokeResult{
			Success: false,
			Error:   "channel_id and thread_ts are required for posting results",
		}, nil
	}

	// Check concurrent task limit (P1: prevent resource exhaustion)
	ts.pendingTasksMu.RLock()
	taskCount := len(ts.pendingTasks)
	ts.pendingTasksMu.RUnlock()

	if taskCount >= MaxConcurrentAsyncTasks {
		log.Warnw("Async task limit reached",
			"current_tasks", taskCount,
			"max_tasks", MaxConcurrentAsyncTasks,
		)
		return AsyncInvokeResult{
			Success: false,
			Error:   fmt.Sprintf("too many pending tasks (%d/%d). Please wait for some tasks to complete.", taskCount, MaxConcurrentAsyncTasks),
		}, nil
	}

	// Generate task ID
	taskID := fmt.Sprintf("async-%s-%d", args.AgentName, time.Now().UnixNano())

	// Track pending task
	pendingTask := &PendingTask{
		ID:        taskID,
		AgentName: args.AgentName,
		Task:      args.Task,
		ChannelID: args.ChannelID,
		ThreadTS:  args.ThreadTS,
		SessionID: args.SessionID,
		StartedAt: time.Now(),
	}

	ts.pendingTasksMu.Lock()
	ts.pendingTasks[taskID] = pendingTask
	ts.pendingTasksMu.Unlock()

	log.Infow("Launching async sub-agent task",
		"task_id", taskID,
		"agent_name", args.AgentName,
		"channel_id", args.ChannelID,
		"thread_ts", args.ThreadTS,
		"endpoint", subAgent.Endpoint,
	)

	// Launch goroutine to handle the async invocation (with WaitGroup for graceful shutdown)
	ts.wg.Add(1)
	go func() {
		defer ts.wg.Done()
		ts.executeAsyncTask(pendingTask, subAgent)
	}()

	return AsyncInvokeResult{
		Success: true,
		TaskID:  taskID,
		Message: fmt.Sprintf("Task sent to %s. Results will be posted to this thread when complete. Task ID: %s", args.AgentName, taskID),
	}, nil
}

// executeAsyncTask runs the sub-agent invocation in the background
func (ts *AsyncSubAgentToolset) executeAsyncTask(task *PendingTask, subAgent config.A2ASubAgentConfig) {
	log := logger.Get()

	// Create context with timeout and shutdown cancellation
	timeout := ts.config.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Also cancel on shutdown
	go func() {
		select {
		case <-ts.shutdown:
			cancel()
		case <-ctx.Done():
		}
	}()

	startTime := time.Now()

	log.Infow("Starting async sub-agent invocation",
		"task_id", task.ID,
		"agent_name", task.AgentName,
		"timeout", timeout,
	)

	// Invoke the sub-agent
	var result string
	var err error

	if ts.a2aInvoker != nil {
		result, err = ts.a2aInvoker.Invoke(ctx, task.AgentName, task.Task)
	} else {
		// Fallback: direct HTTP call to the sub-agent
		result, err = ts.invokeSubAgentHTTP(ctx, subAgent, task.Task)
	}

	duration := time.Since(startTime)

	// Remove from pending tasks
	ts.pendingTasksMu.Lock()
	delete(ts.pendingTasks, task.ID)
	ts.pendingTasksMu.Unlock()

	// Get callbacks thread-safely (P0: race condition fix)
	ts.callbacksMu.RLock()
	slackPoster := ts.slackPoster
	selfInvoker := ts.selfInvoker
	ts.callbacksMu.RUnlock()

	// Check if shutdown is in progress - skip posting if so
	select {
	case <-ts.shutdown:
		log.Warnw("Shutdown in progress, skipping result posting",
			"task_id", task.ID,
			"agent_name", task.AgentName,
		)
		return
	default:
	}

	if err != nil {
		log.Errorw("Async sub-agent task failed",
			"task_id", task.ID,
			"agent_name", task.AgentName,
			"duration", duration,
			"error", err,
		)

		// Post error to Slack if enabled
		if ts.config.PostToSlack && slackPoster != nil {
			errorMsg := fmt.Sprintf("❌ *%s* failed after %s:\n```%s```", task.AgentName, duration.Round(time.Second), err.Error())
			if postErr := slackPoster.PostMessage(task.ChannelID, task.ThreadTS, errorMsg); postErr != nil {
				log.Errorw("Failed to post error to Slack", "error", postErr)
			}
		}
		return
	}

	log.Infow("Async sub-agent task completed",
		"task_id", task.ID,
		"agent_name", task.AgentName,
		"duration", duration,
		"result_length", len(result),
	)

	// Post result to Slack if enabled
	if ts.config.PostToSlack && slackPoster != nil {
		successMsg := fmt.Sprintf("✅ *%s* completed (%s):\n\n%s", task.AgentName, duration.Round(time.Second), result)
		if postErr := slackPoster.PostMessage(task.ChannelID, task.ThreadTS, successMsg); postErr != nil {
			log.Errorw("Failed to post result to Slack", "error", postErr)
		}
	}

	// Callback to agent if enabled (for further processing like save_to_memory)
	if ts.config.CallbackEnabled && selfInvoker != nil {
		callbackReq := CallbackRequest{
			Question:     fmt.Sprintf("[ASYNC CALLBACK from %s]\n\nOriginal task: %s\n\nResult:\n%s", task.AgentName, task.Task, result),
			ChannelID:    task.ChannelID,
			ThreadTS:     task.ThreadTS,
			SessionID:    task.SessionID,
			IsCallback:   true,
			AgentName:    task.AgentName,
			OriginalTask: task.Task,
		}

		if callbackErr := selfInvoker.SelfInvoke(ctx, callbackReq); callbackErr != nil {
			log.Errorw("Failed to callback agent with result", "error", callbackErr)
		}
	}
}

// invokeSubAgentHTTP invokes a sub-agent via direct HTTP call
func (ts *AsyncSubAgentToolset) invokeSubAgentHTTP(ctx context.Context, subAgent config.A2ASubAgentConfig, task string) (string, error) {
	log := logger.Get()

	// Build request body
	reqBody := map[string]any{
		"jsonrpc": "2.0",
		"method":  "tasks/send",
		"params": map[string]any{
			"message": map[string]any{
				"role": "user",
				"parts": []map[string]any{
					{"text": task},
				},
			},
		},
		"id": fmt.Sprintf("async-%d", time.Now().UnixNano()),
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", subAgent.Endpoint+"/a2a", bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Add auth header if configured (P0: fix hardcoded auth)
	if subAgent.Auth.Type != "" && subAgent.Auth.Type != "none" {
		switch subAgent.Auth.Type {
		case "api_key":
			if subAgent.Auth.Header != "" && subAgent.Auth.KeyEnv != "" {
				key := os.Getenv(subAgent.Auth.KeyEnv)
				if key != "" {
					req.Header.Set(subAgent.Auth.Header, key)
				} else {
					log.Warnw("API key environment variable not set",
						"env_var", subAgent.Auth.KeyEnv,
						"agent", subAgent.Name,
					)
				}
			}
		case "bearer":
			if subAgent.Auth.TokenEnv != "" {
				token := os.Getenv(subAgent.Auth.TokenEnv)
				if token != "" {
					req.Header.Set("Authorization", "Bearer "+token)
				} else {
					log.Warnw("Bearer token environment variable not set",
						"env_var", subAgent.Auth.TokenEnv,
						"agent", subAgent.Name,
					)
				}
			}
		}
	}

	// Execute request using reusable client
	resp, err := ts.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body for error reporting
	bodyData, readErr := io.ReadAll(io.LimitReader(resp.Body, 1024*1024)) // Limit to 1MB
	if readErr != nil {
		return "", fmt.Errorf("failed to read response body: %w", readErr)
	}

	if resp.StatusCode != http.StatusOK {
		// Include response body in error for debugging
		return "", fmt.Errorf("sub-agent returned status %d: %s", resp.StatusCode, string(bodyData))
	}

	// Parse response
	var respBody map[string]any
	if err := json.Unmarshal(bodyData, &respBody); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	// Extract result text (simplified A2A response parsing)
	if result, ok := respBody["result"].(map[string]any); ok {
		if artifacts, ok := result["artifacts"].([]any); ok && len(artifacts) > 0 {
			if artifact, ok := artifacts[0].(map[string]any); ok {
				if parts, ok := artifact["parts"].([]any); ok && len(parts) > 0 {
					if part, ok := parts[0].(map[string]any); ok {
						if text, ok := part["text"].(string); ok {
							return text, nil
						}
					}
				}
			}
		}
	}

	return fmt.Sprintf("Response: %v", respBody), nil
}

// GetPendingTasks returns a list of currently pending tasks
func (ts *AsyncSubAgentToolset) GetPendingTasks() []PendingTask {
	ts.pendingTasksMu.RLock()
	defer ts.pendingTasksMu.RUnlock()

	tasks := make([]PendingTask, 0, len(ts.pendingTasks))
	for _, task := range ts.pendingTasks {
		tasks = append(tasks, *task)
	}
	return tasks
}

// SetCallbacks configures the Slack poster and self-invoker for async callbacks
// This must be called after creation to enable posting results and re-invoking the agent
// Thread-safe: can be called while tasks are in progress
func (ts *AsyncSubAgentToolset) SetCallbacks(slackPoster SlackPoster, selfInvoker SelfInvoker) {
	log := logger.Get()

	ts.callbacksMu.Lock()
	ts.slackPoster = slackPoster
	ts.selfInvoker = selfInvoker
	ts.callbacksMu.Unlock()

	log.Infow("Async sub-agent callbacks configured",
		"slack_poster_set", slackPoster != nil,
		"self_invoker_set", selfInvoker != nil,
	)
}

// Close initiates graceful shutdown, cancels pending tasks, and waits for goroutines to finish
func (ts *AsyncSubAgentToolset) Close() error {
	log := logger.Get()

	ts.pendingTasksMu.Lock()
	if ts.closed {
		ts.pendingTasksMu.Unlock()
		return nil
	}
	ts.closed = true
	pendingCount := len(ts.pendingTasks)
	ts.pendingTasksMu.Unlock()

	log.Infow("Closing async sub-agent toolset",
		"pending_tasks", pendingCount,
	)

	// Signal shutdown to all goroutines
	close(ts.shutdown)

	// Wait for all goroutines to finish with timeout
	done := make(chan struct{})
	go func() {
		ts.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Info("All async tasks finished gracefully")
	case <-time.After(30 * time.Second):
		log.Warn("Timeout waiting for async tasks to finish, some tasks may be orphaned")
	}

	return nil
}
