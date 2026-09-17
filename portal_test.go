package portal

import (
	"net/http"
	"testing"

	"github.com/sandertv/gophertunnel/minecraft"
)

// noAuthListenConfig disables the OIDC verifier so Listen doesn't reach out to Microsoft's identity
// provider, keeping the test hermetic.
var noAuthListenConfig = minecraft.ListenConfig{AuthenticationDisabled: true}

func TestListenDefaultsToNetherNet(t *testing.T) {
	p := New(Options{Address: "127.0.0.1:0", ListenConfig: noAuthListenConfig})
	if p.transport != TransportNetherNet {
		t.Fatalf("default transport = %q, want %q", p.transport, TransportNetherNet)
	}
	if err := p.Listen(); err != nil {
		t.Fatalf("Listen() error: %v", err)
	}
	defer p.Close()

	if p.netherNet == nil {
		t.Fatal("Listen() did not set up the nethernet signaling endpoint")
	}

	// The signaling endpoint should actually be bound and serving requests.
	resp, err := http.Get("http://" + p.netherNet.l.Addr().String() + "/v1/join")
	if err != nil {
		t.Fatalf("GET /v1/join: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /v1/join status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestListenRakNet(t *testing.T) {
	p := New(Options{Address: "127.0.0.1:0", Transport: TransportRakNet, ListenConfig: noAuthListenConfig})
	if err := p.Listen(); err != nil {
		t.Fatalf("Listen() error: %v", err)
	}
	if p.netherNet != nil {
		t.Fatal("Listen() set up the nethernet signaling endpoint for the raknet transport")
	}
	if err := p.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}
}

func TestListenUnknownTransport(t *testing.T) {
	p := New(Options{Address: "127.0.0.1:0", Transport: "quic", ListenConfig: noAuthListenConfig})
	if err := p.Listen(); err == nil {
		t.Fatal("Listen() with an unknown transport should return an error")
	}
}
