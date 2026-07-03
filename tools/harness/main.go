// Command harness is a throwaway, in-process integration test driver for the feature/proxy-core-upgrade
// PR (#2). It wires up the real proxy packages (socket server, load balancer, health checker, event bus,
// cluster backend, IP guard, metrics) exactly as examples/main.go does, then exercises every feature the
// PR adds and prints a PASS/FAIL report. It needs no Minecraft client: backend servers are driven through
// the proxy's own socket.Client, and the health checker is pointed at a real go-raknet listener.
//
// Run from the module root with the race detector:
//
//	go run -race ./tools/harness
//
// Optional: set HARNESS_REDIS=host:port (default 127.0.0.1:6379) for the clustering tests. If Redis is
// unreachable those tests are reported as SKIP rather than FAIL.
package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	stdlog "log"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/paroxity/portal"
	"github.com/paroxity/portal/cluster"
	"github.com/paroxity/portal/event"
	"github.com/paroxity/portal/metrics"
	"github.com/paroxity/portal/server"
	"github.com/paroxity/portal/session"
	"github.com/paroxity/portal/socket"
	"github.com/paroxity/portal/socket/packet"
	"github.com/sandertv/go-raknet"
)

// ---------------------------------------------------------------------------
// logging + result recording
// ---------------------------------------------------------------------------

type capLogger struct {
	mu    sync.Mutex
	lines []string
}

func (l *capLogger) add(level, format string, v ...interface{}) {
	s := level + " " + fmt.Sprintf(format, v...)
	l.mu.Lock()
	l.lines = append(l.lines, s)
	l.mu.Unlock()
}
func (l *capLogger) Debugf(f string, v ...interface{}) { l.add("DEBUG", f, v...) }
func (l *capLogger) Infof(f string, v ...interface{})  { l.add("INFO", f, v...) }
func (l *capLogger) Errorf(f string, v ...interface{}) { l.add("ERROR", f, v...) }
func (l *capLogger) Fatalf(f string, v ...interface{}) { l.add("FATAL", f, v...) } // do not exit in tests
func (l *capLogger) has(sub string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, ln := range l.lines {
		if strings.Contains(ln, sub) {
			return true
		}
	}
	return false
}

type result struct {
	feature string
	name    string
	status  string // PASS / FAIL / SKIP
	detail  string
}

var (
	results  []result
	resultMu sync.Mutex
)

func rec(feature, name, status, detail string) {
	resultMu.Lock()
	results = append(results, result{feature, name, status, detail})
	resultMu.Unlock()
	fmt.Printf("  [%-4s] %-28s %s\n", status, name, detail)
}

func check(feature, name string, cond bool, okDetail, failDetail string) bool {
	if cond {
		rec(feature, name, "PASS", okDetail)
	} else {
		rec(feature, name, "FAIL", failDetail)
	}
	return cond
}

// waitFor polls cond every 100ms until it returns true or the timeout elapses.
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

// runTest isolates a test so a panic in one does not abort the whole run.
func runTest(feature, title string, fn func()) {
	fmt.Printf("\n== %s ==\n", title)
	defer func() {
		if r := recover(); r != nil {
			rec(feature, title, "FAIL", fmt.Sprintf("panic: %v", r))
		}
	}()
	fn()
}

// ---------------------------------------------------------------------------
// helpers: socket clients, proxy stacks, certs
// ---------------------------------------------------------------------------

const secret = "test-secret-change-me"

var log = &capLogger{}

func dialClient(addr string, useTLS bool) (*socket.Client, net.Conn, error) {
	var conn net.Conn
	var err error
	if useTLS {
		conn, err = tls.Dial("tcp", addr, &tls.Config{InsecureSkipVerify: true})
	} else {
		conn, err = net.Dial("tcp", addr)
	}
	if err != nil {
		return nil, nil, err
	}
	return socket.NewClient(conn, log, false), conn, nil
}

