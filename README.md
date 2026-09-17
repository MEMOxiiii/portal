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
  <img src="https://img.shields.io/badge/Bedrock-v2193%20%7C%201.26.50-6c5ce7?style=flat-square" alt="Bedrock Protocol">
</p>

<p align="center">
  <a href="https://github.com/MEMOxiiii/portal/wiki"><strong>📖 Full documentation is in the Wiki</strong></a>
</p>

---

**Portal** lets Bedrock players connect once and move between multiple backend servers — no matter what software each one runs — with instant, zero-downtime transfers. Players connect over **NetherNet** (Bedrock's official WebRTC transport) by default — see the [Wiki](https://github.com/MEMOxiiii/portal/wiki/NetherNet-Transport) for setup details, or switch to RakNet with one config line.

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

## Learn more

Everything past this point — configuration reference, network architecture, the socket protocol for integrating your own backend, the Go API for embedding Portal as a library, the event bus, admin console, and clustering — lives in the **[Wiki](https://github.com/MEMOxiiii/portal/wiki)**.

## Credits

Forked from [Paroxity/portal](https://github.com/Paroxity/portal). All credit for the original proxy architecture and protocol design goes to the [Paroxity](https://github.com/Paroxity) team; this fork extends it with additional platform support and improvements.

## License

[Apache License 2.0](LICENCE)
