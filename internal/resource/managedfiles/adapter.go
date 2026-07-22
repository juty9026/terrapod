package managedfiles

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/juty9026/terrapod/internal/chezmoi"
	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/recovery"
	"github.com/juty9026/terrapod/internal/state"
)

type Client interface {
	TargetState(context.Context) ([]chezmoi.Target, error)
	Diff(context.Context, []string) ([]byte, error)
	ApplyTargets(context.Context, []string) error
	ApplyTargetsChecked(context.Context, []string, func(string) error) error
}

type Adapter struct {
	Client Client
	State  *state.Store
	Home   string
	Backup recovery.Backup
}

type pathState struct {
	kind   string
	digest string
	exists bool
}

func Digest(_ string, data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (a *Adapter) Inspect(ctx context.Context, desired model.Resource) (model.Observation, error) {
	if err := a.validate(); err != nil {
		return model.Observation{}, err
	}
	targets, err := a.targets(ctx)
	if err != nil {
		return model.Observation{}, err
	}
	snapshot, err := a.State.Snapshot()
	if err != nil {
		return model.Observation{}, err
	}
	owned := snapshot.Ownership[desired.ID]
	present, healthy := false, true
	if len(targets) == 0 && len(owned.Paths) != 0 {
		for path, receipt := range owned.Paths {
			current, err := a.inspectPath(path)
			if err != nil {
				return model.Observation{}, err
			}
			present = present || current.exists
			kind, digest, ok := decodeOwned(receipt)
			if current.exists && (!ok || current.kind != kind || current.digest != digest) {
				healthy = false
			}
		}
		return model.Observation{Present: present, Healthy: healthy, Provider: desired.Provider, Package: desired.Package, Paths: map[string]string{}}, nil
	}
	for path := range unionPaths(targets, owned.Paths) {
		current, err := a.inspectPath(path)
		if err != nil {
			return model.Observation{}, err
		}
		present = present || current.exists
		if target, ok := targets[path]; ok {
			healthy = healthy && current.exists && current.kind == target.Kind && current.digest == target.Digest
		} else if current.exists {
			healthy = false
		}
	}
	return model.Observation{Present: present, Healthy: healthy, Provider: desired.Provider, Package: desired.Package, Paths: desiredPaths(targets)}, nil
}

func (a *Adapter) Plan(ctx context.Context, desired model.Resource, _ model.Observation, owned model.Ownership) ([]model.Operation, error) {
	targets, err := a.targets(ctx)
	if err != nil {
		return nil, err
	}
	if len(owned.Paths) == 0 {
		allAbsent, allIdentical := true, true
		for path, target := range targets {
			current, err := a.inspectPath(path)
			if err != nil {
				return nil, err
			}
			allAbsent = allAbsent && !current.exists
			allIdentical = allIdentical && current.exists && current.kind == target.Kind && current.digest == target.Digest
			if current.exists && current.kind != target.Kind {
				return nil, fmt.Errorf("managed-files conflict at %s: refusing %s replacement with %s", path, current.kind, target.Kind)
			}
		}
		kind := model.OperationAdopt
		if allAbsent {
			kind = model.OperationInstall
		} else if allIdentical {
			kind = model.OperationAdopt
		}
		if len(targets) == 0 {
			return nil, nil
		}
		return []model.Operation{a.operation(desired, kind)}, nil
	}
	if len(targets) == 0 {
		for path, receipt := range owned.Paths {
			current, err := a.inspectPath(path)
			if err != nil {
				return nil, err
			}
			kind, digest, ok := decodeOwned(receipt)
			if current.exists && (!ok || current.kind != kind || current.digest != digest) {
				return nil, fmt.Errorf("managed-files conflict at %s: local content differs from ownership receipt", path)
			}
		}
		return []model.Operation{a.operation(desired, model.OperationPrune)}, nil
	}
	changed := false
	for path := range unionPaths(targets, owned.Paths) {
		current, err := a.inspectPath(path)
		if err != nil {
			return nil, err
		}
		ownedKind, ownedDigest, ok := decodeOwned(owned.Paths[path])
		target, desiredNow := targets[path]
		if desiredNow && current.exists && current.kind != target.Kind {
			return nil, fmt.Errorf("managed-files conflict at %s: refusing %s replacement with %s", path, current.kind, target.Kind)
		}
		matchesOwned := ok && current.exists && current.kind == ownedKind && current.digest == ownedDigest
		matchesDesired := desiredNow && current.exists && current.kind == target.Kind && current.digest == target.Digest
		if desiredNow && current.kind == "symlink" && !matchesDesired {
			return nil, fmt.Errorf("managed-files conflict at %s: refusing symlink replacement", path)
		}
		if current.exists && !matchesOwned && !matchesDesired {
			return nil, fmt.Errorf("managed-files conflict at %s: local content differs from ownership receipt", path)
		}
		if desiredNow {
			changed = changed || !matchesDesired || !ok || ownedKind != target.Kind || ownedDigest != target.Digest
		} else {
			changed = changed || current.exists
		}
	}
	if !changed {
		return nil, nil
	}
	return []model.Operation{a.operation(desired, model.OperationUpgrade)}, nil
}

func (a *Adapter) Execute(ctx context.Context, operation model.Operation) model.OperationResult {
	result := model.OperationResult{OperationID: operation.ID, ResourceID: operation.ResourceID, FinishedAt: time.Now().UTC()}
	if err := a.execute(ctx, operation); err != nil {
		result.Detail = err.Error()
		return result
	}
	result.Success = true
	return result
}

func (a *Adapter) execute(ctx context.Context, operation model.Operation) error {
	if err := a.validate(); err != nil {
		return err
	}
	snapshot, err := a.State.Snapshot()
	if err != nil {
		return err
	}
	if snapshot.ActiveJournal == nil {
		return errors.New("managed-files: active journal is required")
	}
	owned := snapshot.Ownership[operation.ResourceID]
	if operation.Kind == model.OperationPrune {
		return a.removeOwned(ctx, owned.Paths)
	}
	if operation.Kind != model.OperationInstall && operation.Kind != model.OperationAdopt && operation.Kind != model.OperationUpgrade && operation.Kind != model.OperationRestore {
		return fmt.Errorf("managed-files: unsupported operation %q", operation.Kind)
	}
	targets, err := a.targets(ctx)
	if err != nil {
		return err
	}
	paths := sortedTargetPaths(targets)
	authorized := make(map[string]pathState, len(paths))
	applyPaths := make([]string, 0, len(paths))
	for _, path := range paths {
		target := targets[path]
		current, err := a.inspectPath(path)
		if err != nil {
			return err
		}
		if current.exists && current.kind != target.Kind {
			return fmt.Errorf("managed-files: unsafe type replacement at %s", path)
		}
		if len(owned.Paths) == 0 && current.exists && current.digest != target.Digest {
			if err := a.Backup.Save(snapshot.ActiveJournal.ID, path); err != nil {
				return fmt.Errorf("managed-files: backup %s: %w", path, err)
			}
		}
		if !current.exists || current.kind != target.Kind || current.digest != target.Digest {
			authorized[path] = current
			applyPaths = append(applyPaths, path)
		}
	}
	for _, path := range applyPaths {
		if err := a.authorizeDesiredMutation(path, targets[path], owned.Paths[path], authorized[path]); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := a.Client.ApplyTargetsChecked(ctx, applyPaths, func(path string) error {
		return a.authorizeDesiredMutation(path, targets[path], owned.Paths[path], authorized[path])
	}); err != nil {
		return fmt.Errorf("managed-files: apply desired targets: %w", err)
	}
	obsolete := make(map[string]string)
	for path, receipt := range owned.Paths {
		if _, remains := targets[path]; !remains {
			obsolete[path] = receipt
		}
	}
	return a.removeOwned(ctx, obsolete)
}

func (a *Adapter) Verify(ctx context.Context, desired model.Resource) (model.Observation, error) {
	observation, err := a.Inspect(ctx, desired)
	if err != nil {
		return model.Observation{}, err
	}
	if !observation.Healthy {
		return model.Observation{}, errors.New("managed-files: desired targets are not exact")
	}
	return observation, nil
}

// ResolveConflict is the explicit managed-file conflict action: it always
// creates a recovery copy before accepting the desired target.
func (a *Adapter) ResolveConflict(ctx context.Context, desired model.Resource, journal string) error {
	if desired.Type != model.ResourceManagedFiles || desired.Provider != "chezmoi" || desired.ID == "" {
		return errors.New("managed-files: resolve requires an exact managed-files/chezmoi resource")
	}
	targets, err := a.targets(ctx)
	if err != nil {
		return err
	}
	var paths []string
	authorized := make(map[string]pathState)
	for _, path := range sortedTargetPaths(targets) {
		current, err := a.inspectPath(path)
		if err != nil {
			return err
		}
		if !current.exists || (current.kind == targets[path].Kind && current.digest == targets[path].Digest) {
			continue
		}
		if current.kind != targets[path].Kind {
			return fmt.Errorf("managed-files: unsafe type replacement at %s", path)
		}
		if err := a.Backup.Save(journal, path); err != nil {
			return err
		}
		paths = append(paths, path)
		authorized[path] = current
	}
	if len(paths) == 0 {
		return errors.New("managed-files: resource has no resolvable conflict")
	}
	for _, path := range paths {
		current, err := a.inspectPath(path)
		if err != nil {
			return err
		}
		if current != authorized[path] {
			return fmt.Errorf("managed-files: conflict changed immediately before resolution at %s", path)
		}
	}
	return a.Client.ApplyTargetsChecked(ctx, paths, func(path string) error {
		current, err := a.inspectPath(path)
		if err != nil {
			return err
		}
		if current != authorized[path] {
			return fmt.Errorf("managed-files: conflict changed immediately before resolution at %s", path)
		}
		return nil
	})
}

func (a *Adapter) Diff(ctx context.Context) ([]byte, error) {
	if err := a.validate(); err != nil {
		return nil, err
	}
	return a.Client.Diff(ctx, nil)
}

func (a *Adapter) operation(item model.Resource, kind model.OperationKind) model.Operation {
	operation := model.Operation{ID: "managed-files-" + string(kind) + "-" + string(item.ID), ResourceID: item.ID, Kind: kind, Provider: item.Provider, Package: item.Package, Detail: string(kind) + " managed files"}
	if kind == model.OperationPrune {
		operation.Removes = []string{item.Package}
	}
	return operation
}

func (a *Adapter) validate() error {
	if isNil(a.Client) || a.State == nil || !filepath.IsAbs(a.Home) || filepath.Clean(a.Backup.Base) != filepath.Clean(a.Home) {
		return errors.New("managed-files: client, state, absolute home, and matching backup base are required")
	}
	return nil
}

func isNil(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func (a *Adapter) targets(ctx context.Context) (map[string]chezmoi.Target, error) {
	list, err := a.Client.TargetState(ctx)
	if err != nil {
		return nil, err
	}
	targets := make(map[string]chezmoi.Target, len(list))
	for _, target := range list {
		path := filepath.Clean(target.Path)
		rel, err := filepath.Rel(filepath.Clean(a.Home), path)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("managed-files: target %q escapes home", target.Path)
		}
		if target.Kind != "file" && target.Kind != "symlink" {
			return nil, fmt.Errorf("managed-files: unsupported target kind %q", target.Kind)
		}
		digest := Digest(target.Kind, target.Desired)
		if target.Digest != "" && target.Digest != digest {
			return nil, fmt.Errorf("managed-files: target digest mismatch at %s", path)
		}
		target.Digest = digest
		target.Path = path
		if _, duplicate := targets[path]; duplicate {
			return nil, fmt.Errorf("managed-files: duplicate target %s", path)
		}
		targets[path] = target
	}
	return targets, nil
}

func (a *Adapter) authorizeDesiredMutation(path string, target chezmoi.Target, receipt string, expected pathState) error {
	current, err := a.inspectPath(path)
	if err != nil {
		return err
	}
	if current != expected {
		return fmt.Errorf("managed-files: target changed immediately before mutation at %s", path)
	}
	if !current.exists {
		return nil
	}
	if current.kind != target.Kind {
		return fmt.Errorf("managed-files: target type changed before mutation at %s", path)
	}
	if current.digest == target.Digest {
		return nil
	}
	if receipt == "" {
		return nil
	}
	kind, digest, ok := decodeOwned(receipt)
	if !ok || current.kind != kind || current.digest != digest {
		return fmt.Errorf("managed-files: target changed after planning at %s", path)
	}
	return nil
}

func (a *Adapter) removeOwned(ctx context.Context, paths map[string]string) error {
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		if !within(a.Home, path) {
			return fmt.Errorf("managed-files: refusing ownership path outside home: %s", path)
		}
		ordered = append(ordered, path)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(ordered)))
	for _, path := range ordered {
		if err := ctx.Err(); err != nil {
			return err
		}
		current, err := a.inspectPath(path)
		if err != nil {
			return err
		}
		if !current.exists {
			continue
		}
		kind, digest, ok := decodeOwned(paths[path])
		if !ok || current.kind != kind || current.digest != digest {
			return fmt.Errorf("managed-files: refusing to prune modified path %s", path)
		}
		current, err = a.inspectPath(path)
		if err != nil || !current.exists || current.kind != kind || current.digest != digest {
			return fmt.Errorf("managed-files: path changed immediately before prune at %s", path)
		}
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("managed-files: prune %s: %w", path, err)
		}
		a.removeEmptyParents(filepath.Dir(path))
	}
	return nil
}

