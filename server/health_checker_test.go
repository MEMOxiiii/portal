package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPingNetherNetSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/join" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := pingNetherNet(srv.URL, time.Second); err != nil {
		t.Fatalf("pingNetherNet() error = %v, want nil", err)
	}
}

func TestPingNetherNetNonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	if err := pingNetherNet(srv.URL, time.Second); err == nil {
		t.Fatal("pingNetherNet() error = nil, want error for non-200 status")
	}
}

func TestPingNetherNetUnreachable(t *testing.T) {
	if err := pingNetherNet("http://127.0.0.1:1", 200*time.Millisecond); err == nil {
		t.Fatal("pingNetherNet() error = nil, want error for unreachable endpoint")
	}
}

// TestHealthCheckerPrunesFailuresForRemovedServers guards against failures growing without bound over the
// life of a proxy whose backend server names change over time.
func TestHealthCheckerPrunesFailuresForRemovedServers(t *testing.T) {
	reg := NewDefaultRegistry()
	srv := New("ghost", "127.0.0.1:1", TransportRakNet, "", 1, false)
	reg.AddServer(srv)

	hc := NewHealthChecker(reg, time.Hour, 100*time.Millisecond, 100, nil, nil)
	hc.check(srv)

	hc.mu.Lock()
	_, tracked := hc.failures["ghost"]
	hc.mu.Unlock()
	if !tracked {
		t.Fatal("expected a failure to be recorded for ghost")
	}

	reg.RemoveServer(srv)
	hc.checkAll()

	hc.mu.Lock()
	_, stillTracked := hc.failures["ghost"]
	hc.mu.Unlock()
	if stillTracked {
		t.Fatal("failures entry for removed server was not pruned")
	}
}
