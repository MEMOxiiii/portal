package socket

import (
	"net"
	"sync"
	"testing"
	"time"

	"github.com/paroxity/portal/event"
	"github.com/paroxity/portal/server"
	"github.com/paroxity/portal/session"
	"github.com/paroxity/portal/socket/packet"
)

// TestListenAcceptsAndAuthenticates is the only test exercising the real Listen()/Accept() path (every
// other test constructs Clients directly over a net.Pipe), added alongside switching Listen() to
// net.ListenConfig for TCP keepalive -- a real accepted connection must still authenticate successfully.
func TestListenAcceptsAndAuthenticates(t *testing.T) {
	srv := NewDefaultServer("127.0.0.1:0", "secret", session.NewDefaultStore(), server.NewDefaultRegistry(), nopLogger{}, false, nil)
	if err := srv.Listen(); err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer srv.Close()

	conn, err := net.Dial("tcp", srv.listener.Addr().String())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	c := NewClient(conn, nopLogger{}, false)
	if err := c.WritePacket(&packet.AuthRequest{Protocol: packet.ProtocolVersion, Secret: "secret", Name: "backend1"}); err != nil {
		t.Fatalf("WritePacket: %v", err)
	}
	pk, err := c.ReadPacket()
	if err != nil {
		t.Fatalf("ReadPacket: %v", err)
	}
	resp, ok := pk.(*packet.AuthResponse)
	if !ok || resp.Status != packet.AuthResponseSuccess {
		t.Fatalf("got %#v, want a successful AuthResponse", pk)
	}
}

// TestTryAuthenticateCaseInsensitive guards against a regression where the socket client map (case-sensitive
// keys) and the server registry (case-insensitive keys) disagreed on identity, letting "Lobby" and "lobby"
// both authenticate as separate connections while colliding on a single registry entry.
func TestTryAuthenticateCaseInsensitive(t *testing.T) {
	srv := NewDefaultServer(":0", "secret", session.NewDefaultStore(), server.NewDefaultRegistry(), nopLogger{}, false, nil)

	conn1, _ := net.Pipe()
	c1 := NewClient(conn1, nopLogger{}, false)
	conn2, _ := net.Pipe()
	c2 := NewClient(conn2, nopLogger{}, false)

	if !srv.TryAuthenticate(c1, "Lobby") {
		t.Fatal("TryAuthenticate(\"Lobby\") should succeed for the first connection")
	}
	if srv.TryAuthenticate(c2, "lobby") {
		t.Fatal("TryAuthenticate(\"lobby\") should fail: the name is already taken, case-insensitively")
	}
	if got, ok := srv.Client("LOBBY"); !ok || got != c1 {
		t.Fatalf("Client(\"LOBBY\") = %v, %v, want c1, true", got, ok)
	}
}

// TestHandleClientAuthTimeout guards against a regression where an accepted connection that never sent
// anything (or never finished authenticating) stayed open indefinitely, and against a busy-loop if a read
// deadline error wasn't recognized as terminal.
func TestHandleClientAuthTimeout(t *testing.T) {
	old := authTimeout
	authTimeout = 50 * time.Millisecond
	defer func() { authTimeout = old }()

	srv := NewDefaultServer(":0", "secret", session.NewDefaultStore(), server.NewDefaultRegistry(), nopLogger{}, false, nil)

	serverSide, clientSide := net.Pipe()
	defer clientSide.Close()

	done := make(chan struct{})
	go func() {
		srv.handleClient(NewClient(serverSide, nopLogger{}, false))
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handleClient never returned for a connection that never authenticated within authTimeout")
	}
}

