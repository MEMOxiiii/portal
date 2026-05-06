package session

import (
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/go-gl/mathgl/mgl32"
	"github.com/google/uuid"
	"github.com/paroxity/portal/event"
	"github.com/paroxity/portal/internal"
	"github.com/paroxity/portal/server"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	"github.com/scylladb/go-set/b16set"
	"github.com/scylladb/go-set/i32set"
	"github.com/scylladb/go-set/i64set"
	"github.com/scylladb/go-set/strset"
	"go.uber.org/atomic"
)

// Session stores the data for an active session on the proxy.
type Session struct {
	*translator

	log   internal.Logger
	conn  *minecraft.Conn
	store *Store

	hMutex sync.RWMutex
	// h holds the current handler of the session.
	h Handler

	loginMu        sync.RWMutex
	serverMu       sync.RWMutex
	server         *server.Server
	serverConn     *minecraft.Conn
	tempServerConn *minecraft.Conn

	entities    *i64set.Set
	playerList  *b16set.Set
	effects     *i32set.Set
	bossBars    *i64set.Set
	scoreboards *strset.Set

	uuid uuid.UUID

	transferring atomic.Bool
	postTransfer atomic.Bool
	dead         atomic.Bool
	once         sync.Once
}

// New creates a new Session with the provided connection.
func New(conn *minecraft.Conn, store *Store, loadBalancer LoadBalancer, log internal.Logger) (s *Session, err error) {
	s = &Session{
		log:   log,
		conn:  conn,
		store: store,

		entities:    i64set.New(),
		playerList:  b16set.New(),
		effects:     i32set.New(),
		bossBars:    i64set.New(),
		scoreboards: strset.New(),

		h:    NopHandler{},
		uuid: uuid.MustParse(conn.IdentityData().Identity),
	}

	store.Store(s)
	defer func() {
		if err != nil {
			store.Delete(s.UUID())
		}
	}()

	srv := loadBalancer.FindServer(s)
	if srv == nil {
		return s, errors.New("load balancer did not return a server for the player to join")
	}
	srv.IncrementPlayerCount()
	s.server = srv

	s.loginMu.Lock()
	go func() {
		defer s.loginMu.Unlock()
		srvConn, err := s.dial(srv)
		if err != nil {
			log.Errorf("failed to dial server %s: %w", srv.Address(), err)
			return
		}

		s.serverConn = srvConn
		if err = s.login(); err != nil {
			_ = srvConn.Close()
			log.Errorf("failed to login to server %s: %w", srv.Address(), err)
			return
		}
		log.Infof("%s has been connected to server %s", conn.IdentityData().DisplayName, srv.Name())

		s.translator = newTranslator(s.currentServerGameData())
		handlePackets(s)
	}()
	return s, nil
}

// dial dials a new connection to the provided server. It then returns the connection between the proxy and
// that server, along with any error that may have occurred.
func (s *Session) dial(srv *server.Server) (*minecraft.Conn, error) {
	i := s.conn.IdentityData()
	c := s.conn.ClientData()
	c.PlatformOnlineID = ""
	c.PlatformOfflineID = ""
	c.PlayFabID = ""
	c.ThirdPartyName = i.DisplayName
	c.GameVersion = internal.BedrockGameVersion

	// For legacy auth servers (e.g. PocketMine), clear XUID as it is embedded in the Xbox JWT chain.
	// For non-legacy auth servers (e.g. GeyserMC), keep the real XUID so each player has a unique
	// identifier. GeyserMC uses XUID to detect duplicate sessions — clearing it causes all proxy
	// connections to collide with "already logged in".
	if srv.LegacyAuth() {
		i.XUID = ""
	}
	return minecraft.Dialer{
		ClientData:          c,
		IdentityData:        i,
		EnableLegacyAuth:    srv.LegacyAuth(),
		KeepXBLIdentityData: !srv.LegacyAuth(),
	}.Dial("raknet", srv.Address())
}

// login performs the initial login sequence for the session.
func (s *Session) login() (err error) {
	gameData := s.currentServerGameData()
	var g sync.WaitGroup
	g.Add(2)
	go func() {
		err = s.conn.StartGameTimeout(gameData, time.Minute)
		g.Done()
	}()
	go func() {
		err = s.serverConn.DoSpawnTimeout(time.Minute)
		g.Done()
	}()
	g.Wait()
	return
}

func (s *Session) currentServerGameData() minecraft.GameData {
	data := s.serverConn.GameData()
	data.BaseGameVersion = internal.BedrockGameVersion
	return data
}

