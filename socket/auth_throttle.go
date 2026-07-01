package socket

import (
	"net"
	"sync"
	"time"
)

const (
	// authFailureWindow is the period over which failed authentication attempts are counted.
	authFailureWindow = time.Minute
	// authFailureLimit is the number of failed attempts allowed from a single IP within the window
	// before it is temporarily blocked.
	authFailureLimit = 5
	// authBlockDuration is how long an IP is blocked from authenticating after crossing the limit.
	authBlockDuration = 5 * time.Minute
)

// authThrottle tracks failed socket authentication attempts per IP address and temporarily blocks IPs
// that repeatedly fail to authenticate, mitigating brute-force attempts against the shared secret.
type authThrottle struct {
	mu    sync.Mutex
	state map[string]*ipAuthState
}

type ipAuthState struct {
	failures     int
	windowStart  time.Time
	blockedUntil time.Time
}

func newAuthThrottle() *authThrottle {
	return &authThrottle{state: make(map[string]*ipAuthState)}
}

// Blocked returns whether the IP of the provided address is currently blocked from authenticating.
func (t *authThrottle) Blocked(addr net.Addr) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	s, ok := t.state[hostOf(addr)]
	if !ok {
		return false
	}
	return time.Now().Before(s.blockedUntil)
}

// RecordFailure records a failed authentication attempt from the provided address, blocking the IP for
// authBlockDuration once it crosses authFailureLimit failures within authFailureWindow.
func (t *authThrottle) RecordFailure(addr net.Addr) {
	host := hostOf(addr)
	now := time.Now()

	t.mu.Lock()
	defer t.mu.Unlock()

	s, ok := t.state[host]
	if !ok || now.Sub(s.windowStart) > authFailureWindow {
		s = &ipAuthState{windowStart: now}
		t.state[host] = s
	}
	s.failures++
	if s.failures >= authFailureLimit {
		s.blockedUntil = now.Add(authBlockDuration)
	}
}

// hostOf returns the host portion of a net.Addr, falling back to its full string form if it cannot be
// split into a host and port.
func hostOf(addr net.Addr) string {
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return addr.String()
	}
	return host
}
