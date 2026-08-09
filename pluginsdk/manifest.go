package pluginsdk

import (
	"fmt"
	"regexp"
)

// Manifest describes an external plugin to the proxy. It is sent once, as the very first line the plugin
// writes to its standard output, when the proxy spawns it.
type Manifest struct {
	// Name uniquely identifies the plugin among the other external plugins running on the proxy. It is
	// used for logging, and may only contain letters, digits, underscores and hyphens.
	Name string `json:"name"`
	// Version is the plugin's version, shown when it is loaded. It is not interpreted by the proxy.
	Version string `json:"version,omitempty"`
	// Description is a short, human readable summary of what the plugin does.
	Description string `json:"description,omitempty"`
	// Author names who wrote the plugin.
	Author string `json:"author,omitempty"`
	// Events lists the topics, from the proxy's event package (e.g. "player_join"), the plugin wants
	// delivered to its handler. Subscribing to a topic the plugin never acts on is harmless, but wastes
	// the round trip to the plugin process for every occurrence.
	Events []string `json:"events,omitempty"`
}

var namePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// Validate returns an error if the manifest could not be used to run a plugin.
func (m Manifest) Validate() error {
	if m.Name == "" {
		return fmt.Errorf("plugin name must not be empty")
	}
	if !namePattern.MatchString(m.Name) {
		return fmt.Errorf("plugin name %q must be 1-64 characters of letters, digits, underscores or hyphens", m.Name)
	}
	return nil
}
