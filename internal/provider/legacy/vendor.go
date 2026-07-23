package legacy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/provider"
)

type vendorHandler struct {
	home           string
	root           *os.Root
	recoverySuffix func() (string, error)
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
		handler := &vendorHandler{home: home, root: root, recoverySuffix: randomRecoverySuffix}
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
	if declaration.UninstallKind == "claude-native" {
		return provider.ChangeSet{Removes: []string{declaration.Package}}, nil
	}
	return provider.ChangeSet{}, &ErrUnsupportedSource{Kind: Vendor}
}
func (h *vendorHandler) remove(_ context.Context, resource model.Resource, declaration Declaration) error {
	if !exactDeclaration(resource, declaration) {
		return errors.New("legacy: vendor declaration is not authorized for resource")
	}
	if declaration.UninstallKind == "claude-native" {
		return h.quarantineClaude()
	}
	_, _, err := vendorBinary(declaration.UninstallKind)
	if err != nil {
		return err
	}
	return &ErrUnsupportedSource{Kind: Vendor}
}

const vendorRecoveryRoot = ".local/state/terrapod/recovery/legacy"

func (h *vendorHandler) quarantineClaude() error {
	if err := h.ensureRecoveryRoot(); err != nil {
		return err
	}
	suffix, err := h.recoverySuffix()
	if err != nil || len(suffix) != 32 {
		return errors.New("legacy: create safe recovery suffix")
	}
	for _, char := range suffix {
		if !strings.ContainsRune("0123456789abcdef", char) {
			return errors.New("legacy: create safe recovery suffix")
		}
	}
	transaction := filepath.Join(vendorRecoveryRoot, "claude-"+suffix)
	if _, err := h.root.Lstat(transaction); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			return errors.New("legacy: recovery transaction already exists")
		}
		return fmt.Errorf("legacy: inspect recovery transaction: %w", err)
	}
	if err := h.root.Mkdir(transaction, 0o700); err != nil {
		return fmt.Errorf("legacy: create recovery transaction: %w", err)
	}
	if err := h.root.Rename(".local/share/claude", filepath.Join(transaction, "payload")); err != nil {
		return fmt.Errorf("legacy: quarantine Claude payload: %w", err)
	}
	if err := h.root.Rename(".local/bin/claude", filepath.Join(transaction, "command")); err != nil {
		rollbackErr := h.root.Rename(filepath.Join(transaction, "payload"), ".local/share/claude")
		if rollbackErr != nil {
			return errors.Join(fmt.Errorf("legacy: quarantine Claude command: %w", err), fmt.Errorf("legacy: roll back Claude payload: %w", rollbackErr))
		}
		return fmt.Errorf("legacy: quarantine Claude command; payload rolled back: %w", err)
	}
	return nil
}

func (h *vendorHandler) ensureRecoveryRoot() error {
	components := []string{".local", ".local/state", ".local/state/terrapod", ".local/state/terrapod/recovery", vendorRecoveryRoot}
	for _, component := range components {
		info, err := h.root.Lstat(component)
		if errors.Is(err, os.ErrNotExist) {
			break
		}
		if err != nil {
			return fmt.Errorf("legacy: inspect recovery root: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errors.New("legacy: recovery root contains a non-directory or symlink")
		}
	}
	if err := h.root.MkdirAll(vendorRecoveryRoot, 0o700); err != nil {
		return fmt.Errorf("legacy: create recovery root: %w", err)
	}
	for _, component := range components {
		info, err := h.root.Lstat(component)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errors.New("legacy: recovery root is not a real directory tree")
		}
	}
	return nil
}

func randomRecoverySuffix() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
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
