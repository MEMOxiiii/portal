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

// SplitLoadBalancer attempts to split players evenly across all the servers.
type SplitLoadBalancer struct {
	registry *server.Registry
}

// NewSplitLoadBalancer creates a "split" load balancer with the provided server registry.
func NewSplitLoadBalancer(registry *server.Registry) *SplitLoadBalancer {
	return &SplitLoadBalancer{registry: registry}
}

// FindServer ...
func (b *SplitLoadBalancer) FindServer(*Session) (srv *server.Server) {
	for _, s := range b.registry.Servers() {
		if s.Draining() {
			continue
		}
		if srv == nil || srv.PlayerCount() > s.PlayerCount() {
			srv = s
		}
	}
	return srv
}

// GroupedLoadBalancer splits players evenly across the non-draining servers in a target group, falling
// back to subsequent groups in order if the preceding group has no available servers. This allows backend
// servers to be organised into groups (e.g. "lobby", "survival") and to be drained ahead of a restart
// without being removed from the registry.
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
		for _, s := range b.registry.ServersInGroup(group) {
			if s.Draining() {
				continue
			}
			if srv == nil || srv.PlayerCount() > s.PlayerCount() {
				srv = s
			}
		}
		if srv != nil {
			return srv
		}
	}
	return nil
}
