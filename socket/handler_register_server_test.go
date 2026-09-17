package socket

import (
	"testing"

	"github.com/paroxity/portal/server"
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
