// Command echo is a worked example of a Portal *external* plugin: a standalone binary that is dropped into
// the proxy's plugins directory as "echo.portalplugin" (or "echo.portalplugin.exe" on Windows) and picked
// up at startup without ever rebuilding, or even having imported, the proxy binary itself.
//
// Build it and drop it into a running proxy's plugins directory with:
//
//	go build -o plugins/echo.portalplugin ./examples/plugins/echo
//
// then set "plugins.external.enabled" to true in config.json and (re)start the proxy; look for "loaded
// external plugin echo" in its log output.
package main

import (
	"encoding/json"

	"github.com/paroxity/portal/pluginsdk"
)

func main() {
	manifest := pluginsdk.Manifest{
		Name:        "echo",
		Version:     "1.0.0",
		Description: "Logs player joins, quits and transfers from outside the proxy binary.",
		Author:      "Portal",
		Events:      []string{"player_join", "player_quit", "transfer"},
	}

	_ = pluginsdk.Serve(manifest, func(e pluginsdk.Event) {
		switch e.Topic {
		case "player_join":
			var payload struct{ Name string }
			if err := json.Unmarshal(e.Payload, &payload); err == nil {
				pluginsdk.Infof("%s joined", payload.Name)
			}
		case "player_quit":
			var payload struct{ Name string }
			if err := json.Unmarshal(e.Payload, &payload); err == nil {
				pluginsdk.Infof("%s quit", payload.Name)
			}
		case "transfer":
			var payload struct{ PlayerName, FromServer, ToServer string }
			if err := json.Unmarshal(e.Payload, &payload); err == nil {
				pluginsdk.Infof("%s moved from %s to %s", payload.PlayerName, payload.FromServer, payload.ToServer)
			}
		}
	})
}