func (a *Adapter) removeEmptyParents(path string) {
	home := filepath.Clean(a.Home)
	for current := filepath.Clean(path); current != home; current = filepath.Dir(current) {
		rel, err := filepath.Rel(home, current)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return
		}
		entries, err := os.ReadDir(current)
		if err != nil || len(entries) != 0 {
			return
		}
		if err := os.Remove(current); err != nil {
			return
		}
	}
}

func (a *Adapter) inspectPath(path string) (pathState, error) {
	if !within(a.Home, path) {
		return pathState{}, fmt.Errorf("managed-files: target %q escapes home", path)
	}
	relative, _ := filepath.Rel(filepath.Clean(a.Home), filepath.Clean(path))
	parts := strings.Split(relative, string(filepath.Separator))
	current := filepath.Clean(a.Home)
	for _, part := range parts[:len(parts)-1] {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			break
		}
		if err != nil {
			return pathState{}, err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return pathState{}, fmt.Errorf("managed-files: target parent %q is not a real directory", current)
		}
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return pathState{}, nil
	}
	if err != nil {
		return pathState{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		if err != nil {
			return pathState{}, err
		}
		return pathState{kind: "symlink", digest: Digest("symlink", []byte(target)), exists: true}, nil
	}
	if !info.Mode().IsRegular() {
		if info.IsDir() {
			return pathState{kind: "directory", exists: true}, nil
		}
		return pathState{kind: "special", exists: true}, nil
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return pathState{}, err
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return pathState{}, err
	}
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return pathState{}, errors.New("managed-files: target changed while hashing")
	}
	return pathState{kind: "file", digest: hex.EncodeToString(hash.Sum(nil)), exists: true}, nil
}

func desiredPaths(targets map[string]chezmoi.Target) map[string]string {
	paths := make(map[string]string, len(targets))
	for path, target := range targets {
		paths[path] = target.Kind + ":" + target.Digest
	}
	return paths
}

func decodeOwned(value string) (string, string, bool) {
	kind, digest, ok := strings.Cut(value, ":")
	return kind, digest, ok && (kind == "file" || kind == "symlink") && len(digest) == sha256.Size*2
}

func unionPaths(targets map[string]chezmoi.Target, owned map[string]string) map[string]struct{} {
	paths := make(map[string]struct{}, len(targets)+len(owned))
	for path := range targets {
		paths[path] = struct{}{}
	}
	for path := range owned {
		paths[path] = struct{}{}
	}
	return paths
}

func sortedTargetPaths(targets map[string]chezmoi.Target) []string {
	paths := make([]string, 0, len(targets))
	for path := range targets {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func within(base, path string) bool {
	rel, err := filepath.Rel(filepath.Clean(base), filepath.Clean(path))
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
