package socket

import (
	"net"
	"testing"
	"time"

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

// TestClientWritePacketTimeoutOnStuckPeer guards against a regression where an authenticated connection's
// writes had no deadline at all, so a peer that stopped reading (e.g. a stalled backend) blocked WritePacket
// forever -- and with it, any code writing to several clients in a loop, like ReportPlayerLatency.
func TestClientWritePacketTimeoutOnStuckPeer(t *testing.T) {
	old := writeTimeout
	writeTimeout = 50 * time.Millisecond
	defer func() { writeTimeout = old }()

	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	c := NewClient(server, nil, false)
	c.Authenticate("backend1") // simulates a connection well past its auth phase, no deadline in effect

	done := make(chan error, 1)
	go func() {
		done <- c.WritePacket(&packet.UpdatePlayerLatency{Latency: 1})
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("WritePacket succeeded even though the peer never read it")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("WritePacket blocked forever on a peer that stopped reading")
	}
}
