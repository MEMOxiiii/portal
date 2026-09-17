package socket

import (
	"fmt"

	"github.com/paroxity/portal/event"
	"github.com/paroxity/portal/server"
	"github.com/paroxity/portal/socket/packet"
)

// RegisterServerHandler is responsible for handling the RegisterServer packet sent by servers.
type RegisterServerHandler struct{ requireAuth }

// Handle ...
func (*RegisterServerHandler) Handle(p packet.Packet, srv Server, c *Client) error {
	pk := p.(*packet.RegisterServer)
	transport, err := parseTransport(pk.Transport)
	if err == nil {
		err = validateAddress(transport, pk.Address)
	}
	if err != nil {
		srv.Logger().Errorf("socket connection \"%s\" sent an invalid RegisterServer packet: %v", c.Name(), err)
		return err
	}
	srv.ServerRegistry().AddServer(server.New(c.Name(), pk.Address, transport, pk.Group, pk.Weight, pk.LegacyAuth))
	srv.Logger().Debugf("socket connection \"%s\" has registered itself as a server with the address \"%s\" (transport=%s, group=%q, weight=%d, legacyAuth=%v)", c.Name(), pk.Address, transport, pk.Group, pk.Weight, pk.LegacyAuth)
	if events := srv.Events(); events != nil {
		events.Publish(event.TopicServerRegistered, event.ServerPayload{Name: c.Name(), Address: pk.Address})
	}
	return nil
}

// parseTransport validates the Transport field of a RegisterServer packet, defaulting an empty value to
// server.TransportRakNet for backwards compatibility with clients that predate the field.
func parseTransport(s string) (server.Transport, error) {
	switch server.Transport(s) {
	case "":
		return server.TransportRakNet, nil
	case server.TransportRakNet, server.TransportNetherNet:
		return server.Transport(s), nil
	default:
		return "", fmt.Errorf("unknown transport %q", s)
	}
}

// validateAddress checks that address is well-formed for transport, catching the most common
// misconfiguration up front (a "host:port" pair left over from raknet after switching a server to
// TransportNetherNet, or a NetherNet address missing its port) with a clear error instead of a confusing
// failure later, deep inside a health check or a player transfer.
func validateAddress(transport server.Transport, address string) error {
	if address == "" {
		return fmt.Errorf("address must not be empty")
	}
	if transport != server.TransportNetherNet {
		return nil
	}
	if _, err := server.ParseNetherNetAddress(address); err != nil {
		return fmt.Errorf("nethernet address %w", err)
	}
	return nil
}
