package a2a

import (
	"fmt"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"

	"knowledge-agent/internal/config"
	"knowledge-agent/internal/logger"
)

// Toolset implements tool.Toolset for A2A tools
type Toolset struct {
	agentName   string
	description string
	client      *Client
	tools       []tool.Tool
}

// ToolArgs are the arguments passed to an A2A tool
type ToolArgs struct {
	// Query is the question or command to send to the remote agent
	Query string `json:"query"`
	// Context is optional additional context for the query
	Context string `json:"context,omitempty"`
}

// ToolResult is the result returned by an A2A tool
type ToolResult struct {
	// Success indicates if the query was successful
	Success bool `json:"success"`
	// Answer is the response from the remote agent
	Answer string `json:"answer,omitempty"`
	// Error message if the query failed
	Error string `json:"error,omitempty"`
	// Agent is the name of the agent that responded
	Agent string `json:"agent"`
}

// NewToolset creates a new A2A toolset from agent configuration
func NewToolset(agentCfg config.A2AAgentConfig, selfName string) (*Toolset, error) {
	log := logger.Get()

	// Create client for this agent
	client, err := NewClient(agentCfg, selfName)
	if err != nil {
		return nil, fmt.Errorf("failed to create A2A client: %w", err)
	}

	ts := &Toolset{
		agentName:   agentCfg.Name,
		description: agentCfg.Description,
		client:      client,
	}

	// Create a tool for each configured tool
	for _, toolCfg := range agentCfg.Tools {
		t, err := ts.createTool(toolCfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create tool %s: %w", toolCfg.Name, err)
		}
		ts.tools = append(ts.tools, t)
		log.Infow("A2A tool created",
			"agent", agentCfg.Name,
			"tool", toolCfg.Name,
		)
	}

	log.Infow("A2A toolset created",
		"agent", agentCfg.Name,
		"tools_count", len(ts.tools),
	)

	return ts, nil
}

// Name returns the name of this toolset
func (ts *Toolset) Name() string {
	return fmt.Sprintf("a2a_%s_toolset", ts.agentName)
}

// Tools returns the list of tools in this toolset
func (ts *Toolset) Tools(ctx agent.ReadonlyContext) ([]tool.Tool, error) {
	return ts.tools, nil
}

// createTool creates a function tool that calls the remote agent
func (ts *Toolset) createTool(toolCfg config.A2AToolConfig) (tool.Tool, error) {
	// Build the tool description with agent context
	description := fmt.Sprintf("[A2A:%s] %s", ts.agentName, toolCfg.Description)

	// Create a closure to capture the toolset and tool config
	execFunc := func(ctx tool.Context, args ToolArgs) (ToolResult, error) {
		log := logger.Get()

		// Build the query with tool context
		query := args.Query
		if args.Context != "" {
			query = fmt.Sprintf("%s\n\nContext: %s", query, args.Context)
		}

		log.Debugw("Executing A2A tool",
			"tool", toolCfg.Name,
			"agent", ts.agentName,
			"query_length", len(query),
		)

		// Call the remote agent
		resp, err := ts.client.Query(ctx, query, map[string]any{
			"tool_name": toolCfg.Name,
		})

		if err != nil {
			log.Warnw("A2A tool execution failed",
				"tool", toolCfg.Name,
				"agent", ts.agentName,
				"error", err,
			)
			return ToolResult{
				Success: false,
				Error:   err.Error(),
				Agent:   ts.agentName,
			}, nil // Return error in result, not as Go error
		}

		return ToolResult{
			Success: true,
			Answer:  resp.Answer,
			Agent:   ts.agentName,
		}, nil
	}

	return functiontool.New(
		functiontool.Config{
			Name:        toolCfg.Name,
			Description: description,
		},
		execFunc,
	)
}

// CreateA2AToolsets creates all A2A toolsets from configuration
// Returns toolsets and any errors encountered (uses graceful degradation)
func CreateA2AToolsets(cfg *config.A2AConfig) ([]tool.Toolset, error) {
	log := logger.Get()

	if !cfg.Enabled {
		return nil, nil
	}

	if cfg.SelfName == "" {
		return nil, fmt.Errorf("a2a.self_name is required when A2A is enabled")
	}

	log.Infow("Creating A2A toolsets",
		"self_name", cfg.SelfName,
		"agents_count", len(cfg.Agents),
	)

	var toolsets []tool.Toolset

	for _, agentCfg := range cfg.Agents {
		ts, err := NewToolset(agentCfg, cfg.SelfName)
		if err != nil {
			// Graceful degradation: log warning but continue with other agents
			log.Warnw("Failed to create A2A toolset, skipping",
				"agent", agentCfg.Name,
				"error", err,
			)
			continue
		}
		toolsets = append(toolsets, ts)
	}

	if len(toolsets) > 0 {
		log.Infow("A2A toolsets created successfully",
			"count", len(toolsets),
		)
	} else if len(cfg.Agents) > 0 {
		log.Warn("A2A enabled but no toolsets were created successfully")
	}

	return toolsets, nil
}
