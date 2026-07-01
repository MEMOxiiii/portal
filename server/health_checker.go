package server

import (
	"context"
	"sync"
	"time"

	"github.com/paroxity/portal/event"
	"github.com/paroxity/portal/internal"
	"github.com/sandertv/go-raknet"
)

// HealthChecker periodically sends a RakNet unconnected ping to every server in a Registry to verify it is
// actually reachable and responding, rather than just registered over the socket protocol. A server that
// fails consecutive checks past failureThreshold is marked unhealthy so load balancers skip it; it is
// marked healthy again automatically as soon as a ping succeeds. This protects against a server that is
// still socket-connected but hung, crashed, or otherwise not actually serving the game, regardless of
// whether the proxy is fronting a single small server or a large fleet.
type HealthChecker struct {
	registry         *Registry
	interval         time.Duration
	timeout          time.Duration
	failureThreshold int
	log              internal.Logger
	events           *event.Bus

	mu       sync.Mutex
	failures map[string]int
}

// NewHealthChecker creates a HealthChecker for the provided registry. events may be nil, in which case no
// health transition events are published.
func NewHealthChecker(registry *Registry, interval, timeout time.Duration, failureThreshold int, log internal.Logger, events *event.Bus) *HealthChecker {
	return &HealthChecker{
		registry:         registry,
		interval:         interval,
		timeout:          timeout,
		failureThreshold: failureThreshold,
		log:              log,
		events:           events,

		failures: make(map[string]int),
	}
}

// Start runs health checks at the configured interval until ctx is cancelled.
func (h *HealthChecker) Start(ctx context.Context) {
	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.checkAll()
		}
	}
}

// checkAll pings every registered server concurrently.
func (h *HealthChecker) checkAll() {
	var wg sync.WaitGroup
	for _, srv := range h.registry.Servers() {
		wg.Add(1)
		go func(srv *Server) {
			defer wg.Done()
			h.check(srv)
		}(srv)
	}
	wg.Wait()
}

// check pings a single server and updates its healthy state based on the result.
func (h *HealthChecker) check(srv *Server) {
	_, err := raknet.PingTimeout(srv.Address(), h.timeout)

	h.mu.Lock()
	defer h.mu.Unlock()

	if err != nil {
		h.failures[srv.Name()]++
		if h.failures[srv.Name()] >= h.failureThreshold && srv.Healthy() {
			srv.SetHealthy(false)
			h.logf("server %q failed %d consecutive health checks, marking unhealthy: %v", srv.Name(), h.failures[srv.Name()], err)
			h.publish(srv, false)
		}
		return
	}

	h.failures[srv.Name()] = 0
	if !srv.Healthy() {
		srv.SetHealthy(true)
		h.logf("server %q is responding again, marking healthy", srv.Name())
		h.publish(srv, true)
	}
}

func (h *HealthChecker) logf(format string, v ...interface{}) {
	if h.log != nil {
		h.log.Errorf(format, v...)
	}
}

func (h *HealthChecker) publish(srv *Server, healthy bool) {
	if h.events == nil {
		return
	}
	h.events.Publish(event.TopicServerHealthChanged, event.ServerHealthPayload{
		Name:    srv.Name(),
		Address: srv.Address(),
		Healthy: healthy,
	})
}
