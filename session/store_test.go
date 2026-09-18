package session

import (
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft"
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

// TestStoreConcurrentReconnectSameUUID races a reconnect (a new session Stored under a UUID) against the
// old session's own Delete for the same UUID, many times, to catch the old-session cleanup removing the new
// session under real scheduling rather than just a fixed ordering.
func TestStoreConcurrentReconnectSameUUID(t *testing.T) {
	store := NewDefaultStore()
	id := uuid.New()

	for round := 0; round < 200; round++ {
		old := &Session{uuid: id, conn: &minecraft.Conn{}}
		store.mu.Lock()
		store.sessions[id] = old
		store.mu.Unlock()

		next := &Session{uuid: id, conn: &minecraft.Conn{}}

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			store.Delete(old)
		}()
		go func() {
			defer wg.Done()
			store.mu.Lock()
			store.sessions[id] = next
			store.mu.Unlock()
		}()
		wg.Wait()

		got, ok := store.Load(id)
		if !ok || got != next {
			t.Fatalf("round %d: reconnect session lost; Load(%v) = %v, %v, want next, true", round, id, got, ok)
		}
	}
}
