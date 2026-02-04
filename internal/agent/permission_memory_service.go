package agent

import (
	"context"
	"fmt"

	"knowledge-agent/internal/ctxutil"
	"knowledge-agent/internal/logger"

	"google.golang.org/adk/memory"
	"google.golang.org/adk/session"
)

// PermissionMemoryService wraps a memory service to enforce save permissions
type PermissionMemoryService struct {
	baseService       memory.Service
	permissionChecker *MemoryPermissionChecker
	contextHolder     *contextHolder
}

// NewPermissionMemoryService creates a memory service wrapper with permission checking
func NewPermissionMemoryService(
	baseService memory.Service,
	permissionChecker *MemoryPermissionChecker,
	contextHolder *contextHolder,
) memory.Service {
	return &PermissionMemoryService{
		baseService:       baseService,
		permissionChecker: permissionChecker,
		contextHolder:     contextHolder,
	}
}

// AddSession wraps AddSession with permission checking for save operations
func (s *PermissionMemoryService) AddSession(ctx context.Context, sess session.Session) error {
	log := logger.Get()

	// Determine the best context to use for permission checking
	// Priority:
	// 1. Method context (ctx) if it has permission values (propagated by ADK)
	// 2. contextHolder context (set by request handler)
	// 3. Method context as fallback
	requestCtx := s.resolvePermissionContext(ctx)

	// Check permissions FIRST before allowing save
	canSave, permissionReason := s.permissionChecker.CanSaveToMemory(requestCtx)

	// Extract caller information for logging
	callerID := ctxutil.CallerID(requestCtx)
	userEmail := ctxutil.UserEmail(requestCtx)
	userGroups := ctxutil.UserGroups(requestCtx)
	role := ctxutil.Role(requestCtx)

	logFields := []any{
		"operation", "save_to_memory",
		"caller_id", callerID,
		"role", role,
		"can_save", canSave,
		"permission_reason", permissionReason,
		"session_id", sess.ID(),
	}
	if userEmail != "" {
		logFields = append(logFields, "user_email", userEmail)
	}
	if len(userGroups) > 0 {
		logFields = append(logFields, "user_groups", userGroups)
	}

	// If user doesn't have permission, reject immediately
	if !canSave {
		log.Warnw("save_to_memory BLOCKED: insufficient permissions", logFields...)

		// Return error that will bubble up to the tool and then to the agent
		return fmt.Errorf("⛔ Insufficient permissions. Only authorized users can save information to the knowledge base. Reason: %s", permissionReason)
	}

	// Permission granted - proceed with save
	log.Infow("save_to_memory permission granted, proceeding with save", logFields...)

	// Call the base memory service to actually save
	err := s.baseService.AddSession(ctx, sess)
	if err != nil {
		log.Errorw("Failed to save to memory service",
			append(logFields, "error", err)...)
		return fmt.Errorf("error saving to memory: %w", err)
	}

	log.Infow("save_to_memory completed successfully", logFields...)
	return nil
}

// resolvePermissionContext determines the best context to use for permission checking.
// This handles the race condition where concurrent requests might overwrite contextHolder.
func (s *PermissionMemoryService) resolvePermissionContext(methodCtx context.Context) context.Context {
	// Check if method context already has the required permission values
	// This happens when ADK properly propagates the context
	if hasPermissionValues(methodCtx) {
		return methodCtx
	}

	// Fall back to contextHolder (set by request handler before calling runner)
	// This is needed because ADK may not propagate context values
	holderCtx := s.contextHolder.GetContext()
	if holderCtx != nil && hasPermissionValues(holderCtx) {
		return holderCtx
	}

	// Last resort: use method context even without values
	// This will result in "unknown" caller_id and default role
	return methodCtx
}

// hasPermissionValues checks if a context has the required permission values set
func hasPermissionValues(ctx context.Context) bool {
	// Check if caller_id is set and not the default "unknown"
	callerID := ctxutil.CallerID(ctx)
	return callerID != "" && callerID != "unknown"
}

// Search passes through to base service (no permission check needed for reads)
func (s *PermissionMemoryService) Search(ctx context.Context, req *memory.SearchRequest) (*memory.SearchResponse, error) {
	return s.baseService.Search(ctx, req)
}
