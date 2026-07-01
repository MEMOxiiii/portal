package packet

import "github.com/sandertv/gophertunnel/minecraft/protocol"

// RegisterServer is sent by a connection to register itself as a server with the provided address.
type RegisterServer struct {
	// Address is the address of the server in the format ip:port.
	Address string
	// LegacyAuth indicates whether the proxy should use legacy authentication when dialing this server.
	// PocketMine servers require legacy auth (true), while GeyserMC servers require new auth (false).
	LegacyAuth bool
	// Group is the name of the group the server belongs to, used by group-aware load balancers to route
	// players to the correct set of servers. It may be left empty if the server does not belong to a group.
	Group string
	// Weight controls how large a share of new players the server should receive relative to others in the
	// same group. A weight of 0 is treated as 1, so older clients that don't set this field keep the
	// previous even-split behaviour.
	Weight uint32
}

// ID ...
func (pk *RegisterServer) ID() uint16 {
	return IDRegisterServer
}

// Marshal ...
func (pk *RegisterServer) Marshal(w *protocol.Writer) {
	w.String(&pk.Address)
	w.Bool(&pk.LegacyAuth)
	w.String(&pk.Group)
	w.Varuint32(&pk.Weight)
}

// Unmarshal ...
func (pk *RegisterServer) Unmarshal(r *protocol.Reader) {
	r.String(&pk.Address)
	r.Bool(&pk.LegacyAuth)
	r.String(&pk.Group)
	r.Varuint32(&pk.Weight)
}
