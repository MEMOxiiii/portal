package server

import "testing"

// TestRegistryRemoveServerStale guards against a regression where RemoveServer deleted by name alone: a
// fast re-register under the same name (e.g. a backend reconnecting) could replace the entry before an old
// caller's RemoveServer(oldSrv) ran, and that must not remove the replacement.
func TestRegistryRemoveServerStale(t *testing.T) {
	reg := NewDefaultRegistry()

	old := New("lobby", "127.0.0.1:1", TransportRakNet, "", 1, false)
	reg.AddServer(old)

	replacement := New("lobby", "127.0.0.1:2", TransportRakNet, "", 1, false)
	reg.AddServer(replacement)

	reg.RemoveServer(old)

	got, ok := reg.Server("lobby")
	if !ok || got != replacement {
		t.Fatalf("RemoveServer(old) removed the replacement; Server(\"lobby\") = %v, %v, want replacement, true", got, ok)
	}
}
