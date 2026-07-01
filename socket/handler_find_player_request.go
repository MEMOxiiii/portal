package socket

import (
	"github.com/paroxity/portal/socket/packet"
)

// FindPlayerRequestHandler is responsible for handling the FindPlayerRequest packet sent by servers.
type FindPlayerRequestHandler struct{ requireAuth }

// Handle ...
func (*FindPlayerRequestHandler) Handle(p packet.Packet, srv Server, c *Client) error {
	pk := p.(*packet.FindPlayerRequest)
	s, ok := srv.SessionStore().Load(pk.PlayerUUID)
	if !ok {
		s, ok = srv.SessionStore().LoadFromName(pk.PlayerName)
	}
	if ok {
		return c.WritePacket(&packet.FindPlayerResponse{
			PlayerUUID: s.UUID(),
			PlayerName: s.Conn().IdentityData().DisplayName,
			Online:     true,
			Server:     s.Server().Name(),
		})
	}

	if cl := srv.Cluster(); cl != nil && pk.PlayerName != "" {
		if proxyID, serverName, online, err := cl.Lookup(pk.PlayerName); err != nil {
			srv.Logger().Errorf("cluster lookup failed for %q: %v", pk.PlayerName, err)
		} else if online {
			return c.WritePacket(&packet.FindPlayerResponse{
				PlayerUUID: pk.PlayerUUID,
				PlayerName: pk.PlayerName,
				Online:     true,
				Server:     serverName,
				Proxy:      proxyID,
			})
		}
	}

	return c.WritePacket(&packet.FindPlayerResponse{
		PlayerUUID: pk.PlayerUUID,
		PlayerName: pk.PlayerName,
		Online:     false,
	})
}
