package auth

import (
	"context"
	"sync/atomic"
	"testing"
)

type deleteCountingStore struct {
	saveCount   atomic.Int32
	deleteCount atomic.Int32
}

func (s *deleteCountingStore) List(context.Context) ([]*Auth, error) { return nil, nil }

func (s *deleteCountingStore) Save(context.Context, *Auth) (string, error) {
	s.saveCount.Add(1)
	return "", nil
}

func (s *deleteCountingStore) Delete(context.Context, string) error {
	s.deleteCount.Add(1)
	return nil
}

func TestManager_Delete_RemovesRuntimeAndPersistsDeletion(t *testing.T) {
	store := &deleteCountingStore{}
	mgr := NewManager(store, nil, nil)
	auth := &Auth{
		ID:       "auth-1",
		Provider: "codex",
		Metadata: map[string]any{"type": "codex", "email": "user@example.com"},
	}

	if _, err := mgr.Register(context.Background(), auth); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	removed, err := mgr.Delete(context.Background(), auth.ID)
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if removed == nil || removed.ID != auth.ID {
		t.Fatalf("Delete() removed = %#v, want auth %q", removed, auth.ID)
	}
	if _, ok := mgr.GetByID(auth.ID); ok {
		t.Fatalf("expected auth %q to be removed from manager", auth.ID)
	}
	if got := len(mgr.List()); got != 0 {
		t.Fatalf("List() len = %d, want 0", got)
	}
	if got := store.deleteCount.Load(); got != 1 {
		t.Fatalf("store Delete count = %d, want 1", got)
	}
}

func TestManager_Delete_SkipPersistSkipsStoreDelete(t *testing.T) {
	store := &deleteCountingStore{}
	mgr := NewManager(store, nil, nil)
	auth := &Auth{
		ID:       "auth-1",
		Provider: "codex",
		Metadata: map[string]any{"type": "codex", "email": "user@example.com"},
	}

	if _, err := mgr.Register(context.Background(), auth); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	if _, err := mgr.Delete(WithSkipPersist(context.Background()), auth.ID); err != nil {
		t.Fatalf("Delete(skipPersist) error = %v", err)
	}
	if got := store.deleteCount.Load(); got != 0 {
		t.Fatalf("store Delete count = %d, want 0", got)
	}
}
