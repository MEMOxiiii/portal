package session

import (
	"net"
	"testing"

	"github.com/df-mc/go-nethernet"
	"github.com/pion/webrtc/v4"
)

func TestIPHostOfUDPAddr(t *testing.T) {
	addr := &net.UDPAddr{IP: net.ParseIP("203.0.113.5"), Port: 19132}
	if got := ipHostOf(addr); got != "203.0.113.5" {
		t.Fatalf("ipHostOf(%v) = %q, want %q", addr, got, "203.0.113.5")
	}
}

func TestIPHostOfNetherNetAddr(t *testing.T) {
	t.Run("selected candidate", func(t *testing.T) {
		addr := &nethernet.Addr{
			NetworkID:         "123",
			Candidates:        []webrtc.ICECandidate{{Address: "198.51.100.2"}},
			SelectedCandidate: &webrtc.ICECandidate{Address: "198.51.100.9"},
		}
		if got := ipHostOf(addr); got != "198.51.100.9" {
			t.Fatalf("ipHostOf(%v) = %q, want %q", addr, got, "198.51.100.9")
		}
	})

	t.Run("falls back to first candidate", func(t *testing.T) {
		addr := &nethernet.Addr{
			NetworkID:  "123",
			Candidates: []webrtc.ICECandidate{{Address: "198.51.100.2"}},
		}
		if got := ipHostOf(addr); got != "198.51.100.2" {
			t.Fatalf("ipHostOf(%v) = %q, want %q", addr, got, "198.51.100.2")
		}
	})

	t.Run("falls back to string form", func(t *testing.T) {
		addr := &nethernet.Addr{NetworkID: "123"}
		if got := ipHostOf(addr); got != addr.String() {
			t.Fatalf("ipHostOf(%v) = %q, want %q", addr, got, addr.String())
		}
	})
}
