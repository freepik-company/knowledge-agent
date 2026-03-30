package a2a

import (
	"context"
	"fmt"
	"sync"

	a2acore "github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"

	"knowledge-agent/internal/logger"
)

// OwnershipAwareTaskStore implements a2asrv.TaskStore with task ownership enforcement.
// Each task is associated with the authenticated user who created it.
// Subsequent Get calls are denied if the requesting user does not own the task.
type OwnershipAwareTaskStore struct {
	tasks  map[a2acore.TaskID]*a2acore.Task
	owners map[a2acore.TaskID]string // taskID -> owner username
	mu     sync.RWMutex
}

// NewOwnershipAwareTaskStore creates a new task store with ownership enforcement.
func NewOwnershipAwareTaskStore() *OwnershipAwareTaskStore {
	return &OwnershipAwareTaskStore{
		tasks:  make(map[a2acore.TaskID]*a2acore.Task),
		owners: make(map[a2acore.TaskID]string),
	}
}

// Save stores a task and associates it with the authenticated user from CallContext.
// If the task already exists, only the original owner can update it.
func (s *OwnershipAwareTaskStore) Save(ctx context.Context, task *a2acore.Task) error {
	owner := ownerFromContext(ctx)

	s.mu.Lock()
	defer s.mu.Unlock()

	if existingOwner, exists := s.owners[task.ID]; exists && owner != "" && existingOwner != owner {
		log := logger.Get()
		log.Warnw("Task ownership violation on save",
			"task_id", task.ID,
			"owner", existingOwner,
			"requester", owner,
		)
		return a2acore.ErrTaskNotFound
	}

	s.tasks[task.ID] = task
	if owner != "" {
		s.owners[task.ID] = owner
	}

	return nil
}

// Get retrieves a task by ID, enforcing that the requesting user owns the task.
func (s *OwnershipAwareTaskStore) Get(ctx context.Context, taskID a2acore.TaskID) (*a2acore.Task, error) {
	owner := ownerFromContext(ctx)

	s.mu.RLock()
	defer s.mu.RUnlock()

	task, exists := s.tasks[taskID]
	if !exists {
		return nil, a2acore.ErrTaskNotFound
	}

	taskOwner := s.owners[taskID]
	if taskOwner != "" && owner != "" && taskOwner != owner {
		log := logger.Get()
		log.Warnw("Task ownership violation on get",
			"task_id", taskID,
			"owner", taskOwner,
			"requester", owner,
		)
		return nil, fmt.Errorf("%w: access denied", a2acore.ErrTaskNotFound)
	}

	return task, nil
}

// ownerFromContext extracts the authenticated user identity from the A2A SDK CallContext.
func ownerFromContext(ctx context.Context) string {
	cc, ok := a2asrv.CallContextFrom(ctx)
	if !ok {
		return ""
	}
	if cc.User == nil || !cc.User.Authenticated() {
		return ""
	}
	return cc.User.Name()
}
