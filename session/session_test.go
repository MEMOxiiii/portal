package session

import (
	"testing"
	"time"

	"github.com/paroxity/portal/server"
)

// TestFinishTransferDimensionChangeNilTempConn guards against a regression where the client sending
// PlayerAction{DimensionChangeDone} before a transfer's dial/login to the target server finished (which can
// take up to dialTimeout+DoSpawnTimeout) dereferenced a nil tempServerConn and left serverMu locked forever,
// since the mutex was taken without a defer to release it on that path.
func TestFinishTransferDimensionChangeNilTempConn(t *testing.T) {
	s := &Session{}

	if _, ok := s.finishTransferDimensionChange(); ok {
		t.Fatalf("finishTransferDimensionChange() ok = true, want false for a nil tempServerConn")
	}

	// serverMu must have been released, not left locked by the nil-tempServerConn path.
	done := make(chan struct{})
	go func() {
		s.serverMu.Lock()
		s.serverMu.Unlock()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("serverMu left locked after finishTransferDimensionChange returned ok=false")
	}
}

// TestAbortStuckTransferNoOpWhenAlreadyResolved guards against abortStuckTransfer acting on a transfer that
// already completed (or was never started): it must leave transferring/completeTransfer untouched when
// tempServerConn is nil, rather than unconditionally tearing down state that belongs to a different transfer.
func TestAbortStuckTransferNoOpWhenAlreadyResolved(t *testing.T) {
	s := &Session{log: nopLogger{}}
	s.transferring.Store(true)

	srv := server.New("target", "127.0.0.1:1", server.TransportRakNet, "", 1, false)
	s.abortStuckTransfer(srv)

	if !s.transferring.Load() {
		t.Fatal("abortStuckTransfer reset transferring for a transfer it wasn't waiting on")
	}
}

type nopLogger struct{}

func (nopLogger) Debugf(string, ...interface{}) {}
func (nopLogger) Infof(string, ...interface{})  {}
func (nopLogger) Errorf(string, ...interface{}) {}
func (nopLogger) Fatalf(string, ...interface{}) {}
