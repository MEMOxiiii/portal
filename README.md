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
  <img src="https://img.shields.io/badge/Bedrock-v1001%20%7C%201.26.30-6c5ce7?style=flat-square" alt="Bedrock Protocol">
</p>

---

## Overview

**Portal** is a transfer proxy written in Go that allows Bedrock Edition players to seamlessly move between multiple backend servers — regardless of what server software each one runs. Players connect to Portal once, and transfers between servers happen instantly without disconnecting.

This build targets Minecraft Bedrock protocol **1001** with game version **1.26.30**.

Portal supports any combination of backend server software through its TCP socket API:

### Supported Platforms

| Platform | Library | Type | Engine | Status |
|:---|:---|:---|:---|:---|
| **PocketMine-MP** (PHP) | [PortalPM](https://github.com/MEMOxiiii/PortalPM) | Plugin | [PocketMine-MP](https://github.com/pmmp/PocketMine-MP) | ✅ Supported |
| **Dragonfly** (Go) | [PortalDF](https://github.com/MEMOxiiii/PortalDF) | Library | [Dragonfly](https://github.com/df-mc/dragonfly) | ✅ Supported |
| **GeyserMC** (Java) | [Portal-GeyserMC](https://github.com/MEMOxiiii/Portal-GeyserMC) | Extension | [GeyserMC](https://geysermc.org/) 2.9.5+ | ✅ Supported |
| **NukkitX / PowerNukkitX** (Java) | — | Plugin | [NukkitX](https://github.com/CloudburstMC/Nukkit) / [PowerNukkitX](https://github.com/PowerNukkitX/PowerNukkitX) | 🔜 Coming Soon |
| **EndstoneMC** (BDS) | — | Plugin | [Endstone](https://github.com/EndstoneMC/endstone) (BDS) | ⚠️ Experimental ¹ |

> ¹ **EndstoneMC (BDS):** We will attempt to support Endstone in future releases, but we cannot guarantee full compatibility due to BDS limitations. In the meantime, you can use **[GeyserMC](https://github.com/MEMOxiiii/Portal-GeyserMC)** as an alternative to connect BDS-based servers to the Portal network.

## Features

- **Zero-downtime Transfers** — Players switch servers instantly without disconnects
- **Multi-platform** — Mix PocketMine, Dragonfly, GeyserMC (and more) servers on the same network
- **TCP Socket API** — Simple binary protocol for integrating any server software
- **Resource Packs** — Serve resource packs from the proxy level
- **Resource Pack Hot Reload** — Reload proxy resource packs for new connections without restarting Portal
- **Whitelist** — Built-in whitelist support
- **Latency Reporting** — Real-time player latency tracking sent to backend servers
- **Event Bus** — Subscribe to player join/quit, server register/unregister and transfer events via `Portal.Events()` without forking the proxy
- **Admin Console** — `players`, `servers`, `kick`, `transfer` and `drain` commands from stdin, no restart required
- **Clustering** — Optional Redis-backed presence sharing so `FindPlayerRequest` can resolve players connected to a different proxy in the same network
- **Server Groups & Draining** — Route players into named server pools with fallback chains, and drain a server ahead of a restart without dropping it from the registry
- **Health Checking** — Real RakNet pings catch servers that are registered but hung or crashed, automatically pulling them out of (and back into) load balancing
- **Weighted Load Balancing** — Give servers of different capacity a different share of new players; defaults to a plain even split if you never set it, so it works the same whether you run one server or fifty
- **IP Guard** — Static IP bans and per-IP connection rate limiting at the player listener
- **Metrics** — Optional Prometheus-compatible `/metrics` endpoint for player counts, transfers and socket clients
- **Graceful Shutdown** — Clean disconnects and listener shutdown on SIGINT/SIGTERM
- **Lightweight** — Minimal resource footprint, written in Go

## Installation

### From Releases

1. Download the latest binary for your platform from [Releases](https://github.com/MEMOxiiii/portal/releases)
2. Place it in a directory of your choice
3. Run it from the command line

> **Linux/macOS:** Run `chmod +x portal` to make the binary executable.

### From Source

```bash
git clone https://github.com/MEMOxiiii/portal.git
cd portal
go build -o portal ./examples/main.go
```

## Configuration

On first run, a `config.json` file is generated. Here's the full reference:

```json
{
  "network": {
    "address": ":19132",
    "flush_rate_ms": 20,
    "communication": {
      "address": ":19131",
      "secret": "",
      "tls": {
        "enabled": false,
        "cert_file": "",
        "key_file": ""
      }
    },
    "reader_limits": true
  },
  "logger": {
    "file": "proxy.log",
    "level": "info"
  },
  "player_latency": {
    "report": true,
    "update_interval": 5
  },
  "cluster": {
    "enabled": false,
    "proxy_id": "",
    "ttl_seconds": 300,
    "redis": {
      "address": "localhost:6379",
      "password": "",
      "db": 0
    }
  },
  "metrics": {
    "enabled": false,
    "address": ":9131"
  },
  "security": {
    "banned_ips": [],
    "rate_limit": {
      "enabled": true,
      "window_seconds": 10,
      "max_attempts": 5
    }
  },
  "health_check": {
    "enabled": true,
    "interval_seconds": 10,
    "timeout_seconds": 3,
    "failure_threshold": 3
  },
  "routing": {
    "default_group": "",
    "fallback_groups": []
  },
  "whitelist": {
    "enabled": false,
    "players": []
  },
  "resource_packs": {
    "required": false,
    "directory": "resource_packs",
    "encryption_keys": {},
    "hot_reload": {
      "enabled": false,
      "interval": 30
    }
  },
  "motd": "Portal",
  "sub_motd": "Transfer Proxy"
}
```

### Configuration Reference

| Key | Description | Default |
|:---|:---|:---|
| `network.address` | Address players connect to (`ip:port`) | `:19132` |
| `network.flush_rate_ms` | Maximum client-bound packet buffering delay. Lower values reduce relay latency but increase CPU and network writes; `0` uses gophertunnel's default. | `20` |
| `network.communication.address` | Socket API address for backend servers | `:19131` |
| `network.communication.secret` | Authentication secret (must match backend configs) | `""` |
| `network.communication.tls.enabled` | Serve the communication socket over TLS (backends must dial with TLS too) | `false` |
| `network.communication.tls.cert_file` | Path to the PEM encoded TLS certificate | `""` |
| `network.communication.tls.key_file` | Path to the PEM encoded TLS private key | `""` |
| `network.reader_limits` | Enable protocol reader limits | `true` |
| `logger.file` | Log file path (empty = no file logging) | `proxy.log` |
| `logger.level` | Minimum log level (`debug`, `info`, `warn`, `error`) | `info` |
| `player_latency.report` | Send player latency to backend servers | `true` |
| `player_latency.update_interval` | Latency report interval in seconds | `5` |
| `metrics.enabled` | Serve Prometheus-style metrics at `http://<metrics.address>/metrics` | `false` |
| `metrics.address` | Address the metrics HTTP endpoint listens on | `:9131` |
| `cluster.enabled` | Share player presence with other Portal instances over Redis so `FindPlayerRequest` resolves across proxies | `false` |
| `cluster.proxy_id` | ID this proxy reports itself as in the cluster. Empty = the machine's hostname | `""` |
| `cluster.ttl_seconds` | How long a player's presence record survives without a refresh before expiring (crash safety net) | `300` |
| `cluster.redis.address` | Redis server address (`host:port`) | `localhost:6379` |
| `cluster.redis.password` | Redis auth password, if required | `""` |
| `cluster.redis.db` | Redis logical database index | `0` |
| `security.banned_ips` | IP addresses that are always rejected at the player listener | `[]` |
| `security.rate_limit.enabled` | Reject an IP once it connects too frequently | `true` |
| `security.rate_limit.window_seconds` | Size of the sliding window connection attempts are counted over | `10` |
| `security.rate_limit.max_attempts` | Max connection attempts allowed per IP within the window | `5` |
| `health_check.enabled` | Periodically RakNet-ping registered servers and skip unreachable ones in load balancing | `true` |
| `health_check.interval_seconds` | How often, in seconds, every registered server is pinged | `10` |
| `health_check.timeout_seconds` | How long to wait for a ping response before counting it as failed | `3` |
| `health_check.failure_threshold` | Consecutive failed pings before a server is marked unhealthy | `3` |
| `routing.default_group` | Server group new players are load balanced into on join. Empty = balance across every registered server, ignoring groups | `""` |
| `routing.fallback_groups` | Ordered list of groups to try if `default_group` has no available (non-draining) servers | `[]` |
| `whitelist.enabled` | Enable username whitelist | `false` |
| `whitelist.players` | Array of whitelisted usernames | `[]` |
| `resource_packs.required` | Require resource pack download | `false` |
| `resource_packs.directory` | Directory for resource packs (`.zip`, `.mcpack`, or folders) | `resource_packs` |
| `resource_packs.encryption_keys` | Map of pack UUID → encryption key | `{}` |
| `resource_packs.hot_reload.enabled` | Reload changed resource packs without restarting Portal. New connections receive the latest successful snapshot. | `false` |
| `resource_packs.hot_reload.interval` | Resource pack change check interval in seconds | `30` |
| `motd` | Main server list MOTD line | `Portal` |
| `sub_motd` | Secondary server list MOTD line | `Transfer Proxy` |

## Network Architecture

```
                         ┌──────────────────────┐
                         │     Portal Proxy      │
                         │                       │
  Bedrock Players ──────▶│  :19132 (players)     │
                         │  :19131 (socket API)  │
                         │                       │
                         └───┬──────┬──────┬─────┘
                             │      │      │
                    ┌────────┘      │      └────────┐
                    ▼               ▼               ▼
             ┌────────────┐  ┌────────────┐  ┌────────────┐
             │ PocketMine │  │ Dragonfly  │  │  GeyserMC  │
             │ (PortalPM) │  │ (PortalDF) │  │ (Portal-   │
             │            │  │            │  │  GeyserMC) │
             └────────────┘  └────────────┘  └────────────┘
```

## Socket Protocol

Portal communicates with backend servers via a binary TCP protocol:

- **Frame:** 4-byte little-endian length prefix + payload
- **Packet Header:** 2-byte little-endian packet ID
- **Strings:** Varuint32 length prefix + UTF-8 bytes
- **UUIDs:** 16 bytes (big-endian)
- **Integers:** Little-endian

### Packet Types

| ID | Packet | Direction | Description |
|:---|:---|:---|:---|
| `0x00` | AuthRequest | Server → Proxy | Authenticate with secret |
| `0x01` | AuthResponse | Proxy → Server | Authentication result |
| `0x02` | RegisterServer | Server → Proxy | Register server name + address |
| `0x03` | TransferRequest | Server → Proxy | Request player transfer |
| `0x04` | TransferResponse | Proxy → Server | Transfer result |
| `0x05` | PlayerInfoRequest | Server → Proxy | Query player XUID/IP |
| `0x06` | PlayerInfoResponse | Proxy → Server | Player info result |
| `0x07` | ServerListRequest | Server → Proxy | Request all servers |
| `0x08` | ServerListResponse | Proxy → Server | List of servers + counts |
| `0x09` | FindPlayerRequest | Server → Proxy | Find player on network |
| `0x0A` | FindPlayerResponse | Proxy → Server | Player location result |
| `0x0B` | UpdatePlayerLatency | Proxy → Server | Player latency update |
| `0x0C` | SetServerDraining | Server → Proxy | Mark/unmark the server as draining for load balancing |

`RegisterServer` also carries `Group` (string) and `Weight` (uint32, 0 treated as 1) fields. Servers
registering with the same group name are treated as a pool by a `GroupedLoadBalancer` (see
`routing.default_group`/`routing.fallback_groups` above), and within a pool, `SplitLoadBalancer` and
`GroupedLoadBalancer` both balance new players in proportion to `Weight` — a server with twice the weight of
another gets roughly twice the players before being considered equally loaded. Leaving `Weight` at its
default keeps a plain even split, so this is opt-in whether you're running a single server or a large fleet
of mixed-capacity machines.

A draining server (set via `SetServerDraining`) or a server currently failing health checks (see
`health_check.*` above) is skipped by both load balancers when picking a server for a newly joining player,
while players already on it are unaffected — useful for taking a server out of rotation ahead of a restart,
or for automatically routing around one that's hung or crashed.

`FindPlayerResponse` also carries a `Proxy` field, populated when `cluster.enabled` is set and the player
was found on a *different* proxy instance via the shared Redis backend rather than this proxy's local
session store. Clustering only answers "is this player online, and where" across proxies — it does not
implement proxy-to-proxy transfer routing, so a backend server still needs its own way to act on a remote
result (e.g. relaying a message rather than attempting to transfer the player directly).

## Quick Start Example

Here's a minimal example using the Portal Go library:

```go
package main

import (
    "github.com/paroxity/portal"
    "github.com/sirupsen/logrus"
)

func main() {
    log := logrus.New()
    
    p, err := portal.New(portal.DefaultConfig(), log)
    if err != nil {
        log.Fatalf("Failed to create portal: %v", err)
    }
    
    defer p.Close()
    
    // Portal is now running and accepting connections
    select {}
}
```

## Event Bus

`Portal.Events()` exposes a proxy-wide event bus you can subscribe to without forking the proxy:

```go
p.Events().Subscribe(event.TopicPlayerJoin, func(payload any) {
    p := payload.(event.PlayerPayload)
    log.Infof("%s joined the network", p.Name)
})

p.Events().Subscribe(event.TopicTransfer, func(payload any) {
    t := payload.(event.TransferPayload)
    if t.Err != nil {
        log.Errorf("%s failed to transfer from %s to %s: %v", t.PlayerName, t.FromServer, t.ToServer, t.Err)
    }
})
```

Available topics: `TopicPlayerJoin`, `TopicPlayerQuit`, `TopicServerRegistered`, `TopicServerUnregistered`, `TopicTransfer`, `TopicServerHealthChanged`.

## Admin Console

The bundled binary reads admin commands from stdin while it runs:

| Command | Description |
|:---|:---|
| `help` | List available commands |
| `players` | List online players and the server each is connected to |
| `servers` | List registered servers, their group, weight, player count, draining and health state |
| `kick <player> [reason]` | Disconnect a player |
| `transfer <player> <server>` | Transfer a player to another registered server |
| `drain <server> [on\|off]` | Mark a server as draining (default `on`) so it stops receiving new players |

`Portal.HandleAdminCommand` and `Portal.ServeAdminConsole` are also available directly if you're embedding
Portal as a library and want to wire commands up to a different transport (e.g. an in-game `/portal` command
on a hub server).

## Client Libraries

Integrate your server with Portal using the appropriate library:

| Library | Language | Install |
|:---|:---|:---|
| [PortalPM](https://github.com/MEMOxiiii/PortalPM) | PHP | Drop plugin into `plugins/` |
| [PortalDF](https://github.com/MEMOxiiii/PortalDF) | Go | `go get github.com/MEMOxiiii/PortalDF` |
| [Portal-GeyserMC](https://github.com/MEMOxiiii/Portal-GeyserMC) | Java | Drop JAR into Geyser `extensions/` |

## Credits

This project is originally forked from [**Paroxity/portal**](https://github.com/Paroxity/portal). All credit for the original proxy architecture and protocol design goes to the [Paroxity](https://github.com/Paroxity) team. This fork extends and maintains the project with additional platform support and improvements.

## License

[Apache License 2.0](LICENCE)
