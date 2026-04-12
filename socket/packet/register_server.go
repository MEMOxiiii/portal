package packet

import "github.com/sandertv/gophertunnel/minecraft/protocol"

// RegisterServer is sent by a connection to register itself as a server with the provided address.
type RegisterServer struct {
	// Address is the address of the server in the format ip:port.
	Address string
	// LegacyAuth indicates whether the proxy should use legacy authentication when dialing this server.
	// PocketMine servers require legacy auth (true), while GeyserMC servers require new auth (false).
	LegacyAuth bool
}

// ID ...
func (pk *RegisterServer) ID() uint16 {
	return IDRegisterServer
}

// Marshal ...
func (pk *RegisterServer) Marshal(w *protocol.Writer) {
	w.String(&pk.Address)
	w.Bool(&pk.LegacyAuth)
}

// Unmarshal ...
func (pk *RegisterServer) Unmarshal(r *protocol.Reader) {
	r.String(&pk.Address)
	r.Bool(&pk.LegacyAuth)
}
