package a2a

import (
	"fmt"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/remoteagent"

	"knowledge-agent/internal/config"
	"knowledge-agent/internal/logger"
)

// CreateSubAgents creates remote ADK agents from configuration using remoteagent.NewA2A
// These agents can be used as sub-agents in the main LLM agent
func CreateSubAgents(cfg *config.A2AConfig) ([]agent.Agent, error) {
	log := logger.Get()

	if !cfg.Enabled {
		return nil, nil
	}

	if len(cfg.SubAgents) == 0 {
		log.Debug("A2A enabled but no sub_agents configured")
		return nil, nil
	}

	log.Infow("Creating A2A sub-agents",
		"self_name", cfg.SelfName,
		"sub_agents_count", len(cfg.SubAgents),
	)

	var subAgents []agent.Agent

	for _, subAgentCfg := range cfg.SubAgents {
		remoteAgent, err := createRemoteAgent(subAgentCfg)
		if err != nil {
			// Graceful degradation: log warning but continue with other agents
			log.Warnw("Failed to create remote agent, skipping",
				"agent", subAgentCfg.Name,
				"error", err,
			)
			continue
		}

		subAgents = append(subAgents, remoteAgent)
		log.Infow("Remote sub-agent created",
			"name", subAgentCfg.Name,
			"endpoint", subAgentCfg.Endpoint,
		)
	}

	if len(subAgents) > 0 {
		log.Infow("A2A sub-agents created successfully",
			"count", len(subAgents),
		)
	} else if len(cfg.SubAgents) > 0 {
		log.Warn("A2A enabled but no sub-agents were created successfully")
	}

	return subAgents, nil
}

// createRemoteAgent creates a single remote agent using ADK's remoteagent package
func createRemoteAgent(cfg config.A2ASubAgentConfig) (agent.Agent, error) {
	log := logger.Get()

	log.Debugw("Creating remote agent",
		"name", cfg.Name,
		"endpoint", cfg.Endpoint,
		"description", cfg.Description,
	)

	// Use remoteagent.NewA2A to create a remote agent wrapper
	// This automatically fetches the agent card from the endpoint
	// NOTE: cfg.Timeout is not passed because remoteagent.A2AConfig does not support
	// custom timeouts - the ADK library manages timeouts internally
	remoteAgent, err := remoteagent.NewA2A(remoteagent.A2AConfig{
		Name:            cfg.Name,
		Description:     cfg.Description,
		AgentCardSource: cfg.Endpoint, // URL to fetch agent card from
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create remote agent %s: %w", cfg.Name, err)
	}

	return remoteAgent, nil
}
