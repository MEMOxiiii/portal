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

	"github.com/df-mc/go-nethernet"
	"github.com/df-mc/go-nethernet/endpoint"
	"github.com/paroxity/portal/internal"
	"github.com/paroxity/portal/server"
	"github.com/pion/ice/v4"
	"github.com/pion/webrtc/v4"
	"github.com/sandertv/gophertunnel/minecraft"
)

// Transport identifies a network transport used to reach a Bedrock server, whether that is the proxy's
// own player-facing listener (Options.Transport) or a registered backend server. It is an alias of
// server.Transport, the same type a Server reports from its Transport method, since the player-facing
// listener and a backend server are addressed by the same two transports.
type Transport = server.Transport

const (
	// TransportNetherNet is the official and default player-facing transport. It serves Minecraft's
	// WebRTC-based signaling endpoint over HTTP(S) on Options.Address, the same mechanism Bedrock
	// Dedicated Server exposes when 'transport=nethernet' is set. Clients that support NetherNet try this
	// endpoint before falling back to RakNet, so it should be preferred unless a specific reason requires
	// RakNet.
	TransportNetherNet = server.TransportNetherNet
	// TransportRakNet is the legacy UDP transport used by Bedrock before NetherNet support was introduced.
	TransportRakNet = server.TransportRakNet
)

// NetherNetOptions holds settings specific to the NetherNet transport. It is only used when
// Options.Transport is TransportNetherNet, which is the default.
type NetherNetOptions struct {
	// TLSCertFile and TLSKeyFile are paths to a PEM encoded certificate/key pair used to serve the
	// signaling endpoint over HTTPS. Bedrock clients try HTTPS before plain HTTP when locating a NetherNet
	// server, so setting these is recommended for proxies reachable over the internet. If either field is
	// empty, the endpoint is served over plain HTTP instead.
	TLSCertFile string
	TLSKeyFile  string

	// ICEServers lists the STUN/TURN servers offered to clients for WebRTC NAT traversal. Leaving this
	// empty may prevent players behind restrictive NATs from establishing a connection.
	ICEServers []nethernet.ICEServer

	// UDPPorts is the UDP port, or "min-max" range, used for the actual WebRTC game connection once
	// signaling completes. This is separate from Address (the TCP signaling port) and must not overlap
	// any RakNet listener's port, since the two cannot share a UDP port on the same host. A single port is
	// shared by every connection through a UDP mux and is the simplest to forward through a firewall or
	// NAT; a range spreads connections across more ports but can run out under heavy load. If left empty,
	// the operating system assigns a random ephemeral port per connection, which most firewalls and NATs
	// block by default -- leaving this empty on a proxy reachable from outside its own host will silently
	// prevent every player from finishing the connection even though signaling succeeds.
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

// netherNetListener owns the HTTP(S) server that serves the NetherNet signaling endpoint, along with the
// UDP mux backing its fixed media port (if any). It is kept alongside the minecraft.Listener built from it
// so Portal can shut everything down together.
type netherNetListener struct {
	server *http.Server
	l      net.Listener
	// udpMux is non-nil only when NetherNetOptions.UDPPorts names a single fixed port.
	udpMux *ice.MultiUDPMuxDefault
}

// serve blocks, serving the signaling endpoint until the listener is closed.
func (n *netherNetListener) serve() {
	_ = n.server.Serve(n.l)
}

// Close shuts down the signaling endpoint's HTTP server, its underlying listener, and the UDP mux backing
// its media port, if any. It is safe to call whether or not serve has been started: http.Server.Close only
// closes listeners it is actively serving, so the raw listener is closed directly too, ignoring the
// resulting "already closed" error if serve did pick it up first.
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

// logNetherNetRequests wraps next so every NetherNet signaling request is logged at debug level through
// log. The signaling endpoint is reachable directly by anything that can reach Address, including port
// scanners, so this is the only way to see whether a player's client is even attempting to connect.
func logNetherNetRequests(log internal.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Debugf("nethernet: %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
		next.ServeHTTP(w, r)
	})
}

// newNetherNetNetwork builds the NetherNet signaling endpoint bound to address and the minecraft.Network
// that uses it. The endpoint does not start serving requests until the returned netherNetListener's serve
// method is called; callers must do so only after registering the network with a minecraft.Listener, since
// the signaling handler rejects offers until a listener is registered to receive them.
func newNetherNetNetwork(address string, opts NetherNetOptions, log internal.Logger) (minecraft.Network, *netherNetListener, error) {
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

	handler := endpoint.HandlerConfig{
		Credentials: func(context.Context) (*nethernet.Credentials, error) { return credentials, nil },
	}.New()

	l, err := net.Listen("tcp", address)
	if err != nil {
		if udpMux != nil {
			_ = udpMux.Close()
		}
		return nil, nil, fmt.Errorf("listen nethernet endpoint: %w", err)
	}
	if opts.TLSCertFile != "" || opts.TLSKeyFile != "" {
		cert, err := tls.LoadX509KeyPair(opts.TLSCertFile, opts.TLSKeyFile)
		if err != nil {
			_ = l.Close()
			if udpMux != nil {
				_ = udpMux.Close()
			}
			return nil, nil, fmt.Errorf("load nethernet tls certificate: %w", err)
		}
		l = tls.NewListener(l, &tls.Config{Certificates: []tls.Certificate{cert}})
	}

	network := minecraft.NetherNet{
		Signaling: handler,
		ListenConfig: nethernet.ListenConfig{
			API: webrtc.NewAPI(webrtc.WithSettingEngine(settingEngine)),
		},
	}
	return network, &netherNetListener{
		server: &http.Server{Handler: logNetherNetRequests(log, handler)},
		l:      l,
		udpMux: udpMux,
	}, nil
}
