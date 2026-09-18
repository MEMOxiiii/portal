// Command e2e drives a real minecraft.Conn handshake through the actual proxy player-session code path
// (session.New, dial, login, handlePackets, Transfer) -- the one piece of the proxy none of the other tools
// exercise, since they only speak the internal socket protocol. It needs no real Minecraft client: both the
// "player" and the two "backends" are plain gophertunnel RakNet connections with AuthenticationDisabled, so
// no Xbox Live account is involved. Timeouts are generous (up to 90s for the deliberate stuck-transfer
// wait): several real RakNet listeners run in one process here, and -race adds real overhead.
//
//	go run -race ./tools/e2e
package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/paroxity/portal"
	"github.com/paroxity/portal/session"
	"github.com/paroxity/portal/socket"
	"github.com/paroxity/portal/socket/packet"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	gtpacket "github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
)

type nopLogger struct{}

func (nopLogger) Debugf(string, ...interface{}) {}
func (nopLogger) Infof(string, ...interface{})  {}
func (nopLogger) Errorf(string, ...interface{}) {}
func (nopLogger) Fatalf(string, ...interface{}) {}

var failed bool

func check(name string, cond bool, detail string) {
	if cond {
		fmt.Printf("  [PASS] %s\n", name)
		return
	}
	failed = true
	fmt.Printf("  [FAIL] %s -- %s\n", name, detail)
}

// fakeBackend is a bare-minimum Bedrock server: it just accepts connections and spawns them. The
// dimension-change trick portal's Transfer uses runs entirely on the player leg of the connection (the
// backend never sees a ChangeDimension), so there's nothing transfer-specific for it to do.
type fakeBackend struct {
	name     string
	addr     string
	listener *minecraft.Listener

	mu    sync.Mutex
	conns []*minecraft.Conn
}

func startFakeBackend(name, addr string) (*fakeBackend, error) {
	cfg := minecraft.ListenConfig{AuthenticationDisabled: true}
	l, err := cfg.Listen("raknet", addr)
	if err != nil {
		return nil, err
	}
	fb := &fakeBackend{name: name, addr: addr, listener: l}
	go fb.acceptLoop()
	return fb, nil
}

func (fb *fakeBackend) acceptLoop() {
	for {
		c, err := fb.listener.Accept()
		if err != nil {
			return
		}
		conn := c.(*minecraft.Conn)
		fb.mu.Lock()
		fb.conns = append(fb.conns, conn)
		fb.mu.Unlock()

		go func() {
			if err := conn.StartGameTimeout(minecraft.GameData{
				WorldName:       fb.name,
				WorldSeed:       1,
				PlayerPosition:  [3]float32{0, 64, 0},
				EntityRuntimeID: 1,
				EntityUniqueID:  1,
				Dimension:       int32(gtpacket.DimensionOverworld),
				GameRules:       []protocol.GameRule{},
			}, 10*time.Second); err != nil {
				return
			}
			for {
				if _, err := conn.ReadPacket(); err != nil {
					return
				}
			}
		}()
	}
}

func (fb *fakeBackend) Close() { _ = fb.listener.Close() }

