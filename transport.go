package portal

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"

	"github.com/df-mc/go-nethernet"
	"github.com/df-mc/go-nethernet/endpoint"
	"github.com/sandertv/gophertunnel/minecraft"
)

// Transport identifies the network transport used for the proxy's player-facing listener.
type Transport string

const (
	// TransportNetherNet is the official and default transport. It serves Minecraft's WebRTC-based
	// signaling endpoint over HTTP(S) on Options.Address, the same mechanism Bedrock Dedicated Server
	// exposes when 'transport=nethernet' is set. Clients that support NetherNet try this endpoint before
	// falling back to RakNet, so it should be preferred unless a specific reason requires RakNet.
	TransportNetherNet Transport = "nethernet"
	// TransportRakNet is the legacy UDP transport used by Bedrock before NetherNet support was introduced.
	TransportRakNet Transport = "raknet"
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
}

// netherNetListener owns the HTTP(S) server that serves the NetherNet signaling endpoint. It is kept
// alongside the minecraft.Listener built from it so Portal can shut both down together.
type netherNetListener struct {
	server *http.Server
	l      net.Listener
}

// serve blocks, serving the signaling endpoint until the listener is closed.
func (n *netherNetListener) serve() {
	_ = n.server.Serve(n.l)
}

// Close shuts down the signaling endpoint's HTTP server and its underlying listener. It is safe to call
// whether or not serve has been started: http.Server.Close only closes listeners it is actively serving, so
// the raw listener is closed directly too, ignoring the resulting "already closed" error if serve did pick
// it up first.
func (n *netherNetListener) Close() error {
	err := n.server.Close()
	if lErr := n.l.Close(); lErr != nil && !errors.Is(lErr, net.ErrClosed) {
		err = errors.Join(err, lErr)
	}
	return err
}

// newNetherNetNetwork builds the NetherNet signaling endpoint bound to address and the minecraft.Network
// that uses it. The endpoint does not start serving requests until the returned netherNetListener's serve
// method is called; callers must do so only after registering the network with a minecraft.Listener, since
// the signaling handler rejects offers until a listener is registered to receive them.
func newNetherNetNetwork(address string, opts NetherNetOptions) (minecraft.Network, *netherNetListener, error) {
	credentials := &nethernet.Credentials{ICEServers: opts.ICEServers}
	handler := endpoint.HandlerConfig{
		Credentials: func(context.Context) (*nethernet.Credentials, error) { return credentials, nil },
	}.New()

	l, err := net.Listen("tcp", address)
	if err != nil {
		return nil, nil, fmt.Errorf("listen nethernet endpoint: %w", err)
	}
	if opts.TLSCertFile != "" || opts.TLSKeyFile != "" {
		cert, err := tls.LoadX509KeyPair(opts.TLSCertFile, opts.TLSKeyFile)
		if err != nil {
			_ = l.Close()
			return nil, nil, fmt.Errorf("load nethernet tls certificate: %w", err)
		}
		l = tls.NewListener(l, &tls.Config{Certificates: []tls.Certificate{cert}})
	}

	return minecraft.NetherNet{Signaling: handler}, &netherNetListener{
		server: &http.Server{Handler: handler},
		l:      l,
	}, nil
}
