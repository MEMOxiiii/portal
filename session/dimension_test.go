package session

import (
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func TestSelectProxyDimension(t *testing.T) {
	dimensions := []int32{packet.DimensionOverworld, packet.DimensionNether, packet.DimensionEnd}
	for _, source := range dimensions {
		for _, target := range dimensions {
			got := selectProxyDimension(source, target)
			if got == source || got == target {
				t.Fatalf("source=%d target=%d: selected conflicting dimension %d", source, target, got)
			}
			valid := false
			for _, dimension := range dimensions {
				valid = valid || got == dimension
			}
			if !valid {
				t.Fatalf("source=%d target=%d: selected invalid dimension %d", source, target, got)
			}
		}
	}
}