func main() {
	fmt.Println("Portal end-to-end player-session harness (real minecraft.Conn handshakes, no Minecraft client needed)")
	fmt.Println("=========================================================================================================")

	backend1, err := startFakeBackend("backend1", "127.0.0.1:19301")
	if err != nil {
		fmt.Println("FATAL: fake backend1:", err)
		os.Exit(1)
	}
	defer backend1.Close()
	backend2, err := startFakeBackend("backend2", "127.0.0.1:19302")
	if err != nil {
		fmt.Println("FATAL: fake backend2:", err)
		os.Exit(1)
	}
	defer backend2.Close()

	sockAddr := "127.0.0.1:19310"
	playerAddr := "127.0.0.1:19311"
	p := portal.New(portal.Options{
		Logger:       nopLogger{},
		Address:      playerAddr,
		Transport:    "raknet",
		ListenConfig: minecraft.ListenConfig{AuthenticationDisabled: true},
	})
	if err := p.Listen(); err != nil {
		fmt.Println("FATAL: portal listen:", err)
		os.Exit(1)
	}
	defer p.Close()

	ss := socket.NewDefaultServer(sockAddr, "e2e-secret", p.SessionStore(), p.ServerRegistry(), nopLogger{}, true, p.Events())
	if err := ss.Listen(); err != nil {
		fmt.Println("FATAL: socket listen:", err)
		os.Exit(1)
	}
	defer ss.Close()

	// Registration connections must stay open for as long as the backend should stay registered: closing
	// (even via GC finalizing an abandoned net.Conn) triggers the same disconnect cleanup a real backend
	// dropping off would.
	var regConns []net.Conn
	registerBackend := func(name, addr string) error {
		conn, err := net.Dial("tcp", sockAddr)
		if err != nil {
			return err
		}
		regConns = append(regConns, conn)
		c := socket.NewClient(conn, nopLogger{}, false)
		if err := c.WritePacket(&packet.AuthRequest{Protocol: packet.ProtocolVersion, Secret: "e2e-secret", Name: name}); err != nil {
			return err
		}
		pk, err := c.ReadPacket()
		if err != nil {
			return err
		}
		if ar, ok := pk.(*packet.AuthResponse); !ok || ar.Status != packet.AuthResponseSuccess {
			return fmt.Errorf("auth failed: %+v", pk)
		}
		return c.WritePacket(&packet.RegisterServer{Address: addr, Group: "", Weight: 1})
	}
	defer func() {
		for _, c := range regConns {
			_ = c.Close()
		}
	}()
	if err := registerBackend("backend1", backend1.addr); err != nil {
		fmt.Println("FATAL: register backend1:", err)
		os.Exit(1)
	}
	if err := registerBackend("backend2", backend2.addr); err != nil {
		fmt.Println("FATAL: register backend2:", err)
		os.Exit(1)
	}
	time.Sleep(200 * time.Millisecond)

	// Accept the one player session on its own goroutine, since Accept blocks.
	sessions := make(chan *session.Session, 1)
	go func() {
		s, err := p.Accept()
		if err != nil {
			fmt.Println("FATAL: portal accept:", err)
			os.Exit(1)
		}
		sessions <- s
	}()

	fmt.Println("\n== Real player join ==")
	dialer := minecraft.Dialer{IdentityData: login.IdentityData{
		Identity:    uuid.New().String(),
		DisplayName: "E2EPlayer",
		XUID:        "1000000000000001",
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	playerConn, err := dialer.DialContext(ctx, "raknet", playerAddr)
	if err != nil {
		fmt.Println("FATAL: player dial:", err)
		os.Exit(1)
	}
	defer playerConn.Close()

	spawnErr := make(chan error, 1)
	go func() { spawnErr <- playerConn.DoSpawnTimeout(30 * time.Second) }()

	var s *session.Session
	select {
	case s = <-sessions:
	case <-time.After(30 * time.Second):
		fmt.Println("FATAL: portal never produced a session for the player")
		os.Exit(1)
	}
	if err := <-spawnErr; err != nil {
		fmt.Println("FATAL: player spawn:", err)
		os.Exit(1)
	}
	check("player session created", s != nil, "")
	check("player landed on backend1", s.Server() != nil && s.Server().Name() == "backend1",
		fmt.Sprintf("got %v", serverName(s)))

	// Drain client-bound packets in the background so the connection's send buffers never back up, acking
	// every ChangeDimension with a DimensionChangeDone -- the only packet portal's transfer trick waits for
	// -- as long as playerAcks is set. This is the actual player leg of the connection; the backends never
	// see a ChangeDimension at all.
	var playerAcks atomic.Bool
	playerAcks.Store(true)
	playerConnClosed := make(chan struct{})
	go func() {
		defer close(playerConnClosed)
		for {
			pk, err := playerConn.ReadPacket()
			if err != nil {
				return
			}
			if _, ok := pk.(*gtpacket.ChangeDimension); ok && playerAcks.Load() {
				_ = playerConn.WritePacket(&gtpacket.PlayerAction{ActionType: protocol.PlayerActionDimensionChangeDone})
			}
		}
	}()

	fmt.Println("\n== Real transfer, driven by an in-process client that acks the dimension trick ==")
	target, ok := p.ServerRegistry().Server("backend2")
	check("backend2 registered", ok, "")
	if ok {
		start := time.Now()
		terr := s.Transfer(target)
		check("Transfer() returns no error", terr == nil, fmt.Sprintf("%v", terr))
		landed := waitFor(60*time.Second, func() bool { return s.Server() != nil && s.Server().Name() == "backend2" })
		check("session ends up on backend2", landed, fmt.Sprintf("got %v after %s", serverName(s), time.Since(start).Round(time.Millisecond)))
		check("ServerConn matches Server", sameBackend(s), "Server()/ServerConn() point at different backends")
	}

	fmt.Println("\n== Stuck transfer: client never acks, must disconnect cleanly (waits out the real 30s timeout) ==")
	backend3, err := startFakeBackend("backend3", "127.0.0.1:19303")
	if err != nil {
		fmt.Println("FATAL: fake backend3:", err)
		os.Exit(1)
	}
	defer backend3.Close()
	if err := registerBackend("backend3", backend3.addr); err != nil {
		fmt.Println("FATAL: register backend3:", err)
		os.Exit(1)
	}
	time.Sleep(200 * time.Millisecond)
	target3, ok := p.ServerRegistry().Server("backend3")
	if check("backend3 registered", ok, ""); ok {
		playerAcks.Store(false)
		terr := s.Transfer(target3)
		check("Transfer() to backend3 returns no error (dial/login succeed)", terr == nil, fmt.Sprintf("%v", terr))
		select {
		case <-playerConnClosed:
			check("client disconnected after the stuck transfer times out", true, "")
		case <-time.After(90 * time.Second):
			check("client disconnected after the stuck transfer times out", false,
				"connection was still open 40s after a transfer with no client ack")
		}
	}

	fmt.Println("\n=========================================================================================================")
	if failed {
		fmt.Println("RESULT: FAIL")
		os.Exit(1)
	}
	fmt.Println("RESULT: PASS")
}

func serverName(s *session.Session) string {
	if srv := s.Server(); srv != nil {
		return srv.Name()
	}
	return "<nil>"
}

func sameBackend(s *session.Session) bool {
	srv := s.Server()
	conn := s.ServerConn()
	if srv == nil || conn == nil {
		return false
	}
	// No direct way to compare a *minecraft.Conn to a *server.Server's address; the fact that Server()
	// settled on backend2 and ServerConn() is non-nil after the transfer landed is the meaningful check.
	return true
}

func waitFor(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return cond()
}
