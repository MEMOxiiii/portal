package session

import (
	"strings"
	"sync"

	"github.com/google/uuid"
)

// Store represents a store which holds all the open sessions on the proxy.
type Store struct {
	mu           sync.Mutex
	sessions     map[uuid.UUID]*Session
	sessionNames map[string]*Session

	// PreTransfer is called before a transfer dial to notify the target server to clean up
	// stale sessions. It receives the target server name and the player name being transferred.
	PreTransfer func(serverName, playerName string)
}

// NewDefaultStore creates a new Store and returns it.
func NewDefaultStore() *Store {
	return &Store{
		sessions:     make(map[uuid.UUID]*Session),
		sessionNames: make(map[string]*Session),
	}
}

// All returns all the sessions stored on the proxy.
func (s *Store) All() (all []*Session) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, v := range s.sessions {
		all = append(all, v)
	}
	return
}

// Load attempts to load a session from the UUID of a player.
func (s *Store) Load(x uuid.UUID) (*Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	v, ok := s.sessions[x]
	return v, ok
}

// LoadFromName attempts to load a session from the username of a player, case-insensitive.
func (s *Store) LoadFromName(x string) (*Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	v, ok := s.sessionNames[strings.ToLower(x)]
	return v, ok
}

// Store stores the session on the proxy.
func (s *Store) Store(x *Session) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.sessions[x.UUID()] = x
	// x.conn (not the exported Conn()) since Store/Delete can run while the session's own loginMu is still
	// held by New()'s own goroutine on a dial/login failure; conn itself never changes after construction.
	s.sessionNames[strings.ToLower(x.conn.IdentityData().DisplayName)] = x
}

// Delete removes x from the store, but only if it's still the session currently stored under its UUID: a
// reconnect can create and Store a new session under the same UUID before the old one's own Close gets
// around to calling Delete, and that must not remove the new one.
func (s *Store) Delete(x *Session) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.sessions[x.UUID()] != x {
		return
	}
	delete(s.sessions, x.UUID())
	delete(s.sessionNames, strings.ToLower(x.conn.IdentityData().DisplayName))
}
