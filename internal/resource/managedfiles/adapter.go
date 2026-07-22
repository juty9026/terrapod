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
	ApplyTargetsChecked(context.Context, []chezmoi.ExpectedTarget, func(string) error) error
}

type Adapter struct {
	Client Client
	State  *state.Store
	Home   string
	Backup recovery.Backup
}

type Conflict = model.ManagedFileConflict

func (a *Adapter) Conflicts(ctx context.Context, desired model.Resource, owned model.Ownership) ([]Conflict, error) {
	if err := a.validate(); err != nil {
		return nil, err
	}
	if err := a.validateOwnershipScope(desired, owned.Paths); err != nil {
		return nil, err
	}
	targets, err := a.targets(ctx, desired)
	if err != nil {
		return nil, err
	}
	home, err := a.openHome()
	if err != nil {
		return nil, err
	}
	defer home.root.Close()
	var conflicts []Conflict
	for path := range unionPaths(targets, owned.Paths) {
		current, err := home.inspect(path)
		if err != nil {
			return nil, err
		}
		target, desiredNow := targets[path]
		ownedKind, ownedDigest, hasOwned := decodeOwned(owned.Paths[path])
		currentState := exportedPathState(current)
		if desiredNow {
			matchesDesired := current.exists && current.kind == target.Kind && current.digest == target.Digest
			matchesOwned := hasOwned && current.exists && current.kind == ownedKind && current.digest == ownedDigest
			if current.exists && !matchesDesired && !matchesOwned && (hasOwned || current.kind == "symlink") {
				conflicts = append(conflicts, Conflict{Path: path, Current: currentState, Desired: model.ManagedFilePathState{Exists: true, Kind: target.Kind, Digest: target.Digest}})
			}
		} else if current.exists && (!hasOwned || current.kind != ownedKind || current.digest != ownedDigest) {
			conflicts = append(conflicts, Conflict{Path: path, Obsolete: true, Current: currentState})
		}
	}
	sort.Slice(conflicts, func(i, j int) bool { return conflicts[i].Path < conflicts[j].Path })
	return conflicts, nil
}

func (a *Adapter) ValidateConflictBaseline(desired model.Resource, conflicts []Conflict) error {
	if err := a.validate(); err != nil {
		return err
	}
	if len(conflicts) == 0 {
		return errors.New("managed-files: conflict baseline is empty")
	}
	previous := ""
	paths := make(map[string]string, len(conflicts))
	for _, conflict := range conflicts {
		if !filepath.IsAbs(conflict.Path) || filepath.Clean(conflict.Path) != conflict.Path || conflict.Path <= previous {
			return errors.New("managed-files: conflict baseline is not canonical")
		}
		previous = conflict.Path
		if !validConflictState(conflict.Current, true) {
			return fmt.Errorf("managed-files: invalid current conflict state at %s", conflict.Path)
		}
		if conflict.Obsolete {
			if conflict.Desired != (model.ManagedFilePathState{}) {
				return fmt.Errorf("managed-files: obsolete conflict has desired state at %s", conflict.Path)
			}
		} else if !validConflictState(conflict.Desired, true) || conflict.Current.Kind != conflict.Desired.Kind {
			return fmt.Errorf("managed-files: invalid desired conflict state at %s", conflict.Path)
		}
		paths[conflict.Path] = "bound"
	}
	return a.validateOwnershipScope(desired, paths)
}

func conflictSubset(current, approved []Conflict) bool {
	approvedByPath := make(map[string]Conflict, len(approved))
	for _, conflict := range approved {
		approvedByPath[conflict.Path] = conflict
	}
	for _, conflict := range current {
		if expected, ok := approvedByPath[conflict.Path]; !ok || !reflect.DeepEqual(conflict, expected) {
			return false
		}
	}
	return true
}

type pathState struct {
	kind   string
	digest string
	exists bool
}

func exportedPathState(value pathState) model.ManagedFilePathState {
	return model.ManagedFilePathState{Exists: value.exists, Kind: value.kind, Digest: value.digest}
}

