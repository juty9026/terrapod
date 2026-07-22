package legacy

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/provider"
)

type fakeHandler struct {
	receipt      Receipt
	changes      provider.ChangeSet
	removed      bool
	inspectCalls int
}

func (f *fakeHandler) Inspect(context.Context, model.Resource, Declaration) (Receipt, error) {
	f.inspectCalls++
	if f.removed {
		return Receipt{}, nil
	}
	return f.receipt, nil
}
func (f *fakeHandler) SimulateRemoval(context.Context, model.Resource, Declaration) (provider.ChangeSet, error) {
	return f.changes, nil
}
func (f *fakeHandler) Remove(context.Context, model.Resource, Declaration) error {
	f.removed = true
	return nil
}

type fakePaths struct {
	commands map[string]string
	resolved map[string]string
}

func (f fakePaths) ResolveCommand(command string) (string, error) { return f.commands[command], nil }
func (f fakePaths) EvalSymlinks(path string) (string, error) {
	if resolved, ok := f.resolved[path]; ok {
		return resolved, nil
	}
	return path, nil
}

func resource(metadata map[string]string) model.Resource {
	return model.Resource{ID: "core.ripgrep", Type: model.ResourcePackage, Provider: "homebrew-formula", Package: "ripgrep", Commands: []string{"rg"}, Metadata: metadata}
}

func TestDetectDesiredOnlyLegacyOnlyAndBoth(t *testing.T) {
	tests := []struct {
		name       string
		desired    bool
		legacy     bool
		wantLegacy int
	}{
		{"desired only", true, false, 0},
		{"legacy only", false, true, 1},
		{"both", true, true, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &fakeHandler{receipt: Receipt{Present: tt.legacy, Prefixes: []string{"/legacy/mise"}, Paths: map[string]string{"rg": "/legacy/mise/bin/rg"}}}
			paths := fakePaths{commands: map[string]string{"rg": "/legacy/mise/bin/rg"}}
			if tt.desired {
				paths.commands["rg"] = "/opt/homebrew/bin/rg"
			}
			c, err := New(map[Kind]Handler{Mise: h}, paths)
			if err != nil {
				t.Fatal(err)
			}
			got, err := c.Detect(context.Background(), resource(map[string]string{"legacy.mise.package": "aqua:BurntSushi/ripgrep"}), model.Observation{Present: tt.desired, Provider: "homebrew-formula", Package: "ripgrep"})
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Legacy) != tt.wantLegacy {
				t.Fatalf("Legacy = %#v, want %d item(s)", got.Legacy, tt.wantLegacy)
			}
			if len(c.RemovalOperations(got)) != tt.wantLegacy {
				t.Fatalf("removal operation count mismatch")
			}
		})
	}
}