// authClient sends an AuthRequest and reads the AuthResponse, with a read deadline so a hung/closed
// connection can't block the harness.
func authClient(c *socket.Client, conn net.Conn, sec, name string) (byte, error) {
	if err := c.WritePacket(&packet.AuthRequest{Protocol: packet.ProtocolVersion, Secret: sec, Name: name}); err != nil {
		return 0, err
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	pk, err := c.ReadPacket()
	if err != nil {
		return 0, err
	}
	ar, ok := pk.(*packet.AuthResponse)
	if !ok {
		return 0, fmt.Errorf("expected AuthResponse, got %T", pk)
	}
	return ar.Status, nil
}

// stack bundles a proxy + socket server sharing one registry/store/bus.
type stack struct {
	p   *portal.Portal
	ss  *socket.DefaultServer
	reg *server.Registry
	st  *session.Store
	bus *event.Bus
}

func newStack(commAddr string, tlsCfg *tls.Config) (*stack, error) {
	p := portal.New(portal.Options{Logger: log})
	var ss *socket.DefaultServer
	if tlsCfg != nil {
		ss = socket.NewDefaultTLSServer(commAddr, secret, p.SessionStore(), p.ServerRegistry(), log, true, tlsCfg, p.Events())
	} else {
		ss = socket.NewDefaultServer(commAddr, secret, p.SessionStore(), p.ServerRegistry(), log, true, p.Events())
	}
	if err := ss.Listen(); err != nil {
		return nil, err
	}
	return &stack{p: p, ss: ss, reg: p.ServerRegistry(), st: p.SessionStore(), bus: p.Events()}, nil
}

// registerBackend connects a socket client, authenticates, and registers a server. It returns the still-open
// client so callers can send further packets (e.g. SetServerDraining) or close it.
func registerBackend(addr, name, group string, weight uint32) (*socket.Client, net.Conn, error) {
	c, conn, err := dialClient(addr, false)
	if err != nil {
		return nil, nil, err
	}
	status, err := authClient(c, conn, secret, name)
	if err != nil {
		return nil, nil, err
	}
	if status != packet.AuthResponseSuccess {
		return nil, nil, fmt.Errorf("auth status %d", status)
	}
	_ = conn.SetReadDeadline(time.Time{}) // clear deadline for the persistent client
	if err := c.WritePacket(&packet.RegisterServer{Address: "127.0.0.1:1", Group: group, Weight: weight}); err != nil {
		return nil, nil, err
	}
	return c, conn, nil
}

func writeSelfSignedCert(dir string) (certFile, keyFile string, err error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		return "", "", err
	}
	certFile = filepath.Join(dir, "cert.pem")
	keyFile = filepath.Join(dir, "key.pem")
	cf, err := os.Create(certFile)
	if err != nil {
		return "", "", err
	}
	_ = pem.Encode(cf, &pem.Block{Type: "CERTIFICATE", Bytes: der})
	_ = cf.Close()
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return "", "", err
	}
	kf, err := os.Create(keyFile)
	if err != nil {
		return "", "", err
	}
	_ = pem.Encode(kf, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	_ = kf.Close()
	return certFile, keyFile, nil
}

// ---------------------------------------------------------------------------
// tests
// ---------------------------------------------------------------------------

func testTLS(dir string) {
	certFile, keyFile, err := writeSelfSignedCert(dir)
	if err != nil {
		rec("1-tls", "setup cert", "FAIL", err.Error())
		return
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		rec("1-tls", "load cert", "FAIL", err.Error())
		return
	}
	addr := "127.0.0.1:19182"
	// Mirror examples/main.go exactly: no explicit MinVersion, relying on Go's secure default (TLS 1.2).
	_, err = newStack(addr, &tls.Config{Certificates: []tls.Certificate{cert}})
	if err != nil {
		rec("1-tls", "start TLS socket", "FAIL", err.Error())
		return
	}
	time.Sleep(150 * time.Millisecond)

	// Plaintext client against a TLS listener must not be able to authenticate.
	c, conn, err := dialClient(addr, false)
	if err == nil {
		status, aerr := authClient(c, conn, secret, "plain")
		check("1-tls", "plaintext rejected", aerr != nil || status != packet.AuthResponseSuccess,
			"plaintext handshake failed as expected", fmt.Sprintf("plaintext unexpectedly authed status=%d", status))
		_ = conn.Close()
	} else {
		check("1-tls", "plaintext rejected", true, "plaintext dial failed as expected", "")
	}

	// TLS client authenticates successfully.
	c2, conn2, err := dialClient(addr, true)
	if err != nil {
		rec("1-tls", "tls authenticates", "FAIL", "tls dial: "+err.Error())
	} else {
		status, aerr := authClient(c2, conn2, secret, "tlsclient")
		check("1-tls", "tls authenticates", aerr == nil && status == packet.AuthResponseSuccess,
			"TLS client authenticated (status=0)", fmt.Sprintf("err=%v status=%d", aerr, status))
		_ = conn2.Close()
	}

	// Old TLS version must be refused (Go server default MinVersion is TLS 1.2).
	_, err = tls.Dial("tcp", addr, &tls.Config{InsecureSkipVerify: true, MaxVersion: tls.VersionTLS11})
	check("1-tls", "old TLS refused", err != nil,
		"TLS 1.1 handshake refused as expected", "TLS 1.1 unexpectedly accepted")
}

