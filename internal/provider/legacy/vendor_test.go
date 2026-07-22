package legacy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/juty9026/terrapod/internal/model"
)

func TestVendorHandlerRemovesOnlyDocumentedAntigravityAndClaudePaths(t *testing.T) {
	home := t.TempDir()
	for _, path := range []string{".local/bin/agy", ".local/bin/claude", ".local/share/claude/payload", ".claude/settings.json"} {
		absolute := filepath.Join(home, path)
		if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absolute, []byte("x"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	h := &vendorHandler{home: home}
	tests := []struct {
		id                 model.ResourceID
		pkg, kind, command string
	}{{"optional-ai.antigravity-cli", "antigravity-cli", "antigravity-native", "agy"}, {"optional-ai.claude-code", "claude-code", "claude-native", "claude"}}
	for _, tt := range tests {
		r := model.Resource{ID: tt.id, Type: model.ResourcePackage, Provider: "homebrew-cask", Package: tt.pkg, Commands: []string{tt.command}, Metadata: map[string]string{"legacy.vendor.receipt": tt.kind, "legacy.vendor.uninstall": tt.kind}}
		d := Declaration{Kind: Vendor, Package: tt.pkg, ReceiptKind: tt.kind, UninstallKind: tt.kind}
		receipt, err := h.inspect(context.Background(), r, d)
		if err != nil || !receipt.Present {
			t.Fatalf("kind=%s receipt=%#v error=%v", tt.kind, receipt, err)
		}
		if err := h.remove(context.Background(), r, d); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".local/bin/agy")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("agy remains: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".local/bin/claude")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("claude remains: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".local/share/claude")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Claude payload remains: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude/settings.json")); err != nil {
		t.Fatalf("Claude settings removed: %v", err)
	}
}

func TestVendorHandlerDetectsButDoesNotRemoveCodex(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".local/bin/codex")
	target := filepath.Join(home, ".codex/packages/standalone/bin/codex")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../.codex/packages/standalone/bin/codex", path); err != nil {
		t.Fatal(err)
	}
	h := &vendorHandler{home: home}
	r := model.Resource{ID: "optional-ai.codex", Type: model.ResourcePackage, Provider: "homebrew-cask", Package: "codex", Commands: []string{"codex"}, Metadata: map[string]string{"legacy.vendor.receipt": "codex-standalone", "legacy.vendor.uninstall": "codex-standalone"}}
	d := Declaration{Kind: Vendor, Package: "codex", ReceiptKind: "codex-standalone", UninstallKind: "codex-standalone"}
	if receipt, err := h.inspect(context.Background(), r, d); err != nil || !receipt.Present {
		t.Fatalf("receipt=%#v error=%v", receipt, err)
	}
	var unsupported *ErrUnsupportedSource
	if _, err := h.simulateRemoval(context.Background(), r, d); !errors.As(err, &unsupported) {
		t.Fatalf("error=%v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Codex changed: %v", err)
	}
}
