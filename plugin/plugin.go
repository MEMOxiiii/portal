// Package plugin provides Portal's plugin system. A plugin is a Go type that implements Plugin and
// registers itself with Register from an init function; the proxy binary picks it up by importing the
// package it lives in. Plugins are given access to the running proxy through a Context, which they can use
// to subscribe to proxy-wide events and to replace the routing policies that decide which players may join
// and which server they land on.
//
// A minimal plugin looks like this:
//
//	package hello
//
//	import "github.com/paroxity/portal/plugin"
//
//	func init() { plugin.Register(&Hello{}) }
//
//	type Hello struct{ plugin.Base }
//
//	func (*Hello) Manifest() plugin.Manifest {
//		return plugin.Manifest{Name: "hello", Version: "1.0.0"}
//	}
//
//	func (h *Hello) Enable(ctx *plugin.Context) error {
//		ctx.Log().Infof("hello from a plugin")
//		return nil
//	}
package plugin

import (
	"fmt"
	"regexp"
)

// Plugin is the interface implemented by every Portal plugin. Embedding Base provides no-op implementations
// of every method except Manifest, so a plugin only has to implement the parts of the lifecycle it needs.
type Plugin interface {
	// Manifest returns the metadata describing the plugin. It is called before the plugin is loaded and
	// must not depend on any state set up by Load or Enable.
	Manifest() Manifest
	// Load is called once, before the proxy starts listening for players. It is the point at which a
	// plugin should read its configuration and install routing policies, as those decide how the very
	// first player to connect is handled. Returning an error causes the plugin to be skipped, along with
	// any plugin that hard-depends on it.
	Load(ctx *Context) error
	// Enable is called once every other subsystem is running, but before the proxy accepts its first
	// player. It is the point at which a plugin should subscribe to events and start any background work.
	// Returning an error releases the plugin's tracked subscriptions and leaves it disabled, but does not
	// stop the proxy or the plugins depending on it.
	Enable(ctx *Context) error
	// Disable is called when the proxy shuts down, in the reverse of load order, and should release
	// anything the plugin acquired. Subscriptions made through the Context are released automatically and
	// do not need to be undone here.
	Disable() error
}

// Manifest describes a plugin to the proxy. Only Name is required.
type Manifest struct {
	// Name uniquely identifies the plugin. It is used for logging, for the plugin's data directory, and
	// as the value other plugins refer to in Depends and SoftDepends, so it may only contain letters,
	// digits, underscores and hyphens.
	Name string
	// Version is the plugin's version, shown when it is loaded. It is not interpreted by the proxy.
	Version string
	// Description is a short, human readable summary of what the plugin does.
	Description string
	// Authors names the people who wrote the plugin.
	Authors []string
	// Depends lists the names of plugins that must be loaded before this one. If any of them is missing,
	// disabled, or fails to load, this plugin is skipped as well.
	Depends []string
	// SoftDepends lists the names of plugins that should be loaded before this one if they are present.
	// Unlike Depends, a missing soft dependency is not an error, which makes it the right choice for
	// optional integration with another plugin.
	SoftDepends []string
}

// namePattern matches the plugin names the proxy accepts. It deliberately excludes path separators and
// formatting verbs, as names are used both as path elements and as log prefixes.
var namePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// Validate returns an error if the manifest could not be used to load a plugin.
func (m Manifest) Validate() error {
	if m.Name == "" {
		return fmt.Errorf("plugin name must not be empty")
	}
	if !namePattern.MatchString(m.Name) {
		return fmt.Errorf("plugin name %q must be 1-64 characters of letters, digits, underscores or hyphens", m.Name)
	}
	for _, dep := range m.Depends {
		if dep == m.Name {
			return fmt.Errorf("plugin %q depends on itself", m.Name)
		}
	}
	return nil
}

// Base provides no-op implementations of every Plugin method except Manifest. Plugins may embed it to
// avoid having to implement the parts of the lifecycle they do not use.
type Base struct{}

// Load ...
func (Base) Load(*Context) error { return nil }

// Enable ...
func (Base) Enable(*Context) error { return nil }

// Disable ...
func (Base) Disable() error { return nil }
