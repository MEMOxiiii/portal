// Package pluginsdk is used to write Portal "external" plugins: standalone executables that are dropped
// into the proxy's plugins directory as a single file named "<name>.portalplugin" (or
// "<name>.portalplugin.exe" on Windows) and are discovered and run without ever rebuilding, or even
// restarting the source of, the proxy itself — only the plugin binary needs to be rebuilt and replaced.
//
// A plugin using this package only has to call Serve from its main function:
//
//	func main() {
//		pluginsdk.Serve(pluginsdk.Manifest{
//			Name:    "echo",
//			Version: "1.0.0",
//			Events:  []string{"player_join"},
//		}, func(e pluginsdk.Event) {
//			var payload struct{ Name string }
//			if err := json.Unmarshal(e.Payload, &payload); err == nil {
//				pluginsdk.Infof("%s joined", payload.Name)
//			}
//		})
//	}
//
// Build it with `go build -o plugins/echo.portalplugin` (`plugins\echo.portalplugin.exe` on Windows) and
// drop the resulting binary into the proxy's plugins directory.
//
// See the plugin package instead for plugins that are compiled directly into the proxy binary, which get
// full Go-level access to the running Portal rather than the JSON event stream external plugins receive.
package pluginsdk

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
)

// Event is a proxy event delivered to a plugin's handler.
type Event struct {
	// Topic is the event topic, e.g. "player_join". See the proxy's event package for every topic and the
	// shape of its payload.
	Topic string
	// Payload is the raw JSON encoding of the event's payload struct. Decode it with json.Unmarshal into
	// a local struct declaring only the fields you need.
	Payload json.RawMessage
}

// Serve announces manifest to the proxy and then calls handler for every event published under a topic
// listed in manifest.Events, until the proxy asks the plugin to shut down or its standard input is closed.
// It blocks for the lifetime of the plugin and is meant to be the last call in main. A panic inside
// handler is recovered and reported to the proxy as an error log line rather than crashing the plugin.
func Serve(manifest Manifest, handler func(Event)) error {
	if err := manifest.Validate(); err != nil {
		return err
	}
	return serve(os.Stdin, os.Stdout, manifest, handler)
}

func serve(r io.Reader, w io.Writer, manifest Manifest, handler func(Event)) error {
	if err := writeMessage(w, GuestMessage{Type: "manifest", Manifest: &manifest}); err != nil {
		return fmt.Errorf("pluginsdk: announcing manifest: %w", err)
	}

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		var msg HostMessage
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			continue
		}
		switch msg.Type {
		case "event":
			dispatch(w, handler, Event{Topic: msg.Topic, Payload: msg.Payload})
		case "shutdown":
			return nil
		}
	}
	return scanner.Err()
}

// dispatch runs handler for e, converting a panic into an error log line sent back to the proxy so that a
// bug in one event handler cannot take the whole plugin process down.
func dispatch(w io.Writer, handler func(Event), e Event) {
	defer func() {
		if r := recover(); r != nil {
			_ = writeMessage(w, GuestMessage{Type: "log", Level: "error", Message: fmt.Sprintf("handler for event %q panicked: %v", e.Topic, r)})
		}
	}()
	handler(e)
}

// Logf sends a log line to the proxy, which prefixes it with the plugin's name the same way it does for
// plugins compiled into the proxy binary. level should be "debug", "info" or "error"; see Debugf, Infof
// and Errorf for shorthands.
func Logf(level, format string, args ...any) {
	_ = writeMessage(os.Stdout, GuestMessage{Type: "log", Level: level, Message: fmt.Sprintf(format, args...)})
}

// Debugf logs a line at debug level.
func Debugf(format string, args ...any) { Logf("debug", format, args...) }

// Infof logs a line at info level.
func Infof(format string, args ...any) { Logf("info", format, args...) }

// Errorf logs a line at error level.
func Errorf(format string, args ...any) { Logf("error", format, args...) }

// stdoutMu serialises writes to standard output. serve's own loop and a handler calling Logf from a
// goroutine it started both write to the same stream, and an interleaved write would corrupt the
// line-delimited JSON protocol the proxy is parsing.
var stdoutMu sync.Mutex

func writeMessage(w io.Writer, m GuestMessage) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	if w == os.Stdout {
		stdoutMu.Lock()
		defer stdoutMu.Unlock()
	}
	_, err = w.Write(data)
	return err
}
