package portal

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestResourcePackManagerReloadIfChanged(t *testing.T) {
	dir := t.TempDir()
	writeTestResourcePack(t, dir, "one", "11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222")

	manager, err := NewResourcePackManager(dir, nil)
	if err != nil {
		t.Fatalf("create manager: %v", err)
	}
	if packs := manager.ResourcePacks(); len(packs) != 1 {
		t.Fatalf("expected 1 resource pack, got %d", len(packs))
	}

	changed, err := manager.ReloadIfChanged()
	if err != nil {
		t.Fatalf("reload unchanged packs: %v", err)
	}
	if changed {
		t.Fatal("expected unchanged resource packs")
	}

	time.Sleep(10 * time.Millisecond)
	writeTestResourcePack(t, dir, "two", "33333333-3333-3333-3333-333333333333", "44444444-4444-4444-4444-444444444444")

	changed, err = manager.ReloadIfChanged()
	if err != nil {
		t.Fatalf("reload changed packs: %v", err)
	}
	if !changed {
		t.Fatal("expected changed resource packs")
	}
	if packs := manager.ResourcePacks(); len(packs) != 2 {
		t.Fatalf("expected 2 resource packs, got %d", len(packs))
	}
}

func TestResourcePackManagerKeepsSnapshotAfterFailedReload(t *testing.T) {
	dir := t.TempDir()
	writeTestResourcePack(t, dir, "one", "11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222")

	manager, err := NewResourcePackManager(dir, nil)
	if err != nil {
		t.Fatalf("create manager: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "broken.mcpack"), []byte("not a resource pack"), 0o600); err != nil {
		t.Fatalf("write broken pack: %v", err)
	}
	changed, err := manager.ReloadIfChanged()
	if err == nil {
		t.Fatal("expected failed reload")
	}
	if changed {
		t.Fatal("failed reload should not report a changed active snapshot")
	}
	if packs := manager.ResourcePacks(); len(packs) != 1 {
		t.Fatalf("expected previous snapshot to remain active, got %d pack(s)", len(packs))
	}
}

func TestLoadResourcePacksRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	targetDir := t.TempDir()
	writeTestResourcePack(t, targetDir, "linked", "11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222")

	if err := os.Symlink(filepath.Join(targetDir, "linked"), filepath.Join(dir, "linked")); err != nil {
		t.Skipf("symlinks are not available: %v", err)
	}
	if _, err := LoadResourcePacks(dir); err == nil {
		t.Fatal("expected symlinked resource pack to be rejected")
	}
}

func writeTestResourcePack(t *testing.T, dir, name, packUUID, moduleUUID string) {
	t.Helper()

	packDir := filepath.Join(dir, name)
	if err := os.MkdirAll(packDir, 0o700); err != nil {
		t.Fatalf("create test pack dir: %v", err)
	}
	manifest := `{
	"format_version": 2,
	"header": {
		"name": "` + name + `",
		"description": "test pack",
		"uuid": "` + packUUID + `",
		"version": [1, 0, 0],
		"min_engine_version": [1, 20, 0]
	},
	"modules": [
		{
			"description": "test resources",
			"type": "resources",
			"uuid": "` + moduleUUID + `",
			"version": [1, 0, 0]
		}
	]
}`
	if err := os.WriteFile(filepath.Join(packDir, "manifest.json"), []byte(manifest), 0o600); err != nil {
		t.Fatalf("write test pack manifest: %v", err)
	}
}
