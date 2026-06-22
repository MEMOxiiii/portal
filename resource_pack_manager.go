package portal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/paroxity/portal/internal"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
	"github.com/sandertv/gophertunnel/minecraft/resource"
)

// ResourcePackManager stores the active resource pack set and can reload it without restarting the proxy.
type ResourcePackManager struct {
	dir            string
	encryptionKeys map[string]string

	reloadMu    sync.Mutex
	mu          sync.RWMutex
	packs       []*resource.Pack
	fingerprint string
}

// NewResourcePackManager creates a resource pack manager and loads the initial resource pack snapshot.
func NewResourcePackManager(dir string, encryptionKeys map[string]string) (*ResourcePackManager, error) {
	m := &ResourcePackManager{
		dir:            dir,
		encryptionKeys: cloneStringMap(encryptionKeys),
	}
	if err := m.Reload(); err != nil {
		return nil, err
	}
	return m, nil
}

// ResourcePacks returns the currently active resource pack snapshot.
func (m *ResourcePackManager) ResourcePacks() []*resource.Pack {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return slices.Clone(m.packs)
}

// FetchResourcePacks returns the active pack snapshot using the signature expected by minecraft.ListenConfig.
func (m *ResourcePackManager) FetchResourcePacks(login.IdentityData, login.ClientData, []*resource.Pack) []*resource.Pack {
	return m.ResourcePacks()
}

// Reload loads resource packs from disk and swaps them in atomically if loading succeeds.
func (m *ResourcePackManager) Reload() error {
	m.reloadMu.Lock()
	defer m.reloadMu.Unlock()

	packs, fingerprint, err := m.load()
	if err != nil {
		return err
	}

	m.mu.Lock()
	m.packs = packs
	m.fingerprint = fingerprint
	m.mu.Unlock()
	return nil
}

// ReloadIfChanged reloads resource packs when their files changed since the last successful load.
func (m *ResourcePackManager) ReloadIfChanged() (bool, error) {
	fingerprint, err := resourcePackFingerprint(m.dir)
	if err != nil {
		return false, err
	}

	m.mu.RLock()
	unchanged := fingerprint == m.fingerprint
	m.mu.RUnlock()
	if unchanged {
		return false, nil
	}

	if err := m.Reload(); err != nil {
		return false, err
	}
	return true, nil
}

// StartHotReload checks for resource pack updates until ctx is cancelled. Failed reloads keep the previous snapshot.
func (m *ResourcePackManager) StartHotReload(ctx context.Context, interval time.Duration, log internal.Logger) {
	if interval <= 0 {
		interval = 30 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			changed, err := m.ReloadIfChanged()
			if err != nil {
				log.Errorf("failed to hot reload resource packs: %v", err)
				continue
			}
			if changed {
				log.Infof("hot reloaded %d resource pack(s)", len(m.ResourcePacks()))
			}
		}
	}
}

func (m *ResourcePackManager) load() ([]*resource.Pack, string, error) {
	if err := ensureResourcePackDir(m.dir); err != nil {
		return nil, "", err
	}
	before, err := resourcePackFingerprint(m.dir)
	if err != nil {
		return nil, "", err
	}
	packs, err := LoadResourcePacksWithContentKeys(m.dir, m.encryptionKeys)
	if err != nil {
		return nil, "", err
	}
	after, err := resourcePackFingerprint(m.dir)
	if err != nil {
		return nil, "", err
	}
	if before != after {
		return nil, "", fmt.Errorf("resource packs changed while loading")
	}
	return packs, after, nil
}

func resourcePackFingerprint(dir string) (string, error) {
	hash := sha256.New()
	if err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("resource pack path contains symbolic link: %s", path)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if d.IsDir() {
			_, _ = fmt.Fprintf(hash, "%s\x00dir\x00", rel)
			return nil
		}
		_, _ = fmt.Fprintf(hash, "%s\x00file\x00%d\x00%d\x00", rel, info.Size(), info.ModTime().UnixNano())
		return nil
	}); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func cloneStringMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
