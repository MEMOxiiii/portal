package plugin

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/paroxity/portal"
	"github.com/paroxity/portal/internal"
)

// Manager loads, starts and stops the plugins registered with Register for a single proxy. A proxy is
// expected to create one, call Load before it starts listening, Enable once the rest of its subsystems are
// running, and Disable during shutdown.
type Manager struct {
	portal *portal.Portal
	log    internal.Logger
	dir    string

	disabled map[string]struct{}

	// source returns the plugins the Manager should load. It is the global registry in every case except
	// the package's own tests, which cannot use a process-wide registry.
	source func() []Plugin

	// opMu serialises Load, Enable and Disable against each other. It is deliberately not held while a
	// plugin's own methods run, so that a plugin is free to call back into the Manager, which is exactly
	// what a plugin reaching one of its dependencies through (*Context).Plugin does.
	opMu sync.Mutex

	// mu guards the fields below, and is only ever held for the length of a map or slice access.
	mu      sync.RWMutex
	loaded  bool
	order   []string
	plugins map[string]*loadedPlugin
}

// loadedPlugin tracks a plugin the Manager successfully loaded.
type loadedPlugin struct {
	plugin  Plugin
	ctx     *Context
	name    string
	enabled bool
}

// NewManager creates a Manager that runs the registered plugins against p. Plugins keep their data in
// subdirectories of dir, and the names in disabled are skipped entirely, as though they had never been
// registered.
func NewManager(p *portal.Portal, dir string, log internal.Logger, disabled []string) *Manager {
	if dir == "" {
		dir = "plugins"
	}
	skip := make(map[string]struct{}, len(disabled))
	for _, name := range disabled {
		skip[name] = struct{}{}
	}
	return &Manager{
		portal:   p,
		log:      log,
		dir:      dir,
		disabled: skip,
		source:   Registered,
		plugins:  make(map[string]*loadedPlugin),
	}
}

// Load resolves the load order of every registered plugin and calls Load on each of them. A plugin that
// cannot be ordered, because a dependency is missing or because it takes part in a dependency cycle, and a
// plugin whose own Load fails, are both reported and skipped along with anything that hard-depends on
// them; neither stops the remaining plugins from loading. An error is only returned if Load is called more
// than once.
func (m *Manager) Load() error {
	m.opMu.Lock()
	defer m.opMu.Unlock()

	m.mu.Lock()
	if m.loaded {
		m.mu.Unlock()
		return errors.New("plugin: plugins have already been loaded")
	}
	m.loaded = true
	m.mu.Unlock()

	available := make(map[string]Plugin)
	for _, p := range m.source() {
		name := p.Manifest().Name
		if _, ok := m.disabled[name]; ok {
			m.log.Infof("plugin %s is disabled by the configuration, skipping", name)
			continue
		}
		available[name] = p
	}
	if len(available) == 0 {
		return nil
	}

	order, skipped := resolveOrder(available)
	for _, name := range sortedNames(skipped) {
		m.log.Errorf("plugin %s could not be loaded: %v", name, skipped[name])
	}

	for _, name := range order {
		p := available[name]
		manifest := p.Manifest()
		if dep, ok := m.unmetDependency(manifest); ok {
			m.log.Errorf("plugin %s could not be loaded: dependency %s failed to load", name, dep)
			continue
		}

		ctx := newContext(m, manifest)
		if err := call(func() error { return p.Load(ctx) }); err != nil {
			m.log.Errorf("plugin %s failed to load: %v", name, err)
			ctx.release()
			continue
		}

		m.mu.Lock()
		m.plugins[name] = &loadedPlugin{plugin: p, ctx: ctx, name: name}
		m.order = append(m.order, name)
		m.mu.Unlock()

		m.log.Infof("loaded plugin %s", describe(manifest))
	}
	return nil
}

// Enable calls Enable on every loaded plugin, in load order. A plugin whose Enable fails has its tracked
// subscriptions released and is left disabled, but the plugins that depend on it are still enabled, so a
// plugin exposing an API to others should make that API safe to call after a failed Enable.
func (m *Manager) Enable() {
	m.opMu.Lock()
	defer m.opMu.Unlock()

	for _, l := range m.loadedInOrder() {
		if m.Enabled(l.name) {
			continue
		}
		if err := call(func() error { return l.plugin.Enable(l.ctx) }); err != nil {
			m.log.Errorf("plugin %s failed to enable: %v", l.name, err)
			l.ctx.release()
			continue
		}
		m.setEnabled(l.name, true)
		m.log.Debugf("enabled plugin %s", l.name)
	}
}

// Disable calls Disable on every enabled plugin in the reverse of load order, so that a plugin is always
// stopped before the plugins it depends on, and releases the event subscriptions each of them made through
// its Context.
func (m *Manager) Disable() {
	m.opMu.Lock()
	defer m.opMu.Unlock()

	loaded := m.loadedInOrder()
	for i := len(loaded) - 1; i >= 0; i-- {
		l := loaded[i]
		if m.Enabled(l.name) {
			if err := call(l.plugin.Disable); err != nil {
				m.log.Errorf("plugin %s failed to disable cleanly: %v", l.name, err)
			}
			m.setEnabled(l.name, false)
		}
		l.ctx.release()
	}
}

