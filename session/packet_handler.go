package session

import (
	"errors"
	"net"
	"sync"

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
		for {
			pk, err := s.Conn().ReadPacket()
			if err != nil {
				if !errors.Is(err, net.ErrClosed) {
					s.log.Errorf("failed to read packet from connection: %v", err)
				}
				return
			}
			s.translatePacket(pk)

			switch pk := pk.(type) {
			case *packet.BookEdit:
				pk.XUID = ""
			case *packet.PlayerAction:
				if pk.ActionType == protocol.PlayerActionDimensionChangeDone {
					if s.transferring.Load() {
						s.serverMu.Lock()
						gameData := s.tempServerConn.GameData()
						s.changeDimension(gameData.Dimension, gameData.PlayerPosition)

						var w sync.WaitGroup
						w.Add(2)
						go func() {
							s.clearEntities()
							s.clearEffects()
							w.Done()
						}()
						go func() {
							s.clearPlayerList()
							s.clearBossBars()
							s.clearScoreboard()
							w.Done()
						}()

						_ = s.conn.WritePacket(&packet.MovePlayer{
							EntityRuntimeID: s.originalRuntimeID,
							Position:        gameData.PlayerPosition,
							Pitch:           gameData.Pitch,
							Yaw:             gameData.Yaw,
							Mode:            packet.MoveModeReset,
						})

						_ = s.conn.WritePacket(&packet.LevelEvent{EventType: packet.LevelEventStopRaining, EventData: 10000})
						_ = s.conn.WritePacket(&packet.LevelEvent{EventType: packet.LevelEventStopThunderstorm})
						_ = s.conn.WritePacket(&packet.SetDifficulty{Difficulty: uint32(gameData.Difficulty)})
						_ = s.conn.WritePacket(&packet.GameRulesChanged{GameRules: gameData.GameRules})
						_ = s.conn.WritePacket(&packet.SetPlayerGameType{GameType: gameData.PlayerGameMode})

						// Tell the client to request chunks around the new position immediately.
						_ = s.conn.WritePacket(&packet.NetworkChunkPublisherUpdate{
							Position: protocol.BlockPos{
								int32(gameData.PlayerPosition.X()),
								int32(gameData.PlayerPosition.Y()),
								int32(gameData.PlayerPosition.Z()),
							},
							Radius: uint32(gameData.ChunkRadius) << 4,
						})

						// Always clear the death screen after transfer, regardless of the proxy's
						// tracked dead state. The new server may have queued Respawn{SearchingForSpawn}
						// packets that haven't been read yet, so we preemptively tell the client
						// they are alive to prevent the death screen from appearing.
						s.dead.Store(false)
						_ = s.conn.WritePacket(&packet.Respawn{
							Position:        gameData.PlayerPosition,
							State:           packet.RespawnStateReadyToSpawn,
							EntityRuntimeID: s.originalRuntimeID,
						})

						w.Wait()

						// Send a Disconnect packet before closing so the downstream server
						// (e.g. GeyserMC → Spigot) immediately cleans up the player session
						// instead of waiting for a Raknet timeout.
						_ = s.serverConn.WritePacket(&packet.Disconnect{
							Message: "Server transfer",
						})
						_ = s.serverConn.Close()

						s.serverConn = s.tempServerConn
						s.tempServerConn = nil
						s.serverMu.Unlock()

						s.updateTranslatorData(gameData)

						s.transferring.Store(false)
						s.postTransfer.Store(true)

						s.log.Infof("%s finished transferring to %s", s.Conn().IdentityData().DisplayName, s.Server().Name())
						continue
					} else if s.postTransfer.CAS(true, false) {
						continue
					}
				}
			case *packet.Text:
				pk.XUID = ""
			}

			if s.Transferring() {
				continue
			}

			ctx := event.C()
			s.handler().HandleServerBoundPacket(ctx, pk)

			ctx.Continue(func() {
				_ = s.ServerConn().WritePacket(pk)
			})
		}
	}()

	go func() {
		for {
			conn := s.ServerConn()
			pk, err := conn.ReadPacket()
			if err != nil {
				if conn != s.ServerConn() {
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
				if pk.ActionType == packet.PlayerListActionAdd {
					for _, e := range pk.Entries {
						s.playerList.Add(e.UUID)
					}
				} else {
					for _, e := range pk.Entries {
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
				_ = s.Conn().WritePacket(pk)
			})
		}
	}()
}
