package session

import (
	"net"
	"sync"
	"time"

	"github.com/sandertv/gophertunnel/minecraft/text"
)

// IPGuard handles incoming connections at the network level, before any whitelist or game-layer
// authentication runs, to protect the proxy from banned or abusive IP addresses.
type IPGuard interface {
	// Allow returns whether a connection from the given address should be allowed to proceed, and a
	// message to show the client if it should not.
	Allow(addr net.Addr) (bool, string)
}

// NopIPGuard allows every connection through without any checks. It is used by default if no IPGuard is
// configured.
type NopIPGuard struct{}

// Allow ...
func (NopIPGuard) Allow(net.Addr) (bool, string) { return true, "" }

// ipWindow tracks connection attempts from a single IP within the current rate limit window.
type ipWindow struct {
	count       int
	windowStart time.Time
}

// SimpleIPGuard rejects connections from a static list of banned IPs and, optionally, IPs that have
// connected more than a configured number of times within a sliding time window.
type SimpleIPGuard struct {
	banned map[string]struct{}

	rateLimitEnabled bool
	window           time.Duration
	limit            int

	mu   sync.Mutex
	hits map[string]*ipWindow
}

// NewSimpleIPGuard creates an IPGuard which bans the IPs provided and, if rateLimitEnabled is true, rejects
// connections once an IP has connected more than limit times within window.
func NewSimpleIPGuard(bannedIPs []string, rateLimitEnabled bool, window time.Duration, limit int) *SimpleIPGuard {
	banned := make(map[string]struct{}, len(bannedIPs))
	for _, ip := range bannedIPs {
		banned[ip] = struct{}{}
	}
	return &SimpleIPGuard{
		banned: banned,

		rateLimitEnabled: rateLimitEnabled,
		window:           window,
		limit:            limit,

		hits: make(map[string]*ipWindow),
	}
}

// Allow ...
func (g *SimpleIPGuard) Allow(addr net.Addr) (bool, string) {
	host := ipHostOf(addr)
	if _, ok := g.banned[host]; ok {
		return false, text.Colourf("<red>You are banned from this server</red>")
	}
	if !g.rateLimitEnabled {
		return true, ""
	}

	now := time.Now()
	g.mu.Lock()
	defer g.mu.Unlock()

	w, ok := g.hits[host]
	if !ok || now.Sub(w.windowStart) > g.window {
		w = &ipWindow{windowStart: now}
		g.hits[host] = w
	}
	w.count++
	if w.count > g.limit {
		return false, text.Colourf("<red>You are connecting too frequently, please wait a moment</red>")
	}
	return true, ""
}

// ipHostOf returns the host portion of a net.Addr, falling back to its full string form if it cannot be
// split into a host and port.
func ipHostOf(addr net.Addr) string {
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return addr.String()
	}
	return host
}
