package session

import (
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	"go.uber.org/atomic"
)

func newTestTranslator() *translator {
	return &translator{
		originalRuntimeID: 11,
		originalUniqueID:  22,
		currentRuntimeID:  *atomic.NewUint64(33),
		currentUniqueID:   *atomic.NewInt64(44),
	}
}

func TestTranslateEntityMetadataHandlesMixedNumericTypes(t *testing.T) {
	tr := newTestTranslator()
	meta := map[uint32]interface{}{
		5:   int64(22),
		6:   int32(22),
		17:  uint64(22),
		88:  uint32(22),
		124: uint32(11),
		999: "keep-me",
	}

	got := tr.translateEntityMetadata(meta)

	if v, ok := got[5].(int64); !ok || v != 44 {
		t.Fatalf("owner unique ID was not translated correctly: %#v", got[5])
	}
	if v, ok := got[6].(int32); !ok || v != 44 {
		t.Fatalf("target unique ID was not translated correctly: %#v", got[6])
	}
	if v, ok := got[17].(uint64); !ok || v != 44 {
		t.Fatalf("shooter unique ID was not translated correctly: %#v", got[17])
	}
	if v, ok := got[88].(uint32); !ok || v != 44 {
		t.Fatalf("player agent unique ID was not translated correctly: %#v", got[88])
	}
	if v, ok := got[124].(uint32); !ok || v != 33 {
		t.Fatalf("base runtime ID was not translated correctly: %#v", got[124])
	}
	if got[999] != "keep-me" {
		t.Fatalf("unexpected mutation of unrelated metadata: %#v", got[999])
	}
}

func TestTranslatePacketChangeMobPropertyUsesUniqueID(t *testing.T) {
	tr := newTestTranslator()
	pk := &packet.ChangeMobProperty{EntityUniqueID: 22}

	tr.translatePacket(pk)

	if pk.EntityUniqueID != 44 {
		t.Fatalf("expected translated entity unique ID 44, got %d", pk.EntityUniqueID)
	}
}

func TestTranslateRuntimeIDHelpers(t *testing.T) {
	tr := newTestTranslator()

	if got := tr.translateRuntimeID32(11); got != 33 {
		t.Fatalf("expected translated uint32 runtime ID 33, got %d", got)
	}
	if got := tr.translateRuntimeIDInt64(11); got != 33 {
		t.Fatalf("expected translated int64 runtime ID 33, got %d", got)
	}
}

func BenchmarkTranslateEntityMetadata(b *testing.B) {
	tr := newTestTranslator()
	meta := make(map[uint32]interface{}, 40)
	for i := uint32(0); i < 40; i++ {
		meta[i] = int64(i)
	}
	meta[5] = int64(22)
	meta[124] = uint32(11)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tr.translateEntityMetadata(meta)
	}
}
