package packet

import "github.com/sandertv/gophertunnel/minecraft/protocol"

// SetServerDraining is sent by a server to tell the proxy whether load balancers should stop routing new
// players to it. Players already connected to the server are unaffected. This is typically sent before a
// planned restart or deployment so the server can finish serving its current players before going down.
type SetServerDraining struct {
	// Draining is whether the server should be marked as draining (true) or available (false).
	Draining bool
}

// ID ...
func (*SetServerDraining) ID() uint16 {
	return IDSetServerDraining
}

// Marshal ...
func (pk *SetServerDraining) Marshal(w *protocol.Writer) {
	w.Bool(&pk.Draining)
}

// Unmarshal ...
func (pk *SetServerDraining) Unmarshal(r *protocol.Reader) {
	r.Bool(&pk.Draining)
}
