package plugin

import (
	"testing"

	"github.com/paroxity/portal"
)

// registryPlugin is registered with the process-wide registry in this file's init, so that the default
// path a real proxy takes, Register followed by a Manager reading Registered, is covered rather than only
// the injected source the other tests use.
type registryPlugin struct {
	Base
	loaded bool
}

func (p *registryPlugin) Manifest() Manifest {
	return Manifest{Name: "registry-test", Version: "1.0.0"}
}

func (p *registryPlugin) Load(*Context) error {
	p.loaded = true
	return nil
}

var globalPlugin = &registryPlugin{}

func init() { Register(globalPlugin) }

func TestRegisteredPluginsAreLoadedByDefault(t *testing.T) {
	log := &testLogger{t: t}
	m := NewManager(portal.New(portal.Options{Logger: log}), t.TempDir(), log, nil)

	if err := m.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !globalPlugin.loaded {
		t.Fatal("a plugin added with Register was not loaded by a default Manager")
	}
	if _, ok := m.Plugin("registry-test"); !ok {
		t.Error("registered plugin is not reachable by name")
	}
}

func TestRegisterRejectsDuplicateName(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("registering two plugins with the same name should panic")
		}
	}()
	Register(&registryPlugin{})
}

func TestRegisterRejectsInvalidManifest(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("registering a plugin with an invalid manifest should panic")
		}
	}()
	Register(invalidPlugin{})
}

type invalidPlugin struct{ Base }

func (invalidPlugin) Manifest() Manifest { return Manifest{Name: "../escape"} }
