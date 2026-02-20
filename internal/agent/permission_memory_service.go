package agent

import (
	"context"
	"fmt"

	"knowledge-agent/internal/ctxutil"
	"knowledge-agent/internal/logger"

	"github.com/achetronic/adk-utils-go/memory/memorytypes"
	"google.golang.org/adk/memory"
	"google.golang.org/adk/session"
)

// PermissionMemoryService wraps a memory service to enforce save permissions.
// It implements memorytypes.ExtendedMemoryService so the toolset can detect
// update_memory and delete_memory capabilities via type assertion.
//
// Permission context is obtained from the method's context.Context parameter,
// which ADK propagates from the HTTP request context through the runner.
type PermissionMemoryService struct {
	baseService       memorytypes.ExtendedMemoryService
	permissionChecker *MemoryPermissionChecker
}

// NewPermissionMemoryService creates a memory service wrapper with permission checking.
// The returned service implements memorytypes.ExtendedMemoryService.
func NewPermissionMemoryService(
	baseService memorytypes.ExtendedMemoryService,
	permissionChecker *MemoryPermissionChecker,
) memorytypes.ExtendedMemoryService {
	return &PermissionMemoryService{
		baseService:       baseService,
		permissionChecker: permissionChecker,
	}
}

// AddSession wraps AddSession with permission checking for save operations
func (s *PermissionMemoryService) AddSession(ctx context.Context, sess session.Session) error {
	log := logger.Get()

	// Check permissions using the method context, which ADK propagates
	// from the HTTP request context through the runner and tool callbacks.
	canSave, permissionReason := s.permissionChecker.CanSaveToMemory(ctx)

	// Extract caller information for logging
	callerID := ctxutil.CallerID(ctx)
	userEmail := ctxutil.UserEmail(ctx)
	userGroups := ctxutil.UserGroups(ctx)
	role := ctxutil.Role(ctx)

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

// Search passes through to base service (no permission check needed for reads)
func (s *PermissionMemoryService) Search(ctx context.Context, req *memory.SearchRequest) (*memory.SearchResponse, error) {
	return s.baseService.Search(ctx, req)
}

// SearchWithID passes through to base service (no permission check needed for reads)
func (s *PermissionMemoryService) SearchWithID(ctx context.Context, req *memory.SearchRequest) ([]memorytypes.EntryWithID, error) {
	return s.baseService.SearchWithID(ctx, req)
}

// UpdateMemory wraps UpdateMemory with permission checking (write operation)
func (s *PermissionMemoryService) UpdateMemory(ctx context.Context, appName, userID string, entryID int, newContent string) error {
	log := logger.Get()

	canWrite, permissionReason := s.permissionChecker.CanSaveToMemory(ctx)

	callerID := ctxutil.CallerID(ctx)
	userEmail := ctxutil.UserEmail(ctx)
	userGroups := ctxutil.UserGroups(ctx)
	role := ctxutil.Role(ctx)

	logFields := []any{
		"operation", "update_memory",
		"caller_id", callerID,
		"role", role,
		"can_write", canWrite,
		"permission_reason", permissionReason,
		"entry_id", entryID,
	}
	if userEmail != "" {
		logFields = append(logFields, "user_email", userEmail)
	}
	if len(userGroups) > 0 {
		logFields = append(logFields, "user_groups", userGroups)
	}

	if !canWrite {
		log.Warnw("update_memory BLOCKED: insufficient permissions", logFields...)
		return fmt.Errorf("⛔ Insufficient permissions. Only authorized users can update information in the knowledge base. Reason: %s", permissionReason)
	}

	log.Infow("update_memory permission granted, proceeding with update", logFields...)

	err := s.baseService.UpdateMemory(ctx, appName, userID, entryID, newContent)
	if err != nil {
		log.Errorw("Failed to update memory",
			append(logFields, "error", err)...)
		return fmt.Errorf("error updating memory: %w", err)
	}

	log.Infow("update_memory completed successfully", logFields...)
	return nil
}

// DeleteMemory wraps DeleteMemory with permission checking (write operation)
func (s *PermissionMemoryService) DeleteMemory(ctx context.Context, appName, userID string, entryID int) error {
	log := logger.Get()

	canWrite, permissionReason := s.permissionChecker.CanSaveToMemory(ctx)

	callerID := ctxutil.CallerID(ctx)
	userEmail := ctxutil.UserEmail(ctx)
	userGroups := ctxutil.UserGroups(ctx)
	role := ctxutil.Role(ctx)

	logFields := []any{
		"operation", "delete_memory",
		"caller_id", callerID,
		"role", role,
		"can_write", canWrite,
		"permission_reason", permissionReason,
		"entry_id", entryID,
	}
	if userEmail != "" {
		logFields = append(logFields, "user_email", userEmail)
	}
	if len(userGroups) > 0 {
		logFields = append(logFields, "user_groups", userGroups)
	}

	if !canWrite {
		log.Warnw("delete_memory BLOCKED: insufficient permissions", logFields...)
		return fmt.Errorf("⛔ Insufficient permissions. Only authorized users can delete information from the knowledge base. Reason: %s", permissionReason)
	}

	log.Infow("delete_memory permission granted, proceeding with delete", logFields...)

	err := s.baseService.DeleteMemory(ctx, appName, userID, entryID)
	if err != nil {
		log.Errorw("Failed to delete memory",
			append(logFields, "error", err)...)
		return fmt.Errorf("error deleting memory: %w", err)
	}

	log.Infow("delete_memory completed successfully", logFields...)
	return nil
}
