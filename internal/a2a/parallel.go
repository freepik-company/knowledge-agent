package a2a

import (
	"fmt"
	"sync"

	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"

	"knowledge-agent/internal/logger"
)

// ParallelQueryArgs represents a single agent query in a parallel request
type ParallelQueryArgs struct {
	Agent string `json:"agent" description:"The agent name (e.g., 'logs_agent', 'metrics_agent')"`
	Query string `json:"query" description:"The query or task to send to this agent"`
}

// QueryMultipleAgentsArgs are the arguments for parallel sub-agent queries
type QueryMultipleAgentsArgs struct {
	Queries []ParallelQueryArgs `json:"queries" description:"List of agent queries to execute in parallel. Each query specifies the agent name and the query to send."`
}

// ParallelQueryResult represents the result from a single agent in a parallel request
type ParallelQueryResult struct {
	Agent    string `json:"agent"`
	Success  bool   `json:"success"`
	Response string `json:"response,omitempty"`
	Error    string `json:"error,omitempty"`
}

// QueryMultipleAgentsResult is the aggregated result from parallel sub-agent queries
type QueryMultipleAgentsResult struct {
	Results     []ParallelQueryResult `json:"results"`
	TotalAgents int                   `json:"total_agents"`
	Successful  int                   `json:"successful"`
	Failed      int                   `json:"failed"`
}

// createParallelQueryTool creates a tool for querying multiple sub-agents in parallel
// Works with both A2A and REST clients via the SubAgentClient interface
func createParallelQueryTool(clients map[string]SubAgentClient) (tool.Tool, error) {
	handler := createParallelQueryHandler(clients)

	return functiontool.New(functiontool.Config{
		Name: "query_multiple_agents",
		Description: `Execute queries to multiple sub-agents IN PARALLEL for faster results.

Use this tool when you need to query 2 or more agents simultaneously. Instead of calling query_logs_agent, query_metrics_agent, and query_kube_agent sequentially, use this tool to call them all at once.

Example usage:
{
  "queries": [
    {"agent": "logs_agent", "query": "Search for errors in payment service"},
    {"agent": "metrics_agent", "query": "Get error rate for payment service"},
    {"agent": "kube_agent", "query": "Check pod status for payment service"}
  ]
}

This significantly reduces response time when multiple agents are needed.`,
	}, handler)
}

// createParallelQueryHandler creates the handler function for parallel queries
func createParallelQueryHandler(clients map[string]SubAgentClient) functiontool.Func[QueryMultipleAgentsArgs, QueryMultipleAgentsResult] {
	return func(ctx tool.Context, args QueryMultipleAgentsArgs) (QueryMultipleAgentsResult, error) {
		log := logger.Get()

		if len(args.Queries) == 0 {
			return QueryMultipleAgentsResult{
				Results:     []ParallelQueryResult{},
				TotalAgents: 0,
			}, nil
		}

		log.Infow("Parallel sub-agent query started",
			"total_queries", len(args.Queries),
			"agents", extractAgentNames(args.Queries),
		)

		// Channel to collect results
		resultsChan := make(chan ParallelQueryResult, len(args.Queries))

		// WaitGroup to wait for all goroutines
		var wg sync.WaitGroup

		// Launch parallel queries
		for _, q := range args.Queries {
			wg.Add(1)
			go func(query ParallelQueryArgs) {
				defer wg.Done()
				result := executeAgentQuery(ctx, clients, query)
				resultsChan <- result
			}(q)
		}

		// Wait for all queries to complete in a separate goroutine
		go func() {
			wg.Wait()
			close(resultsChan)
		}()

		// Collect results
		var results []ParallelQueryResult
		successful := 0
		failed := 0

		for result := range resultsChan {
			results = append(results, result)
			if result.Success {
				successful++
			} else {
				failed++
			}
		}

		log.Infow("Parallel sub-agent query completed",
			"total_agents", len(args.Queries),
			"successful", successful,
			"failed", failed,
		)

		return QueryMultipleAgentsResult{
			Results:     results,
			TotalAgents: len(args.Queries),
			Successful:  successful,
			Failed:      failed,
		}, nil
	}
}

// executeAgentQuery executes a single query to an agent (called from goroutine)
// Works with both A2A and REST clients via the SubAgentClient interface
func executeAgentQuery(ctx tool.Context, clients map[string]SubAgentClient, query ParallelQueryArgs) ParallelQueryResult {
	log := logger.Get()

	log.Infow("Parallel query: executing agent call",
		"agent", query.Agent,
		"query_length", len(query.Query),
		"query_preview", truncateString(query.Query, 100),
	)

	// Validate agent exists
	client, exists := clients[query.Agent]
	if !exists {
		log.Warnw("Parallel query: agent not found",
			"agent", query.Agent,
			"available_agents", getAvailableAgents(clients),
		)
		return ParallelQueryResult{
			Agent:   query.Agent,
			Success: false,
			Error:   fmt.Sprintf("agent '%s' not found. Available agents: %v", query.Agent, getAvailableAgents(clients)),
		}
	}

	// Validate query
	if query.Query == "" {
		return ParallelQueryResult{
			Agent:   query.Agent,
			Success: false,
			Error:   "query cannot be empty",
		}
	}

	// Query the sub-agent using the unified interface
	responseText, err := client.Query(ctx, query.Query)
	if err != nil {
		log.Errorw("Parallel query: agent call failed",
			"agent", query.Agent,
			"error", err,
		)
		return ParallelQueryResult{
			Agent:   query.Agent,
			Success: false,
			Error:   fmt.Sprintf("failed to call %s: %v", query.Agent, err),
		}
	}

	// Truncate response if too long
	if len(responseText) > maxResponseTextLength {
		responseText = responseText[:maxResponseTextLength] + "\n[TRUNCATED - response exceeded 100KB limit]"
	}

	log.Infow("Parallel query: agent call completed",
		"agent", query.Agent,
		"response_length", len(responseText),
		"response_preview", truncateString(responseText, 100),
	)

	return ParallelQueryResult{
		Agent:    query.Agent,
		Success:  true,
		Response: responseText,
	}
}

// extractAgentNames extracts agent names from queries for logging
func extractAgentNames(queries []ParallelQueryArgs) []string {
	names := make([]string, len(queries))
	for i, q := range queries {
		names[i] = q.Agent
	}
	return names
}

// getAvailableAgents returns list of available agent names
func getAvailableAgents(clients map[string]SubAgentClient) []string {
	agents := make([]string, 0, len(clients))
	for name := range clients {
		agents = append(agents, name)
	}
	return agents
}