// waitForLogin uses the login mutex to wait for the login to complete. If the player is still logging in, loginMu will
// be locked causing this method to block until the login is complete.
func (s *Session) waitForLogin() {
	s.loginMu.RLock()
	s.loginMu.RUnlock()
}

// Conn returns the active connection for the session.
func (s *Session) Conn() *minecraft.Conn {
	s.waitForLogin()
	return s.conn
}

// Server returns the server the session is currently connected to.
func (s *Session) Server() *server.Server {
	s.waitForLogin()
	s.serverMu.RLock()
	defer s.serverMu.RUnlock()
	return s.server
}

// ServerConn returns the connection for the session's current server.
func (s *Session) ServerConn() *minecraft.Conn {
	s.waitForLogin()
	s.serverMu.RLock()
	defer s.serverMu.RUnlock()
	return s.serverConn
}

// UUID returns the UUID from the session's connection.
func (s *Session) UUID() uuid.UUID {
	return s.uuid
}

// Handle sets the handler for the current session which can be used to handle different events from the
// session. If the handler is nil, a NopHandler is used instead.
func (s *Session) Handle(h Handler) {
	s.hMutex.Lock()
	defer s.hMutex.Unlock()

	if h == nil {
		h = NopHandler{}
	}
	s.h = h
}

// Transfer transfers the session to the provided server, returning any error that may have occurred during
// the initial transfer.
func (s *Session) Transfer(srv *server.Server) (err error) {
	s.waitForLogin()
	if !s.transferring.CAS(false, true) {
		return errors.New("already being transferred")
	}

	s.log.Infof("%s is being transferred from %s to %s", s.conn.IdentityData().DisplayName, s.Server().Name(), srv.Name())

	ctx := event.C()
	s.handler().HandleTransfer(ctx, srv)

	ctx.Continue(func() {
		// If the player is dead, force-respawn them before transferring.
		// Without this, the dimension trick fails and the player gets stuck.
		if s.dead.Load() {
			_ = s.conn.WritePacket(&packet.Respawn{
				Position:        s.conn.GameData().PlayerPosition,
				State:           packet.RespawnStateReadyToSpawn,
				EntityRuntimeID: s.originalRuntimeID,
			})
			s.dead.Store(false)
		}

		// Notify the target server to disconnect any stale session for this player.
		// This prevents the "already logged in" error when the old Raknet session
		// hasn't been cleaned up yet on servers like GeyserMC/Spigot.
		if s.store.PreTransfer != nil {
			s.store.PreTransfer(srv.Name(), s.conn.IdentityData().DisplayName)
			time.Sleep(1 * time.Second)
		}

		conn, err := s.dial(srv)
		if err != nil {
			// If the server still thinks the player is logged in, retry once after a longer delay
			// to allow the Spigot server to fully clean up the kicked session.
			if strings.Contains(err.Error(), "already logged in") {
				s.log.Debugf("retrying transfer to %s for %s after 'already logged in' error", srv.Name(), s.conn.IdentityData().DisplayName)
				time.Sleep(3 * time.Second)
				conn, err = s.dial(srv)
			}
			if err != nil {
				s.log.Errorf("transfer failed: could not dial %s: %v", srv.Name(), err)
				s.setTransferring(false)
				return
			}
		}
		if err = conn.DoSpawnTimeout(time.Minute); err != nil {
			_ = conn.Close()
			s.log.Errorf("transfer failed: spawn timeout on %s: %v", srv.Name(), err)
			s.setTransferring(false)
			return
		}

		// Force-respawn on the new server in case the player was dead from a previous session.
		// Without this, GeyserMC/Spigot may keep the player in a dead state, and the queued
		// Respawn{SearchingForSpawn} packet will interfere with the dimension trick.
		_ = conn.WritePacket(&packet.Respawn{
			Position:        conn.GameData().PlayerPosition,
			State:           packet.RespawnStateClientReadyToSpawn,
			EntityRuntimeID: conn.GameData().EntityRuntimeID,
		})

		s.serverMu.Lock()
		s.tempServerConn = conn
		s.serverMu.Unlock()

		var proxyDimension int32
		for _, dimension := range []int32{packet.DimensionOverworld, packet.DimensionNether, packet.DimensionEnd} {
			if dimension != s.serverConn.GameData().Dimension && dimension != conn.GameData().Dimension {
				proxyDimension = dimension
				break
			}
		}

		pos := s.conn.GameData().PlayerPosition
		s.changeDimension(proxyDimension, pos)

		chunkX := int32(pos.X()) >> 4
		chunkZ := int32(pos.Z()) >> 4
		for x := int32(-2); x <= 2; x++ {
			for z := int32(-2); z <= 2; z++ {
				_ = s.conn.WritePacket(&packet.LevelChunk{
					Position:      protocol.ChunkPos{chunkX + x, chunkZ + z},
					Dimension:     packet.DimensionNether,
					SubChunkCount: 1,
					RawPayload:    emptyChunk(proxyDimension),
				})
			}
		}

		s.serverMu.Lock()
		s.server.DecrementPlayerCount()
		s.server = srv
		s.server.IncrementPlayerCount()
		s.serverMu.Unlock()
	})

	ctx.Stop(func() {
		s.setTransferring(false)
	})

	return
}

