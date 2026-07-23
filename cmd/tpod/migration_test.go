package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/juty9026/terrapod/internal/model"
)

func TestMigrationDesiredUsesProfileAndConfigGates(t *testing.T) {
	catalog := model.Catalog{Resources: []model.Resource{
		{ID: "core.git"},
		{ID: "optional.ai", Profiles: []model.Profile{model.ProfileMacOSTerminal}, Metadata: map[string]string{"enabledByConfig": "enableAI"}},
		{ID: "linux.only", Profiles: []model.Profile{model.ProfileVPSShell}},
	}}
	config := model.Config{Terrapod: map[string]any{"profile": string(model.ProfileMacOSTerminal), "enableAI": true}}
	desired := migrationDesired(catalog, config, model.ProfileMacOSTerminal)
	if !desired["core.git"] || !desired["optional.ai"] || desired["linux.only"] {
		t.Fatalf("desired=%v", desired)
	}
}

func TestMigrationApplyResourcesKeepsDisabledOwnedBaselineHistorical(t *testing.T) {
	current := []model.Resource{{ID: "core.git"}, {ID: "optional.ai"}}
	baseline := append([]model.Resource(nil), current...)
	receipts := map[model.ResourceID]model.Ownership{
		"core.git":    {ResourceID: "core.git"},
		"optional.ai": {ResourceID: "optional.ai"},
	}
	enabled, historical := migrationApplyResources(current, baseline, map[model.ResourceID]bool{"core.git": true}, receipts, "legacy-digest")
	if len(enabled) != 1 || enabled[0] != "core.git" {
		t.Fatalf("enabled=%v", enabled)
	}
	if historical["optional.ai"].CatalogDigest != "legacy-digest" || historical["optional.ai"].Resource.ID != "optional.ai" {
		t.Fatalf("historical=%#v", historical)
	}
}

func TestMigrationWarningArchivalMovesExactMarker(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, "state", "terrapod")
	warningDir := filepath.Join(stateDir, "install-warnings")
	if err := os.MkdirAll(warningDir, 0o700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(warningDir, "homebrew-core")
	if err := os.WriteFile(source, []byte("warning"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := archiveLegacyWarnings(stateDir, []string{source}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(source); !os.IsNotExist(err) {
		t.Fatalf("source remains: %v", err)
	}
	archived, err := os.ReadFile(filepath.Join(stateDir, "recovery", "install-warnings", "homebrew-core"))
	if err != nil || string(archived) != "warning" {
		t.Fatalf("archive=%q err=%v", archived, err)
	}
}

func TestMigrationWarningArchivalConflictFailsBeforeRemovingMarker(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, "state", "terrapod")
	warningDir := filepath.Join(stateDir, "install-warnings")
	archiveDir := filepath.Join(stateDir, "recovery", "install-warnings")
	if err := os.MkdirAll(warningDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(archiveDir, 0o700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(warningDir, "homebrew-core")
	target := filepath.Join(archiveDir, "homebrew-core")
	if err := os.WriteFile(source, []byte("current"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := preflightLegacyWarnings(stateDir, []string{source}); err == nil {
		t.Fatal("conflicting archive passed preflight")
	}
	if err := archiveLegacyWarnings(stateDir, []string{source}); err == nil {
		t.Fatal("conflicting archive was overwritten")
	}
	contents, err := os.ReadFile(source)
	if err != nil || string(contents) != "current" {
		t.Fatalf("source=%q err=%v", contents, err)
	}
}
