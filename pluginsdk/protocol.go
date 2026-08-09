package pluginsdk

import "encoding/json"

// HostMessage is sent from the proxy to a running external plugin, one JSON object per line on the
// plugin's standard input.
type HostMessage struct {
	// Type is "event", delivering a proxy event the plugin subscribed to via its Manifest, or "shutdown",
	// telling the plugin to exit.
	Type string `json:"type"`
	// Topic is the event topic, set when Type is "event". It matches one of the event.Topic* constants in
	// the proxy's event package, e.g. "player_join".
	Topic string `json:"topic,omitempty"`
	// Payload is the JSON encoding of the event's payload struct, set when Type is "event". Decode it
	// into a local struct declaring only the fields you need; see the event package for each topic's
	// payload shape.
	Payload json.RawMessage `json:"payload,omitempty"`
}

// GuestMessage is sent from a running external plugin to the proxy, one JSON object per line on the
// plugin's standard output.
type GuestMessage struct {
	// Type is "manifest", sent exactly once as the very first line a plugin writes, or "log".
	Type string `json:"type"`
	// Manifest describes the plugin. Set when Type is "manifest".
	Manifest *Manifest `json:"manifest,omitempty"`
	// Level is the log level: "debug", "info" or "error". Set when Type is "log".
	Level string `json:"level,omitempty"`
	// Message is the log line's text. Set when Type is "log".
	Message string `json:"message,omitempty"`
}
