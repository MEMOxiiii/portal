package extplugin

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/paroxity/portal/event"
	"github.com/paroxity/portal/internal"
	"github.com/paroxity/portal/pluginsdk"
)

// process is a running external plugin: a subprocess the Manager has completed a handshake with and
// subscribed to its declared events.
type process struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	manifest pluginsdk.Manifest
	log      internal.Logger

	subs []*event.Subscription

	writeMu sync.Mutex
	// done is closed once the process has exited and its output has been fully drained, which is also the
	// point at which it is safe to know no more log lines will arrive from it.
	done chan struct{}
}

// forward returns a handler that encodes a published event's payload and sends it to the plugin.
func (p *process) forward(topic string) func(any) {
	return func(payload any) {
		data, err := json.Marshal(payload)
		if err != nil {
			p.log.Errorf("failed to encode %s event: %v", topic, err)
			return
		}
		p.send(pluginsdk.HostMessage{Type: "event", Topic: topic, Payload: data})
	}
}

// send writes msg to the plugin's standard input. It is best effort: a plugin that has already exited
// simply stops receiving events, which is reported once when its process exits rather than on every send.
func (p *process) send(msg pluginsdk.HostMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	data = append(data, '\n')

	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	_, _ = p.stdin.Write(data)
}

// run reads the plugin's log output, started by the Manager with scanner already positioned just after
// the manifest handshake line, until it exits, and then reaps it. Per exec.Cmd's contract, Wait must not
// be called until every read from the process's stdout has completed, so the two happen in sequence on
// this one goroutine rather than concurrently.
func (p *process) run(scanner *bufio.Scanner) {
	for scanner.Scan() {
		var msg pluginsdk.GuestMessage
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil || msg.Type != "log" {
			continue
		}
		switch msg.Level {
		case "error":
			p.log.Errorf("%s", msg.Message)
		case "debug":
			p.log.Debugf("%s", msg.Message)
		default:
			p.log.Infof("%s", msg.Message)
		}
	}

	if err := p.cmd.Wait(); err != nil {
		p.log.Errorf("exited: %v", err)
	} else {
		p.log.Infof("exited")
	}
	close(p.done)
}

// stop releases the plugin's event subscriptions, asks it to shut down, and waits up to timeout for its
// process to exit before killing it.
func (p *process) stop(timeout time.Duration) {
	for _, sub := range p.subs {
		sub.Unsubscribe()
	}

	p.send(pluginsdk.HostMessage{Type: "shutdown"})
	_ = p.stdin.Close()

	select {
	case <-p.done:
	case <-time.After(timeout):
		_ = p.cmd.Process.Kill()
		<-p.done
	}
}

// lineLogger implements io.Writer, splitting arbitrary writes on newlines and logging each complete line
// separately. It is used to attribute a plugin's stray standard-error output, such as an unhandled Go
// panic that bypassed pluginsdk's own recovery, to that plugin in the proxy's log.
type lineLogger struct {
	log    internal.Logger
	prefix string

	mu  sync.Mutex
	buf []byte
}

// Write ...
func (l *lineLogger) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.buf = append(l.buf, p...)
	for {
		i := bytes.IndexByte(l.buf, '\n')
		if i < 0 {
			break
		}
		if line := strings.TrimRight(string(l.buf[:i]), "\r"); line != "" {
			l.log.Errorf("%s%s", l.prefix, line)
		}
		l.buf = l.buf[i+1:]
	}
	return len(p), nil
}
