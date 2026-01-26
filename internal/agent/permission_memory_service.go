package agent

import (
	"context"
	"fmt"

	"knowledge-agent/internal/ctxutil"
	"knowledge-agent/internal/logger"
	"knowledge-agent/internal/permissions"

	"google.golang.org/adk/memory"
	"google.golang.org/adk/session"
)

// PermissionMemoryService wraps a memory service to enforce save permissions
type PermissionMemoryService struct {
	baseService       memory.Service
	permissionChecker *permissions.MemoryPermissionChecker
	contextHolder     *contextHolder
}

// NewPermissionMemoryService creates a memory service wrapper with permission checking
func NewPermissionMemoryService(
	baseService memory.Service,
	permissionChecker *permissions.MemoryPermissionChecker,
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

	// Get current request context from contextHolder
	requestCtx := s.contextHolder.GetContext()
	if requestCtx == nil {
		requestCtx = ctx
	}

	// Check permissions FIRST before allowing save
	canSave, permissionReason := s.permissionChecker.CanSaveToMemory(requestCtx)

	// Extract caller information for logging
	callerID := ctxutil.CallerID(requestCtx)
	slackUserID := ctxutil.SlackUserID(requestCtx)

	logFields := []any{
		"operation", "save_to_memory",
		"caller_id", callerID,
		"can_save", canSave,
		"permission_reason", permissionReason,
		"session_id", sess.ID(),
	}
	if slackUserID != "" {
		logFields = append(logFields, "slack_user_id", slackUserID)
	}

	// If user doesn't have permission, reject immediately
	// Note: When lists are empty, CanSaveToMemory returns false (deny by default)
	if !canSave {
		log.Warnw("save_to_memory BLOCKED: insufficient permissions", logFields...)

		// Return error that will bubble up to the tool and then to the agent
		return fmt.Errorf("⛔ Permisos insuficientes. Solo los usuarios autorizados pueden guardar información en la base de conocimiento. Razón: %s", permissionReason)
	}

	// Permission granted or no permission system configured - proceed with save
	log.Infow("save_to_memory permission granted, proceeding with save", logFields...)

	// Call the base memory service to actually save
	err := s.baseService.AddSession(ctx, sess)
	if err != nil {
		log.Errorw("Failed to save to memory service",
			append(logFields, "error", err)...)
		return fmt.Errorf("error al guardar en memoria: %w", err)
	}

	log.Infow("save_to_memory completed successfully", logFields...)
	return nil
}

// Search passes through to base service (no permission check needed for reads)
func (s *PermissionMemoryService) Search(ctx context.Context, req *memory.SearchRequest) (*memory.SearchResponse, error) {
	return s.baseService.Search(ctx, req)
}

// Note: memory.Service interface doesn't have Close() method
// The underlying service (memorypostgres) will be closed via agent.Close()
