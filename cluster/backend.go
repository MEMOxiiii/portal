// Package cluster provides a pluggable cross-proxy player presence backend, allowing multiple Portal
// instances to share a view of which proxy and server each player is currently connected to.
package cluster

// Backend is a pluggable cross-proxy presence backend.
type Backend interface {
	// Announce records that the named player is online on the given proxy, connected to the given server.
	// It is also used to refresh an existing record (e.g. after a transfer, or as a periodic heartbeat).
	Announce(proxyID, playerName, serverName string) error
	// Remove removes the presence record for the named player, but only if it still belongs to proxyID, so
	// a delayed Remove can't clobber a newer Announce made by a different proxy the player reconnected to.
	Remove(proxyID, playerName string) error
	// Lookup returns the proxy and server a player is currently connected to across the cluster, and
	// whether they were found.
	Lookup(playerName string) (proxyID, serverName string, online bool, err error)
	// Close releases any resources held by the backend.
	Close() error
}
