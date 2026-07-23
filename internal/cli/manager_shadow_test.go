package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/juty9026/terrapod/internal/model"
)

type legacyMutationManifest struct {
	Version      int                         `json:"version"`
	MutationSets map[string][]legacyMutation `json:"mutationSets"`
}

type legacyMutation struct {
	ResourceID string
	Provider   string
	Package    string
}

func TestManagerActivationHasNoChezmoiMutationScripts(t *testing.T) {
	repo := filepath.Clean(filepath.Join("..", ".."))
	scripts, err := filepath.Glob(filepath.Join(repo, ".chezmoiscripts", "*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(scripts) != 0 {
		t.Fatalf("chezmoi mutation scripts remain: %v", scripts)
	}
}

func TestManagerCatalogOwnsEveryRecordedLegacyMutation(t *testing.T) {
	repo := filepath.Clean(filepath.Join("..", ".."))
	var manifest legacyMutationManifest
	readJSON(t, filepath.Join("testdata", "legacy_mutations.json"), &manifest)
	if manifest.Version != 2 || len(manifest.MutationSets) == 0 {
		t.Fatalf("legacy mutation evidence is incomplete: %#v", manifest)
	}
	var catalog model.Catalog
	readJSON(t, filepath.Join(repo, "catalog", "v1", "resources.json"), &catalog)
	byID := make(map[string]model.Resource, len(catalog.Resources))
	for _, item := range catalog.Resources {
		byID[string(item.ID)] = item
	}
	for set, mutations := range manifest.MutationSets {
		for _, mutation := range mutations {
			item, ok := byID[mutation.ResourceID]
			if !ok {
				t.Fatalf("%s mutation %q has no catalog owner", set, mutation.ResourceID)
			}
			if item.Provider != mutation.Provider || item.Package != mutation.Package {
				t.Fatalf("%s mutation %q = %s/%s, catalog = %s/%s", set, mutation.ResourceID, mutation.Provider, mutation.Package, item.Provider, item.Package)
			}
		}
	}
}

func readJSON(t *testing.T, path string, target any) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(contents, target); err != nil {
		t.Fatal(err)
	}
}
