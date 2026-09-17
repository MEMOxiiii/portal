package server

// Transport identifies the network transport used to reach a server, whether that is the proxy's own
// player-facing listener or a registered backend server dialed on a player's behalf.
type Transport string

const (
	// TransportRakNet is the classic UDP transport used by Bedrock before NetherNet support existed.
	TransportRakNet Transport = "raknet"
	// TransportNetherNet is Bedrock's WebRTC-based transport. A server reached over TransportNetherNet is
	// addressed by the URL of its HTTP(S) signaling endpoint (e.g. "http://host:port"), not a bare
	// "host:port" pair.
	TransportNetherNet Transport = "nethernet"
)
