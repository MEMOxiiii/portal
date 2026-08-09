// Package greeter is a worked example of a Portal plugin. It greets players as they join, logs transfers
// between servers, and adds its own ban list on top of whatever whitelist the proxy is configured with.
//
// It is not compiled into the proxy by default. To use it, import it for its side effects from the same
// package as your main function, which is all that is needed to register a plugin:
//
//	import _ "github.com/paroxity/portal/examples/plugins/greeter"
//
// On first run the plugin writes "plugins/greeter/config.json" with the defaults below, which the operator
// can then edit.
package greeter

import (
	"strings"

	"github.com/paroxity/portal/event"
	"github.com/paroxity/portal/internal"
	"github.com/paroxity/portal/plugin"
	"github.com/paroxity/portal/session"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	"github.com/sandertv/gophertunnel/minecraft/text"
)

// init registers the plugin, so that importing this package is enough to add it to a proxy.
func init() {
	plugin.Register(&Greeter{})
}

// Config holds the plugin's settings, read from "plugins/greeter/config.json".
type Config struct {
	// WelcomeMessage is sent to a player once they have joined their first server. Colour codes in the
	// gophertunnel text format, such as "<green>", are supported, and a single "%v" is replaced with the
	// player's name.
	WelcomeMessage string `json:"welcome_message"`
	// BannedPlayers is a list of usernames that are refused at the door, in addition to any player the
	// proxy's own whitelist rejects. Names are compared case insensitively.
	BannedPlayers []string `json:"banned_players"`
}

// Greeter is the plugin itself. It embeds plugin.Base so it only has to implement the parts of the
// lifecycle it actually uses.
type Greeter struct {
	plugin.Base

	log  internal.Logger
	conf Config
}

// Manifest ...
func (*Greeter) Manifest() plugin.Manifest {
	return plugin.Manifest{
		Name:        "greeter",
		Version:     "1.0.0",
		Description: "Greets players on join and refuses a configurable list of names.",
		Authors:     []string{"Portal"},
	}
}

// Load reads the plugin's configuration and installs its whitelist. Routing policies are installed here
// rather than in Enable so that they are in place before the proxy accepts its first player.
func (g *Greeter) Load(ctx *plugin.Context) error {
	g.log = ctx.Log()

	g.conf = Config{
		WelcomeMessage: "<green>Welcome to the network, %v!</green>",
		BannedPlayers:  []string{},
	}
	if err := ctx.Config(&g.conf); err != nil {
		return err
	}

	if len(g.conf.BannedPlayers) > 0 {
		banned := make(map[string]struct{}, len(g.conf.BannedPlayers))
		for _, name := range g.conf.BannedPlayers {
			banned[strings.ToLower(name)] = struct{}{}
		}
		// The configured whitelist is wrapped rather than replaced, so the plugin adds a rule instead of
		// throwing the operator's own configuration away.
		ctx.SetWhitelist(&banList{next: ctx.Portal().Whitelist(), banned: banned})
		g.log.Infof("refusing %d banned player(s)", len(banned))
	}
	return nil
}

// Enable subscribes to the proxy events the plugin reacts to. The subscriptions are released
// automatically when the plugin is disabled, so Disable does not need to undo them.
func (g *Greeter) Enable(ctx *plugin.Context) error {
	store := ctx.Portal().SessionStore()

	ctx.Subscribe(event.TopicPlayerJoin, func(payload any) {
		player, ok := payload.(event.PlayerPayload)
		if !ok {
			return
		}
		s, ok := store.Load(player.UUID)
		if !ok {
			// The player disconnected between joining and this handler running.
			return
		}
		_ = s.Conn().WritePacket(&packet.Text{
			TextType: packet.TextTypeRaw,
			Message:  text.Colourf(g.conf.WelcomeMessage, player.Name),
		})
		g.log.Infof("%s joined on %s", player.Name, s.Server().Name())
	})

	ctx.Subscribe(event.TopicTransfer, func(payload any) {
		transfer, ok := payload.(event.TransferPayload)
		if !ok {
			return
		}
		if transfer.Err != nil {
			g.log.Errorf("%s failed to reach %s: %v", transfer.PlayerName, transfer.ToServer, transfer.Err)
			return
		}
		g.log.Infof("%s moved from %s to %s", transfer.PlayerName, transfer.FromServer, transfer.ToServer)
	})

	return nil
}

// Disable ...
func (g *Greeter) Disable() error {
	g.log.Infof("goodbye")
	return nil
}

// banList is a session.Whitelist that rejects a set of names before deferring to the whitelist it wraps.
type banList struct {
	next   session.Whitelist
	banned map[string]struct{}
}

// Authorize ...
func (b *banList) Authorize(conn *minecraft.Conn) (bool, string) {
	name := strings.ToLower(conn.IdentityData().DisplayName)
	if _, ok := b.banned[name]; ok {
		return false, text.Colourf("<red>You are banned from this network.</red>")
	}
	return b.next.Authorize(conn)
}
