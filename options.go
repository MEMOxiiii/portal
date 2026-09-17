package portal

import (
	"github.com/paroxity/portal/internal"
	"github.com/paroxity/portal/session"
	"github.com/sandertv/gophertunnel/minecraft"
)

// Options represents the options that control how the proxy should be set up. After the proxy has been
// instantiated, the options below are immutable unless instantiated again.
type Options struct {
	// Logger represents the logger that will be used for the lifetime of the proxy.
	Logger internal.Logger

	// Address is the address that the proxy should run on. It should be in the format of "address:port".
	Address string
	// ListenConfig contains settings that can be changed for the listener. It can be used to change the MOTD
	// and add resource packs etc.
	ListenConfig minecraft.ListenConfig

	// Transport selects the network transport used for the player-facing listener. If left empty, the
	// official NetherNet transport (TransportNetherNet) is used. Set it to TransportRakNet to fall back to
	// the legacy UDP transport instead.
	Transport Transport
	// NetherNet holds settings specific to the NetherNet transport. It is only used when Transport is
	// TransportNetherNet.
	NetherNet NetherNetOptions

	// LoadBalancer is the method used to balance load across the servers on the proxy. It can be used to
	// change which servers players connect to when they join the proxy.
	LoadBalancer session.LoadBalancer

	// Whitelist is used to limit the proxy to only allow certain players to join.
	Whitelist session.Whitelist

	// IPGuard is used to reject connections from banned or abusive IP addresses before they reach the
	// whitelist or game-layer authentication.
	IPGuard session.IPGuard
}
