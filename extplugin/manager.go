// Package extplugin runs Portal's "external" plugins: standalone executables dropped into the proxy's
// plugins directory that are discovered and spawned as subprocesses, speaking the line-delimited JSON
// protocol implemented by the pluginsdk package. Unlike the plugin package, an external plugin can be
// written, built and replaced without ever rebuilding the proxy binary itself — see the pluginsdk package
// for how to write one.
package extplugin

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/paroxity/portal"
	"github.com/paroxity/portal/internal"
	"github.com/paroxity/portal/pluginsdk"
)

// handshakeTimeout bounds how long the Manager waits for a spawned plugin to announce its manifest before
// giving up on it and killing it.
const handshakeTimeout = 5 * time.Second

// shutdownTimeout bounds how long the Manager waits for a plugin to exit on its own after being asked to,
// before killing it.
const shutdownTimeout = 5 * time.Second

// suffix identifies an external plugin binary within the plugins directory. Windows requires an
// executable's name to end in ".exe" to run it at all, so a Windows build uses ".portalplugin.exe"; every
// other platform uses the bare ".portalplugin".
func suffix() string {
	if runtime.GOOS == "windows" {
		return ".portalplugin.exe"
	}
	return ".portalplugin"
}

// Manager discovers and runs the external plugins in a directory. A proxy is expected to create one, call
// Start once every other subsystem is running, and Stop it during shutdown.
type Manager struct {
	portal *portal.Portal
	dir    string
	log    internal.Logger

	mu      sync.Mutex
	running []*process
}

// NewManager creates a Manager that discovers external plugin binaries in dir.
func NewManager(p *portal.Portal, dir string, log internal.Logger) *Manager {
	return &Manager{portal: p, dir: dir, log: log}
}

// Start discovers every external plugin binary in the plugins directory, spawns it, and waits for its
// handshake. A plugin that cannot be started, does not complete its handshake in time, or announces an
// invalid manifest, is logged and skipped; it does not stop the other plugins from starting or the proxy
// itself from running. The directory not existing yet is not an error. Start is not meant to be called
// more than once.
func (m *Manager) Start() error {
	entries, err := os.ReadDir(m.dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("extplugin: reading %s: %w", m.dir, err)
	}

	want := suffix()
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), want) {
			continue
		}

		path := filepath.Join(m.dir, entry.Name())
		proc, err := m.spawn(path)
		if err != nil {
			m.log.Errorf("external plugin %s failed to start: %v", entry.Name(), err)
			continue
		}

		m.mu.Lock()
		m.running = append(m.running, proc)
		m.mu.Unlock()
		m.log.Infof("loaded external plugin %s", describe(proc.manifest))
	}
	return nil
}

// Stop asks every running external plugin to shut down and waits up to shutdownTimeout for each to exit
// on its own, killing it if it does not, and releases the event subscriptions made on its behalf.
func (m *Manager) Stop() {
	m.mu.Lock()
	running := m.running
	m.running = nil
	m.mu.Unlock()

	var wg sync.WaitGroup
	for _, proc := range running {
		wg.Add(1)
		go func(proc *process) {
			defer wg.Done()
			proc.stop(shutdownTimeout)
		}(proc)
	}
	wg.Wait()
}

// Running returns the manifest of every currently running external plugin.
func (m *Manager) Running() []pluginsdk.Manifest {
	m.mu.Lock()
	defer m.mu.Unlock()

	manifests := make([]pluginsdk.Manifest, 0, len(m.running))
	for _, proc := range m.running {
		manifests = append(manifests, proc.manifest)
	}
	return manifests
}

// spawn starts the executable at path, performs its manifest handshake, and subscribes it to the events
// its manifest declared interest in. The process is left running on success.
func (m *Manager) spawn(path string) (*process, error) {
	cmd := exec.Command(path)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = &lineLogger{log: m.log, prefix: "[" + filepath.Base(path) + "] "}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	// A plugin that never sends its manifest would otherwise block Start forever; the timer force-closes
	// its output so the blocking Scan below gives up.
	timer := time.AfterFunc(handshakeTimeout, func() { _ = cmd.Process.Kill() })
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	scanned := scanner.Scan()
	timer.Stop()

	if !scanned {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, fmt.Errorf("exited or timed out before sending a manifest: %w", scanner.Err())
	}
	var msg pluginsdk.GuestMessage
	if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil || msg.Type != "manifest" || msg.Manifest == nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, fmt.Errorf("first line was not a valid manifest handshake")
	}
	manifest := *msg.Manifest
	if err := manifest.Validate(); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, err
	}

	proc := &process{
		cmd:      cmd,
		stdin:    stdin,
		manifest: manifest,
		log:      prefixedLogger{Logger: m.log, prefix: "[" + manifest.Name + "] "},
		done:     make(chan struct{}),
	}

	seen := make(map[string]bool, len(manifest.Events))
	for _, topic := range manifest.Events {
		if seen[topic] {
			continue
		}
		seen[topic] = true
		proc.subs = append(proc.subs, m.portal.Events().Subscribe(topic, proc.forward(topic)))
	}

	go proc.run(scanner)

	return proc, nil
}

// describe renders a manifest for a log line.
func describe(m pluginsdk.Manifest) string {
	s := m.Name
	if m.Version != "" {
		s += " v" + m.Version
	}
	if m.Author != "" {
		s += " by " + m.Author
	}
	return s
}

// prefixedLogger attributes log lines to a plugin by prefixing them with its name. Plugin names are
// validated to contain no formatting verbs, so the prefix is safe to concatenate onto a format string.
type prefixedLogger struct {
	internal.Logger
	prefix string
}

// Debugf ...
func (l prefixedLogger) Debugf(format string, v ...any) { l.Logger.Debugf(l.prefix+format, v...) }

// Infof ...
func (l prefixedLogger) Infof(format string, v ...any) { l.Logger.Infof(l.prefix+format, v...) }

// Errorf ...
func (l prefixedLogger) Errorf(format string, v ...any) { l.Logger.Errorf(l.prefix+format, v...) }

// Fatalf ...
func (l prefixedLogger) Fatalf(format string, v ...any) { l.Logger.Fatalf(l.prefix+format, v...) }
