package plugin

import (
	"fmt"

	"github.com/paroxity/portal"
	"github.com/paroxity/portal/event"
)

// exampleGreeter is a minimal Plugin that greets a player when they join. It only implements Enable;
// embedding Base supplies no-op Load and Disable. A real plugin registers itself from an init function in
// its own package with Register(&exampleGreeter{}) instead of being wired in explicitly the way Example
// does below, which is done purely to keep this example self-contained and runnable on its own.
type exampleGreeter struct {
	Base
}

// Manifest ...
func (*exampleGreeter) Manifest() Manifest {
	return Manifest{Name: "greeter", Version: "1.0.0"}
}

// Enable ...
func (*exampleGreeter) Enable(ctx *Context) error {
	ctx.Subscribe(event.TopicPlayerJoin, func(payload any) {
		fmt.Printf("%s joined\n", payload.(event.PlayerPayload).Name)
	})
	return nil
}

// Example shows a plugin's full lifecycle against a running Portal: loading it, enabling it, having it
// react to a published event, and disabling it again.
func Example() {
	p := portal.New(portal.Options{Logger: exampleLogger{}})

	m := NewManager(p, "", exampleLogger{}, nil)
	m.source = func() []Plugin { return []Plugin{&exampleGreeter{}} }

	if err := m.Load(); err != nil {
		fmt.Println(err)
		return
	}
	m.Enable()
	defer m.Disable()

	p.Events().Publish(event.TopicPlayerJoin, event.PlayerPayload{Name: "Steve"})

	// Output:
	// Steve joined
}

// exampleLogger discards every log line, keeping Example's output limited to what the plugin itself
// prints, since testable examples are matched against their output verbatim.
type exampleLogger struct{}

// Debugf ...
func (exampleLogger) Debugf(string, ...any) {}

// Infof ...
func (exampleLogger) Infof(string, ...any) {}

// Errorf ...
func (exampleLogger) Errorf(string, ...any) {}

// Fatalf ...
func (exampleLogger) Fatalf(string, ...any) {}
