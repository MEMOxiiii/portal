package session

import (
	"github.com/paroxity/portal/server"
)

// LoadBalancer represents a load balancer which helps balance the load of players on the proxy.
type LoadBalancer interface {
	// FindServer finds a server for the session to connect to when they first join. If nil is returned, the
	// player is kicked from the proxy.
	FindServer(session *Session) *server.Server
}

// SplitLoadBalancer splits players across all available servers in proportion to their weight.
type SplitLoadBalancer struct {
	registry *server.Registry
}

// NewSplitLoadBalancer creates a "split" load balancer with the provided server registry.
func NewSplitLoadBalancer(registry *server.Registry) *SplitLoadBalancer {
	return &SplitLoadBalancer{registry: registry}
}

// FindServer ...
func (b *SplitLoadBalancer) FindServer(*Session) (srv *server.Server) {
	return leastLoaded(b.registry.Servers())
}

// GroupedLoadBalancer splits players, in proportion to their weight, across the available servers in a
// target group, falling back to subsequent groups in order if the preceding group has no available
// servers. This allows backend servers to be organised into groups (e.g. "lobby", "survival") and to be
// drained ahead of a restart without being removed from the registry.
type GroupedLoadBalancer struct {
	registry *server.Registry
	groups   []string
}

// NewGroupedLoadBalancer creates a "grouped" load balancer which balances players across the servers in
// primaryGroup, falling back to the groups in fallbackGroups, in order, if primaryGroup has no available
// servers.
func NewGroupedLoadBalancer(registry *server.Registry, primaryGroup string, fallbackGroups ...string) *GroupedLoadBalancer {
	return &GroupedLoadBalancer{
		registry: registry,
		groups:   append([]string{primaryGroup}, fallbackGroups...),
	}
}

// FindServer ...
func (b *GroupedLoadBalancer) FindServer(*Session) (srv *server.Server) {
	for _, group := range b.groups {
		if srv = leastLoaded(b.registry.ServersInGroup(group)); srv != nil {
			return srv
		}
	}
	return nil
}

// leastLoaded returns the available (non-draining, healthy) server from servers with the lowest player
// count relative to its weight, or nil if none are available. A server with twice the weight of another
// receives roughly twice as many players before being considered equally loaded, which lets a network mix
// servers of different capacity within the same pool; servers all default to a weight of 1, so a network
// that never sets weights gets a plain even split.
func leastLoaded(servers []*server.Server) (srv *server.Server) {
	var lowest float64
	for _, s := range servers {
		if s.Draining() || !s.Healthy() {
			continue
		}
		ratio := float64(s.PlayerCount()) / float64(s.Weight())
		if srv == nil || ratio < lowest {
			srv, lowest = s, ratio
		}
	}
	return srv
}
