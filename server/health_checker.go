package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/paroxity/portal/event"
	"github.com/paroxity/portal/internal"
	"github.com/sandertv/go-raknet"
)

// HealthChecker periodically checks every server in a Registry to verify it is actually reachable and
// responding, rather than just registered over the socket protocol. A TransportRakNet server is checked
// with a RakNet unconnected ping; a TransportNetherNet server, which has no equivalent unconnected ping, is
// checked with a plain HTTP GET of its signaling endpoint. A server that fails consecutive checks past
// failureThreshold is marked unhealthy so load balancers skip it; it is marked healthy again automatically
// as soon as a check succeeds. This protects against a server that is still socket-connected but hung,
// crashed, or otherwise not actually serving the game, regardless of whether the proxy is fronting a single
// small server or a large fleet.
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
	err := h.ping(srv)

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

// ping checks reachability of srv using the strategy appropriate for its transport.
func (h *HealthChecker) ping(srv *Server) error {
	if srv.Transport() == TransportNetherNet {
		return pingNetherNet(srv.Address(), h.timeout)
	}
	_, err := raknet.PingTimeout(srv.Address(), h.timeout)
	return err
}

// pingNetherNet checks reachability of a NetherNet server's signaling endpoint with a plain HTTP GET of
// its "/v1/join" ping route, the same route a Bedrock client uses to discover the endpoint. NetherNet has
// no unconnected-ping equivalent of RakNet's, since signaling requires a full HTTP round trip.
func pingNetherNet(address string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(address, "/")+"/v1/join", nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return nil
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
