package a2a

import (
	"encoding/json"
	"net/http"

	"knowledge-agent/internal/config"
	"knowledge-agent/internal/logger"
)

// HTTP status code for loop detection (RFC 5765)
const StatusLoopDetected = 508

// LoopPreventionMiddleware validates incoming requests for A2A loops
// It checks:
// 1. If this agent is already in the call chain → 508 Loop Detected
// 2. If call depth exceeds max_call_depth → 508 Loop Detected
// If validation passes, adds this agent to the call chain in the context
func LoopPreventionMiddleware(cfg *config.A2AConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log := logger.Get()

			// If A2A is not enabled, skip loop prevention
			if !cfg.Enabled || cfg.SelfName == "" {
				next.ServeHTTP(w, r)
				return
			}

			// Extract call context from headers
			cc := ExtractCallContext(r)

			// Log incoming A2A headers for debugging
			if cc.CallDepth > 0 || len(cc.CallChain) > 0 {
				log.Debugw("A2A request received",
					"request_id", cc.RequestID,
					"call_chain", cc.CallChain,
					"call_depth", cc.CallDepth,
					"self_name", cfg.SelfName,
					"path", r.URL.Path,
				)
			}

			// Check 1: Is this agent already in the call chain?
			if cc.ContainsAgent(cfg.SelfName) {
				log.Warnw("A2A loop detected: agent already in call chain",
					"request_id", cc.RequestID,
					"call_chain", cc.CallChain,
					"self_name", cfg.SelfName,
					"path", r.URL.Path,
				)
				jsonError(w, "Loop detected: agent '"+cfg.SelfName+"' is already in the call chain", StatusLoopDetected)
				return
			}

			// Check 2: Has max call depth been exceeded?
			if cc.CallDepth >= cfg.MaxCallDepth {
				log.Warnw("A2A max call depth exceeded",
					"request_id", cc.RequestID,
					"call_chain", cc.CallChain,
					"call_depth", cc.CallDepth,
					"max_call_depth", cfg.MaxCallDepth,
					"path", r.URL.Path,
				)
				jsonError(w, "Max call depth exceeded", StatusLoopDetected)
				return
			}

			// Add this agent to the call chain
			newCC := cc.AddAgent(cfg.SelfName)

			// Add the updated call context to the request context
			ctx := WithCallContext(r.Context(), newCC)

			// Continue with the updated context
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// jsonError sends an error response in JSON format
func jsonError(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]any{
		"success": false,
		"error":   message,
	})
}
