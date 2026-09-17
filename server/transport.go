package server

import (
	"fmt"
	"net/url"
)

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

// ParseNetherNetAddress validates and parses address as the signaling endpoint URL required for
// TransportNetherNet, returning the parsed URL on success. It is the single source of truth for what a
// usable NetherNet address looks like, shared by every caller that needs to either validate one (e.g. at
// RegisterServer time) or extract its port (e.g. to dial it) -- so the two can never silently disagree
// about what's acceptable.
//
// A valid address is an absolute "http://" or "https://" URL with an explicit port and no path: something
// that can be dialed directly and unambiguously reports its own port, which callers that need to embed
// that port elsewhere (see minecraft.Dialer's ClientData.ServerAddress requirements) depend on.
func ParseNetherNetAddress(address string) (*url.URL, error) {
	u, err := url.Parse(address)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("must be the full URL of the server's signaling endpoint (e.g. %q), got %q", "http://host:port", address)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("must use the \"http\" or \"https\" scheme, got %q", address)
	}
	if u.Port() == "" {
		return nil, fmt.Errorf("must include an explicit port, got %q", address)
	}
	if u.Path != "" {
		return nil, fmt.Errorf("must not have a path (not even a trailing \"/\"), got %q", address)
	}
	return u, nil
}
