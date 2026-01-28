package a2a

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// Header constants for A2A loop prevention
const (
	// HeaderRequestID is a unique identifier for the original request
	HeaderRequestID = "X-Request-ID"
	// HeaderCallChain tracks the chain of agents that have handled this request (CSV)
	HeaderCallChain = "X-Call-Chain"
	// HeaderCallDepth tracks the current depth in the call chain
	HeaderCallDepth = "X-Call-Depth"
)

// Context key types for A2A data
type contextKey string

const (
	requestIDKey  contextKey = "a2a_request_id"
	callChainKey  contextKey = "a2a_call_chain"
	callDepthKey  contextKey = "a2a_call_depth"
)

// CallContext holds the A2A call chain tracking information
type CallContext struct {
	RequestID string   // Unique ID for the original request
	CallChain []string // List of agent names that have handled this request
	CallDepth int      // Current depth in the call chain
}

// ExtractCallContext extracts A2A call chain information from HTTP headers
func ExtractCallContext(r *http.Request) *CallContext {
	cc := &CallContext{
		RequestID: r.Header.Get(HeaderRequestID),
		CallDepth: 0,
	}

	// Generate request ID if not present
	if cc.RequestID == "" {
		cc.RequestID = uuid.New().String()
	}

	// Parse call chain
	if chain := r.Header.Get(HeaderCallChain); chain != "" {
		cc.CallChain = strings.Split(chain, ",")
		// Trim whitespace from each agent name
		for i := range cc.CallChain {
			cc.CallChain[i] = strings.TrimSpace(cc.CallChain[i])
		}
	}

	// Parse call depth (reject negative values to prevent bypass)
	if depth := r.Header.Get(HeaderCallDepth); depth != "" {
		if d, err := strconv.Atoi(depth); err == nil && d >= 0 {
			cc.CallDepth = d
		}
	}

	return cc
}

// ContainsAgent checks if the given agent name is already in the call chain
func (cc *CallContext) ContainsAgent(agentName string) bool {
	for _, name := range cc.CallChain {
		if strings.EqualFold(name, agentName) {
			return true
		}
	}
	return false
}

// AddAgent adds an agent to the call chain and returns a new CallContext
func (cc *CallContext) AddAgent(agentName string) *CallContext {
	newChain := make([]string, len(cc.CallChain), len(cc.CallChain)+1)
	copy(newChain, cc.CallChain)
	newChain = append(newChain, agentName)

	return &CallContext{
		RequestID: cc.RequestID,
		CallChain: newChain,
		CallDepth: cc.CallDepth + 1,
	}
}

// SetHeaders sets the A2A headers on an outgoing HTTP request
func (cc *CallContext) SetHeaders(req *http.Request) {
	req.Header.Set(HeaderRequestID, cc.RequestID)
	req.Header.Set(HeaderCallChain, strings.Join(cc.CallChain, ","))
	req.Header.Set(HeaderCallDepth, strconv.Itoa(cc.CallDepth))
}

// WithCallContext adds CallContext to a context.Context
func WithCallContext(ctx context.Context, cc *CallContext) context.Context {
	ctx = context.WithValue(ctx, requestIDKey, cc.RequestID)
	ctx = context.WithValue(ctx, callChainKey, cc.CallChain)
	ctx = context.WithValue(ctx, callDepthKey, cc.CallDepth)
	return ctx
}

// GetCallContext retrieves CallContext from a context.Context
func GetCallContext(ctx context.Context) *CallContext {
	cc := &CallContext{}

	if id, ok := ctx.Value(requestIDKey).(string); ok {
		cc.RequestID = id
	}
	if chain, ok := ctx.Value(callChainKey).([]string); ok {
		cc.CallChain = chain
	}
	if depth, ok := ctx.Value(callDepthKey).(int); ok {
		cc.CallDepth = depth
	}

	// Generate request ID if not present
	if cc.RequestID == "" {
		cc.RequestID = uuid.New().String()
	}

	return cc
}
