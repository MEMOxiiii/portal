package socket

import (
	"github.com/paroxity/portal/socket/packet"
)

// SetServerDrainingHandler is responsible for handling the SetServerDraining packet sent by servers.
type SetServerDrainingHandler struct{ requireAuth }

// Handle ...
func (*SetServerDrainingHandler) Handle(p packet.Packet, srv Server, c *Client) error {
	pk := p.(*packet.SetServerDraining)

	s, ok := srv.ServerRegistry().Server(c.Name())
	if !ok {
		return nil
	}

	s.SetDraining(pk.Draining)
	srv.Logger().Debugf("server \"%s\" set draining=%v", c.Name(), pk.Draining)
	return nil
}
