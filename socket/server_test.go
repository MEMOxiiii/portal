package socket

import (
	"net"
	"testing"
	"time"

	"github.com/paroxity/portal/event"
	"github.com/paroxity/portal/server"
	"github.com/paroxity/portal/session"
)

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