func internalPathState(value model.ManagedFilePathState) pathState {
	return pathState{exists: value.Exists, kind: value.Kind, digest: value.Digest}
}

func validConflictState(value model.ManagedFilePathState, mustExist bool) bool {
	if value.Exists != mustExist {
		return false
	}
	if !value.Exists {
		return value.Kind == "" && value.Digest == ""
	}
	digest, err := hex.DecodeString(value.Digest)
	return (value.Kind == "file" || value.Kind == "symlink") && err == nil && len(digest) == sha256.Size
}

type homeFS struct {
	root *os.Root
	path string
	info os.FileInfo
}

var beforeManagedRemove func()
var beforeManagedConflictMutation func()

func Digest(_ string, data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (a *Adapter) Inspect(ctx context.Context, desired model.Resource) (model.Observation, error) {
	if err := a.validate(); err != nil {
		return model.Observation{}, err
	}
	targets, err := a.targets(ctx, desired)
	if err != nil {
		return model.Observation{}, err
	}
	snapshot, err := a.State.Snapshot()
	if err != nil {
		return model.Observation{}, err
	}
	owned := snapshot.Ownership[desired.ID]
	if err := a.validateOwnershipScope(desired, owned.Paths); err != nil {
		return model.Observation{}, err
	}
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
	if err := a.validateOwnershipScope(desired, owned.Paths); err != nil {
		return nil, err
	}
	targets, err := a.targets(ctx, desired)
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
			if current.exists && current.kind == "symlink" && current.digest != target.Digest {
				return nil, fmt.Errorf("managed-files conflict at %s: refusing unowned symlink replacement", path)
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
		if desiredNow && current.kind == "symlink" && !matchesDesired && !matchesOwned {
			return nil, fmt.Errorf("managed-files conflict at %s: refusing symlink replacement", path)
		}
		if current.exists && !matchesOwned && !matchesDesired {
			return nil, fmt.Errorf("managed-files conflict at %s: local content differs from ownership receipt", path)
		}
		if desiredNow {
			changed = changed || !matchesDesired || !ok || ownedKind != target.Kind || ownedDigest != target.Digest
		} else {
			changed = true
		}
	}
	if !changed {
		return nil, nil
	}
	return []model.Operation{a.operation(desired, model.OperationUpgrade)}, nil
}

func (a *Adapter) Execute(ctx context.Context, operation model.Operation) model.OperationResult {
	return model.OperationResult{OperationID: operation.ID, ResourceID: operation.ResourceID, Detail: "managed-files: signed resource scope is required", FinishedAt: time.Now().UTC()}
}

func (a *Adapter) ExecuteResource(ctx context.Context, desired model.Resource, operation model.Operation) model.OperationResult {
	result := model.OperationResult{OperationID: operation.ID, ResourceID: operation.ResourceID, FinishedAt: time.Now().UTC()}
	if err := a.execute(ctx, desired, operation); err != nil {
		result.Detail = err.Error()
		return result
	}
	result.Success = true
	return result
}

func (a *Adapter) execute(ctx context.Context, desired model.Resource, operation model.Operation) error {
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
	if err := a.validateOwnershipScope(desired, owned.Paths); err != nil {
		return err
	}
	home, err := a.openHome()
	if err != nil {
		return err
	}
	defer home.root.Close()
	if operation.Kind == model.OperationPrune {
		return a.removeOwned(ctx, home, owned.Paths)
	}
	if operation.Kind != model.OperationInstall && operation.Kind != model.OperationAdopt && operation.Kind != model.OperationUpgrade && operation.Kind != model.OperationRestore {
		return fmt.Errorf("managed-files: unsupported operation %q", operation.Kind)
	}
	targets, err := a.targets(ctx, desired)
	if err != nil {
		return err
	}
	paths := sortedTargetPaths(targets)
	authorized := make(map[string]pathState, len(paths))
	applyPaths := make([]string, 0, len(paths))
	for _, path := range paths {
		target := targets[path]
		current, err := home.inspect(path)
		if err != nil {
			return err
		}
		if current.exists && current.kind != target.Kind {
			return fmt.Errorf("managed-files: unsafe type replacement at %s", path)
		}
		if owned.Paths[path] == "" && current.exists && current.digest != target.Digest {
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
		if err := a.authorizeDesiredMutation(home, path, targets[path], owned.Paths[path], authorized[path]); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := a.Client.ApplyTargetsChecked(ctx, expectedTargets(targets, applyPaths), func(path string) error {
		return a.authorizeDesiredMutation(home, path, targets[path], owned.Paths[path], authorized[path])
	}); err != nil {
		return fmt.Errorf("managed-files: apply desired targets: %w", err)
	}
	obsolete := make(map[string]string)
	for path, receipt := range owned.Paths {
		if _, remains := targets[path]; !remains {
			obsolete[path] = receipt
		}
	}
	return a.removeOwned(ctx, home, obsolete)
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
func (a *Adapter) ResolveConflict(ctx context.Context, desired model.Resource, journal string, approved []Conflict) error {
	if err := a.validate(); err != nil {
		return err
	}
	if desired.Type != model.ResourceManagedFiles || desired.Provider != "chezmoi" || desired.ID == "" {
		return errors.New("managed-files: resolve requires an exact managed-files/chezmoi resource")
	}
	snapshot, err := a.State.Snapshot()
	if err != nil {
		return err
	}
	if err := validateActiveConflictAuthorization(snapshot, desired, journal, approved); err != nil {
		return err
	}
	owned := snapshot.Ownership[desired.ID]
	if err := a.validateOwnershipScope(desired, owned.Paths); err != nil {
		return err
	}
	if err := a.ValidateConflictBaseline(desired, approved); err != nil {
		return err
	}
	fresh, err := a.Conflicts(ctx, desired, owned)
	if err != nil {
		return err
	}
	if !conflictSubset(fresh, approved) {
		return errors.New("managed-files: current conflicts exceed approved baseline")
	}
	if len(fresh) == 0 {
		return errors.New("managed-files: resource has no resolvable conflict")
	}
	for _, conflict := range fresh {
		if err := a.Backup.Save(journal, conflict.Path); err != nil {
			return err
		}
	}
	if beforeManagedConflictMutation != nil {
		beforeManagedConflictMutation()
	}
	latest, err := a.Conflicts(ctx, desired, owned)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(latest, fresh) {
		return errors.New("managed-files: conflicts changed before first mutation")
	}
	home, err := a.openHome()
	if err != nil {
		return err
	}
	defer home.root.Close()
	authorized := make(map[string]pathState, len(fresh))
	var expected []chezmoi.ExpectedTarget
	var obsolete []string
	for _, conflict := range fresh {
		authorized[conflict.Path] = internalPathState(conflict.Current)
		if conflict.Obsolete {
			obsolete = append(obsolete, conflict.Path)
		} else {
			expected = append(expected, chezmoi.ExpectedTarget{Path: conflict.Path, Kind: conflict.Desired.Kind, Digest: conflict.Desired.Digest})
		}
	}
	if err := a.Client.ApplyTargetsChecked(ctx, expected, func(path string) error {
		current, err := home.inspect(path)
		if err != nil {
			return err
		}
		if current != authorized[path] {
			return fmt.Errorf("managed-files: conflict changed immediately before resolution at %s", path)
		}
		return nil
	}); err != nil {
		return err
	}
	for _, path := range obsolete {
		current, err := home.inspect(path)
		if err != nil {
			return err
		}
		if current != authorized[path] {
			return fmt.Errorf("managed-files: obsolete conflict changed immediately before resolution at %s", path)
		}
		if beforeManagedRemove != nil {
			beforeManagedRemove()
		}
		if err := home.removeExact(path, authorized[path]); err != nil {
			return err
		}
		if err := home.removeEmptyParents(filepath.Dir(path)); err != nil {
			return err
		}
	}
	return nil
}

func validateActiveConflictAuthorization(snapshot model.Snapshot, desired model.Resource, journal string, approved []Conflict) error {
	if snapshot.ActiveJournal == nil || snapshot.ActiveJournal.ID != journal || len(snapshot.ActiveJournal.Plan.Operations) != 1 {
		return errors.New("managed-files: exact active resolution journal is required")
	}
	operation := snapshot.ActiveJournal.Plan.Operations[0]
	authorization := operation.ManagedFileAuthorization
	if authorization == nil || authorization.Version != 1 || !reflect.DeepEqual(authorization.Resource, desired) || !reflect.DeepEqual(authorization.Conflicts, approved) ||
		operation.ID != "resolve-managed-files-"+string(desired.ID) || operation.ResourceID != desired.ID || operation.Provider != desired.Provider || operation.Package != desired.Package {
		return errors.New("managed-files: active journal does not authorize the conflict baseline")
	}
	if authorization.Mode == "current" {
		if operation.Kind != model.OperationVerify || len(operation.Removes) != 0 {
			return errors.New("managed-files: active journal has invalid current resolution semantics")
		}
	} else if authorization.Mode == "historical" {
		if operation.Kind != model.OperationPrune || !reflect.DeepEqual(operation.Removes, []string{desired.Package}) {
			return errors.New("managed-files: active journal has invalid historical resolution semantics")
		}
	} else {
		return errors.New("managed-files: active journal has invalid resolution mode")
	}
	return nil
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

func (a *Adapter) targets(ctx context.Context, desired model.Resource) (map[string]chezmoi.Target, error) {
	scope, err := managedScope(desired)
	if err != nil {
		return nil, err
	}
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
		if !scopeContains(scope, filepath.ToSlash(rel)) {
			continue
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

func managedScope(item model.Resource) (string, error) {
	scope, ok := item.Metadata[model.ManagedFilesScopeMetadataKey]
	if !ok || scope == "" || filepath.IsAbs(scope) || filepath.ToSlash(filepath.Clean(scope)) != scope || scope == ".." || strings.HasPrefix(scope, "../") || strings.Contains(scope, "\\") {
		return "", fmt.Errorf("managed-files: resource %q has no safe signed target scope", item.ID)
	}
	return scope, nil
}

func scopeContains(scope, relative string) bool {
	return scope == "." || relative == scope || strings.HasPrefix(relative, scope+"/")
}

func (a *Adapter) validateOwnershipScope(item model.Resource, paths map[string]string) error {
	scope, err := managedScope(item)
	if err != nil {
		return err
	}
	for path := range paths {
		rel, err := filepath.Rel(filepath.Clean(a.Home), filepath.Clean(path))
		if err != nil || !scopeContains(scope, filepath.ToSlash(rel)) {
			return fmt.Errorf("managed-files: ownership path %q is outside signed scope %q", path, scope)
		}
	}
	return nil
}

func (a *Adapter) authorizeDesiredMutation(home *homeFS, path string, target chezmoi.Target, receipt string, expected pathState) error {
	current, err := home.inspect(path)
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

func (a *Adapter) removeOwned(ctx context.Context, home *homeFS, paths map[string]string) error {
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
		current, err := home.inspect(path)
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
		current, err = home.inspect(path)
		if err != nil || !current.exists || current.kind != kind || current.digest != digest {
			return fmt.Errorf("managed-files: path changed immediately before prune at %s", path)
		}
		if beforeManagedRemove != nil {
			beforeManagedRemove()
		}
		if err := home.removeExact(path, current); err != nil {
			return fmt.Errorf("managed-files: prune %s: %w", path, err)
		}
		if err := home.removeEmptyParents(filepath.Dir(path)); err != nil {
			return err
		}
	}
	return nil
}

func (a *Adapter) inspectPath(path string) (pathState, error) {
	home, err := a.openHome()
	if err != nil {
		return pathState{}, err
	}
	defer home.root.Close()
	return home.inspect(path)
}

func (a *Adapter) openHome() (*homeFS, error) {
	info, err := os.Lstat(a.Home)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("managed-files: home is not a real directory")
	}
	root, err := os.OpenRoot(a.Home)
	if err != nil {
		return nil, err
	}
	anchored, err := root.Stat(".")
	if err != nil || !os.SameFile(info, anchored) {
		root.Close()
		return nil, errors.New("managed-files: home changed while opening")
	}
	return &homeFS{root: root, path: filepath.Clean(a.Home), info: info}, nil
}
func (h *homeFS) relative(path string) (string, error) {
	if !within(h.path, path) {
		return "", fmt.Errorf("managed-files: target %q escapes home", path)
	}
	return filepath.Rel(h.path, filepath.Clean(path))
}
func (h *homeFS) verifyAnchor() error {
	current, err := os.Lstat(h.path)
	if err != nil || current.Mode()&os.ModeSymlink != 0 || !os.SameFile(h.info, current) {
		return errors.New("managed-files: home root changed before mutation")
	}
	return nil
}
func (h *homeFS) inspect(path string) (pathState, error) {
	relative, err := h.relative(path)
	if err != nil {
		return pathState{}, err
	}
	parts := strings.Split(relative, string(filepath.Separator))
	current := ""
	for _, part := range parts[:len(parts)-1] {
		current = filepath.Join(current, part)
		info, err := h.root.Lstat(current)
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
	info, err := h.root.Lstat(relative)
	if errors.Is(err, os.ErrNotExist) {
		return pathState{}, nil
	}
	if err != nil {
		return pathState{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := h.root.Readlink(relative)
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
	file, err := h.root.OpenFile(relative, os.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return pathState{}, err
	}
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

func (h *homeFS) removeExact(path string, expected pathState) error {
	if err := h.verifyAnchor(); err != nil {
		return err
	}
	current, err := h.inspect(path)
	if err != nil {
		return err
	}
	if current != expected {
		return fmt.Errorf("managed-files: path changed immediately before remove at %s", path)
	}
	relative, err := h.relative(path)
	if err != nil {
		return err
	}
	if err := h.root.Remove(relative); err != nil {
		return err
	}
	return h.syncDirectory(filepath.Dir(relative))
}
func (h *homeFS) removeEmptyParents(path string) error {
	for current := filepath.Clean(path); current != h.path; current = filepath.Dir(current) {
		relative, err := h.relative(current)
		if err != nil {
			return err
		}
		info, err := h.root.Lstat(relative)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		directory, err := h.root.OpenRoot(relative)
		if err != nil {
			return err
		}
		anchored, statErr := directory.Stat(".")
		if statErr != nil || !os.SameFile(info, anchored) {
			directory.Close()
			if statErr != nil {
				return statErr
			}
			return errors.New("managed-files: directory changed while pruning parents")
		}
		opened, err := directory.Open(".")
		if err != nil {
			directory.Close()
			return err
		}
		_, readErr := opened.Readdirnames(1)
		_ = opened.Close()
		_ = directory.Close()
		if readErr != io.EOF {
			if readErr == nil {
				return nil
			}
			return readErr
		}
		if err := h.verifyAnchor(); err != nil {
			return err
		}
		latest, err := h.root.Lstat(relative)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if latest.Mode()&os.ModeSymlink != 0 || !os.SameFile(info, latest) {
			return errors.New("managed-files: directory changed while pruning parents")
		}
		if err := h.root.Remove(relative); err != nil {
			return err
		}
		if err := h.syncDirectory(filepath.Dir(relative)); err != nil {
			return err
		}
	}
	return nil
}

func (h *homeFS) syncDirectory(path string) error {
	directory, err := h.root.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
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

func expectedTargets(targets map[string]chezmoi.Target, paths []string) []chezmoi.ExpectedTarget {
	expected := make([]chezmoi.ExpectedTarget, len(paths))
	for index, path := range paths {
		target := targets[path]
		expected[index] = chezmoi.ExpectedTarget{Path: path, Kind: target.Kind, Digest: target.Digest}
	}
	return expected
}

func within(base, path string) bool {
	rel, err := filepath.Rel(filepath.Clean(base), filepath.Clean(path))
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
