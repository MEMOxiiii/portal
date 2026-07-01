package packet

import (
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// FindPlayerResponse is sent by the proxy in response to PlayerInfoRequest to tell the connection the XUID
// and IP address of the requested player.
type FindPlayerResponse struct {
	// PlayerUUID is the UUID of the player that has been searched for.
	PlayerUUID uuid.UUID
	// PlayerName is the name of the player that has been searched for.
	PlayerName string
	// Online is if the player is connected to the proxy, or to another proxy in the same cluster.
	Online bool
	// Server is the server within the group the player is in, if connected.
	Server string
	// Proxy is the ID of the proxy the player is connected to. It is empty when the player is connected to
	// this proxy directly, and only set when the player was found through a cluster.Backend lookup on
	// another proxy instance.
	Proxy string
}

// ID ...
func (*FindPlayerResponse) ID() uint16 {
	return IDFindPlayerResponse
}

// Marshal ...
func (pk *FindPlayerResponse) Marshal(w *protocol.Writer) {
	w.UUID(&pk.PlayerUUID)
	w.String(&pk.PlayerName)
	w.Bool(&pk.Online)
	if pk.Online {
		w.String(&pk.Server)
		w.String(&pk.Proxy)
	}
}

// Unmarshal ...
func (pk *FindPlayerResponse) Unmarshal(r *protocol.Reader) {
	r.UUID(&pk.PlayerUUID)
	r.String(&pk.PlayerName)
	r.Bool(&pk.Online)
	if pk.Online {
		r.String(&pk.Server)
		r.String(&pk.Proxy)
	}
}
