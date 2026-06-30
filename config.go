package portal

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/sandertv/gophertunnel/minecraft/resource"
)

// Config represents the base configuration for portal. It holds settings that affect different aspects of the
// proxy.
type Config struct {
	// Network holds settings related to network aspects of the proxy.
	Network struct {
		// Address is the address on which the proxy should listen. Players may connect to this address in
		// order to join. It should be in the format of "ip:port".
		Address string `json:"address"`
		// Communication holds settings related to the communication aspects of the proxy.
		Communication struct {
			// Address is the address on which the communication service should listen. External connections
			// can use this address in order to communicate with the proxy. It should be in the format of
			// "ip:port".
			Address string `json:"address"`
			// Secret is the authentication secret required by external connections in order to authenticate
			// to the proxy and start communicating.
			Secret string `json:"secret"`
			// TLS holds settings to encrypt the communication socket. When enabled, backend servers must
			// connect using TLS as well.
			TLS struct {
				// Enabled determines whether the communication socket should be served over TLS.
				Enabled bool `json:"enabled"`
				// CertFile is the path to the PEM encoded certificate file.
				CertFile string `json:"cert_file"`
				// KeyFile is the path to the PEM encoded private key file.
				KeyFile string `json:"key_file"`
			} `json:"tls"`
		} `json:"communication"`
		// ReaderLimits determines if things like slices will have a maximum length as they are read from socket clients.
		// It is recommended that this is always set to true in order to prevent possible attack vectors, however if any
		// non-malicious clients are reaching these limits, you may want to disable it.
		ReaderLimits bool `json:"reader_limits"`
	} `json:"network"`
	// Logger holds settings related to the logging aspects of the proxy.
	Logger struct {
		// File is the path to the file in which logs should be stored. If the path is empty then logs will
		// not be written to a file.
		File string `json:"file"`
		// Level is the required level logs should have to be shown in console or in the file above.
		Level string `json:"level"`
	} `json:"logger"`
	// PlayerLatency holds settings related to the latency reporting aspects of the proxy.
	PlayerLatency struct {
		// Report is if the proxy should send the proxy of a player to their server at a regular interval.
		Report bool `json:"report"`
		// UpdateInterval is the interval to report a player's ping if Report is true.
		UpdateInterval int `json:"update_interval"`
	} `json:"player_latency"`
	// Security holds settings related to protecting the proxy's player listener from abusive connections.
	Security struct {
		// BannedIPs is a list of IP addresses that are never allowed to connect to the proxy.
		BannedIPs []string `json:"banned_ips"`
		// RateLimit holds settings to limit how often a single IP may attempt to connect.
		RateLimit struct {
			// Enabled determines whether connection rate limiting is active.
			Enabled bool `json:"enabled"`
			// WindowSeconds is the size, in seconds, of the sliding window attempts are counted over.
			WindowSeconds int `json:"window_seconds"`
			// MaxAttempts is the maximum number of connection attempts allowed from a single IP within
			// WindowSeconds before further attempts are rejected.
			MaxAttempts int `json:"max_attempts"`
		} `json:"rate_limit"`
	} `json:"security"`
	// Routing holds settings related to how players are load balanced onto groups of backend servers.
	Routing struct {
		// DefaultGroup is the server group new players are load balanced into when they first join the
		// proxy. If left empty, players are load balanced across every registered server regardless of
		// group.
		DefaultGroup string `json:"default_group"`
		// FallbackGroups is an ordered list of groups to try, in order, if DefaultGroup has no available
		// (non-draining) servers.
		FallbackGroups []string `json:"fallback_groups"`
	} `json:"routing"`
	// Whitelist holds settings related to the proxy whitelist.
	Whitelist struct {
		// Enabled is if the whitelist is enabled.
		Enabled bool `json:"enabled"`
		// Players is a list of whitelisted players' usernames.
		Players []string `json:"players"`
	} `json:"whitelist"`
	// ResourcePacks holds settings related to sending resource packs to players.
	ResourcePacks struct {
		// Required is if players are required to download the resource packs before connecting.
		Required bool `json:"required"`
		// Directory is the directory to load resource packs from. They can be directories, .zip files or .mcpack files.
		Directory string `json:"directory"`
		// EncryptionKeys is a map of resource pack UUIDs to their encryption key.
		EncryptionKeys map[string]string `json:"encryption_keys,omitempty"`
		// HotReload holds settings related to updating resource packs without restarting the proxy.
		HotReload struct {
			// Enabled controls whether the proxy should watch for resource pack changes and reload them.
			Enabled bool `json:"enabled"`
			// Interval is the amount of seconds between resource pack change checks.
			Interval int `json:"interval"`
		} `json:"hot_reload"`
	} `json:"resource_packs"`
	// MOTD is the message of the day shown in the server list ping.
	MOTD string `json:"motd"`
	// SubMOTD is the secondary MOTD line shown in the server list ping.
	SubMOTD string `json:"sub_motd"`
}

// DefaultConfig returns a configuration with the default values filled out.
func DefaultConfig() (c Config) {
	c.Network.Address = ":19132"
	c.Network.Communication.Address = ":19131"
	c.Network.ReaderLimits = true
	c.Logger.File = "proxy.log"
	c.Logger.Level = "debug"
	c.PlayerLatency.Report = true
	c.PlayerLatency.UpdateInterval = 5
	c.Security.RateLimit.Enabled = true
	c.Security.RateLimit.WindowSeconds = 10
	c.Security.RateLimit.MaxAttempts = 5
	c.ResourcePacks.Directory = "resource_packs"
	c.ResourcePacks.HotReload.Interval = 30
	c.MOTD = "Portal"
	c.SubMOTD = "Transfer Proxy"
	return
}

// LoadResourcePacks attempts to load all the resource packs in the provided directory. If the directory does not exist,
// it will be created. If any pack fails to compile, the error will be returned.
func LoadResourcePacks(dir string) ([]*resource.Pack, error) {
	return LoadResourcePacksWithContentKeys(dir, nil)
}

// LoadResourcePacksWithContentKeys attempts to load all resource packs in the provided directory and applies the
// provided content keys to matching pack UUIDs.
func LoadResourcePacksWithContentKeys(dir string, encryptionKeys map[string]string) ([]*resource.Pack, error) {
	if err := ensureResourcePackDir(dir); err != nil {
		return nil, err
	}

	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	packs := make([]*resource.Pack, 0, len(files))
	for _, file := range files {
		path := filepath.Join(dir, file.Name())
		if err := validateResourcePackPath(path); err != nil {
			return nil, err
		}
		pack, err := resource.ReadPath(path)
		if err != nil {
			return nil, err
		}
		if key, ok := encryptionKeys[pack.UUID().String()]; ok {
			pack = pack.WithContentKey(key)
		}
		packs = append(packs, pack)
	}
	return packs, nil
}

func ensureResourcePackDir(dir string) error {
	err := os.MkdirAll(dir, os.ModePerm)
	if err != nil && !os.IsExist(err) {
		return err
	}
	return nil
}

func validateResourcePackPath(path string) error {
	return filepath.WalkDir(path, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("resource pack path contains symbolic link: %s", path)
		}
		return nil
	})
}
