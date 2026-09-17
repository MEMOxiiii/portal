package socket

import (
	"net"
	"testing"

	"github.com/paroxity/portal/server"
	"github.com/paroxity/portal/session"
	"github.com/paroxity/portal/socket/packet"
)

type nopLogger struct{}

func (nopLogger) Debugf(string, ...interface{}) {}
func (nopLogger) Infof(string, ...interface{})  {}
func (nopLogger) Errorf(string, ...interface{}) {}
func (nopLogger) Fatalf(string, ...interface{}) {}

// TestAuthRequestHandlerEmptySecretRejected guards against a regression where a communication server
// constructed with an empty secret (e.g. a default, never-configured config.json) authenticated any
// client, including one that also sent an empty secret, since subtle.ConstantTimeCompare("", "") == 1.
func TestAuthRequestHandlerEmptySecretRejected(t *testing.T) {
	srv := NewDefaultServer(":0", "", session.NewDefaultStore(), server.NewDefaultRegistry(), nopLogger{}, false, nil)

	serverSide, clientSide := net.Pipe()
	defer serverSide.Close()
	defer clientSide.Close()
	c := NewClient(serverSide, nopLogger{}, false)

	h := &AuthRequestHandler{}
	done := make(chan error, 1)
	go func() {
		done <- h.Handle(&packet.AuthRequest{Protocol: packet.ProtocolVersion, Secret: "", Name: "backend"}, srv, c)
	}()

	reader := NewClient(clientSide, nopLogger{}, false)
	pk, err := reader.ReadPacket()
	if err != nil {
		t.Fatalf("ReadPacket: %v", err)
	}
	resp, ok := pk.(*packet.AuthResponse)
	if !ok {
		t.Fatalf("got %T, want *packet.AuthResponse", pk)
	}
	if resp.Status == packet.AuthResponseSuccess {
		t.Fatal("authenticated with an empty configured secret")
	}
	if c.Authenticated() {
		t.Fatal("client marked authenticated with an empty configured secret")
	}
	if err := <-done; err != nil {
		t.Fatalf("Handle: %v", err)
	}
}
