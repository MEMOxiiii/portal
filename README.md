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
