package session

import (
	"testing"

	"github.com/google/uuid"
)

// TestStoreDeleteDoesNotRemoveReplacedSession guards against a regression where Delete looked sessions up
// by UUID alone: a reconnecting player creates and Stores a new session under the same UUID before the old
// session's own cleanup gets around to calling Delete, and that must not remove the new one.
func TestStoreDeleteDoesNotRemoveReplacedSession(t *testing.T) {
	id := uuid.New()
	oldSession := &Session{uuid: id}
	newSession := &Session{uuid: id}

	store := NewDefaultStore()
	store.sessions[id] = newSession

	store.Delete(oldSession)

	got, ok := store.Load(id)
	if !ok || got != newSession {
		t.Fatalf("Delete(oldSession) removed the replacement session; Load(%v) = %v, %v, want newSession, true", id, got, ok)
	}
}
