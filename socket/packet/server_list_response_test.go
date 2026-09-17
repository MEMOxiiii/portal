package packet

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// TestServerListResponseUnmarshalHugeCount guards against a regression where Unmarshal preallocated a
// []ServerEntry sized directly from an attacker-controlled wire value, which let a tiny crafted packet
// (a huge count with no backing data) trigger a multi-gigabyte allocation attempt instead of failing fast.
func TestServerListResponseUnmarshalHugeCount(t *testing.T) {
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.LittleEndian, uint32(0xFFFFFFFF))

	r := protocol.NewReader(&buf, 0, true)

	defer func() {
		if recover() == nil {
			t.Fatal("expected Unmarshal to panic/fail on truncated data instead of succeeding")
		}
	}()

	pk := &ServerListResponse{}
	pk.Unmarshal(r)
}
