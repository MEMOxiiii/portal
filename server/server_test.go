package server

import "testing"

func TestNewDefaultsToRakNetTransport(t *testing.T) {
	s := New("test", "127.0.0.1:19132", "", "", 0, false)
	if got := s.Transport(); got != TransportRakNet {
		t.Fatalf("Transport() = %q, want %q", got, TransportRakNet)
	}
}

func TestNewKeepsExplicitTransport(t *testing.T) {
	s := New("test", "http://127.0.0.1:19132", TransportNetherNet, "", 0, false)
	if got := s.Transport(); got != TransportNetherNet {
		t.Fatalf("Transport() = %q, want %q", got, TransportNetherNet)
	}
}
