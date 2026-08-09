package plugin

import (
	"fmt"
	"sync"
)

var (
	registryMu sync.RWMutex
	registry   []Plugin
	registered = map[string]struct{}{}
)

// Register adds p to the global plugin registry, from which a Manager loads the plugins it runs. It is
// meant to be called from an init function in the plugin's own package, so that importing that package is
// all a proxy binary has to do to gain the plugin:
//
//	func init() { plugin.Register(&MyPlugin{}) }
//
// Register panics if the plugin's manifest is invalid or if a plugin with the same name was already
// registered, as both indicate a mistake in how the binary was assembled that cannot be recovered from at
// runtime.
func Register(p Plugin) {
	if p == nil {
		panic("plugin: Register called with a nil plugin")
	}
	manifest := p.Manifest()
	if err := manifest.Validate(); err != nil {
		panic(fmt.Sprintf("plugin: %v", err))
	}

	registryMu.Lock()
	defer registryMu.Unlock()

	if _, ok := registered[manifest.Name]; ok {
		panic(fmt.Sprintf("plugin: a plugin named %q is already registered", manifest.Name))
	}
	registered[manifest.Name] = struct{}{}
	registry = append(registry, p)
}

// Registered returns every plugin added with Register, in registration order. The returned slice is a copy
// and may be modified freely by the caller.
func Registered() []Plugin {
	registryMu.RLock()
	defer registryMu.RUnlock()

	return append([]Plugin(nil), registry...)
}
