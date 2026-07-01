package server

import (
	"go.uber.org/atomic"
)

// Server represents a server connected to the proxy which players can join and play on.
type Server struct {
	name       string
	address    string
	group      string
	weight     uint32
	legacyAuth bool

	draining    atomic.Bool
	healthy     atomic.Bool
	playerCount atomic.Int64
}

// New creates a new Server with the provided name, address, group, weight and legacy auth setting. Group
// may be empty if the server does not belong to a named group. Weight controls how large a share of new
// players the server should receive relative to others in the same group when load balancing; a weight of
// 0 is treated as 1 (the default), so omitting it keeps the previous even-split behaviour. The server
// starts out marked healthy; a HealthChecker may mark it unhealthy if it stops responding.
func New(name, address, group string, weight uint32, legacyAuth bool) *Server {
	if weight == 0 {
		weight = 1
	}
	s := &Server{
		name:       name,
		address:    address,
		group:      group,
		weight:     weight,
		legacyAuth: legacyAuth,
	}
	s.healthy.Store(true)

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

// Group returns the group the server was registered with. It may be empty if the server does not belong
// to a named group.
func (s *Server) Group() string {
	return s.group
}

// Weight returns the server's load balancing weight. A server with twice the weight of another in the same
// group should, on average, receive twice as many new players.
func (s *Server) Weight() uint32 {
	return s.weight
}

// Draining returns whether the server is currently draining. A draining server should not receive any new
// players from load balancing, but players already connected to it are unaffected.
func (s *Server) Draining() bool {
	return s.draining.Load()
}

// SetDraining sets whether the server is currently draining.
func (s *Server) SetDraining(v bool) {
	s.draining.Store(v)
}

// Healthy returns whether the server last responded to a health check. Like a draining server, an
// unhealthy server should not receive any new players from load balancing, but players already connected
// to it are unaffected since the proxy can't know if they're actually impacted.
func (s *Server) Healthy() bool {
	return s.healthy.Load()
}

// SetHealthy sets whether the server is currently considered healthy.
func (s *Server) SetHealthy(v bool) {
	s.healthy.Store(v)
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
