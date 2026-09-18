package socket

import (
	"net"
	"testing"

	"github.com/paroxity/portal/server"
	"github.com/paroxity/portal/session"
	"github.com/paroxity/portal/socket/packet"
)

func TestParseTransport(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    server.Transport
		wantErr bool
	}{
		{name: "empty defaults to raknet", in: "", want: server.TransportRakNet},
		{name: "raknet", in: "raknet", want: server.TransportRakNet},
		{name: "nethernet", in: "nethernet", want: server.TransportNetherNet},
		{name: "unknown", in: "quic", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseTransport(test.in)
			if test.wantErr {
				if err == nil {
					t.Fatalf("parseTransport(%q) error = nil, want error", test.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseTransport(%q) error = %v, want nil", test.in, err)
			}
			if got != test.want {
				t.Fatalf("parseTransport(%q) = %q, want %q", test.in, got, test.want)
			}
		})
	}
}

func TestValidateAddress(t *testing.T) {
	tests := []struct {
		name      string
		transport server.Transport
		address   string
		wantErr   bool
	}{
		{name: "raknet host:port", transport: server.TransportRakNet, address: "127.0.0.1:19132"},
		{name: "raknet empty", transport: server.TransportRakNet, address: "", wantErr: true},
		{name: "nethernet http url", transport: server.TransportNetherNet, address: "http://127.0.0.1:19133"},
		{name: "nethernet https url", transport: server.TransportNetherNet, address: "https://example.com:19133"},
		{
			name:      "nethernet host:port left over from raknet",
			transport: server.TransportNetherNet,
			address:   "127.0.0.1:19135",
			wantErr:   true,
		},
		{name: "nethernet empty", transport: server.TransportNetherNet, address: "", wantErr: true},
		{name: "nethernet non-http scheme", transport: server.TransportNetherNet, address: "ftp://127.0.0.1:19135", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateAddress(test.transport, test.address)
			if test.wantErr && err == nil {
				t.Fatalf("validateAddress(%q, %q) error = nil, want error", test.transport, test.address)
			}
			if !test.wantErr && err != nil {
				t.Fatalf("validateAddress(%q, %q) error = %v, want nil", test.transport, test.address, err)
			}
		})
	}
}

// TestRegisterServerHandlerPreservesStateOnReregister guards against a regression where re-registering
// under a name already in the registry (e.g. a client library retrying after a delayed ack) replaced the
// entry with a fresh *server.Server, silently resetting player count, health, and draining status back to
// defaults even though players were actually still connected to it.
func TestRegisterServerHandlerPreservesStateOnReregister(t *testing.T) {
	srv := NewDefaultServer(":0", "secret", session.NewDefaultStore(), server.NewDefaultRegistry(), nopLogger{}, false, nil)

	conn, _ := net.Pipe()
	c := NewClient(conn, nopLogger{}, false)
	if !srv.TryAuthenticate(c, "backend1") {
		t.Fatal("TryAuthenticate should succeed")
	}

	h := &RegisterServerHandler{}
	pk := &packet.RegisterServer{Address: "127.0.0.1:19132", Group: "lobby", Weight: 1}
	if err := h.Handle(pk, srv, c); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	first, ok := srv.ServerRegistry().Server("backend1")
	if !ok {
		t.Fatal("server not registered")
	}
	first.IncrementPlayerCount()
	first.IncrementPlayerCount()
	first.SetDraining(true)
	first.SetHealthy(false)

	if err := h.Handle(pk, srv, c); err != nil {
		t.Fatalf("Handle (re-register): %v", err)
	}

	second, ok := srv.ServerRegistry().Server("backend1")
	if !ok {
		t.Fatal("server not registered after re-registering")
	}
	if second.PlayerCount() != 2 {
		t.Fatalf("PlayerCount() = %d, want 2 (preserved across re-registration)", second.PlayerCount())
	}
	if !second.Draining() {
		t.Fatal("Draining() = false, want true (preserved across re-registration)")
	}
	if second.Healthy() {
		t.Fatal("Healthy() = true, want false (preserved across re-registration)")
	}
}