func TestDetectKnownVendorReceipt(t *testing.T) {
	h := &fakeHandler{receipt: Receipt{Present: true, Paths: map[string]string{"claude": "/Users/test/.local/bin/claude"}}}
	c, err := New(map[Kind]Handler{Vendor: h}, fakePaths{commands: map[string]string{"claude": "/Users/test/.local/bin/claude"}})
	if err != nil {
		t.Fatal(err)
	}
	r := model.Resource{ID: "optional-ai.claude-code", Type: model.ResourcePackage, Provider: "homebrew-cask", Package: "claude-code", Commands: []string{"claude"}, Metadata: map[string]string{"legacy.vendor.receipt": "claude-native", "legacy.vendor.uninstall": "claude-native"}}
	got, err := c.Detect(context.Background(), r, model.Observation{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Legacy) != 1 || got.Legacy[0].ReceiptKind != "claude-native" {
		t.Fatalf("legacy = %#v", got.Legacy)
	}
}

func TestDetectRejectsUnknownAndEscapingPaths(t *testing.T) {
	tests := []struct {
		name, command string
		resolved      map[string]string
	}{
		{"unknown source", "/tmp/rg", nil},
		{"symlink outside prefix", "/legacy/mise/bin/rg", map[string]string{"/legacy/mise/bin/rg": "/tmp/rg"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &fakeHandler{receipt: Receipt{Present: true, Prefixes: []string{"/legacy/mise"}}}
			c, err := New(map[Kind]Handler{Mise: h}, fakePaths{commands: map[string]string{"rg": tt.command}, resolved: tt.resolved})
			if err != nil {
				t.Fatal(err)
			}
			inventory, err := c.Detect(context.Background(), resource(map[string]string{"legacy.mise.package": "aqua:BurntSushi/ripgrep"}), model.Observation{})
			var provenance *ErrUnknownProvenance
			if !errors.As(err, &provenance) {
				t.Fatalf("error = %v, want ErrUnknownProvenance", err)
			}
			if operations := c.RemovalOperations(inventory); len(operations) != 0 {
				t.Fatalf("operations = %#v", operations)
			}
		})
	}
}

func TestRemoveValidatesChangesAndReinventories(t *testing.T) {
	h := &fakeHandler{receipt: Receipt{Present: true}, changes: provider.ChangeSet{Removes: []string{"ripgrep", "unmanaged"}}}
	c, err := New(map[Kind]Handler{APT: h}, fakePaths{})
	if err != nil {
		t.Fatal(err)
	}
	inv := Inventory{Resource: resource(nil), Legacy: []Observation{{Kind: APT, Package: "ripgrep", Present: true}}}
	op := c.RemovalOperations(inv)[0]
	err = c.Remove(context.Background(), op)
	var unmanaged *provider.ErrUnmanagedRemoval
	if !errors.As(err, &unmanaged) {
		t.Fatalf("error = %v, want ErrUnmanagedRemoval", err)
	}
	if h.removed {
		t.Fatal("unsafe removal executed")
	}

	h.changes = provider.ChangeSet{Removes: []string{"ripgrep"}}
	if err := c.Remove(context.Background(), op); err != nil {
		t.Fatal(err)
	}
	if !h.removed || h.inspectCalls != 1 {
		t.Fatalf("removed=%v inspectCalls=%d", h.removed, h.inspectCalls)
	}
}

func TestParseDeclarationsRejectsUnknownVendorKind(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join("testdata", "fixtures.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		KnownVendorKinds  []string `json:"knownVendorKinds"`
		UnknownVendorKind string   `json:"unknownVendorKind"`
	}
	if err := json.Unmarshal(contents, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, kind := range fixture.KnownVendorKinds {
		declarations, err := ParseDeclarations(resource(map[string]string{"legacy.vendor.receipt": kind, "legacy.vendor.uninstall": kind}))
		if err != nil || len(declarations) != 1 {
			t.Fatalf("known kind %q declarations=%#v error=%v", kind, declarations, err)
		}
	}
	_, err = ParseDeclarations(resource(map[string]string{"legacy.vendor.receipt": fixture.UnknownVendorKind, "legacy.vendor.uninstall": fixture.UnknownVendorKind}))
	if err == nil {
		t.Fatal("expected error")
	}
	got := RemovalOperation{}
	if !reflect.DeepEqual(got, RemovalOperation{}) {
		t.Fatal("zero operation changed")
	}
}

func TestDetectWithoutTypedVendorHandlerFailsClosed(t *testing.T) {
	c, err := New(nil, fakePaths{commands: map[string]string{"codex": "/Users/test/.local/bin/codex"}})
	if err != nil {
		t.Fatal(err)
	}
	r := model.Resource{ID: "optional-ai.codex", Type: model.ResourcePackage, Provider: "homebrew-cask", Package: "codex", Commands: []string{"codex"}, Metadata: map[string]string{"legacy.vendor.receipt": "codex-standalone", "legacy.vendor.uninstall": "codex-standalone"}}
	inventory, err := c.Detect(context.Background(), r, model.Observation{})
	var unsupported *ErrUnsupportedSource
	if !errors.As(err, &unsupported) {
		t.Fatalf("error = %v, want ErrUnsupportedSource", err)
	}
	if operations := c.RemovalOperations(inventory); len(operations) != 0 {
		t.Fatalf("operations = %#v", operations)
	}
}
