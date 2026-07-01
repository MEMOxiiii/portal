package socket

import (
	"crypto/tls"
	"errors"
	"github.com/paroxity/portal/cluster"
	"github.com/paroxity/portal/event"
	"github.com/paroxity/portal/internal"
	"github.com/paroxity/portal/server"
	"github.com/paroxity/portal/session"
	"github.com/paroxity/portal/socket/packet"
	"net"
	"strings"
	"sync"
)

type Server interface {
	// Listen starts listening for connections on an address.
	Listen() error

	// Logger returns the logger attached to the socket server.
	Logger() internal.Logger

	// Secret returns the secret required for connections to authenticate.
	Secret() string

	// Clients returns all the clients that are connected to the socket server.
	Clients() []*Client
	// Client attempts to return a client from the provided name, case-sensitive.
	Client(name string) (*Client, bool)
	// Authenticate marks the client as authenticated with the provided name. It is safe to assume that the provided
	// name is not in use, unless called by places other than the socket server.
	Authenticate(c *Client, name string)

	// AuthBlocked returns whether the remote address has failed authentication too many times recently and
	// is temporarily blocked from authenticating.
	AuthBlocked(addr net.Addr) bool
	// RecordAuthFailure records a failed authentication attempt from the remote address, which may cause it
	// to become temporarily blocked.
	RecordAuthFailure(addr net.Addr)

	// SessionStore returns the store used to hold the open sessions on the proxy.
	SessionStore() *session.Store
	// ServerRegistry returns the registry used to store available servers on the proxy.
	ServerRegistry() *server.Registry

	// Events returns the event bus used to publish proxy-wide occurrences, such as servers
	// registering/unregistering. It may be nil if no bus was configured.
	Events() *event.Bus

	// Cluster returns the cross-proxy presence backend used to look up players connected to other proxies
	// in the same cluster. It may be nil if clustering is not configured.
	Cluster() cluster.Backend

	// Close closes the socket server's listener, preventing it from accepting any further connections.
	Close() error
}

// DefaultServer represents a basic TCP socket server implementation. It allows external connections to
// connect and authenticate to be able to communicate with the proxy.
type DefaultServer struct {
	log internal.Logger

	addr         string
	secret       string
	readerLimits bool
	tlsConfig    *tls.Config
	authThrottle *authThrottle

	listener           net.Listener
	clientsMu          sync.RWMutex
	clients            map[string]*Client
	unconnectedClients map[net.Addr]*Client

	sessionStore   *session.Store
	serverRegistry *server.Registry
	events         *event.Bus
	cluster        cluster.Backend
}

// NewDefaultServer creates a new default server to be used for accepting socket connections. events may be
// nil, in which case no events are published for server registration/unregistration.
func NewDefaultServer(addr, secret string, sessionStore *session.Store, serverRegistry *server.Registry, log internal.Logger, readerLimits bool, events *event.Bus) *DefaultServer {
	return &DefaultServer{
		log: log,

		addr:         addr,
		secret:       secret,
		readerLimits: readerLimits,
		authThrottle: newAuthThrottle(),

		clients:            make(map[string]*Client),
		unconnectedClients: make(map[net.Addr]*Client),

		sessionStore:   sessionStore,
		serverRegistry: serverRegistry,
		events:         events,
	}
}

// NewDefaultTLSServer creates a new default server which serves the communication socket over TLS using the
// provided certificate. Backend servers must dial using TLS in order to connect.
func NewDefaultTLSServer(addr, secret string, sessionStore *session.Store, serverRegistry *server.Registry, log internal.Logger, readerLimits bool, tlsConfig *tls.Config, events *event.Bus) *DefaultServer {
	s := NewDefaultServer(addr, secret, sessionStore, serverRegistry, log, readerLimits, events)
	s.tlsConfig = tlsConfig
	return s
}

// Listen ...
func (s *DefaultServer) Listen() error {
	var listener net.Listener
	var err error
	if s.tlsConfig != nil {
		listener, err = tls.Listen("tcp", s.addr, s.tlsConfig)
	} else {
		listener, err = net.Listen("tcp", s.addr)
	}
	if err != nil {
		return err
	}
	s.log.Infof("socket server listening on %s\n", s.addr)
	s.listener = listener

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				if errors.Is(err, net.ErrClosed) {
					return
				}
				s.log.Infof("socket server unable to accept connection: %v", err)
				continue
			}
			s.log.Debugf("socket server accepted a new connection")

			go s.handleClient(NewClient(conn, s.log, s.readerLimits))
		}
	}()
	return nil
}

