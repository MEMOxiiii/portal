// Command fixture is a minimal external plugin used only by extplugin's own tests, which build it with `go
// build` into a temporary directory and run it as a real subprocess. It is kept outside the extplugin
// package's import graph, in a "testdata" directory, so `go build ./...` and `go vet ./...` skip it.
package main

import (
	"encoding/json"

	"github.com/paroxity/portal/pluginsdk"
)

func main() {
	_ = pluginsdk.Serve(pluginsdk.Manifest{
		Name:    "fixture",
		Version: "1.0.0",
		Events:  []string{"player_join"},
	}, func(e pluginsdk.Event) {
		var payload struct{ Name string }
		if err := json.Unmarshal(e.Payload, &payload); err != nil {
			pluginsdk.Errorf("bad payload for %s: %v", e.Topic, err)
			return
		}
		pluginsdk.Infof("saw %s join as %s", e.Topic, payload.Name)
	})
}
