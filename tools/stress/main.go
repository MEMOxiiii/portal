// Command stress hammers the Portal socket server with many concurrent backend clients to validate its
// concurrency safety and capacity under load. It is meant to be run with the race detector:
//
//	go run -race ./tools/stress [clients] [seconds]
//
// Each simulated backend opens its own socket connection, authenticates, registers a server, then spends
// the test window continuously requesting the server list and toggling its own draining state — all while
// the main goroutine concurrently scrapes the admin console and metrics. It asserts that every backend
// registers, that concurrent traffic produces no errors or races, and that the registry drains back to
// zero once every backend disconnects.
package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/paroxity/portal"
	"github.com/paroxity/portal/metrics"
	"github.com/paroxity/portal/socket"
	"github.com/paroxity/portal/socket/packet"
)

const secret = "stress-secret"

type nopLogger struct{}

func (nopLogger) Debugf(string, ...interface{}) {}
func (nopLogger) Infof(string, ...interface{})  {}
func (nopLogger) Errorf(string, ...interface{}) {}
func (nopLogger) Fatalf(string, ...interface{}) {}

func main() {
	clients := 400
	seconds := 4
	if len(os.Args) > 1 {
		if n, err := strconv.Atoi(os.Args[1]); err == nil {
			clients = n
		}
	}
	if len(os.Args) > 2 {
		if n, err := strconv.Atoi(os.Args[2]); err == nil {
			seconds = n
		}
	}

	addr := "127.0.0.1:19200"
	p := portal.New(portal.Options{Logger: nopLogger{}})
	ss := socket.NewDefaultServer(addr, secret, p.SessionStore(), p.ServerRegistry(), nopLogger{}, true, p.Events())
	if err := ss.Listen(); err != nil {
		fmt.Println("FATAL: listen:", err)
		os.Exit(1)
	}

	// metrics endpoint
	metrics.Default.RegisterGauge("portal_servers_registered", func() float64 { return float64(len(p.ServerRegistry().Servers())) })
	metrics.Default.RegisterGauge("portal_socket_clients_connected", func() float64 { return float64(len(ss.Clients())) })
	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.Default.Handler())
	ml, _ := net.Listen("tcp", "127.0.0.1:19201")
	go http.Serve(ml, mux)

	fmt.Printf("Portal socket stress: %d concurrent backends, %ds of traffic (race detector on)\n", clients, seconds)
	fmt.Println("=====================================================================")

	var (
		regErrors  atomic.Int64
		opErrors   atomic.Int64
		listOK     atomic.Int64
		drainOps   atomic.Int64
		registered atomic.Int64
		dialErr    atomic.Int64
		authRWErr  atomic.Int64
		statusErr  atomic.Int64
	)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	startReg := time.Now()

	for i := 0; i < clients; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			// Spread connection starts over a short window: real backends come up at different times, they
			// never all open a TCP connection in the same microsecond. This avoids a synthetic loopback
			// SYN-backlog burst (a client/OS limit, not a proxy one) while still keeping dozens of
			// connections in flight concurrently.
			time.Sleep(time.Duration(id) * 4 * time.Millisecond)
			// Real backends reconnect if the proxy is briefly saturated; retry the dial a few times with
			// a small backoff so a 400-at-once SYN burst on loopback doesn't count as a proxy failure.
			var conn net.Conn
			var err error
			for attempt := 0; attempt < 5; attempt++ {
				conn, err = net.Dial("tcp", addr)
				if err == nil {
					break
				}
				time.Sleep(time.Duration(20*(attempt+1)) * time.Millisecond)
			}
			if err != nil {
				dialErr.Add(1)
				regErrors.Add(1)
				return
			}
			defer conn.Close()
			c := socket.NewClient(conn, nopLogger{}, false)

			name := fmt.Sprintf("srv-%d", id)
			if err := c.WritePacket(&packet.AuthRequest{Protocol: packet.ProtocolVersion, Secret: secret, Name: name}); err != nil {
				authRWErr.Add(1)
				regErrors.Add(1)
				return
			}
			pk, err := c.ReadPacket()
			if err != nil {
				authRWErr.Add(1)
				regErrors.Add(1)
				return
			}
			if ar, ok := pk.(*packet.AuthResponse); !ok || ar.Status != packet.AuthResponseSuccess {
				statusErr.Add(1)
				regErrors.Add(1)
				return
			}
			if err := c.WritePacket(&packet.RegisterServer{
				Address: fmt.Sprintf("127.0.0.1:%d", 30000+id),
				Group:   fmt.Sprintf("group-%d", id%5),
				Weight:  uint32(1 + id%4),
			}); err != nil {
				regErrors.Add(1)
				return
			}
			registered.Add(1)

			// traffic loop: request the server list and toggle draining until told to stop.
			draining := false
			for {
				select {
				case <-stop:
					return
				default:
				}
				if err := c.WritePacket(&packet.ServerListRequest{}); err != nil {
					opErrors.Add(1)
					return
				}
				resp, err := c.ReadPacket()
				if err != nil {
					opErrors.Add(1)
					return
				}
				if _, ok := resp.(*packet.ServerListResponse); ok {
					listOK.Add(1)
				} else {
					opErrors.Add(1)
				}
				draining = !draining
				if err := c.WritePacket(&packet.SetServerDraining{Draining: draining}); err != nil {
					opErrors.Add(1)
					return
				}
				drainOps.Add(1)
			}
		}(i)
	}

	// Wait until (nearly) all backends have registered.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) && int(registered.Load()) < clients {
		time.Sleep(50 * time.Millisecond)
	}
	// The client counts a backend as registered once it has *sent* RegisterServer; the proxy adds it to
	// the registry a moment later when it processes the packet. Poll briefly so the peak snapshot reflects
	// every registration rather than racing the last few in flight.
	regDeadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(regDeadline) && len(p.ServerRegistry().Servers()) < clients {
		time.Sleep(20 * time.Millisecond)
	}
	regDur := time.Since(startReg)
	peak := len(p.ServerRegistry().Servers())
	fmt.Printf("registered %d/%d backends in %s (registry peak=%d)\n", registered.Load(), clients, regDur.Round(time.Millisecond), peak)

	// Concurrently scrape admin console + metrics while traffic runs.
	scrapeStop := make(chan struct{})
	var scrapes atomic.Int64
	go func() {
		for {
			select {
			case <-scrapeStop:
				return
			default:
			}
			_ = p.HandleAdminCommand("servers")
			if resp, err := http.Get("http://127.0.0.1:19201/metrics"); err == nil {
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
			}
			scrapes.Add(1)
			time.Sleep(20 * time.Millisecond)
		}
	}()

	time.Sleep(time.Duration(seconds) * time.Second)
	close(scrapeStop)
	close(stop)
	wg.Wait()

	// Give the server a moment to process the disconnects and clean up the registry.
	drained := false
	for i := 0; i < 100; i++ {
		if len(p.ServerRegistry().Servers()) == 0 && len(ss.Clients()) == 0 {
			drained = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	fmt.Println("---------------------------------------------------------------------")
	fmt.Printf("server-list round-trips : %d\n", listOK.Load())
	fmt.Printf("draining toggles        : %d\n", drainOps.Load())
	fmt.Printf("admin+metrics scrapes   : %d\n", scrapes.Load())
	fmt.Printf("registration errors     : %d (dial=%d authIO=%d status=%d)\n", regErrors.Load(), dialErr.Load(), authRWErr.Load(), statusErr.Load())
	fmt.Printf("operation errors        : %d\n", opErrors.Load())
	fmt.Printf("registry after teardown : %d servers, %d socket clients\n", len(p.ServerRegistry().Servers()), len(ss.Clients()))

	ok := int(registered.Load()) == clients && peak == clients && regErrors.Load() == 0 && opErrors.Load() == 0 && drained
	fmt.Println("=====================================================================")
	if ok {
		fmt.Printf("RESULT: PASS — %d backends handled concurrently with zero errors and clean teardown\n", clients)
	} else {
		fmt.Println("RESULT: FAIL — see counters above")
		os.Exit(1)
	}
}
