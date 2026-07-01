// Package metrics exposes lightweight, dependency-free counters and gauges for the proxy in the
// Prometheus text exposition format, avoiding the need to pull in the full Prometheus client library for a
// handful of values.
package metrics

import (
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// GaugeFunc returns the current value of a gauge metric on demand, evaluated each time the registry is
// scraped.
type GaugeFunc func() float64

// Registry holds the proxy's metrics and can serve them over HTTP in the Prometheus text exposition
// format.
type Registry struct {
	mu         sync.Mutex
	gaugeFuncs map[string]GaugeFunc

	transfersSucceeded       atomic.Int64
	transfersFailed          atomic.Int64
	transferDurationNanosSum atomic.Int64
	transferDurationCount    atomic.Int64
}

// NewRegistry creates an empty metrics registry.
func NewRegistry() *Registry {
	return &Registry{gaugeFuncs: make(map[string]GaugeFunc)}
}

// Default is the registry used by the proxy unless a different one is wired up explicitly.
var Default = NewRegistry()

// RegisterGauge registers a function whose return value is reported under the given metric name every time
// the registry is scraped. Registering under a name that is already in use overwrites the previous
// function.
func (r *Registry) RegisterGauge(name string, fn GaugeFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gaugeFuncs[name] = fn
}

// RecordTransfer records the outcome and duration of a player transfer between servers.
func (r *Registry) RecordTransfer(success bool, duration time.Duration) {
	if success {
		r.transfersSucceeded.Add(1)
	} else {
		r.transfersFailed.Add(1)
	}
	r.transferDurationNanosSum.Add(duration.Nanoseconds())
	r.transferDurationCount.Add(1)
}

// Handler returns an http.Handler that serves the registry's metrics in the Prometheus text exposition
// format.
func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")

		r.mu.Lock()
		for name, fn := range r.gaugeFuncs {
			fmt.Fprintf(w, "# TYPE %s gauge\n%s %v\n", name, name, fn())
		}
		r.mu.Unlock()

		fmt.Fprintf(w, "# TYPE portal_transfers_total counter\n")
		fmt.Fprintf(w, "portal_transfers_total{result=\"success\"} %d\n", r.transfersSucceeded.Load())
		fmt.Fprintf(w, "portal_transfers_total{result=\"failed\"} %d\n", r.transfersFailed.Load())

		fmt.Fprintf(w, "# TYPE portal_transfer_duration_seconds_sum counter\n")
		fmt.Fprintf(w, "portal_transfer_duration_seconds_sum %f\n", time.Duration(r.transferDurationNanosSum.Load()).Seconds())
		fmt.Fprintf(w, "# TYPE portal_transfer_duration_seconds_count counter\n")
		fmt.Fprintf(w, "portal_transfer_duration_seconds_count %d\n", r.transferDurationCount.Load())
	})
}
