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
