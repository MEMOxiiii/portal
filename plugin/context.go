package plugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/paroxity/portal"
	"github.com/paroxity/portal/event"
	"github.com/paroxity/portal/internal"
	"github.com/paroxity/portal/session"
)

// Context is the handle a plugin is given onto the running proxy. It is passed to Load and Enable, and the
// same Context is used for both, so a plugin may keep a reference to it for the rest of its lifetime.
type Context struct {
	manager  *Manager
	manifest Manifest
	log      internal.Logger
	dataDir  string

	mu   sync.Mutex
	subs []*event.Subscription
}

// newContext creates the Context handed to the plugin described by manifest.
func newContext(m *Manager, manifest Manifest) *Context {
	return &Context{
		manager:  m,
		manifest: manifest,
		log:      prefixedLogger{Logger: m.log, prefix: "[" + manifest.Name + "] "},
		dataDir:  filepath.Join(m.dir, manifest.Name),
	}
}

// Manifest returns the manifest the plugin was loaded with.
func (c *Context) Manifest() Manifest {
	return c.manifest
}

// Portal returns the running proxy. It is the entry point to everything the proxy exposes, such as the
// session store, the server registry and the routing policies.
func (c *Context) Portal() *portal.Portal {
	return c.manager.portal
}

// Log returns a logger that prefixes every line with the plugin's name.
func (c *Context) Log() internal.Logger {
	return c.log
}

// DataDir returns the directory the plugin may store its files in, creating it if it does not exist yet.
// It is named after the plugin and lives inside the proxy's plugin directory.
func (c *Context) DataDir() (string, error) {
	if err := os.MkdirAll(c.dataDir, 0o755); err != nil {
		return "", err
	}
	return c.dataDir, nil
}

// Config decodes the plugin's "config.json", from the directory returned by DataDir, into dst. If the file
// does not exist yet it is created from the current contents of dst, which lets a plugin populate dst with
// its defaults, call Config once, and have those defaults written out for the operator to edit:
//
//	conf := Config{Message: "hello"}
//	if err := ctx.Config(&conf); err != nil {
//		return err
//	}
//
// dst must be a pointer.
func (c *Context) Config(dst any) error {
	dir, err := c.DataDir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "config.json")

	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		// HTML escaping is turned off so that values an operator is meant to read and edit, such as the
		// angle brackets in Minecraft colour codes, are written out literally.
		var defaults bytes.Buffer
		enc := json.NewEncoder(&defaults)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "\t")
		if err := enc.Encode(dst); err != nil {
			return err
		}
		return os.WriteFile(path, defaults.Bytes(), 0o644)
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dst)
}

// Events returns the proxy-wide event bus. Handlers registered directly on the bus are not released when
// the plugin is disabled; prefer Subscribe, which tracks them, unless the handler is meant to outlive the
// plugin.
func (c *Context) Events() *event.Bus {
	return c.manager.portal.Events()
}

// Subscribe registers fn to be called every time an event is published under topic, in the same way as
// (*event.Bus).Subscribe, with two differences: a panic in fn is reported against this plugin rather than
// anonymously, and the subscription is released automatically when the plugin is disabled. The returned
// Subscription may be used to release it earlier, and may otherwise be ignored.
func (c *Context) Subscribe(topic string, fn func(payload any)) *event.Subscription {
	sub := c.Events().Subscribe(topic, func(payload any) {
		defer func() {
			if r := recover(); r != nil {
				c.log.Errorf("handler for event %q panicked: %v", topic, r)
			}
		}()
		fn(payload)
	})

	c.mu.Lock()
	defer c.mu.Unlock()
	c.subs = append(c.subs, sub)
	return sub
}

// SetLoadBalancer replaces the policy deciding which server a player is sent to when they first join. It
// is shorthand for ctx.Portal().SetLoadBalancer, and is best called from Load so that it is in place before
// the first player connects.
func (c *Context) SetLoadBalancer(balancer session.LoadBalancer) {
	c.Portal().SetLoadBalancer(balancer)
}

// SetWhitelist replaces the policy deciding which players may join the proxy. To add a rule instead of
// replacing the operator's configured whitelist, wrap the existing one:
//
//	previous := ctx.Portal().Whitelist()
//	ctx.SetWhitelist(myWhitelist{next: previous})
func (c *Context) SetWhitelist(whitelist session.Whitelist) {
	c.Portal().SetWhitelist(whitelist)
}

// SetIPGuard replaces the policy rejecting connections before they reach the whitelist or game-layer
// authentication. As with SetWhitelist, wrap the existing guard to add a rule rather than replace it.
func (c *Context) SetIPGuard(guard session.IPGuard) {
	c.Portal().SetIPGuard(guard)
}

// Plugin returns another loaded plugin by name, which is how a plugin reaches an API exposed by one of its
// dependencies. The result should be type asserted to the interface that plugin documents. It reports
// false if no plugin with that name is loaded, which is the expected outcome for a soft dependency.
func (c *Context) Plugin(name string) (Plugin, bool) {
	return c.manager.Plugin(name)
}

// release removes every subscription the plugin made through Subscribe. It is called by the Manager when
// the plugin is disabled or fails to start.
func (c *Context) release() {
	c.mu.Lock()
	subs := c.subs
	c.subs = nil
	c.mu.Unlock()

	for _, sub := range subs {
		sub.Unsubscribe()
	}
}

// prefixedLogger attributes log lines to a plugin by prefixing them with its name. Plugin names are
// validated to contain no formatting verbs, so the prefix is safe to concatenate onto a format string.
type prefixedLogger struct {
	internal.Logger
	prefix string
}

// Debugf ...
func (l prefixedLogger) Debugf(format string, v ...any) { l.Logger.Debugf(l.prefix+format, v...) }

// Infof ...
func (l prefixedLogger) Infof(format string, v ...any) { l.Logger.Infof(l.prefix+format, v...) }

// Errorf ...
func (l prefixedLogger) Errorf(format string, v ...any) { l.Logger.Errorf(l.prefix+format, v...) }

// Fatalf ...
func (l prefixedLogger) Fatalf(format string, v ...any) { l.Logger.Fatalf(l.prefix+format, v...) }
