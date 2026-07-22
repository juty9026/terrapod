package legacy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/provider"
)

type vendorHandler struct {
	home string
	root *os.Root
}

func WithVendor(home string) Option {
	return func(c *Coordinator) error {
		unsafe := map[string]struct{}{"/": {}, "/home": {}, "/Users": {}, "/var": {}, "/tmp": {}, "/private/tmp": {}}
		_, broad := unsafe[home]
		if !cleanAbsolute(home) || broad || strings.HasPrefix(home, "/tmp/") || strings.HasPrefix(home, "/private/tmp/") {
			return fmt.Errorf("legacy: unsafe vendor home %q", home)
		}
		return withVendorRoot(home)(c)
	}
}

func withVendorRoot(home string) Option {
	return func(c *Coordinator) error {
		parent, err := os.OpenRoot(filepath.Dir(home))
		if err != nil {
			return fmt.Errorf("legacy: open vendor home parent: %w", err)
		}
		defer parent.Close()
		base := filepath.Base(home)
		info, err := parent.Lstat(base)
		if err != nil {
			return fmt.Errorf("legacy: inspect vendor home: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errors.New("legacy: vendor home must be a real directory")
		}
		root, err := parent.OpenRoot(base)
		if err != nil {
			return fmt.Errorf("legacy: pin vendor home: %w", err)
		}
		handler := &vendorHandler{home: home, root: root}
		if err := withHandler(Vendor, handler)(c); err != nil {
			_ = root.Close()
			return err
		}
		c.closers = append(c.closers, root.Close)
		return nil
	}
}

func (h *vendorHandler) inspect(_ context.Context, resource model.Resource, declaration Declaration) (Receipt, error) {
	if !exactDeclaration(resource, declaration) {
		return Receipt{}, errors.New("legacy: vendor declaration is not authorized for resource")
	}
	rel, command, err := vendorBinary(declaration.ReceiptKind)
	if err != nil {
		return Receipt{}, err
	}
	if h.root == nil {
		return Receipt{}, errors.New("legacy: vendor home is not pinned")
	}
	linkInfo, lstatErr := h.root.Lstat(rel)
	if errors.Is(lstatErr, os.ErrNotExist) {
		return Receipt{}, nil
	}
	if lstatErr != nil {
		return Receipt{}, lstatErr
	}
	if declaration.ReceiptKind == "codex-standalone" {
		if linkInfo.Mode()&os.ModeSymlink == 0 {
			return Receipt{}, errors.New("legacy: Codex receipt is not a standalone symlink")
		}
		target, readErr := h.root.Readlink(rel)
		if readErr != nil {
			return Receipt{}, readErr
		}
		absoluteTarget := target
		if !filepath.IsAbs(target) {
			absoluteTarget = filepath.Clean(filepath.Join(h.home, filepath.Dir(filepath.FromSlash(rel)), target))
		}
		payloadRoot := filepath.Join(h.home, ".codex", "packages", "standalone")
		if !cleanAbsolute(absoluteTarget) || !pathWithin(absoluteTarget, payloadRoot) {
			return Receipt{}, errors.New("legacy: Codex symlink does not target standalone payload")
		}
	}
	if declaration.ReceiptKind == "claude-native" {
		share, shareErr := h.root.Lstat(".local/share/claude")
		if shareErr != nil || share.Mode()&os.ModeSymlink != 0 || !share.IsDir() {
			return Receipt{}, errors.New("legacy: Claude payload root must be a real directory")
		}
		if linkInfo.Mode()&os.ModeSymlink == 0 {
			return Receipt{}, errors.New("legacy: Claude command receipt must be a symlink")
		}
		target, readErr := h.root.Readlink(rel)
		if readErr != nil {
			return Receipt{}, readErr
		}
		absoluteTarget := target
		if !filepath.IsAbs(target) {
			absoluteTarget = filepath.Clean(filepath.Join(h.home, filepath.Dir(filepath.FromSlash(rel)), target))
		}
		shareRoot := filepath.Join(h.home, ".local/share/claude")
		if !cleanAbsolute(absoluteTarget) || !pathWithin(absoluteTarget, shareRoot) {
			return Receipt{}, errors.New("legacy: Claude command symlink escapes payload root")
		}
	}
	info, err := h.root.Stat(rel)
	if errors.Is(err, os.ErrNotExist) {
		return Receipt{}, nil
	}
	if err != nil {
		return Receipt{}, fmt.Errorf("legacy: inspect vendor receipt: %w", err)
	}
	if info.IsDir() || info.Mode().Perm()&0o111 == 0 {
		return Receipt{}, errors.New("legacy: vendor command receipt is not an executable")
	}
	return Receipt{Present: true, Paths: map[string]string{command: filepath.Join(h.home, filepath.FromSlash(rel))}}, nil
}
func (h *vendorHandler) simulateRemoval(_ context.Context, resource model.Resource, declaration Declaration) (provider.ChangeSet, error) {
	if !exactDeclaration(resource, declaration) {
		return provider.ChangeSet{}, errors.New("legacy: vendor declaration is not authorized for resource")
	}
	return provider.ChangeSet{}, &ErrUnsupportedSource{Kind: Vendor}
}
func (h *vendorHandler) remove(_ context.Context, resource model.Resource, declaration Declaration) error {
	if !exactDeclaration(resource, declaration) {
		return errors.New("legacy: vendor declaration is not authorized for resource")
	}
	if declaration.UninstallKind == "codex-standalone" {
		return &ErrUnsupportedSource{Kind: Vendor}
	}
	_, _, err := vendorBinary(declaration.UninstallKind)
	if err != nil {
		return err
	}
	return &ErrUnsupportedSource{Kind: Vendor}
}
func vendorBinary(kind string) (string, string, error) {
	switch kind {
	case "antigravity-native":
		return ".local/bin/agy", "agy", nil
	case "claude-native":
		return ".local/bin/claude", "claude", nil
	case "codex-standalone":
		return ".local/bin/codex", "codex", nil
	default:
		return "", "", fmt.Errorf("legacy: unsupported vendor kind %q", kind)
	}
}
