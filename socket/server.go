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
	"time"
)

// authTimeout bounds how long an accepted connection has to authenticate before it's dropped. A var, not a
// const, so tests can shrink it.
var authTimeout = 10 * time.Second

type Server interface {
	// Listen starts listening for connections on an address.
	Listen() error

	// Logger returns the logger attached to the socket server.
	Logger() internal.Logger

	// Secret returns the secret required for connections to authenticate.
	Secret() string

	// Clients returns all the clients that are connected to the socket server.
	Clients() []*Client
	// Client attempts to return a client from the provided name, case-insensitive.
	Client(name string) (*Client, bool)
	// TryAuthenticate atomically marks c as authenticated with the provided name (case-insensitive) and
	// returns true, unless a different client is already authenticated with that name, in which case it
	// returns false without changing anything.
	TryAuthenticate(c *Client, name string) bool

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

	_ = c.conn.SetDeadline(time.Now().Add(authTimeout))

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
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				s.log.Debugf("closing socket connection from %s: timed out waiting for authentication", c.conn.RemoteAddr())
				_ = c.Close()
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
			} else if c.Authenticated() {
				_ = c.conn.SetDeadline(time.Time{})
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
	name := c.Name()

	s.clientsMu.Lock()
	owned := name != "" && s.clients[strings.ToLower(name)] == c
	var srv *server.Server
	if owned {
		delete(s.clients, strings.ToLower(name))
		// Looked up here, not after unlocking below: while owned is true, no other connection could have
		// registered under name yet, so this is guaranteed to be c's own registration, not a replacement's.
		srv, _ = s.serverRegistry.Server(name)
	}
	delete(s.unconnectedClients, c.conn.RemoteAddr())
	s.clientsMu.Unlock()

	s.log.Debugf("socket connection \"%s\" closed", name)
	if srv == nil {
		return
	}

	s.serverRegistry.RemoveServer(srv)
	s.log.Debugf("removed server for socket connection \"%s\"", name)
	if s.events != nil {
		s.events.Publish(event.TopicServerUnregistered, event.ServerPayload{Name: srv.Name(), Address: srv.Address()})
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
	client, ok := s.clients[strings.ToLower(name)]
	return client, ok
}

// TryAuthenticate ...
func (s *DefaultServer) TryAuthenticate(c *Client, name string) bool {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	if _, ok := s.clients[strings.ToLower(name)]; ok {
		return false
	}
	delete(s.unconnectedClients, c.conn.RemoteAddr())
	s.clients[strings.ToLower(name)] = c
	c.Authenticate(name)
	return true
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
	s.authThrottle.Close()
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
