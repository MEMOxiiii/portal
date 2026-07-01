package event

import "github.com/google/uuid"

// Proxy-wide event bus topics. See Bus.
const (
	// TopicPlayerJoin is published, with a PlayerPayload, once a player has fully logged into the proxy and
	// their first backend server.
	TopicPlayerJoin = "player_join"
	// TopicPlayerQuit is published, with a PlayerPayload, when a session is closed for any reason.
	TopicPlayerQuit = "player_quit"
	// TopicServerRegistered is published, with a ServerPayload, when a backend server registers itself over
	// the socket protocol.
	TopicServerRegistered = "server_registered"
	// TopicServerUnregistered is published, with a ServerPayload, when a backend server's socket connection
	// is closed and it is removed from the registry.
	TopicServerUnregistered = "server_unregistered"
	// TopicTransfer is published, with a TransferPayload, when a player transfer between servers completes,
	// successfully or not.
	TopicTransfer = "transfer"
	// TopicServerHealthChanged is published, with a ServerHealthPayload, when a server's health check
	// status flips between healthy and unhealthy.
	TopicServerHealthChanged = "server_health_changed"
)

// PlayerPayload is published for TopicPlayerJoin and TopicPlayerQuit.
type PlayerPayload struct {
	// UUID is the UUID of the player.
	UUID uuid.UUID
	// Name is the display name of the player.
	Name string
}

// ServerPayload is published for TopicServerRegistered and TopicServerUnregistered.
type ServerPayload struct {
	// Name is the name the server registered itself with.
	Name string
	// Address is the address of the server.
	Address string
}

// TransferPayload is published for TopicTransfer.
type TransferPayload struct {
	// PlayerName is the display name of the player that was transferred.
	PlayerName string
	// FromServer is the name of the server the player was transferred from.
	FromServer string
	// ToServer is the name of the server the player was being transferred to.
	ToServer string
	// Err is the error that caused the transfer to fail, or nil if it succeeded.
	Err error
}

// ServerHealthPayload is published for TopicServerHealthChanged.
type ServerHealthPayload struct {
	// Name is the name the server registered itself with.
	Name string
	// Address is the address of the server.
	Address string
	// Healthy is the server's new health status.
	Healthy bool
}