func testAuthThrottle() {
	addr := "127.0.0.1:19183"
	s, err := newStack(addr, nil)
	if err != nil {
		rec("2-throttle", "start socket", "FAIL", err.Error())
		return
	}
	time.Sleep(100 * time.Millisecond)

	var blocked bool
	statuses := make([]int, 0, 6)
	for i := 1; i <= 6; i++ {
		c, conn, derr := dialClient(addr, false)
		if derr != nil {
			// A blocked IP may have its connection refused/closed at dial+read time.
			blocked = true
			statuses = append(statuses, -1)
			continue
		}
		status, aerr := authClient(c, conn, "wrong-secret", fmt.Sprintf("bad%d", i))
		if aerr != nil {
			blocked = true
			statuses = append(statuses, -1)
		} else {
			statuses = append(statuses, int(status))
		}
		_ = conn.Close()
	}

	// Attempts 1..5 should each get IncorrectSecret; the 6th must be blocked (no auth round-trip).
	firstFive := true
	for i := 0; i < 5; i++ {
		if statuses[i] != int(packet.AuthResponseIncorrectSecret) {
			firstFive = false
		}
	}
	check("2-throttle", "5 bad secrets rejected", firstFive,
		"attempts 1-5 returned IncorrectSecret", fmt.Sprintf("statuses=%v", statuses))
	check("2-throttle", "6th attempt blocked", statuses[5] == -1,
		"6th attempt blocked before auth round-trip", fmt.Sprintf("6th status=%d", statuses[5]))
	check("2-throttle", "block logged", log.has("too many failed authentication attempts"),
		"proxy logged the block", "no block log line found")
	_ = blocked
	// No authenticated clients should have leaked.
	check("2-throttle", "no leaked clients", len(s.ss.Clients()) == 0,
		"socket client table clean after blocked attempts", fmt.Sprintf("clients=%d", len(s.ss.Clients())))
}

