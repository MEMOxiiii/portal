<p align="center">
  <img src="https://raw.githubusercontent.com/MEMOxiiii/portal/master/banner.png" alt="Portal Banner" width="100%"/>
</p>

<p align="center">
  <strong>Portal</strong> — A lightweight transfer proxy for Minecraft: Bedrock Edition
</p>

<p align="center">
  <a href="https://github.com/MEMOxiiii/portal/releases"><img src="https://img.shields.io/github/v/release/MEMOxiiii/portal?style=flat-square&color=%2300b894" alt="Release"></a>
  <a href="https://github.com/MEMOxiiii/portal/blob/master/LICENCE"><img src="https://img.shields.io/badge/License-Apache%202.0-0984e3?style=flat-square" alt="License"></a>
  <img src="https://img.shields.io/badge/Go-1.24+-00cec9?style=flat-square&logo=go&logoColor=white" alt="Go 1.24+">
  <img src="https://img.shields.io/badge/Bedrock-v2168%20%7C%201.26.40-6c5ce7?style=flat-square" alt="Bedrock Protocol">
</p>

<p align="center">
  <a href="https://github.com/MEMOxiiii/portal/wiki"><strong>📖 Full documentation is in the Wiki</strong></a>
</p>

---

**Portal** lets Bedrock players connect once and move between multiple backend servers — no matter what software each one runs — with instant, zero-downtime transfers.

## Supported backends

| Platform | Library | Status |
|:---|:---|:---|
| [PocketMine-MP](https://github.com/pmmp/PocketMine-MP) | [PortalPM](https://github.com/MEMOxiiii/PortalPM) | ✅ Supported |
| [Dragonfly](https://github.com/df-mc/dragonfly) | [PortalDF](https://github.com/MEMOxiiii/PortalDF) | ✅ Supported |
| [GeyserMC](https://geysermc.org/) 2.9.5+ | [Portal-GeyserMC](https://github.com/MEMOxiiii/Portal-GeyserMC) | ✅ Supported |
| NukkitX / PowerNukkitX | — | 🔜 Coming soon |
| EndstoneMC (BDS) | — | ⚠️ Experimental — use [Portal-GeyserMC](https://github.com/MEMOxiiii/Portal-GeyserMC) as a workaround |

## Quick start

```bash
# Download a release from https://github.com/MEMOxiiii/portal/releases, then:
chmod +x portal   # Linux/macOS only
./portal
```

Or build from source (requires Go 1.24+):

```bash
git clone https://github.com/MEMOxiiii/portal.git
cd portal
go build -o portal ./examples/main.go
```

On first run, Portal writes a `config.json` next to the binary. See the [Wiki](https://github.com/MEMOxiiii/portal/wiki/Configuration) for every setting.

## Plugins

Portal can be extended without forking it. A plugin is a Go package that registers itself with the [`plugin`](plugin/) package; importing that package from your `main` is all it takes to include it in the binary.

```go
package hello

import "github.com/paroxity/portal/plugin"

func init() { plugin.Register(&Hello{}) }

type Hello struct{ plugin.Base }

func (*Hello) Manifest() plugin.Manifest {
	return plugin.Manifest{Name: "hello", Version: "1.0.0"}
}

func (h *Hello) Enable(ctx *plugin.Context) error {
	ctx.Subscribe(event.TopicPlayerJoin, func(payload any) {
		ctx.Log().Infof("%s joined", payload.(event.PlayerPayload).Name)
	})
	return nil
}
```

Plugins get a `Context` with the running proxy, a name-prefixed logger, a `plugins/<name>/` data directory, and a `config.json` that is written from your defaults on first run. Load order follows `Depends`/`SoftDepends`; a plugin that panics or fails is skipped rather than taking the proxy down with it. See [`examples/plugins/greeter/`](examples/plugins/greeter/) for a worked example, and the `plugins` block in `config.json` to disable one without rebuilding.

### What plugins can do today

- Subscribe to proxy events — player join/quit, transfers, server register/unregister, health changes
- Replace or wrap the load balancer, whitelist, and IP guard
- Reach the session store, server registry, and any player's connection

### External plugins — no proxy rebuild required

For a plugin you want to drop in like a `.jar` or `.phar`, without ever rebuilding `portal` itself, write it against [`pluginsdk`](pluginsdk/) instead:

```go
package main

import "github.com/paroxity/portal/pluginsdk"

func main() {
	pluginsdk.Serve(pluginsdk.Manifest{
		Name:   "echo",
		Events: []string{"player_join"},
	}, func(e pluginsdk.Event) {
		pluginsdk.Infof("event: %s", e.Topic)
	})
}
```

Build it on its own — `go build -o plugins/echo.portalplugin ./path/to/plugin` (`echo.portalplugin.exe` on Windows) — and drop the single resulting binary into the proxy's `plugins/` directory. It never touches the proxy's own source or build. The proxy (via the [`extplugin`](extplugin/) package) spawns it as a subprocess and talks to it over a small line-delimited JSON protocol on stdin/stdout: the plugin announces a manifest, the proxy forwards the events it asked for, and the plugin can log back through the proxy's own logger. This is off by default — set `plugins.external.enabled: true` in `config.json` to turn it on, since unlike a compiled-in plugin, anything matching `*.portalplugin` in that directory runs automatically. See [`examples/plugins/echo/`](examples/plugins/echo/) for the full worked example.

### Roadmap

The plugin API is deliberately being grown in stages. Still to come:

- [ ] **Packet interception.** `session.Handler` is currently a single slot, so only one consumer can intercept packets at a time. Needs a priority-ordered handler chain before it can be exposed to plugins.
- [ ] **Command registry.** Replace the hardcoded `switch` in [admin.go](admin.go) with a registry plugins can add to, and route in-game `/` commands to the same place.
- [ ] **Socket protocol extension.** Let plugins register their own socket packet types so they can talk to the backend server plugins (PortalPM / PortalDF / Portal-GeyserMC).
- [ ] **External plugins in other languages.** `pluginsdk` plugins are Go binaries; a `plugin` role on the communication socket would let the same drop-in model work for PHP/Python/JS plugins, matching the backend server integrations.
- [ ] **External plugin data dir/config.json**, `DataDir()`/`Config()` equivalents to what compiled-in plugins get via `plugin.Context`.
- [ ] **Hot reload.** External plugins are currently only discovered at proxy startup; adding/removing one still needs a restart.

## Learn more

Everything past this point — configuration reference, network architecture, the socket protocol for integrating your own backend, the Go API for embedding Portal as a library, the event bus, admin console, and clustering — lives in the **[Wiki](https://github.com/MEMOxiiii/portal/wiki)**.

## Credits

Forked from [Paroxity/portal](https://github.com/Paroxity/portal). All credit for the original proxy architecture and protocol design goes to the [Paroxity](https://github.com/Paroxity) team; this fork extends it with additional platform support and improvements.

## License

[Apache License 2.0](LICENCE)
