package portal

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// HandleAdminCommand parses and executes a single admin console command line, returning the text that
// should be shown to the operator as a result. An empty string is returned for blank input.
func (p *Portal) HandleAdminCommand(line string) string {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return ""
	}
	cmd, args := strings.ToLower(fields[0]), fields[1:]

	switch cmd {
	case "help":
		return "Commands: help, players, servers, kick <player> [reason], transfer <player> <server>, drain <server> [on|off]"
	case "players":
		return p.adminListPlayers()
	case "servers":
		return p.adminListServers()
	case "kick":
		return p.adminKick(args)
	case "transfer":
		return p.adminTransfer(args)
	case "drain":
		return p.adminDrain(args)
	default:
		return fmt.Sprintf("unknown command %q, type 'help' for a list of commands", cmd)
	}
}

func (p *Portal) adminListPlayers() string {
	sessions := p.sessionStore.All()
	if len(sessions) == 0 {
		return "No players online."
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%d player(s) online:", len(sessions))
	for _, s := range sessions {
		fmt.Fprintf(&b, "\n  %s -> %s", s.Conn().IdentityData().DisplayName, s.Server().Name())
	}
	return b.String()
}

func (p *Portal) adminListServers() string {
	servers := p.serverRegistry.Servers()
	if len(servers) == 0 {
		return "No servers registered."
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%d server(s) registered:", len(servers))
	for _, srv := range servers {
		fmt.Fprintf(&b, "\n  %s (%s) group=%q weight=%d players=%d draining=%v healthy=%v", srv.Name(), srv.Address(), srv.Group(), srv.Weight(), srv.PlayerCount(), srv.Draining(), srv.Healthy())
	}
	return b.String()
}

func (p *Portal) adminKick(args []string) string {
	if len(args) < 1 {
		return "usage: kick <player> [reason]"
	}
	s, ok := p.sessionStore.LoadFromName(args[0])
	if !ok {
		return fmt.Sprintf("player %q is not online", args[0])
	}
	reason := strings.Join(args[1:], " ")
	if reason == "" {
		reason = "Kicked by an operator."
	}
	s.Disconnect(reason)
	return fmt.Sprintf("kicked %s", args[0])
}

func (p *Portal) adminTransfer(args []string) string {
	if len(args) < 2 {
		return "usage: transfer <player> <server>"
	}
	s, ok := p.sessionStore.LoadFromName(args[0])
	if !ok {
		return fmt.Sprintf("player %q is not online", args[0])
	}
	srv, ok := p.serverRegistry.Server(args[1])
	if !ok {
		return fmt.Sprintf("server %q is not registered", args[1])
	}
	if err := s.Transfer(srv); err != nil {
		return fmt.Sprintf("failed to transfer %s: %v", args[0], err)
	}
	return fmt.Sprintf("transferring %s to %s", args[0], args[1])
}

func (p *Portal) adminDrain(args []string) string {
	if len(args) < 1 {
		return "usage: drain <server> [on|off]"
	}
	srv, ok := p.serverRegistry.Server(args[0])
	if !ok {
		return fmt.Sprintf("server %q is not registered", args[0])
	}
	if len(args) >= 2 && strings.EqualFold(args[1], "off") {
		srv.SetDraining(false)
		return fmt.Sprintf("%s is no longer draining", args[0])
	}
	srv.SetDraining(true)
	return fmt.Sprintf("%s is now draining", args[0])
}

// ServeAdminConsole reads newline-delimited admin commands from r and writes their results to w until r is
// exhausted or returns an error. It is typically run in its own goroutine reading from os.Stdin.
func (p *Portal) ServeAdminConsole(r io.Reader, w io.Writer) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if result := p.HandleAdminCommand(line); result != "" {
			fmt.Fprintln(w, result)
		}
	}
}
