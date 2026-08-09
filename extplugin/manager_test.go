package extplugin

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/paroxity/portal"
	"github.com/paroxity/portal/event"
)

func TestManagerStartOnMissingDirectoryIsNotAnError(t *testing.T) {
	m := NewManager(portal.New(portal.Options{Logger: &testLogger{t: t}}), filepath.Join(t.TempDir(), "does-not-exist"), &testLogger{t: t})
	if err := m.Start(); err != nil {
		t.Fatalf("Start on a missing plugins directory: %v", err)
	}
	if len(m.Running()) != 0 {
		t.Error("Running() should be empty when the plugins directory does not exist")
	}
}

func TestManagerIgnoresFilesWithoutTheExtension(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := NewManager(portal.New(portal.Options{Logger: &testLogger{t: t}}), dir, &testLogger{t: t})
	if err := m.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if len(m.Running()) != 0 {
		t.Error("a plain file without the plugin suffix should have been ignored")
	}
}

// TestManagerHandshakesRunsAndStopsARealPlugin spawns the fixture binary under testdata/fixture as a real
// subprocess, exercising the full handshake, event delivery and log forwarding, and shutdown path — the
// parts that a purely in-process test of process.go's methods could not cover.
func TestManagerHandshakesRunsAndStopsARealPlugin(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "fixture"+suffix())
	buildFixture(t, binPath)

	log := newTestLogger(t)
	proxy := portal.New(portal.Options{Logger: log})
	m := NewManager(proxy, dir, log)

	if err := m.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	running := m.Running()
	if len(running) != 1 || running[0].Name != "fixture" || running[0].Version != "1.0.0" {
		t.Fatalf("Running() = %+v, want one plugin named fixture v1.0.0", running)
	}

	proxy.Events().Publish(event.TopicPlayerJoin, event.PlayerPayload{Name: "Steve"})
	if !log.await(t, "saw player_join join as Steve", 3*time.Second) {
		t.Fatalf("did not see the fixture plugin's log line for the event; log so far: %v", log.snapshot())
	}

	m.Stop()
	if len(m.Running()) != 0 {
		t.Error("Stop should clear the running plugin list")
	}
	if !log.await(t, "[fixture] exited", 3*time.Second) {
		t.Errorf("did not see the plugin's process report its own exit; log so far: %v", log.snapshot())
	}
}

// buildFixture compiles extplugin/testdata/fixture into a binary at outPath, named the way the Manager
// expects to find one.
func buildFixture(t *testing.T, outPath string) {
	t.Helper()

	cmd := exec.Command("go", "build", "-o", outPath, "github.com/paroxity/portal/extplugin/testdata/fixture")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("building fixture plugin: %v\n%s", err, out)
	}
}

// testLogger is an internal.Logger that records every line for assertions, and can also fail the test via
// Fatalf like the real logger it doubles for.
type testLogger struct {
	t *testing.T

	mu    sync.Mutex
	lines []string
}

func newTestLogger(t *testing.T) *testLogger { return &testLogger{t: t} }

func (l *testLogger) Debugf(format string, v ...any) { l.add(fmt.Sprintf(format, v...)) }
func (l *testLogger) Infof(format string, v ...any)  { l.add(fmt.Sprintf(format, v...)) }
func (l *testLogger) Errorf(format string, v ...any) { l.add(fmt.Sprintf(format, v...)) }
func (l *testLogger) Fatalf(format string, v ...any) { l.t.Fatalf(format, v...) }

func (l *testLogger) add(line string) {
	l.mu.Lock()
	l.lines = append(l.lines, line)
	l.mu.Unlock()
	l.t.Log(line)
}

func (l *testLogger) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.lines...)
}

// await polls, up to timeout, for a logged line containing substr. Log lines from a subprocess arrive
// asynchronously over a pipe, so there is no synchronous point to assert against.
func (l *testLogger) await(t *testing.T, substr string, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, line := range l.snapshot() {
			if strings.Contains(line, substr) {
				return true
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}