// Plugin returns the loaded plugin with the given name. It reports false if no plugin with that name was
// loaded, either because it is not registered, is disabled, or failed to load.
func (m *Manager) Plugin(name string) (Plugin, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	l, ok := m.plugins[name]
	if !ok {
		return nil, false
	}
	return l.plugin, true
}

// Plugins returns every successfully loaded plugin, in load order.
func (m *Manager) Plugins() []Plugin {
	loaded := m.loadedInOrder()
	plugins := make([]Plugin, 0, len(loaded))
	for _, l := range loaded {
		plugins = append(plugins, l.plugin)
	}
	return plugins
}

// Enabled reports whether the named plugin is currently enabled.
func (m *Manager) Enabled(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	l, ok := m.plugins[name]
	return ok && l.enabled
}

// loadedInOrder returns the plugins that loaded successfully, in load order.
func (m *Manager) loadedInOrder() []*loadedPlugin {
	m.mu.RLock()
	defer m.mu.RUnlock()

	loaded := make([]*loadedPlugin, 0, len(m.order))
	for _, name := range m.order {
		loaded = append(loaded, m.plugins[name])
	}
	return loaded
}

// setEnabled records whether the named plugin is currently enabled.
func (m *Manager) setEnabled(name string, enabled bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if l, ok := m.plugins[name]; ok {
		l.enabled = enabled
	}
}

// unmetDependency returns the name of the first hard dependency of manifest that is not loaded. Because
// plugins are loaded in dependency order, a dependency that is absent by the time its dependent is reached
// is one that failed to load.
func (m *Manager) unmetDependency(manifest Manifest) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, dep := range manifest.Depends {
		if _, ok := m.plugins[dep]; !ok {
			return dep, true
		}
	}
	return "", false
}

// resolveOrder sorts the plugins in available so that each appears after the plugins it depends on, and
// returns that order alongside the plugins that had to be left out and the reason why. Both hard and soft
// dependencies affect the order; only a hard dependency can leave a plugin out.
func resolveOrder(available map[string]Plugin) (order []string, skipped map[string]error) {
	skipped = make(map[string]error)

	// Repeatedly drop plugins with an unsatisfied hard dependency, so that a plugin depending on a dropped
	// plugin is dropped in turn.
	viable := make(map[string]Plugin, len(available))
	for name, p := range available {
		viable[name] = p
	}
	for dropped := true; dropped; {
		dropped = false
		for _, name := range sortedNames(viable) {
			for _, dep := range viable[name].Manifest().Depends {
				if _, ok := viable[dep]; ok {
					continue
				}
				if _, ok := available[dep]; ok {
					skipped[name] = fmt.Errorf("dependency %q could not be loaded", dep)
				} else {
					skipped[name] = fmt.Errorf("dependency %q is not registered or is disabled", dep)
				}
				delete(viable, name)
				dropped = true
				break
			}
		}
	}

	// Kahn's algorithm over the dependency edges between the remaining plugins. The frontier is kept
	// sorted so that plugins with no dependency relation between them always load in the same order.
	dependents := make(map[string][]string, len(viable))
	remaining := make(map[string]int, len(viable))
	for _, name := range sortedNames(viable) {
		manifest := viable[name].Manifest()
		seen := make(map[string]bool)
		for _, dep := range append(append([]string{}, manifest.Depends...), manifest.SoftDepends...) {
			if dep == name || seen[dep] {
				continue
			}
			if _, ok := viable[dep]; !ok {
				continue
			}
			seen[dep] = true
			dependents[dep] = append(dependents[dep], name)
			remaining[name]++
		}
	}

	var frontier []string
	for _, name := range sortedNames(viable) {
		if remaining[name] == 0 {
			frontier = append(frontier, name)
		}
	}
	for len(frontier) > 0 {
		sort.Strings(frontier)
		name := frontier[0]
		frontier = frontier[1:]
		order = append(order, name)

		for _, dependent := range dependents[name] {
			remaining[dependent]--
			if remaining[dependent] == 0 {
				frontier = append(frontier, dependent)
			}
		}
	}

	// Anything the sort could not reach takes part in a dependency cycle, or depends on one.
	var cyclic []string
	for _, name := range sortedNames(viable) {
		if remaining[name] > 0 {
			cyclic = append(cyclic, name)
		}
	}
	for _, name := range cyclic {
		skipped[name] = fmt.Errorf("part of a dependency cycle between: %s", strings.Join(cyclic, ", "))
	}
	return order, skipped
}

// call runs fn, converting a panic into an error so that a misbehaving plugin cannot take the proxy down
// with it.
func call(fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return fn()
}

// describe renders a manifest for a log line.
func describe(m Manifest) string {
	var b strings.Builder
	b.WriteString(m.Name)
	if m.Version != "" {
		b.WriteString(" v")
		b.WriteString(m.Version)
	}
	if len(m.Authors) > 0 {
		b.WriteString(" by ")
		b.WriteString(strings.Join(m.Authors, ", "))
	}
	return b.String()
}

// sortedNames returns the keys of m in ascending order, so that iteration over a plugin map is
// deterministic.
func sortedNames[V any](m map[string]V) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
