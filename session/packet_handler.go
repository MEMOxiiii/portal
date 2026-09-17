package session

import (
	"errors"
	"net"

	"github.com/paroxity/portal/event"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// handlePackets handles the packets sent between the client and the server. Processes such as runtime
// translations are also handled here.
func handlePackets(s *Session) {
	go func() {
		defer s.Close()
		defer recoverPacketLoop(s, "client->server")
		for {
			pk, err := s.conn.ReadPacket()
			if err != nil {
				if !errors.Is(err, net.ErrClosed) {
					s.log.Errorf("failed to read packet from connection: %v", err)
				}
				return
			}
			s.translatePacket(pk)
			clearLegacyIdentity(pk, s.currentServer().LegacyAuth())

			switch pk := pk.(type) {
			case *packet.PlayerAction:
				if pk.ActionType == protocol.PlayerActionDimensionChangeDone {
					if s.transferring.Load() {
						gameData, ok := s.finishTransferDimensionChange()
						if ok {
							s.updateTranslatorData(gameData)

							s.transferring.Store(false)
							s.postTransfer.Store(true)

							s.log.Infof("%s finished transferring to %s", s.conn.IdentityData().DisplayName, s.currentServer().Name())
							s.completeTransfer(nil)
							continue
						}
						// tempServerConn wasn't set yet: the client sent DimensionChangeDone before the
						// transfer's dial/login to the target server finished (transferring is set well
						// before tempServerConn is). Fall through and forward it like any other packet.
					} else if s.postTransfer.CAS(true, false) {
						continue
					}
				}
			}

			if s.Transferring() {
				continue
			}

			ctx := event.C()
			s.handler().HandleServerBoundPacket(ctx, pk)

			ctx.Continue(func() {
				_ = s.currentServerConn().WritePacket(pk)
			})
		}
	}()

	go func() {
		defer s.Close()
		defer recoverPacketLoop(s, "server->client")
		for {
			conn := s.currentServerConn()
			pk, err := conn.ReadPacket()
			if err != nil {
				if conn != s.currentServerConn() {
					continue
				}
				ctx := event.C()
				s.handler().HandleServerDisconnect(ctx, err)

				c := false
				ctx.Continue(func() {
					c = true
					if disconnect, ok := errors.Unwrap(err).(minecraft.DisconnectError); ok {
						s.log.Debugf(disconnect.Error())
						_ = s.conn.WritePacket(&packet.Disconnect{Message: disconnect.Error()})
					}
					s.Close()
				})
				if c {
					return
				}
				continue
			}
			s.translatePacket(pk)

			switch pk := pk.(type) {
			case *packet.AddActor:
				s.entities.Add(pk.EntityUniqueID)
			case *packet.AddItemActor:
				s.entities.Add(pk.EntityUniqueID)
			case *packet.AddPainting:
				s.entities.Add(pk.EntityUniqueID)
			case *packet.AddPlayer:
				s.entities.Add(pk.AbilityData.EntityUniqueID)
			case *packet.BossEvent:
				if pk.EventType == packet.BossEventShow {
					s.bossBars.Add(pk.BossEntityUniqueID)
				} else if pk.EventType == packet.BossEventHide {
					s.bossBars.Remove(pk.BossEntityUniqueID)
				}
			case *packet.MobEffect:
				if pk.Operation == packet.MobEffectAdd {
					s.effects.Add(pk.EffectType)
				} else if pk.Operation == packet.MobEffectRemove {
					s.effects.Remove(pk.EffectType)
				}
			case *packet.PlayerList:
				for _, e := range pk.Entries {
					if e.ActionType == protocol.PlayerListActionAdd {
						s.playerList.Add(e.UUID)
					} else {
						s.playerList.Remove(e.UUID)
					}
				}
			case *packet.RemoveActor:
				s.entities.Remove(pk.EntityUniqueID)
			case *packet.RemoveObjective:
				s.scoreboards.Remove(pk.ObjectiveName)
			case *packet.SetDisplayObjective:
				s.scoreboards.Add(pk.ObjectiveName)
			case *packet.Respawn:
				if pk.State == packet.RespawnStateSearchingForSpawn {
					s.dead.Store(true)
					// During the post-transfer phase, the new server may have queued a
					// Respawn{SearchingForSpawn} from a previous session's death state.
					// Suppress it to prevent the death screen from blocking the second
					// dimension change, and auto-respond to respawn on the server.
					if s.postTransfer.Load() {
						_ = conn.WritePacket(&packet.Respawn{
							Position:        pk.Position,
							State:           packet.RespawnStateClientReadyToSpawn,
							EntityRuntimeID: pk.EntityRuntimeID,
						})
						continue
					}
				} else if pk.State == packet.RespawnStateReadyToSpawn {
					s.dead.Store(false)
				}
			}

			ctx := event.C()
			s.handler().HandleClientBoundPacket(ctx, pk)

			ctx.Continue(func() {
				_ = s.conn.WritePacket(pk)
			})
		}
	}()
}

// recoverPacketLoop stops a panic while marshaling/unmarshaling one packet from crashing the whole proxy
// process, closing only the affected session instead: gophertunnel's minecraft.Conn.WritePacket doesn't
// recover its own panics.
func recoverPacketLoop(s *Session, direction string) {
	if r := recover(); r != nil {
		s.log.Errorf("session %s: recovered from panic in %s packet loop: %v", s.uuid, direction, r)
	}
}

func clearLegacyIdentity(pk packet.Packet, legacyAuth bool) {
	if !legacyAuth {
		return
	}
	switch pk := pk.(type) {
	case *packet.BookEdit:
		pk.XUID = ""
	case *packet.Text:
		pk.XUID = ""
	}
}
