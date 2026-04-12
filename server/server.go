package server

import (
	"go.uber.org/atomic"
)

// Server represents a server connected to the proxy which players can join and play on.
type Server struct {
	name       string
	address    string
	legacyAuth bool

	playerCount atomic.Int64
}

// New creates a new Server with the provided name, address and legacy auth setting.
func New(name, address string, legacyAuth bool) *Server {
	s := &Server{
		name:       name,
		address:    address,
		legacyAuth: legacyAuth,
	}

	return s
}

// Name returns the name the server was registered with.
func (s *Server) Name() string {
	return s.name
}

// Address returns the IP address the server was registered with. This should also contain the port separated
// by a colon. E.g. "127.0.0.1:19132".
func (s *Server) Address() string {
	return s.address
}

// LegacyAuth returns whether the proxy should use legacy authentication when dialing this server.
func (s *Server) LegacyAuth() bool {
	return s.legacyAuth
}

// IncrementPlayerCount increments the player count of the server.
func (s *Server) IncrementPlayerCount() {
	s.playerCount.Add(1)
}

// DecrementPlayerCount decreases the player count of the server.
func (s *Server) DecrementPlayerCount() {
	s.playerCount.Sub(1)
}

// PlayerCount returns the player count of the server controlled by the IncrementPlayerCount and
// DecrementPlayerCount functions above.
func (s *Server) PlayerCount() int {
	return int(s.playerCount.Load())
}