func testGroupsDrainingAdminMetrics() {
	addr := "127.0.0.1:19184"
	s, err := newStack(addr, nil)
	if err != nil {
		rec("3-groups", "start socket", "FAIL", err.Error())
		return
	}
	time.Sleep(100 * time.Millisecond)

	// Subscribe to registration events to also cover part of the event bus feature.
	var regEvents int
	var regMu sync.Mutex
	s.bus.Subscribe(event.TopicServerRegistered, func(any) { regMu.Lock(); regEvents++; regMu.Unlock() })

	c1, conn1, err := registerBackend(addr, "Lobby1", "lobby", 1)
	if err != nil {
		rec("3-groups", "register Lobby1", "FAIL", err.Error())
		return
	}
	defer conn1.Close()
	c2, conn2, err := registerBackend(addr, "Lobby2", "lobby", 3)
	if err != nil {
		rec("3-groups", "register Lobby2", "FAIL", err.Error())
		return
	}
	defer conn2.Close()
	time.Sleep(200 * time.Millisecond)

	// Registry reflects group + weight.
	l1, ok1 := s.reg.Server("Lobby1")
	l2, ok2 := s.reg.Server("Lobby2")
	check("3-groups", "both registered", ok1 && ok2 && len(s.reg.Servers()) == 2,
		"2 servers registered", fmt.Sprintf("ok1=%v ok2=%v count=%d", ok1, ok2, len(s.reg.Servers())))
	if ok1 && ok2 {
		check("3-groups", "group+weight stored", l1.Group() == "lobby" && l1.Weight() == 1 && l2.Group() == "lobby" && l2.Weight() == 3,
			"Lobby1(group=lobby,w=1) Lobby2(group=lobby,w=3)",
			fmt.Sprintf("l1(%q,%d) l2(%q,%d)", l1.Group(), l1.Weight(), l2.Group(), l2.Weight()))
	}

	// Admin console reflects the same, before draining.
	out := s.p.HandleAdminCommand("servers")
	check("6-admin", "servers command", strings.Contains(out, "group=\"lobby\"") && strings.Contains(out, "draining=false"),
		"admin 'servers' shows group + draining=false", "admin output: "+strings.ReplaceAll(out, "\n", " | "))

	// Drain Lobby2 via SetServerDraining packet.
	if err := c2.WritePacket(&packet.SetServerDraining{Draining: true}); err != nil {
		rec("3-groups", "send drain", "FAIL", err.Error())
	}
	time.Sleep(200 * time.Millisecond)
	check("3-groups", "draining applied", l2.Draining() && !l1.Draining(),
		"Lobby2 draining=true, Lobby1 unaffected", fmt.Sprintf("l1.drain=%v l2.drain=%v", l1.Draining(), l2.Draining()))
	out2 := s.p.HandleAdminCommand("servers")
	check("3-groups", "admin shows draining", strings.Contains(out2, "Lobby2") && strings.Contains(out2, "draining=true"),
		"admin 'servers' now shows draining=true", "admin output: "+strings.ReplaceAll(out2, "\n", " | "))
	_ = c1

	// Registration events fired (event bus).
	regMu.Lock()
	got := regEvents
	regMu.Unlock()
	check("5-events", "server_registered fired", got == 2,
		"event bus delivered 2 server_registered events", fmt.Sprintf("got=%d", got))

	// --- metrics endpoint bound to this live stack ---
	metrics.Default.RegisterGauge("portal_players_online", func() float64 { return float64(len(s.st.All())) })
	metrics.Default.RegisterGauge("portal_servers_registered", func() float64 { return float64(len(s.reg.Servers())) })
	metrics.Default.RegisterGauge("portal_socket_clients_connected", func() float64 { return float64(len(s.ss.Clients())) })
	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.Default.Handler())
	ml, err := net.Listen("tcp", "127.0.0.1:19191")
	if err != nil {
		rec("4-metrics", "listen", "FAIL", err.Error())
	} else {
		go http.Serve(ml, mux)
		time.Sleep(100 * time.Millisecond)
		resp, gerr := http.Get("http://127.0.0.1:19191/metrics")
		if gerr != nil {
			rec("4-metrics", "scrape", "FAIL", gerr.Error())
		} else {
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			b := string(body)
			check("4-metrics", "servers_registered gauge", strings.Contains(b, "portal_servers_registered 2"),
				"portal_servers_registered 2", "not found in body")
			check("4-metrics", "players+clients gauges", strings.Contains(b, "portal_players_online") && strings.Contains(b, "portal_socket_clients_connected 2"),
				"players_online + socket_clients_connected 2 present", "gauges missing")
			check("4-metrics", "prometheus format", strings.Contains(b, "# TYPE portal_transfers_total counter"),
				"transfer counters exposed in Prometheus format", "counter block missing")
		}
	}
}

func testEventBusPanic() {
	// Capture Go's standard logger, which event.Bus.call uses to report a panicking handler.
	var buf syncBuffer
	stdlog.SetOutput(&buf)
	defer stdlog.SetOutput(os.Stderr)

	bus := event.NewBus()
	var normalRan bool
	bus.Subscribe("t", func(any) { panic("boom") })
	bus.Subscribe("t", func(any) { normalRan = true })

	done := make(chan struct{})
	go func() {
		defer close(done)
		bus.Publish("t", "payload") // must not crash the process
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		rec("5-events", "panic isolation", "FAIL", "Publish hung")
		return
	}
	check("5-events", "panic recovered", buf.has("event: handler panicked: boom"),
		"panicking handler recovered + logged", "no recovery log line")
	check("5-events", "sibling handler ran", normalRan,
		"a sibling handler still ran after a panic", "sibling handler did not run")
}

