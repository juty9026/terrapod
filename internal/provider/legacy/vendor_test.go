package legacy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/juty9026/terrapod/internal/model"
)

func vendorCoordinator(t *testing.T, home string) (*Coordinator, *vendorHandler) {
	t.Helper()
	c, err := New(fakePaths{}, withVendorRoot(home))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c, c.handlers[Vendor].(*vendorHandler)
}

func TestVendorHandlerDetectsKnownReceiptsButDoesNotMutateAmbiguousInstallers(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(filepath.Join(home, ".local/bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".local/bin/agy"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	claudeTarget := filepath.Join(home, ".local/share/claude/bin/claude")
	if err := os.MkdirAll(filepath.Dir(claudeTarget), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(claudeTarget, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../share/claude/bin/claude", filepath.Join(home, ".local/bin/claude")); err != nil {
		t.Fatal(err)
	}
	_, h := vendorCoordinator(t, home)
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
		var unsupported *ErrUnsupportedSource
		if _, err := h.simulateRemoval(context.Background(), r, d); !errors.As(err, &unsupported) {
			t.Fatalf("kind=%s error=%v", tt.kind, err)
		}
	}
	for _, path := range []string{".local/bin/agy", ".local/bin/claude", ".local/share/claude/bin/claude"} {
		if _, err := os.Lstat(filepath.Join(home, path)); err != nil {
			t.Fatalf("receipt %s changed: %v", path, err)
		}
	}
}

func TestVendorHandlerPinsHomeAndRejectsSymlinkHome(t *testing.T) {
	parent := t.TempDir()
	realHome := filepath.Join(parent, "real")
	if err := os.Mkdir(realHome, 0o755); err != nil {
		t.Fatal(err)
	}
	symlinkHome := filepath.Join(parent, "link")
	if err := os.Symlink(realHome, symlinkHome); err != nil {
		t.Fatal(err)
	}
	if _, err := New(fakePaths{}, withVendorRoot(symlinkHome)); err == nil {
		t.Fatal("accepted symlink home")
	}
	home := filepath.Join(parent, "home")
	if err := os.Mkdir(home, 0o755); err != nil {
		t.Fatal(err)
	}
	c, h := vendorCoordinator(t, home)
	moved := filepath.Join(parent, "moved")
	if err := os.Rename(home, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := h.root.Lstat("."); err != nil {
		t.Fatalf("pinned root lost after swap: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := h.root.Lstat("."); err == nil {
		t.Fatal("closed root remained usable")
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestVendorHandlerDetectsButDoesNotRemoveCodex(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
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
	_, h := vendorCoordinator(t, home)
	r := model.Resource{ID: "optional-ai.codex", Type: model.ResourcePackage, Provider: "homebrew-cask", Package: "codex", Commands: []string{"codex"}, Metadata: map[string]string{"legacy.vendor.receipt": "codex-standalone", "legacy.vendor.uninstall": "codex-standalone"}}
	d := Declaration{Kind: Vendor, Package: "codex", ReceiptKind: "codex-standalone", UninstallKind: "codex-standalone"}
	if receipt, err := h.inspect(context.Background(), r, d); err != nil || !receipt.Present {
		t.Fatalf("receipt=%#v error=%v", receipt, err)
	}
	var unsupported *ErrUnsupportedSource
	if _, err := h.simulateRemoval(context.Background(), r, d); !errors.As(err, &unsupported) {
		t.Fatalf("error=%v", err)
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("Codex changed: %v", err)
	}
}

func TestVendorHandlerRejectsSpoofedClaudeLayouts(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T, string)
	}{
		{"missing payload root", func(t *testing.T, home string) {
			t.Helper()
			if err := os.MkdirAll(filepath.Join(home, ".local/bin"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("../share/claude/bin/claude", filepath.Join(home, ".local/bin/claude")); err != nil {
				t.Fatal(err)
			}
		}},
		{"regular command file", func(t *testing.T, home string) {
			t.Helper()
			if err := os.MkdirAll(filepath.Join(home, ".local/share/claude"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(home, ".local/bin"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(home, ".local/bin/claude"), []byte("x"), 0o755); err != nil {
				t.Fatal(err)
			}
		}},
		{"escaping command symlink", func(t *testing.T, home string) {
			t.Helper()
			if err := os.MkdirAll(filepath.Join(home, ".local/share/claude"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(home, ".local/bin"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("/usr/bin/true", filepath.Join(home, ".local/bin/claude")); err != nil {
				t.Fatal(err)
			}
		}},
		{"symlink payload root", func(t *testing.T, home string) {
			t.Helper()
			actual := filepath.Join(home, "actual-claude")
			if err := os.MkdirAll(filepath.Join(actual, "bin"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(actual, "bin/claude"), []byte("x"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(home, ".local/share"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(actual, filepath.Join(home, ".local/share/claude")); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(home, ".local/bin"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("../share/claude/bin/claude", filepath.Join(home, ".local/bin/claude")); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := filepath.Join(t.TempDir(), "home")
			if err := os.Mkdir(home, 0o755); err != nil {
				t.Fatal(err)
			}
			tt.setup(t, home)
			_, handler := vendorCoordinator(t, home)
			resource := model.Resource{ID: "optional-ai.claude-code", Type: model.ResourcePackage, Provider: "homebrew-cask", Package: "claude-code", Commands: []string{"claude"}, Metadata: map[string]string{"legacy.vendor.receipt": "claude-native", "legacy.vendor.uninstall": "claude-native"}}
			declaration := Declaration{Kind: Vendor, Package: "claude-code", ReceiptKind: "claude-native", UninstallKind: "claude-native"}
			if _, err := handler.inspect(context.Background(), resource, declaration); err == nil {
				t.Fatal("accepted spoofed Claude layout")
			}
		})
	}
}

func TestWithVendorRejectsTemporaryHome(t *testing.T) {
	if _, err := New(fakePaths{}, WithVendor("/tmp/terrapod-vendor-home")); err == nil {
		t.Fatal("accepted temporary vendor home")
	}
}

func TestNewClosesPinnedVendorRootWhenLaterOptionFails(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(home, 0o755); err != nil {
		t.Fatal(err)
	}
	var pinned *os.Root
	fail := func(c *Coordinator) error {
		pinned = c.handlers[Vendor].(*vendorHandler).root
		return errors.New("later option failed")
	}
	if coordinator, err := New(fakePaths{}, withVendorRoot(home), fail); err == nil || coordinator != nil {
		t.Fatalf("coordinator=%#v error=%v", coordinator, err)
	}
	if _, err := pinned.Lstat("."); err == nil {
		t.Fatal("pinned root remained open after constructor failure")
	}
}
