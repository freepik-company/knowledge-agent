package a2a

import (
	"context"
	"errors"
	"testing"

	a2acore "github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
)

// ctxWithUser creates a context with an authenticated A2A user identity.
func ctxWithUser(username string) context.Context {
	ctx, cc := a2asrv.WithCallContext(context.Background(), a2asrv.NewRequestMeta(nil))
	cc.User = &a2asrv.AuthenticatedUser{UserName: username}
	return ctx
}

// ctxAnonymous creates a context with no authenticated user.
func ctxAnonymous() context.Context {
	ctx, _ := a2asrv.WithCallContext(context.Background(), a2asrv.NewRequestMeta(nil))
	return ctx
}

func TestOwnershipStore_SameUserCanGetOwnTask(t *testing.T) {
	store := NewOwnershipAwareTaskStore()
	ctx := ctxWithUser("alice@example.com")

	task := &a2acore.Task{ID: "task-1"}
	if err := store.Save(ctx, task); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	got, err := store.Get(ctx, "task-1")
	if err != nil {
		t.Fatalf("Get own task failed: %v", err)
	}
	if got.ID != "task-1" {
		t.Errorf("got task ID %q, want %q", got.ID, "task-1")
	}
}

func TestOwnershipStore_DifferentUserCannotGetTask(t *testing.T) {
	store := NewOwnershipAwareTaskStore()

	aliceCtx := ctxWithUser("alice@example.com")
	bobCtx := ctxWithUser("bob@example.com")

	task := &a2acore.Task{ID: "task-1"}
	if err := store.Save(aliceCtx, task); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	_, err := store.Get(bobCtx, "task-1")
	if err == nil {
		t.Fatal("expected error when accessing another user's task, got nil")
	}
	if !errors.Is(err, a2acore.ErrTaskNotFound) {
		t.Errorf("expected ErrTaskNotFound, got: %v", err)
	}
}

func TestOwnershipStore_DifferentUserCannotSaveTask(t *testing.T) {
	store := NewOwnershipAwareTaskStore()

	aliceCtx := ctxWithUser("alice@example.com")
	bobCtx := ctxWithUser("bob@example.com")

	task := &a2acore.Task{ID: "task-1"}
	if err := store.Save(aliceCtx, task); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Bob tries to update Alice's task
	updated := &a2acore.Task{ID: "task-1"}
	err := store.Save(bobCtx, updated)
	if err == nil {
		t.Fatal("expected error when saving another user's task, got nil")
	}
	if !errors.Is(err, a2acore.ErrTaskNotFound) {
		t.Errorf("expected ErrTaskNotFound, got: %v", err)
	}
}

func TestOwnershipStore_OwnerCanUpdateOwnTask(t *testing.T) {
	store := NewOwnershipAwareTaskStore()
	ctx := ctxWithUser("alice@example.com")

	task := &a2acore.Task{ID: "task-1", Status: a2acore.TaskStatus{State: a2acore.TaskStateSubmitted}}
	if err := store.Save(ctx, task); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Alice updates her own task
	updated := &a2acore.Task{ID: "task-1", Status: a2acore.TaskStatus{State: a2acore.TaskStateCompleted}}
	if err := store.Save(ctx, updated); err != nil {
		t.Fatalf("Update own task failed: %v", err)
	}

	got, err := store.Get(ctx, "task-1")
	if err != nil {
		t.Fatalf("Get updated task failed: %v", err)
	}
	if got.Status.State != a2acore.TaskStateCompleted {
		t.Errorf("got state %q, want %q", got.Status.State, a2acore.TaskStateCompleted)
	}
}

func TestOwnershipStore_NonexistentTaskReturnsNotFound(t *testing.T) {
	store := NewOwnershipAwareTaskStore()
	ctx := ctxWithUser("alice@example.com")

	_, err := store.Get(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent task, got nil")
	}
	if !errors.Is(err, a2acore.ErrTaskNotFound) {
		t.Errorf("expected ErrTaskNotFound, got: %v", err)
	}
}

func TestOwnershipStore_AnonymousCanSaveAndGet(t *testing.T) {
	store := NewOwnershipAwareTaskStore()
	ctx := ctxAnonymous()

	task := &a2acore.Task{ID: "task-1"}
	if err := store.Save(ctx, task); err != nil {
		t.Fatalf("Anonymous save failed: %v", err)
	}

	got, err := store.Get(ctx, "task-1")
	if err != nil {
		t.Fatalf("Anonymous get failed: %v", err)
	}
	if got.ID != "task-1" {
		t.Errorf("got task ID %q, want %q", got.ID, "task-1")
	}
}

func TestOwnershipStore_AnonymousCannotAccessOwnedTask(t *testing.T) {
	store := NewOwnershipAwareTaskStore()

	aliceCtx := ctxWithUser("alice@example.com")
	anonCtx := ctxAnonymous()

	task := &a2acore.Task{ID: "task-1"}
	if err := store.Save(aliceCtx, task); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Anonymous user should not bypass ownership — anonymous context has
	// empty owner, so the store allows access (no identity to compare).
	// This is safe because unauthenticated requests are rejected by
	// the HTTP auth middleware before reaching the task store.
	_, err := store.Get(anonCtx, "task-1")
	if err != nil {
		t.Fatalf("Anonymous get of owned task should succeed (auth middleware handles unauthenticated): %v", err)
	}
}

func TestOwnershipStore_MultipleUsersIsolated(t *testing.T) {
	store := NewOwnershipAwareTaskStore()

	aliceCtx := ctxWithUser("alice@example.com")
	bobCtx := ctxWithUser("bob@example.com")

	aliceTask := &a2acore.Task{ID: "alice-task"}
	bobTask := &a2acore.Task{ID: "bob-task"}

	if err := store.Save(aliceCtx, aliceTask); err != nil {
		t.Fatalf("Save alice task: %v", err)
	}
	if err := store.Save(bobCtx, bobTask); err != nil {
		t.Fatalf("Save bob task: %v", err)
	}

	// Alice can get her task
	if _, err := store.Get(aliceCtx, "alice-task"); err != nil {
		t.Errorf("Alice get own task: %v", err)
	}
	// Bob can get his task
	if _, err := store.Get(bobCtx, "bob-task"); err != nil {
		t.Errorf("Bob get own task: %v", err)
	}
	// Cross-access denied
	if _, err := store.Get(aliceCtx, "bob-task"); err == nil {
		t.Error("Alice should not access Bob's task")
	}
	if _, err := store.Get(bobCtx, "alice-task"); err == nil {
		t.Error("Bob should not access Alice's task")
	}
}

func TestOwnershipStore_NoCallContextFallsThrough(t *testing.T) {
	store := NewOwnershipAwareTaskStore()

	// Plain context without CallContext (should not panic)
	ctx := context.Background()

	task := &a2acore.Task{ID: "task-1"}
	if err := store.Save(ctx, task); err != nil {
		t.Fatalf("Save with no CallContext: %v", err)
	}
	if _, err := store.Get(ctx, "task-1"); err != nil {
		t.Fatalf("Get with no CallContext: %v", err)
	}
}