// Transferring returns if the session is currently transferring to a different server or not.
func (s *Session) Transferring() bool {
	return s.transferring.Load()
}

// setTransferring sets if the session is transferring to a different server.
func (s *Session) setTransferring(v bool) {
	s.transferring.Store(v)
}

// handler() returns the handler connected to the session.
func (s *Session) handler() Handler {
	s.hMutex.RLock()
	defer s.hMutex.RUnlock()
	return s.h
}

// Close closes the session and any linked connections/counters.
func (s *Session) Close() {
	s.once.Do(func() {
		s.handler().HandleQuit()
		s.Handle(NopHandler{})

		s.store.Delete(s.UUID())

		_ = s.conn.Close()
		if s.serverConn != nil {
			_ = s.serverConn.Close()
		}
		if s.tempServerConn != nil {
			_ = s.tempServerConn.Close()
		}

		if s.server != nil {
			s.server.DecrementPlayerCount()
		}
	})
}

// Disconnect disconnects the session from the proxy and shows them the provided message. If the message is empty, the
// player will be immediately sent to the server list instead of seeing the disconnect screen.
func (s *Session) Disconnect(message string) {
	_ = s.conn.WritePacket(&packet.Disconnect{
		HideDisconnectionScreen: message == "",
		Message:                 message,
	})
	s.Close()
}

// clearEntities flushes the entities map and despawns the entities for the client.
func (s *Session) clearEntities() {
	s.entities.Each(func(id int64) bool {
		_ = s.conn.WritePacket(&packet.RemoveActor{EntityUniqueID: id})
		return true
	})

	s.entities.Clear()
}

// clearPlayerList flushes the playerList map and removes all the entries for the client.
func (s *Session) clearPlayerList() {
	var entries = make([]protocol.PlayerListEntry, s.playerList.Size())
	s.playerList.Each(func(uid [16]byte) bool {
		entries = append(entries, protocol.PlayerListEntry{UUID: uid})
		return true
	})

	_ = s.conn.WritePacket(&packet.PlayerList{ActionType: packet.PlayerListActionRemove, Entries: entries})

	s.playerList.Clear()
}

// clearEffects flushes the effects map and removes all the effects for the client.
func (s *Session) clearEffects() {
	s.effects.Each(func(i int32) bool {
		_ = s.conn.WritePacket(&packet.MobEffect{
			EntityRuntimeID: s.originalRuntimeID,
			Operation:       packet.MobEffectRemove,
			EffectType:      i,
		})
		return true
	})

	s.effects.Clear()
}

// clearBossBars clears all the boss bars currently visible the client.
func (s *Session) clearBossBars() {
	s.bossBars.Each(func(b int64) bool {
		_ = s.conn.WritePacket(&packet.BossEvent{
			BossEntityUniqueID: b,
			EventType:          packet.BossEventHide,
		})
		return true
	})

	s.bossBars.Clear()
}

// clearScoreboard clears the current scoreboard visible by the client.
func (s *Session) clearScoreboard() {
	s.scoreboards.Each(func(sb string) bool {
		_ = s.conn.WritePacket(&packet.RemoveObjective{ObjectiveName: sb})
		return true
	})

	s.scoreboards.Clear()
}

func (s *Session) changeDimension(dimension int32, pos mgl32.Vec3) {
	_ = s.conn.WritePacket(&packet.ChangeDimension{
		Dimension: dimension,
		Position:  pos,
	})
	_ = s.conn.WritePacket(&packet.StopSound{StopAll: true})
	_ = s.conn.WritePacket(&packet.PlayerAction{ActionType: protocol.PlayerActionDimensionChangeDone})
}
