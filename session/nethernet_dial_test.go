package session

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/df-mc/go-nethernet"
	"github.com/df-mc/go-nethernet/endpoint"
)

func TestNetherNetDialAddress(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "http with port", in: "http://127.0.0.1:19135", want: "http://127.0.0.1:19135:19135"},
		{name: "https with port", in: "https://example.com:19133", want: "https://example.com:19133:19133"},
		{name: "no port", in: "http://127.0.0.1", wantErr: true},
		{name: "not a url", in: "127.0.0.1:19135", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := netherNetDialAddress(test.in)
			if test.wantErr {
				if err == nil {
					t.Fatalf("netherNetDialAddress(%q) error = nil, want error", test.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("netherNetDialAddress(%q) error = %v, want nil", test.in, err)
			}
			if got != test.want {
				t.Fatalf("netherNetDialAddress(%q) = %q, want %q", test.in, got, test.want)
			}
		})
	}
}

// stubNotifier records every signal it receives.
type stubNotifier struct {
	received []*nethernet.Signal
}

func (s *stubNotifier) NotifySignal(signal *nethernet.Signal) bool {
	s.received = append(s.received, signal)
	return true
}

func TestNetherNetRewritingNotifier(t *testing.T) {
	stub := &stubNotifier{}
	notifier := netherNetRewritingNotifier{Notifier: stub, from: "clean", to: "dirty"}

	notifier.NotifySignal(&nethernet.Signal{NetworkID: "clean", ConnectionID: 1})
	notifier.NotifySignal(&nethernet.Signal{NetworkID: "unrelated", ConnectionID: 2})

	if len(stub.received) != 2 {
		t.Fatalf("got %d received signals, want 2", len(stub.received))
	}
	if got := stub.received[0].NetworkID; got != "dirty" {
		t.Fatalf("matching signal NetworkID = %q, want %q (rewritten)", got, "dirty")
	}
	if got := stub.received[1].NetworkID; got != "unrelated" {
		t.Fatalf("non-matching signal NetworkID = %q, want %q (untouched)", got, "unrelated")
	}
}

// TestNetherNetDialSignalingUsesCleanURL proves the actual HTTP request built by the underlying
// endpoint.Client targets the clean signaling URL, even though the dirty (doubled-port) address is what
// was logically "dialed" -- the specific bug this file exists to work around.
func TestNetherNetDialSignalingUsesCleanURL(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/sdp")
		_, _ = w.Write([]byte("0")) // a numeric body is treated as a negotiation error code, not a real SDP answer
	}))
	defer srv.Close()

	clean := srv.URL
	dirty, err := netherNetDialAddress(clean)
	if err != nil {
		t.Fatalf("netherNetDialAddress(%q) error: %v", clean, err)
	}

	signaling := &netherNetDialSignaling{
		Client: endpoint.NewClient(),
		dirty:  dirty,
		clean:  clean,
	}

	// Signal targets s.clean regardless of what NetworkID the caller (nethernet.Dialer, normally) passes
	// in -- here, dirty, matching how session.dial actually calls it.
	err = signaling.Signal(context.Background(), &nethernet.Signal{
		Type:      nethernet.SignalTypeOffer,
		NetworkID: dirty,
		Data:      "test-offer",
	})
	// The stub server's numeric response body ("0") is reported back as a negotiation error, which is
	// expected here: this test only cares that the request reached the server at all, and at the right path.
	if err == nil {
		t.Fatal("Signal() error = nil, want negotiation error from the stub server's placeholder response")
	}
	if !strings.Contains(gotPath, "/v1/join/") {
		t.Fatalf("request path = %q, want it to contain \"/v1/join/\"", gotPath)
	}
}
