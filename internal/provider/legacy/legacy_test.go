package legacy

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/provider"
)

type fakeHandler struct {
	receipt       Receipt
	changes       provider.ChangeSet
	removed       bool
	inspectCalls  int
	simulateCalls int
	removeCalls   int
}

func (f *fakeHandler) Inspect(context.Context, model.Resource, Declaration) (Receipt, error) {
	f.inspectCalls++
	if f.removed {
		return Receipt{}, nil
	}
	return f.receipt, nil
}
func (f *fakeHandler) SimulateRemoval(context.Context, model.Resource, Declaration) (provider.ChangeSet, error) {
	f.simulateCalls++
	return f.changes, nil
}
func (f *fakeHandler) Remove(context.Context, model.Resource, Declaration) error {
	f.removeCalls++
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
			desired := model.Observation{Present: tt.desired, Provider: "homebrew-formula", Package: "ripgrep"}
			if tt.desired {
				desired.Paths = map[string]string{"rg": "/opt/homebrew/bin/rg"}
			}
			got, err := c.Detect(context.Background(), model.ProfileMacOSTerminal, resource(map[string]string{"legacy.mise.package": "aqua:BurntSushi/ripgrep"}), desired)
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Legacy) != tt.wantLegacy {
				t.Fatalf("Legacy = %#v, want %d item(s)", got.Legacy, tt.wantLegacy)
			}
			operations, err := c.RemovalOperations(got)
			if err != nil || len(operations) != tt.wantLegacy {
				t.Fatalf("removal operations=%#v error=%v", operations, err)
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
	got, err := c.Detect(context.Background(), model.ProfileMacOSTerminal, r, model.Observation{})
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
			h := &fakeHandler{receipt: Receipt{Present: true, Prefixes: []string{"/legacy/mise"}, Paths: map[string]string{"rg": "/legacy/mise/bin/rg"}}}
			c, err := New(map[Kind]Handler{Mise: h}, fakePaths{commands: map[string]string{"rg": tt.command}, resolved: tt.resolved})
			if err != nil {
				t.Fatal(err)
			}
			inventory, err := c.Detect(context.Background(), model.ProfileMacOSTerminal, resource(map[string]string{"legacy.mise.package": "aqua:BurntSushi/ripgrep"}), model.Observation{})
			var provenance *ErrUnknownProvenance
			if !errors.As(err, &provenance) {
				t.Fatalf("error = %v, want ErrUnknownProvenance", err)
			}
			if operations, _ := c.RemovalOperations(inventory); len(operations) != 0 {
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
	aptResource := model.Resource{ID: "core.gum", Type: model.ResourcePackage, Provider: "homebrew-formula", Package: "gum", Commands: []string{"gum"}, Metadata: map[string]string{"legacy.apt.package": "gum"}}
	inv := Inventory{Resource: aptResource, Profile: model.ProfileVPSShell, Legacy: []Observation{{Kind: APT, Package: "gum", Present: true}}}
	ops, err := c.RemovalOperations(inv)
	if err != nil {
		t.Fatal(err)
	}
	op := ops[0]
	err = c.Remove(context.Background(), op)
	var unmanaged *provider.ErrUnmanagedRemoval
	if !errors.As(err, &unmanaged) {
		t.Fatalf("error = %v, want ErrUnmanagedRemoval", err)
	}
	if h.removed {
		t.Fatal("unsafe removal executed")
	}

	h.changes = provider.ChangeSet{Removes: []string{"gum"}}
	if err := c.Remove(context.Background(), op); err != nil {
		t.Fatal(err)
	}
	if !h.removed || h.inspectCalls != 3 {
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
	resources := map[string]model.Resource{
		"antigravity-native": {ID: "optional-ai.antigravity-cli", Type: model.ResourcePackage, Provider: "homebrew-cask", Package: "antigravity-cli"},
		"claude-native":      {ID: "optional-ai.claude-code", Type: model.ResourcePackage, Provider: "homebrew-cask", Package: "claude-code"},
		"codex-standalone":   {ID: "optional-ai.codex", Type: model.ResourcePackage, Provider: "homebrew-cask", Package: "codex"},
	}
	for _, kind := range fixture.KnownVendorKinds {
		r := resources[kind]
		r.Metadata = map[string]string{"legacy.vendor.receipt": kind, "legacy.vendor.uninstall": kind}
		declarations, err := ParseDeclarations(r)
		if err != nil || len(declarations) != 1 {
			t.Fatalf("known kind %q declarations=%#v error=%v", kind, declarations, err)
		}
	}
	r := resources["codex-standalone"]
	r.Metadata = map[string]string{"legacy.vendor.receipt": fixture.UnknownVendorKind, "legacy.vendor.uninstall": fixture.UnknownVendorKind}
	_, err = ParseDeclarations(r)
	if err == nil {
		t.Fatal("expected error")
	}
	h := &fakeHandler{}
	c, newErr := New(map[Kind]Handler{Vendor: h}, fakePaths{})
	if newErr != nil {
		t.Fatal(newErr)
	}
	inventory, detectErr := c.Detect(context.Background(), model.ProfileMacOSTerminal, r, model.Observation{})
	if detectErr == nil {
		t.Fatal("unknown fixture was detected")
	}
	operations, operationErr := c.RemovalOperations(inventory)
	if operationErr == nil || len(operations) != 0 {
		t.Fatalf("operations=%#v error=%v", operations, operationErr)
	}
	if h.simulateCalls != 0 || h.removeCalls != 0 {
		t.Fatalf("simulate=%d remove=%d", h.simulateCalls, h.removeCalls)
	}
}

func TestDetectWithoutTypedVendorHandlerFailsClosed(t *testing.T) {
	c, err := New(nil, fakePaths{commands: map[string]string{"codex": "/Users/test/.local/bin/codex"}})
	if err != nil {
		t.Fatal(err)
	}
	r := model.Resource{ID: "optional-ai.codex", Type: model.ResourcePackage, Provider: "homebrew-cask", Package: "codex", Commands: []string{"codex"}, Metadata: map[string]string{"legacy.vendor.receipt": "codex-standalone", "legacy.vendor.uninstall": "codex-standalone"}}
	inventory, err := c.Detect(context.Background(), model.ProfileMacOSTerminal, r, model.Observation{})
	var unsupported *ErrUnsupportedSource
	if !errors.As(err, &unsupported) {
		t.Fatalf("error = %v, want ErrUnsupportedSource", err)
	}
	if operations, _ := c.RemovalOperations(inventory); len(operations) != 0 {
		t.Fatalf("operations = %#v", operations)
	}
}

func TestRemovalOperationsRejectsForgedInventory(t *testing.T) {
	c, err := New(map[Kind]Handler{APT: &fakeHandler{}}, fakePaths{})
	if err != nil {
		t.Fatal(err)
	}
	tests := []Inventory{
		{Resource: resource(map[string]string{"legacy.mise.package": "aqua:BurntSushi/ripgrep"}), Legacy: []Observation{{Kind: APT, Package: "ripgrep", Present: true}}},
		{Resource: resource(map[string]string{"legacy.mise.package": "aqua:BurntSushi/ripgrep"}), Legacy: []Observation{{Kind: Mise, Package: "aqua:sharkdp/fd", Present: true}}},
		{Resource: model.Resource{ID: "optional-ai.claude-code", Type: model.ResourcePackage, Provider: "homebrew-cask", Package: "claude-code", Metadata: map[string]string{"legacy.vendor.receipt": "claude-native", "legacy.vendor.uninstall": "claude-native"}}, Legacy: []Observation{{Kind: Vendor, Package: "claude-code", ReceiptKind: "codex-standalone", UninstallKind: "codex-standalone", Present: true}}},
	}
	for _, inventory := range tests {
		operations, err := c.RemovalOperations(inventory)
		if err == nil || len(operations) != 0 {
			t.Fatalf("operations=%#v error=%v", operations, err)
		}
	}
}

func TestRemoveRejectsChangedReceiptBeforeMutation(t *testing.T) {
	h := &fakeHandler{receipt: Receipt{Present: true, Paths: map[string]string{"rg": "/legacy/bin/rg"}}, changes: provider.ChangeSet{Removes: []string{"aqua:BurntSushi/ripgrep"}}}
	c, err := New(map[Kind]Handler{Mise: h}, fakePaths{commands: map[string]string{"rg": "/legacy/bin/rg"}})
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := c.Detect(context.Background(), model.ProfileMacOSTerminal, resource(map[string]string{"legacy.mise.package": "aqua:BurntSushi/ripgrep"}), model.Observation{})
	if err != nil {
		t.Fatal(err)
	}
	operations, err := c.RemovalOperations(inventory)
	if err != nil {
		t.Fatal(err)
	}
	h.receipt.Paths["rg"] = "/changed/bin/rg"
	if err := c.Remove(context.Background(), operations[0]); err == nil {
		t.Fatal("expected stale receipt error")
	}
	if h.removed {
		t.Fatal("changed receipt was removed")
	}
}

func TestPrefixAloneNeverAuthorizesActiveCommand(t *testing.T) {
	h := &fakeHandler{receipt: Receipt{Present: true, Prefixes: []string{"/legacy"}, Paths: map[string]string{"rg": "/legacy/bin/other"}}}
	c, err := New(map[Kind]Handler{Mise: h}, fakePaths{commands: map[string]string{"rg": "/legacy/bin/rg"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Detect(context.Background(), model.ProfileMacOSTerminal, resource(map[string]string{"legacy.mise.package": "aqua:BurntSushi/ripgrep"}), model.Observation{})
	var provenance *ErrUnknownProvenance
	if !errors.As(err, &provenance) {
		t.Fatalf("error=%v, want ErrUnknownProvenance", err)
	}
}

func TestBtopLegacyMiseIsVPSShellOnly(t *testing.T) {
	h := &fakeHandler{receipt: Receipt{Present: true, Paths: map[string]string{"btop": "/legacy/bin/btop"}}}
	c, err := New(map[Kind]Handler{Mise: h}, fakePaths{})
	if err != nil {
		t.Fatal(err)
	}
	r := model.Resource{ID: "core.btop", Type: model.ResourcePackage, Provider: "homebrew-formula", Package: "btop", Commands: []string{"btop"}, Metadata: map[string]string{"legacy.mise.package": "aqua:aristocratos/btop", "legacy.mise.profile": "vps-shell"}}
	mac, err := c.Detect(context.Background(), model.ProfileMacOSTerminal, r, model.Observation{})
	if err != nil || len(mac.Legacy) != 0 || h.inspectCalls != 0 {
		t.Fatalf("mac inventory=%#v calls=%d error=%v", mac, h.inspectCalls, err)
	}
	c, err = New(map[Kind]Handler{Mise: h}, fakePaths{commands: map[string]string{"btop": "/legacy/bin/btop"}})
	if err != nil {
		t.Fatal(err)
	}
	vps, err := c.Detect(context.Background(), model.ProfileVPSShell, r, model.Observation{})
	if err != nil || len(vps.Legacy) != 1 || h.inspectCalls != 1 {
		t.Fatalf("vps inventory=%#v calls=%d error=%v", vps, h.inspectCalls, err)
	}
}

func TestRemovalOperationsRejectsDuplicateObservation(t *testing.T) {
	c, err := New(map[Kind]Handler{Mise: &fakeHandler{}}, fakePaths{})
	if err != nil {
		t.Fatal(err)
	}
	observation := Observation{Kind: Mise, Package: "aqua:BurntSushi/ripgrep", Present: true}
	inventory := Inventory{Resource: resource(map[string]string{"legacy.mise.package": "aqua:BurntSushi/ripgrep"}), Profile: model.ProfileMacOSTerminal, Legacy: []Observation{observation, observation}}
	operations, err := c.RemovalOperations(inventory)
	if err == nil || len(operations) != 0 {
		t.Fatalf("operations=%#v error=%v", operations, err)
	}
}

func TestRemoveIsIdempotentWhenFreshReceiptDisappeared(t *testing.T) {
	h := &fakeHandler{receipt: Receipt{Present: false}, changes: provider.ChangeSet{Removes: []string{"gum"}}}
	c, err := New(map[Kind]Handler{APT: h}, fakePaths{})
	if err != nil {
		t.Fatal(err)
	}
	r := model.Resource{ID: "core.gum", Type: model.ResourcePackage, Provider: "homebrew-formula", Package: "gum", Metadata: map[string]string{"legacy.apt.package": "gum"}}
	inventory := Inventory{Resource: r, Profile: model.ProfileVPSShell, Legacy: []Observation{{Kind: APT, Package: "gum", Present: true}}}
	operations, err := c.RemovalOperations(inventory)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Remove(context.Background(), operations[0]); err != nil {
		t.Fatal(err)
	}
	if h.simulateCalls != 0 || h.removeCalls != 0 {
		t.Fatalf("simulate=%d remove=%d", h.simulateCalls, h.removeCalls)
	}
}