// TestHandleClientAuthResponseWriteTimeout guards against a regression where authTimeout only bounded
// reads: a client that completed the handshake read but never read the response back could block the
// server forever inside WritePacket.
func TestHandleClientAuthResponseWriteTimeout(t *testing.T) {
	oldAuth, oldWrite := authTimeout, writeTimeout
	authTimeout, writeTimeout = 50*time.Millisecond, 50*time.Millisecond
	defer func() { authTimeout, writeTimeout = oldAuth, oldWrite }()

	srv := NewDefaultServer(":0", "secret", session.NewDefaultStore(), server.NewDefaultRegistry(), nopLogger{}, false, nil)

	serverSide, clientSide := net.Pipe()
	defer clientSide.Close()

	done := make(chan struct{})
	go func() {
		srv.handleClient(NewClient(serverSide, nopLogger{}, false))
		close(done)
	}()

	clientWriter := NewClient(clientSide, nopLogger{}, false)
	if err := clientWriter.WritePacket(&packet.AuthRequest{Protocol: packet.ProtocolVersion, Secret: "secret", Name: "backend"}); err != nil {
		t.Fatalf("WritePacket: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handleClient blocked writing the auth response to a client that never read it")
	}
}

// TestHandleClientDisconnectConcurrentReconnectSameName guards against a regression where
// handleClientDisconnect looked up the registry entry to remove after releasing clientsMu: a new connection
// racing in under the same name could authenticate and register in that window, and the old connection's
// cleanup would then remove the new registration instead of its own (RemoveServer's own identity check, #16,
// doesn't help here, since the code re-derives "the current occupant of the name" by name after the window
// has already passed, rather than holding a reference to what it actually owned).
func TestHandleClientDisconnectConcurrentReconnectSameName(t *testing.T) {
	srv := NewDefaultServer(":0", "secret", session.NewDefaultStore(), server.NewDefaultRegistry(), nopLogger{}, false, nil)

	for round := 0; round < 200; round++ {
		conn1, _ := net.Pipe()
		c1 := NewClient(conn1, nopLogger{}, false)
		if !srv.TryAuthenticate(c1, "backend1") {
			t.Fatalf("round %d: TryAuthenticate(c1) failed", round)
		}
		srv.ServerRegistry().AddServer(server.New("backend1", "127.0.0.1:1", server.TransportRakNet, "", 1, false))

		conn2, _ := net.Pipe()
		c2 := NewClient(conn2, nopLogger{}, false)

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			srv.handleClientDisconnect(c1)
		}()
		go func() {
			defer wg.Done()
			if srv.TryAuthenticate(c2, "backend1") {
				srv.ServerRegistry().AddServer(server.New("backend1", "127.0.0.1:2", server.TransportRakNet, "", 1, false))
			}
		}()
		wg.Wait()

		if got, ok := srv.Client("backend1"); ok && got == c2 {
			if _, regOK := srv.ServerRegistry().Server("backend1"); !regOK {
				t.Fatalf("round %d: c2 authenticated but its server registration was removed by c1's disconnect cleanup", round)
			}
		}

		srv.handleClientDisconnect(c2)
	}
}

// TestHandleClientDisconnectDoesNotDeadlockOnSubscriber guards against a regression where
// handleClientDisconnect published TopicServerUnregistered while still holding clientsMu, deadlocking
// against a subscriber that called back into Clients()/Client() from the same goroutine that publish runs on.
func TestHandleClientDisconnectDoesNotDeadlockOnSubscriber(t *testing.T) {
	bus := event.NewBus()
	srv := NewDefaultServer(":0", "secret", session.NewDefaultStore(), server.NewDefaultRegistry(), nopLogger{}, false, bus)

	conn, _ := net.Pipe()
	c := NewClient(conn, nopLogger{}, false)
	if !srv.TryAuthenticate(c, "backend1") {
		t.Fatal("TryAuthenticate should succeed")
	}
	srv.ServerRegistry().AddServer(server.New("backend1", "127.0.0.1:1", server.TransportRakNet, "", 1, false))

	done := make(chan struct{})
	bus.Subscribe(event.TopicServerUnregistered, func(any) {
		srv.Clients()
		close(done)
	})

	go srv.handleClientDisconnect(c)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("subscriber never returned: handleClientDisconnect likely still holds clientsMu while publishing")
	}
}
