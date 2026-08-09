package pluginsdk

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestServeAnnouncesManifestFirst(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader(line(t, HostMessage{Type: "shutdown"}))

	manifest := Manifest{Name: "demo", Version: "1.0.0", Events: []string{"player_join"}}
	if err := serve(in, &out, manifest, func(Event) {}); err != nil {
		t.Fatalf("serve: %v", err)
	}

	msg := firstMessage(t, &out)
	if msg.Type != "manifest" || msg.Manifest == nil || msg.Manifest.Name != "demo" {
		t.Errorf("first message = %+v, want a manifest announcing %q", msg, "demo")
	}
}

func TestServeRejectsInvalidManifest(t *testing.T) {
	if err := Serve(Manifest{Name: "../escape"}, func(Event) {}); err == nil {
		t.Error("Serve with an invalid manifest should have returned an error before touching stdio")
	}
}

func TestServeDispatchesEventsToHandler(t *testing.T) {
	var out bytes.Buffer
	payload, _ := json.Marshal(map[string]string{"name": "Steve"})
	in := strings.NewReader(
		line(t, HostMessage{Type: "event", Topic: "player_join", Payload: payload}) +
			line(t, HostMessage{Type: "shutdown"}),
	)

	var got []Event
	if err := serve(in, &out, Manifest{Name: "demo"}, func(e Event) { got = append(got, e) }); err != nil {
		t.Fatalf("serve: %v", err)
	}
	if len(got) != 1 || got[0].Topic != "player_join" {
		t.Fatalf("handler saw %+v, want exactly one player_join event", got)
	}
	var decoded struct{ Name string }
	if err := json.Unmarshal(got[0].Payload, &decoded); err != nil || decoded.Name != "Steve" {
		t.Errorf("payload = %s, want a name of Steve", got[0].Payload)
	}
}

func TestServeStopsOnShutdown(t *testing.T) {
	var out bytes.Buffer
	// The event line placed after "shutdown" must never reach the handler.
	in := strings.NewReader(
		line(t, HostMessage{Type: "shutdown"}) +
			line(t, HostMessage{Type: "event", Topic: "player_join"}),
	)

	called := false
	if err := serve(in, &out, Manifest{Name: "demo"}, func(Event) { called = true }); err != nil {
		t.Fatalf("serve: %v", err)
	}
	if called {
		t.Error("handler was called for an event sent after shutdown")
	}
}

func TestServeRecoversPanickingHandler(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader(
		line(t, HostMessage{Type: "event", Topic: "player_join"}) +
			line(t, HostMessage{Type: "shutdown"}),
	)

	if err := serve(in, &out, Manifest{Name: "demo"}, func(Event) { panic("boom") }); err != nil {
		t.Fatalf("serve returned an error instead of recovering the panic: %v", err)
	}

	found := false
	scanner := bufio.NewScanner(&out)
	for scanner.Scan() {
		var msg GuestMessage
		if json.Unmarshal(scanner.Bytes(), &msg) == nil && msg.Type == "log" && msg.Level == "error" {
			found = true
		}
	}
	if !found {
		t.Error("expected an error log line reporting the handler's panic")
	}
}

func TestServeIgnoresMalformedLines(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader("not json at all\n" + line(t, HostMessage{Type: "shutdown"}))

	if err := serve(in, &out, Manifest{Name: "demo"}, func(Event) {}); err != nil {
		t.Fatalf("serve: %v", err)
	}
}

func TestManifestValidate(t *testing.T) {
	for _, test := range []struct {
		name    string
		m       Manifest
		wantErr bool
	}{
		{name: "simple", m: Manifest{Name: "echo"}},
		{name: "punctuation", m: Manifest{Name: "my_plugin-2"}},
		{name: "empty", m: Manifest{}, wantErr: true},
		{name: "path separator", m: Manifest{Name: "a/b"}, wantErr: true},
		{name: "traversal", m: Manifest{Name: ".."}, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.m.Validate(); (err != nil) != test.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

// line marshals m into a single line of the wire protocol, including the trailing newline.
func line(t *testing.T, m HostMessage) string {
	t.Helper()
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshaling: %v", err)
	}
	return string(data) + "\n"
}

// firstMessage decodes the first line written to buf as a GuestMessage.
func firstMessage(t *testing.T, buf *bytes.Buffer) GuestMessage {
	t.Helper()
	scanner := bufio.NewScanner(bytes.NewReader(buf.Bytes()))
	if !scanner.Scan() {
		t.Fatalf("no output was written")
	}
	var msg GuestMessage
	if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
		t.Fatalf("decoding first line %q: %v", scanner.Text(), err)
	}
	return msg
}
