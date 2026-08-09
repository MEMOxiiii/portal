package portal

import (
	"fmt"
	"sync"

	"github.com/paroxity/portal/event"
	"github.com/paroxity/portal/internal"
	"github.com/paroxity/portal/server"
	"github.com/paroxity/portal/session"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sirupsen/logrus"
)

// Portal represents the proxy and controls its functionality.
type Portal struct {
	log internal.Logger

	address      string
	listenConfig minecraft.ListenConfig
	listener     *minecraft.Listener

	sessionStore   *session.Store
	serverRegistry *server.Registry
	events         *event.Bus

	// routingMu guards the routing policies below. They may be replaced at any point during the proxy's
	// lifetime, including by plugins reacting to a command or a configuration reload, while the accept
	// loop is concurrently reading them.
	routingMu    sync.RWMutex
	loadBalancer session.LoadBalancer
	whitelist    session.Whitelist
	ipGuard      session.IPGuard
}

// New instantiates portal using the provided options and returns it. If some options are not set, default
// values will be used in replacement.
func New(opts Options) *Portal {
	if opts.Logger == nil {
		opts.Logger = logrus.New()
	}
	serverRegistry := server.NewDefaultRegistry()
	if opts.LoadBalancer == nil {
		opts.LoadBalancer = session.NewSplitLoadBalancer(serverRegistry)
	}
	if opts.Whitelist == nil {
		opts.Whitelist = session.NewSimpleWhitelist(false, []string{})
	}
	if opts.IPGuard == nil {
		opts.IPGuard = session.NopIPGuard{}
	}
	return &Portal{
		log: opts.Logger,

		address:      opts.Address,
		listenConfig: opts.ListenConfig,

		sessionStore:   session.NewDefaultStore(),
		serverRegistry: serverRegistry,
		loadBalancer:   opts.LoadBalancer,
		whitelist:      opts.Whitelist,
		ipGuard:        opts.IPGuard,
		events:         event.NewBus(),
	}
}

// Events returns the proxy-wide event bus, which can be used to subscribe to occurrences such as players
// joining/quitting, servers registering, and transfers completing, without needing to fork the proxy.
func (p *Portal) Events() *event.Bus {
	return p.events
}

// Logger returns the global logger used by the proxy.
func (p *Portal) Logger() internal.Logger {
	return p.log
}

// SessionStore returns the session store provided to portal. It is used to store all the open sessions.
func (p *Portal) SessionStore() *session.Store {
	return p.sessionStore
}

// ServerRegistry returns the server registry provided to portal. It is used to store all the available servers.
func (p *Portal) ServerRegistry() *server.Registry {
	return p.serverRegistry
}

// LoadBalancer returns the load balancer that handles the server a player joins when they first connect to the proxy.
func (p *Portal) LoadBalancer() session.LoadBalancer {
	p.routingMu.RLock()
	defer p.routingMu.RUnlock()
	return p.loadBalancer
}

// SetLoadBalancer sets the load balancer that handles the server a player joins when they first connect to the proxy.
// A nil load balancer is ignored, as the proxy would have no way to route new players without one.
func (p *Portal) SetLoadBalancer(loadBalancer session.LoadBalancer) {
	if loadBalancer == nil {
		return
	}
	p.routingMu.Lock()
	defer p.routingMu.Unlock()
	p.loadBalancer = loadBalancer
}

// Whitelist returns the whitelist used to decide which players are allowed to join the proxy. Callers that
// want to add a rule rather than replace the existing policy should wrap the value returned here and pass
// the wrapper to SetWhitelist.
func (p *Portal) Whitelist() session.Whitelist {
	p.routingMu.RLock()
	defer p.routingMu.RUnlock()
	return p.whitelist
}

// SetWhitelist sets the whitelist used to decide which players are allowed to join the proxy. A nil
// whitelist is ignored; use session.NewSimpleWhitelist(false, nil) to allow every player instead.
func (p *Portal) SetWhitelist(whitelist session.Whitelist) {
	if whitelist == nil {
		return
	}
	p.routingMu.Lock()
	defer p.routingMu.Unlock()
	p.whitelist = whitelist
}

// IPGuard returns the IP guard used to reject connections before they reach the whitelist or game-layer
// authentication. Callers that want to add a rule rather than replace the existing policy should wrap the
// value returned here and pass the wrapper to SetIPGuard.
func (p *Portal) IPGuard() session.IPGuard {
	p.routingMu.RLock()
	defer p.routingMu.RUnlock()
	return p.ipGuard
}

// SetIPGuard sets the IP guard used to reject connections before they reach the whitelist or game-layer
// authentication. A nil guard is ignored; use session.NopIPGuard{} to allow every connection instead.
func (p *Portal) SetIPGuard(ipGuard session.IPGuard) {
	if ipGuard == nil {
		return
	}
	p.routingMu.Lock()
	defer p.routingMu.Unlock()
	p.ipGuard = ipGuard
}

// Listen starts to listen on the set address and allows connections from minecraft clients. An error is
// returned if the listener failed to listen.
func (p *Portal) Listen() error {
	l, err := p.listenConfig.Listen("raknet", p.address)
	if err != nil {
		return err
	}
	p.listener = l
	return nil
}

// Accept accepts a fully connected (on Minecraft layer) connection which is ready to receive and send packets. If the
// listener is closed or the player failed to spawn in then an error will be returned. When an error is returned the
// session is also returned, but it may be incomplete and contain nil values.
func (p *Portal) Accept() (*session.Session, error) {
	p.Logger().Debugf("waiting to accept...")
	if p.listener == nil {
		return nil, fmt.Errorf("no active listener")
	}
	conn, err := p.listener.Accept()
	p.Logger().Debugf("accepted connection")
	if err != nil {
		return nil, err
	}
	c := conn.(*minecraft.Conn)
	if ok, m := p.IPGuard().Allow(c.RemoteAddr()); !ok {
		_ = p.Disconnect(c, m)
		return nil, fmt.Errorf("connection rejected by IP guard: %s", m)
	}
	if ok, m := p.Whitelist().Authorize(c); !ok {
		_ = p.Disconnect(c, m)
		return nil, fmt.Errorf("player is not whitelisted: %s", m)
	}
	return session.New(c, p.sessionStore, p.LoadBalancer(), p.log, p.events)
}

// Disconnect disconnects a Minecraft Conn passed by first sending a disconnect with the message passed, and
// closing the connection after. If the message passed is empty, the client will be immediately sent to the
// player list instead of a disconnect screen.
func (p *Portal) Disconnect(conn *minecraft.Conn, message string) error {
	if p.listener == nil {
		return fmt.Errorf("no listener to disconnect connection")
	}
	return p.listener.Disconnect(conn, message)
}

// Close closes the proxy's listener, preventing it from accepting any further player connections. It does
// not close any of the sessions already connected; callers are expected to disconnect them beforehand.
func (p *Portal) Close() error {
	if p.listener == nil {
		return nil
	}
	return p.listener.Close()
}