func testHealthCheck() {
	addr := "127.0.0.1:19271"
	lst, err := raknet.Listen(addr)
	if err != nil {
		rec("7-health", "start fake backend", "FAIL", err.Error())
		return
	}
	go func() {
		for {
			conn, aerr := lst.Accept()
			if aerr != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	reg := server.NewDefaultRegistry()
	reg.AddServer(server.New("backend", addr, "", 1, false))
	hcLog := &capLogger{}
	// interval 200ms, timeout 400ms, threshold 3 -> ~3 failed pings (each up to 400ms) to flip unhealthy.
	checker := server.NewHealthChecker(reg, 200*time.Millisecond, 400*time.Millisecond, 3, hcLog, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go checker.Start(ctx)

	srv, _ := reg.Server("backend")
	time.Sleep(1 * time.Second)
	check("7-health", "healthy while up", srv.Healthy(),
		"server stays healthy while responding to RakNet pings", "server marked unhealthy while up")

	// Kill the backend; poll (up to 8s) for it to flip unhealthy after consecutive ping failures.
	_ = lst.Close()
	wentDown := waitFor(8*time.Second, func() bool { return !srv.Healthy() })
	check("7-health", "unhealthy when down", wentDown,
		"server marked unhealthy after consecutive ping failures", "still healthy 8s after backend down")
	check("7-health", "unhealthy logged", hcLog.has("marking unhealthy"),
		"health checker logged the transition", "no 'marking unhealthy' log")

	// Bring it back; it should recover.
	var lst2 *raknet.Listener
	for i := 0; i < 5; i++ {
		lst2, err = raknet.Listen(addr)
		if err == nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if err != nil {
		rec("7-health", "restart backend", "FAIL", err.Error())
		return
	}
	go func() {
		for {
			conn, aerr := lst2.Accept()
			if aerr != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	defer lst2.Close()
	recovered := waitFor(5*time.Second, func() bool { return srv.Healthy() })
	check("7-health", "recovers when back", recovered,
		"server marked healthy again once responding", "did not recover")
	check("7-health", "recovery logged", hcLog.has("responding again"),
		"health checker logged recovery", "no recovery log")
}

func testWeightedLoadBalancing() {
	reg := server.NewDefaultRegistry()
	reg.AddServer(server.New("w1", "127.0.0.1:1", "lobby", 1, false))
	reg.AddServer(server.New("w3", "127.0.0.1:2", "lobby", 3, false))
	lb := session.NewGroupedLoadBalancer(reg, "lobby")

	const total = 1000
	for i := 0; i < total; i++ {
		srv := lb.FindServer(nil)
		if srv == nil {
			rec("8-loadbalance", "1000 placements", "FAIL", fmt.Sprintf("nil server at join %d", i))
			return
		}
		srv.IncrementPlayerCount()
	}
	w1, _ := reg.Server("w1")
	w3, _ := reg.Server("w3")
	c1, c3 := w1.PlayerCount(), w3.PlayerCount()
	check("8-loadbalance", "1000 placements weighted", c1+c3 == total && c3 >= 680 && c3 <= 820 && c1 >= 180 && c1 <= 320,
		fmt.Sprintf("weight 1:3 -> w1=%d w3=%d (~250/750)", c1, c3),
		fmt.Sprintf("skewed distribution w1=%d w3=%d", c1, c3))

	// Draining server is excluded from new placements.
	w3.SetDraining(true)
	before1 := w1.PlayerCount()
	for i := 0; i < 100; i++ {
		lb.FindServer(nil).IncrementPlayerCount()
	}
	check("8-loadbalance", "draining excluded", w1.PlayerCount() == before1+100 && w3.PlayerCount() == c3,
		"all 100 new joins avoided the draining server", fmt.Sprintf("w1 +%d, w3 changed to %d", w1.PlayerCount()-before1, w3.PlayerCount()))
	w3.SetDraining(false)

	// Unhealthy server is excluded too.
	w3.SetHealthy(false)
	before1 = w1.PlayerCount()
	for i := 0; i < 50; i++ {
		lb.FindServer(nil).IncrementPlayerCount()
	}
	check("8-loadbalance", "unhealthy excluded", w1.PlayerCount() == before1+50,
		"all 50 new joins avoided the unhealthy server", "unhealthy server received joins")

	// No available server -> nil (player would be kicked cleanly).
	empty := server.NewDefaultRegistry()
	elb := session.NewGroupedLoadBalancer(empty, "lobby")
	check("8-loadbalance", "no server -> nil", elb.FindServer(nil) == nil,
		"empty group returns nil (clean kick)", "returned a server from an empty group")
}

func testIPGuard() {
	g := session.NewSimpleIPGuard([]string{"203.0.113.9"}, true, 10*time.Second, 5)

	banned := &net.TCPAddr{IP: net.ParseIP("203.0.113.9"), Port: 5000}
	ok, msg := g.Allow(banned)
	check("9-ipguard", "banned IP rejected", !ok && strings.Contains(msg, "banned"),
		"banned IP rejected with ban message", fmt.Sprintf("ok=%v msg=%q", ok, msg))

	client := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 6000}
	allowed := 0
	var lastMsg string
	for i := 0; i < 6; i++ {
		ok, lastMsg = g.Allow(client)
		if ok {
			allowed++
		}
	}
	check("9-ipguard", "rate limit triggers", allowed == 5 && strings.Contains(lastMsg, "too frequently"),
		"5 allowed then 6th throttled", fmt.Sprintf("allowed=%d lastMsg=%q", allowed, lastMsg))

	other := &net.TCPAddr{IP: net.ParseIP("10.0.0.7"), Port: 7000}
	ok2, _ := g.Allow(other)
	check("9-ipguard", "per-IP isolation", ok2,
		"a different IP is unaffected by another IP's rate limit", "different IP wrongly throttled")
}

func testClustering() {
	redisAddr := os.Getenv("HARNESS_REDIS")
	if redisAddr == "" {
		redisAddr = "127.0.0.1:6379"
	}
	backend, err := cluster.NewRedisBackend(redisAddr, "", 0, 300*time.Second)
	if err != nil {
		rec("11-cluster", "redis backend", "SKIP", "Redis unreachable at "+redisAddr+": "+err.Error())
		return
	}
	defer backend.Close()

	_ = backend.Remove("proxy-a", "Steve")
	if err := backend.Announce("proxy-a", "Steve", "lobby"); err != nil {
		rec("11-cluster", "announce", "FAIL", err.Error())
		return
	}
	pid, sv, online, err := backend.Lookup("Steve")
	check("11-cluster", "announce+lookup", err == nil && online && pid == "proxy-a" && sv == "lobby",
		"Lookup returns proxy-a/lobby/online", fmt.Sprintf("pid=%q sv=%q online=%v err=%v", pid, sv, online, err))

	// Remove with the wrong owner must NOT delete the record.
	_ = backend.Remove("proxy-b", "Steve")
	_, _, stillOnline, _ := backend.Lookup("Steve")
	check("11-cluster", "ownership-safe remove", stillOnline,
		"Remove by a non-owner proxy left the record intact", "non-owner Remove deleted the record")

	// Remove with the correct owner deletes it.
	_ = backend.Remove("proxy-a", "Steve")
	_, _, goneOnline, _ := backend.Lookup("Steve")
	check("11-cluster", "owner remove deletes", !goneOnline,
		"owner Remove deleted the record", "record survived owner Remove")

	// Cross-proxy find through the real socket handler.
	addr := "127.0.0.1:19185"
	s, err := newStack(addr, nil)
	if err != nil {
		rec("11-cluster", "start socket", "FAIL", err.Error())
		return
	}
	s.ss.SetCluster(backend)
	time.Sleep(100 * time.Millisecond)
	_ = backend.Announce("proxy-a", "Alice", "survival")
	defer backend.Remove("proxy-a", "Alice")

	c, conn, err := dialClient(addr, false)
	if err != nil {
		rec("11-cluster", "dial", "FAIL", err.Error())
		return
	}
	defer conn.Close()
	if st, aerr := authClient(c, conn, secret, "finder"); aerr != nil || st != packet.AuthResponseSuccess {
		rec("11-cluster", "auth finder", "FAIL", fmt.Sprintf("st=%d err=%v", st, aerr))
		return
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err := c.WritePacket(&packet.FindPlayerRequest{PlayerName: "Alice"}); err != nil {
		rec("11-cluster", "send find", "FAIL", err.Error())
		return
	}
	pk, err := c.ReadPacket()
	if err != nil {
		rec("11-cluster", "read find response", "FAIL", err.Error())
		return
	}
	fpr, ok := pk.(*packet.FindPlayerResponse)
	check("11-cluster", "cross-proxy find", ok && fpr.Online && fpr.Proxy == "proxy-a" && fpr.Server == "survival",
		"FindPlayerResponse online=true proxy=proxy-a server=survival",
		fmt.Sprintf("got %T %+v", pk, pk))
}

func testAdminConsoleMisc() {
	s, err := newStack("127.0.0.1:19186", nil)
	if err != nil {
		rec("6-admin", "start socket", "FAIL", err.Error())
		return
	}
	check("6-admin", "help command", strings.Contains(s.p.HandleAdminCommand("help"), "Commands:"),
		"help lists commands", "help output unexpected")
	check("6-admin", "players (empty)", s.p.HandleAdminCommand("players") == "No players online.",
		"players reports none online", "unexpected players output")
	check("6-admin", "kick missing player", strings.Contains(s.p.HandleAdminCommand("kick ghost"), "not online"),
		"kick of absent player handled", "unexpected kick output")
	check("6-admin", "transfer missing player", strings.Contains(s.p.HandleAdminCommand("transfer ghost lobby"), "not online"),
		"transfer of absent player handled", "unexpected transfer output")
	check("6-admin", "unknown command", strings.Contains(s.p.HandleAdminCommand("frobnicate"), "unknown command"),
		"unknown command reported", "unexpected output")
}

func testGracefulShutdownPartial() {
	addr := "127.0.0.1:19187"
	s, err := newStack(addr, nil)
	if err != nil {
		rec("10-shutdown", "start socket", "FAIL", err.Error())
		return
	}
	// A backend is connected.
	_, conn, err := registerBackend(addr, "svc", "", 1)
	if err != nil {
		rec("10-shutdown", "register", "FAIL", err.Error())
		return
	}
	defer conn.Close()
	// Close the socket listener (part of the graceful shutdown path).
	if err := s.ss.Close(); err != nil {
		rec("10-shutdown", "socket close", "FAIL", err.Error())
		return
	}
	time.Sleep(100 * time.Millisecond)
	// New backend connections must be refused after close.
	c2, conn2, derr := dialClient(addr, false)
	refused := derr != nil
	if !refused {
		// Dial may still succeed on loopback; an auth attempt must then fail (listener gone).
		if st, aerr := authClient(c2, conn2, secret, "late"); aerr != nil || st != packet.AuthResponseSuccess {
			refused = true
		}
		_ = conn2.Close()
	}
	check("10-shutdown", "listener closed", refused,
		"socket listener refuses new connections after Close()", "listener still accepting after Close()")
	// Portal.Close() with no active player listener must not panic.
	check("10-shutdown", "portal close safe", s.p.Close() == nil,
		"Portal.Close() returns cleanly", "Portal.Close() errored")
}

type syncBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}
func (b *syncBuffer) has(sub string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.Contains(b.buf.String(), sub)
}

func main() {
	dir, err := os.MkdirTemp("", "portal-harness")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	fmt.Println("Portal PR #2 — in-process feature verification harness")
	fmt.Println("======================================================")

	runTest("1-tls", "Feature 1: Socket TLS", func() { testTLS(dir) })
	runTest("2-throttle", "Feature 2: Per-IP auth throttle", testAuthThrottle)
	runTest("3-groups", "Feature 3/4/5/6: groups, draining, metrics, admin, events", testGroupsDrainingAdminMetrics)
	runTest("5-events", "Feature 5: Event bus panic isolation", testEventBusPanic)
	runTest("6-admin", "Feature 6: Admin console (no client)", testAdminConsoleMisc)
	runTest("7-health", "Feature 7: Health checking (real RakNet)", testHealthCheck)
	runTest("8-loadbalance", "Feature 8: Weighted load balancing (1000)", testWeightedLoadBalancing)
	runTest("9-ipguard", "Feature 9: IP guard (bans + rate limit)", testIPGuard)
	runTest("10-shutdown", "Feature 10: Graceful shutdown (partial)", testGracefulShutdownPartial)
	runTest("11-cluster", "Feature 11: Clustering (Redis)", testClustering)

	// summary
	fmt.Println("\n======================================================")
	fmt.Println("SUMMARY")
	var pass, fail, skip int
	for _, r := range results {
		switch r.status {
		case "PASS":
			pass++
		case "FAIL":
			fail++
		case "SKIP":
			skip++
		}
	}
	for _, r := range results {
		if r.status == "FAIL" {
			fmt.Printf("  FAIL: [%s] %s — %s\n", r.feature, r.name, r.detail)
		}
	}
	for _, r := range results {
		if r.status == "SKIP" {
			fmt.Printf("  SKIP: [%s] %s — %s\n", r.feature, r.name, r.detail)
		}
	}
	fmt.Printf("\nTOTAL: %d checks — %d PASS, %d FAIL, %d SKIP\n", len(results), pass, fail, skip)
	if fail > 0 {
		os.Exit(1)
	}
}
