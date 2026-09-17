package socket

import (
	"net"
	"testing"

	"github.com/google/uuid"
	"github.com/paroxity/portal/socket/packet"
)

// TestClientWritePacketConcurrent guards against a regression where WritePacket released its send lock
// before the marshaled bytes were actually written to the connection, letting a concurrent WritePacket
// call overwrite the shared buffer mid-send and corrupt the packet on the wire.
func TestClientWritePacketConcurrent(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	writer := NewClient(client, nil, false)
	reader := NewClient(server, nil, false)

	const n = 50
	ids := make([]uuid.UUID, n)
	for i := range ids {
		ids[i] = uuid.New()
	}

	writeErrs := make(chan error, n)
	for _, id := range ids {
		go func(id uuid.UUID) {
			writeErrs <- writer.WritePacket(&packet.UpdatePlayerLatency{PlayerUUID: id, Latency: 1})
		}(id)
	}

	type result struct {
		id  uuid.UUID
		err error
	}
	reads := make(chan result, n)
	go func() {
		for i := 0; i < n; i++ {
			pk, err := reader.ReadPacket()
			if err != nil {
				reads <- result{err: err}
				continue
			}
			latency, ok := pk.(*packet.UpdatePlayerLatency)
			if !ok {
				reads <- result{err: err}
				continue
			}
			reads <- result{id: latency.PlayerUUID}
		}
	}()

	for i := 0; i < n; i++ {
		if err := <-writeErrs; err != nil {
			t.Fatalf("WritePacket: %v", err)
		}
	}

	seen := make(map[uuid.UUID]bool, n)
	for i := 0; i < n; i++ {
		r := <-reads
		if r.err != nil {
			t.Fatalf("ReadPacket: %v", r.err)
		}
		if seen[r.id] {
			t.Fatalf("received duplicate/corrupted UUID %v", r.id)
		}
		seen[r.id] = true
	}

	for _, id := range ids {
		if !seen[id] {
			t.Errorf("never received packet for UUID %v", id)
		}
	}
}
