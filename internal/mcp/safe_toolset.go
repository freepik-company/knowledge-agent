package mcp

import (
	"google.golang.org/adk/agent"
	"google.golang.org/adk/tool"

	"knowledge-agent/internal/logger"
)

// safeToolset wraps an MCP toolset to handle errors gracefully during tool enumeration.
// If the underlying MCP server becomes unavailable, this wrapper returns an empty
// tool list instead of propagating errors that could disrupt the agent.
//
// Note: Runtime errors during tool execution are handled by the ADK framework
// and will be returned to the LLM as error messages. This wrapper focuses on
// graceful degradation during startup and tool enumeration.
type safeToolset struct {
	inner      tool.Toolset
	serverName string
}

// NewSafeToolset creates a new safe toolset wrapper.
// The wrapper catches errors during tool enumeration and returns an empty list
// instead of propagating errors that could prevent the agent from functioning.
func NewSafeToolset(inner tool.Toolset, serverName string) tool.Toolset {
	return &safeToolset{
		inner:      inner,
		serverName: serverName,
	}
}

// Name returns the toolset name.
func (s *safeToolset) Name() string {
	return s.inner.Name()
}

// Tools returns the list of tools from the underlying MCP server.
// If the server is unavailable, it logs a warning and returns an empty list
// instead of propagating the error.
func (s *safeToolset) Tools(ctx agent.ReadonlyContext) ([]tool.Tool, error) {
	log := logger.Get()

	tools, err := s.inner.Tools(ctx)
	if err != nil {
		log.Warnw("MCP toolset unavailable during tool enumeration",
			"server", s.serverName,
			"error", err,
		)
		// Return empty list instead of error - allows agent to continue without these tools
		return nil, nil
	}

	return tools, nil
}
