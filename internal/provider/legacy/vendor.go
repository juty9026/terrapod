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

type vendorHandler struct{ home string }

func WithVendor(home string) Option {
	return func(c *Coordinator) error {
		unsafe := map[string]struct{}{"/": {}, "/home": {}, "/Users": {}, "/var": {}, "/tmp": {}, "/private/tmp": {}}
		_, broad := unsafe[home]
		if !cleanAbsolute(home) || broad || strings.HasPrefix(home, "/tmp/") || strings.HasPrefix(home, "/private/tmp/") {
			return fmt.Errorf("legacy: unsafe vendor home %q", home)
		}
		return withHandler(Vendor, &vendorHandler{home: home})(c)
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
	root, err := os.OpenRoot(h.home)
	if err != nil {
		return Receipt{}, fmt.Errorf("legacy: open vendor home: %w", err)
	}
	defer root.Close()
	linkInfo, lstatErr := root.Lstat(rel)
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
		target, readErr := root.Readlink(rel)
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
	info, err := root.Stat(rel)
	if errors.Is(err, os.ErrNotExist) {
		return Receipt{}, nil
	}
	if err != nil {
		return Receipt{}, fmt.Errorf("legacy: inspect vendor receipt: %w", err)
	}
	if info.IsDir() || info.Mode().Perm()&0o111 == 0 {
		return Receipt{}, errors.New("legacy: vendor command receipt is not an executable")
	}
	if declaration.ReceiptKind == "claude-native" {
		if share, shareErr := root.Lstat(".local/share/claude"); shareErr == nil && share.Mode()&os.ModeSymlink != 0 {
			return Receipt{}, errors.New("legacy: Claude payload root is a symlink")
		} else if shareErr != nil && !errors.Is(shareErr, os.ErrNotExist) {
			return Receipt{}, shareErr
		}
	}
	return Receipt{Present: true, Paths: map[string]string{command: filepath.Join(h.home, filepath.FromSlash(rel))}}, nil
}
func (h *vendorHandler) simulateRemoval(_ context.Context, resource model.Resource, declaration Declaration) (provider.ChangeSet, error) {
	if !exactDeclaration(resource, declaration) {
		return provider.ChangeSet{}, errors.New("legacy: vendor declaration is not authorized for resource")
	}
	if declaration.UninstallKind == "codex-standalone" {
		return provider.ChangeSet{}, &ErrUnsupportedSource{Kind: Vendor}
	}
	return provider.ChangeSet{Removes: []string{declaration.Package}}, nil
}
func (h *vendorHandler) remove(_ context.Context, resource model.Resource, declaration Declaration) error {
	if !exactDeclaration(resource, declaration) {
		return errors.New("legacy: vendor declaration is not authorized for resource")
	}
	if declaration.UninstallKind == "codex-standalone" {
		return &ErrUnsupportedSource{Kind: Vendor}
	}
	rel, _, err := vendorBinary(declaration.UninstallKind)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(h.home)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := root.Remove(rel); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("legacy: remove vendor command: %w", err)
	}
	if declaration.UninstallKind == "claude-native" {
		if err := root.RemoveAll(".local/share/claude"); err != nil {
			return fmt.Errorf("legacy: remove Claude payload: %w", err)
		}
	}
	return nil
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
