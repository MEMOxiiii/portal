package portal

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/df-mc/go-nethernet"
	"github.com/df-mc/go-nethernet/endpoint"
	"github.com/paroxity/portal/internal"
	"github.com/paroxity/portal/server"
	"github.com/pion/ice/v4"
	"github.com/pion/webrtc/v4"
	"github.com/sandertv/gophertunnel/minecraft"
)

// Transport identifies a network transport used to reach a Bedrock server -- the proxy's own player-facing
// listener (Options.Transport) or a registered backend server. Alias of server.Transport.
type Transport = server.Transport

const (
	// TransportNetherNet is the official and default player-facing transport: Minecraft's WebRTC-based
	// signaling endpoint, served over HTTP(S) on Options.Address.
	TransportNetherNet = server.TransportNetherNet
	// TransportRakNet is the legacy UDP transport used before NetherNet support was introduced.
	TransportRakNet = server.TransportRakNet
)

// NetherNetOptions holds settings specific to the NetherNet transport, used when Options.Transport is
// TransportNetherNet (the default).
type NetherNetOptions struct {
	// TLSCertFile and TLSKeyFile serve the signaling endpoint over HTTPS instead of plain HTTP.
	TLSCertFile string
	TLSKeyFile  string

	// ICEServers lists the STUN/TURN servers offered for WebRTC NAT traversal.
	ICEServers []nethernet.ICEServer

	// UDPPorts is the UDP port, or "min-max" range, used for the WebRTC media connection -- separate from
	// Address (the TCP signaling port), and must not overlap RakNet's or any other NetherNet listener's
	// port on the same host. Left empty, the OS assigns a random ephemeral port per connection, which most
	// firewalls block -- required for a proxy reachable from outside its own host.
	UDPPorts string
}

// netherNetPortRange is an inclusive UDP port range for the NetherNet media transport. A single port
// (Min == Max) is shared by all connections through a UDP mux; a wider range can run out of ports under
// load. Zero bounds leave port selection to the operating system.
type netherNetPortRange struct {
	Min, Max uint16
}

// parseNetherNetPortRange parses "port" or "min-max" into a netherNetPortRange. An empty string yields the
// zero range, meaning the operating system assigns ports.
func parseNetherNetPortRange(s string) (netherNetPortRange, error) {
	if s == "" {
		return netherNetPortRange{}, nil
	}
	min_, max_, ok := strings.Cut(s, "-")
	if !ok {
		v, err := strconv.ParseUint(s, 10, 16)
		if err != nil {
			return netherNetPortRange{}, fmt.Errorf("parse port: %w", err)
		}
		return netherNetPortRange{Min: uint16(v), Max: uint16(v)}, nil
	}
	minV, err := strconv.ParseUint(min_, 10, 16)
	if err != nil {
		return netherNetPortRange{}, fmt.Errorf("parse minimum port: %w", err)
	}
	maxV, err := strconv.ParseUint(max_, 10, 16)
	if err != nil {
		return netherNetPortRange{}, fmt.Errorf("parse maximum port: %w", err)
	}
	if minV > maxV {
		return netherNetPortRange{}, fmt.Errorf("invalid port range: %d-%d", minV, maxV)
	}
	return netherNetPortRange{Min: uint16(minV), Max: uint16(maxV)}, nil
}

// netherNetListener owns the HTTP(S) server for the NetherNet signaling endpoint and the UDP mux backing
// its fixed media port (if any), so Portal can shut everything down together.
type netherNetListener struct {
	server *http.Server
	l      net.Listener
	udpMux *ice.MultiUDPMuxDefault // non-nil only for a single fixed UDPPorts port
}

// serve blocks, serving the signaling endpoint until the listener is closed.
func (n *netherNetListener) serve() {
	_ = n.server.Serve(n.l)
}

// Close closes the HTTP server, its listener, and the UDP mux, if any. l is closed directly too (not just
// via server.Close) since http.Server.Close only closes listeners it's actively serving.
func (n *netherNetListener) Close() error {
	err := n.server.Close()
	if lErr := n.l.Close(); lErr != nil && !errors.Is(lErr, net.ErrClosed) {
		err = errors.Join(err, lErr)
	}
	if n.udpMux != nil {
		if mErr := n.udpMux.Close(); mErr != nil {
			err = errors.Join(err, mErr)
		}
	}
	return err
}

// logNetherNetRequests logs every NetherNet signaling request at debug level -- the only way to see
// whether a client is attempting to connect, since the endpoint has no other visibility.
func logNetherNetRequests(log internal.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Debugf("nethernet: %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
		next.ServeHTTP(w, r)
	})
}

// newNetherNetNetwork builds the NetherNet signaling endpoint bound to address and the minecraft.Network
// that uses it. Callers must call the returned netherNetListener's serve only after registering the
// network with a minecraft.Listener -- the signaling handler rejects offers until then.
func newNetherNetNetwork(address string, opts NetherNetOptions, log internal.Logger) (network minecraft.Network, nn *netherNetListener, err error) {
	credentials := &nethernet.Credentials{ICEServers: opts.ICEServers}

	ports, err := parseNetherNetPortRange(opts.UDPPorts)
	if err != nil {
		return nil, nil, fmt.Errorf("parse nethernet udp_ports: %w", err)
	}

	var udpMux *ice.MultiUDPMuxDefault
	settingEngine := webrtc.SettingEngine{}
	switch {
	case ports.Min != 0 && ports.Min == ports.Max:
		if udpMux, err = ice.NewMultiUDPMuxFromPort(int(ports.Min)); err != nil {
			return nil, nil, fmt.Errorf("allocate nethernet udp mux on port %d: %w", ports.Min, err)
		}
		settingEngine.SetICEUDPMux(udpMux)
	case ports.Min != 0 || ports.Max != 0:
		if err := settingEngine.SetEphemeralUDPPortRange(ports.Min, ports.Max); err != nil {
			return nil, nil, fmt.Errorf("configure nethernet ephemeral udp port range: %w", err)
		}
	}
	defer func() { // closes udpMux (if allocated) on any error return below
		if err != nil && udpMux != nil {
			_ = udpMux.Close()
		}
	}()

	handler := endpoint.HandlerConfig{
		Credentials: func(context.Context) (*nethernet.Credentials, error) { return credentials, nil },
	}.New()

	l, err := net.Listen("tcp", address)
	if err != nil {
		return nil, nil, fmt.Errorf("listen nethernet endpoint: %w", err)
	}
	defer func() {
		if err != nil {
			_ = l.Close()
		}
	}()
	if opts.TLSCertFile != "" || opts.TLSKeyFile != "" {
		cert, certErr := tls.LoadX509KeyPair(opts.TLSCertFile, opts.TLSKeyFile)
		if certErr != nil {
			return nil, nil, fmt.Errorf("load nethernet tls certificate: %w", certErr)
		}
		l = tls.NewListener(l, &tls.Config{Certificates: []tls.Certificate{cert}})
	}

	network = minecraft.NetherNet{
		Signaling: handler,
		ListenConfig: nethernet.ListenConfig{
			API: webrtc.NewAPI(webrtc.WithSettingEngine(settingEngine)),
		},
	}
	return network, &netherNetListener{
		server: &http.Server{Handler: logNetherNetRequests(log, handler), ReadHeaderTimeout: 10 * time.Second},
		l:      l,
		udpMux: udpMux,
	}, nil
}