// handleClient handles a client that has been accepted from the socket server.
func (s *DefaultServer) handleClient(c *Client) {
	if s.AuthBlocked(c.conn.RemoteAddr()) {
		s.log.Debugf("rejected socket connection from %s: too many failed authentication attempts", c.conn.RemoteAddr())
		_ = c.Close()
		return
	}

	defer s.handleClientDisconnect(c)
	s.clientsMu.Lock()
	s.unconnectedClients[c.conn.RemoteAddr()] = c
	s.clientsMu.Unlock()

	for {
		if !c.Authenticated() && s.AuthBlocked(c.conn.RemoteAddr()) {
			s.log.Debugf("closing socket connection from %s: too many failed authentication attempts", c.conn.RemoteAddr())
			_ = c.Close()
			return
		}

		pk, err := c.ReadPacket()
		if err != nil {
			if containsAny(err.Error(), "EOF", "closed") {
				return
			}
			s.log.Errorf("socket server unable to read packet: %v", err)
			continue
		}

		h, ok := handlers[pk.ID()]
		if ok {
			if !c.Authenticated() && h.RequiresAuth() {
				_ = c.WritePacket(&packet.AuthResponse{Status: packet.AuthResponseUnauthenticated})
				s.log.Debugf("received packet %T from unauthenticated client", pk)
				continue
			}
			if err := h.Handle(pk, s, c); err != nil {
				s.log.Errorf("socket server unable to handle packet: %v", err)
			}
		} else {
			if c.name == "" {
				s.log.Debugf("unhandled packet %T from unauthenticated socket connection", pk)
			} else {
				s.log.Debugf("unhandled packet %T from %s socket connection", pk, c.name)
			}
		}
	}
}

// handleClientDisconnect handles a client that has been disconnected from the socket server.
func (s *DefaultServer) handleClientDisconnect(c *Client) {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	delete(s.clients, c.Name())
	delete(s.unconnectedClients, c.conn.RemoteAddr())
	s.log.Debugf("socket connection \"%s\" closed", c.name)
	srv, ok := s.serverRegistry.Server(c.Name())
	if ok {
		s.serverRegistry.RemoveServer(srv)
		s.log.Debugf("removed server for socket connection \"%s\"", c.Name())
		if s.events != nil {
			s.events.Publish(event.TopicServerUnregistered, event.ServerPayload{Name: srv.Name(), Address: srv.Address()})
		}
	}
}

// Logger ...
func (s *DefaultServer) Logger() internal.Logger {
	return s.log
}

// Secret ...
func (s *DefaultServer) Secret() string {
	return s.secret
}

// Clients ...
func (s *DefaultServer) Clients() (clients []*Client) {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()
	for _, client := range s.clients {
		clients = append(clients, client)
	}
	return
}

// Client ...
func (s *DefaultServer) Client(name string) (*Client, bool) {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()
	client, ok := s.clients[name]
	return client, ok
}

// Authenticate ...
func (s *DefaultServer) Authenticate(c *Client, name string) {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	delete(s.unconnectedClients, c.conn.RemoteAddr())
	s.clients[name] = c
	c.Authenticate(name)
}

// AuthBlocked ...
func (s *DefaultServer) AuthBlocked(addr net.Addr) bool {
	return s.authThrottle.Blocked(addr)
}

// RecordAuthFailure ...
func (s *DefaultServer) RecordAuthFailure(addr net.Addr) {
	s.authThrottle.RecordFailure(addr)
}

// SessionStore ...
func (s *DefaultServer) SessionStore() *session.Store {
	return s.sessionStore
}

// ServerRegistry ...
func (s *DefaultServer) ServerRegistry() *server.Registry {
	return s.serverRegistry
}

// Events ...
func (s *DefaultServer) Events() *event.Bus {
	return s.events
}

// Cluster ...
func (s *DefaultServer) Cluster() cluster.Backend {
	return s.cluster
}

// SetCluster sets the cross-proxy presence backend used to look up players connected to other proxies in
// the same cluster. Passing nil disables cluster lookups.
func (s *DefaultServer) SetCluster(c cluster.Backend) {
	s.cluster = c
}

// Close ...
func (s *DefaultServer) Close() error {
	if s.listener == nil {
		return nil
	}
	return s.listener.Close()
}

// containsAny checks if the string contains any of the provided sub strings.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}

	return false
}
