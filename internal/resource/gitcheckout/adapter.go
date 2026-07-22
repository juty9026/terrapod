package gitcheckout

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/juty9026/terrapod/internal/execx"
	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/recovery"
	"github.com/juty9026/terrapod/internal/state"
)

const (
	MetadataRemote      = "git.remote"
	MetadataRef         = "git.ref"
	MetadataCommit      = "git.commit"
	MetadataDestination = "git.destination"
	MetadataVerifyFiles = "git.verifyFiles"
)

var commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var refPattern = regexp.MustCompile(`^refs/(tags|heads)/[A-Za-z0-9][A-Za-z0-9._/-]*$`)

var requiredEnv = []string{"GIT_ATTR_NOSYSTEM", "GIT_CONFIG_GLOBAL", "GIT_CONFIG_NOSYSTEM", "GIT_TERMINAL_PROMPT"}

func RequiredEnv() []string { return append([]string(nil), requiredEnv...) }

func NewRunner(preflight func(context.Context) error, effectiveUID func() int) *execx.Runner {
	return execx.NewRunner(RequiredEnv(), preflight, effectiveUID)
}

type Runner interface {
	Run(context.Context, execx.Request) (execx.Result, error)
}

type Adapter struct {
	Runner  Runner
	Git     string
	Home    string
	State   *state.Store
	Backup  recovery.Backup
	mu      sync.Mutex
	staging map[string]stagingRecord
}

type stagingCapability struct{ token, path string }
type stagingRecord struct {
	path string
	info os.FileInfo
}

var afterSnapshotValidated func()
var beforeGitMetadataCommit func() error

type declaration struct {
	remote, ref, commit, destination string
	verify                           []string
}
type checkout struct {
	exists, git, clean bool
	remote, head       string
	paths              map[string]string
}

type homeRoot struct {
	root *os.Root
	path string
	info os.FileInfo
}

func (a *Adapter) openHome() (*homeRoot, error) {
	info, err := os.Lstat(a.Home)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("git-checkout: home must be a real directory")
	}
	root, err := os.OpenRoot(a.Home)
	if err != nil {
		return nil, err
	}
	home := &homeRoot{root: root, path: a.Home, info: info}
	if err := home.verify(); err != nil {
		root.Close()
		return nil, err
	}
	return home, nil
}

func (h *homeRoot) close() { _ = h.root.Close() }

func (h *homeRoot) verify() error {
	current, err := os.Lstat(h.path)
	if err != nil || current.Mode()&os.ModeSymlink != 0 || !os.SameFile(h.info, current) {
		return errors.New("git-checkout: home identity changed")
	}
	anchored, err := h.root.Stat(".")
	if err != nil || !os.SameFile(h.info, anchored) {
		return errors.New("git-checkout: anchored home identity changed")
	}
	return nil
}

func (h *homeRoot) validateDirectory(relative string, allowMissing bool) (bool, error) {
	relative = filepath.Clean(filepath.FromSlash(relative))
	if !safeRelative(filepath.ToSlash(relative)) {
		return false, errors.New("git-checkout: unsafe home-relative directory")
	}
	current := ""
	parts := strings.Split(relative, string(filepath.Separator))
	for _, part := range parts {
		current = filepath.Join(current, part)
		info, err := h.root.Lstat(current)
		if errors.Is(err, os.ErrNotExist) && allowMissing {
			return false, h.verify()
		}
		if err != nil {
			return false, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return false, fmt.Errorf("git-checkout: directory component %q is not a real directory", current)
		}
	}
	return true, h.verify()
}

func (a *Adapter) validatedGitDir(path string) (*homeRoot, string, os.FileInfo, error) {
	relative, err := filepath.Rel(a.Home, path)
	if err != nil || !safeRelative(filepath.ToSlash(relative)) {
		return nil, "", nil, errors.New("git-checkout: Git directory escapes home")
	}
	home, err := a.openHome()
	if err != nil {
		return nil, "", nil, err
	}
	if _, err := home.validateDirectory(relative, false); err != nil {
		home.close()
		return nil, "", nil, err
	}
	info, err := home.root.Lstat(relative)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		home.close()
		return nil, "", nil, errors.New("git-checkout: Git directory identity is unsafe")
	}
	return home, relative, info, nil
}

func validateLocalConfig(contents []byte, remote string) error {
	for _, entry := range splitNUL(contents) {
		parts := strings.SplitN(entry, "\n", 2)
		if len(parts) != 2 {
			return errors.New("git-checkout: malformed local config")
		}
		key, value := parts[0], parts[1]
		allowed := false
		switch key {
		case "core.repositoryformatversion":
			allowed = value == "0"
		case "core.filemode", "core.ignorecase", "core.precomposeunicode":
			allowed = value == "true" || value == "false"
		case "core.bare":
			allowed = value == "false"
		case "core.logallrefupdates":
			allowed = value == "true"
		case "remote.origin.url":
			allowed = value == remote
		case "remote.origin.fetch":
			allowed = value == "+refs/heads/*:refs/remotes/origin/*"
		default:
			if strings.HasPrefix(key, "branch.") {
				allowed = (strings.HasSuffix(key, ".remote") && value == "origin") || (strings.HasSuffix(key, ".merge") && refPattern.MatchString(value))
			}
		}
		if !allowed {
			return fmt.Errorf("git-checkout: unsafe or unknown local config key %q", key)
		}
	}
	return nil
}

type fixedSpec struct {
	remote, destination string
	verify              []string
}

var fixed = map[model.ResourceID]fixedSpec{
	"shell.oh-my-zsh":  {"https://github.com/ohmyzsh/ohmyzsh.git", ".oh-my-zsh", []string{"oh-my-zsh.sh"}},
	"shell.zinit":      {"https://github.com/zdharma-continuum/zinit.git", ".local/share/zinit/zinit.git", []string{"zinit.zsh"}},
	"shell.scm-breeze": {"https://github.com/scmbreeze/scm_breeze.git", ".scm_breeze", []string{"install.sh"}},
}

func (a *Adapter) Inspect(ctx context.Context, item model.Resource) (model.Observation, error) {
	d, err := a.declaration(item)
	if err != nil {
		return model.Observation{}, err
	}
	if owned, pruning, err := a.pruneOwnership(item); err != nil {
		return model.Observation{}, err
	} else if pruning {
		if err := a.validateOwned(d, owned.Paths); err != nil {
			return model.Observation{}, err
		}
		return a.inspectOwned(d, item, owned)
	}
	current, err := a.inspect(ctx, d)
	if err != nil {
		return model.Observation{}, err
	}
	return a.observation(item, d, current), nil
}

func (a *Adapter) PlanHistorical(_ context.Context, item model.Resource, _ model.Observation, owned model.Ownership) ([]model.Operation, error) {
	d, err := a.declaration(item)
	if err != nil {
		return nil, err
	}
	if owned.ResourceID != item.ID || owned.Provider != item.Provider || owned.Package != item.Package || len(owned.Paths) == 0 {
		return nil, errors.New("git-checkout: exact historical ownership is required")
	}
	if err := a.validateOwned(d, owned.Paths); err != nil {
		return nil, err
	}
	for path, expected := range owned.Paths {
		current, err := a.ownedReceipt(d, path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || current != expected {
			return nil, fmt.Errorf("git-checkout conflict: owned path differs before prune: %s", path)
		}
	}
	return []model.Operation{operation(item, model.OperationPrune)}, nil
}

func (a *Adapter) Plan(ctx context.Context, item model.Resource, _ model.Observation, owned model.Ownership) ([]model.Operation, error) {
	d, err := a.declaration(item)
	if err != nil {
		return nil, err
	}
	current, err := a.inspect(ctx, d)
	if err != nil {
		return nil, err
	}
	if len(owned.Paths) > 0 {
		if err := a.validateOwned(d, owned.Paths); err != nil {
			return nil, err
		}
		if !current.exists {
			return []model.Operation{operation(item, model.OperationRestore)}, nil
		}
	}
	if !current.exists {
		return []model.Operation{operation(item, model.OperationInstall)}, nil
	}
	if !current.git {
		if len(owned.Paths) > 0 {
			return nil, errors.New("git-checkout conflict: owned checkout is no longer a Git repository")
		}
		return []model.Operation{operation(item, model.OperationRestore)}, nil
	}
	if current.remote != d.remote {
		return nil, errors.New("git-checkout conflict: origin remote differs from signed declaration")
	}
	if !current.clean {
		return nil, errors.New("git-checkout conflict: tracked local modifications exist")
	}
	if current.head != d.commit {
		return []model.Operation{operation(item, model.OperationUpgrade)}, nil
	}
	if len(owned.Paths) == 0 {
		return []model.Operation{operation(item, model.OperationAdopt)}, nil
	}
	return nil, nil
}

func (a *Adapter) Execute(context.Context, model.Operation) model.OperationResult {
	return model.OperationResult{Detail: "git-checkout: signed resource is required", FinishedAt: time.Now().UTC()}
}

func (a *Adapter) ExecuteResource(ctx context.Context, item model.Resource, op model.Operation) model.OperationResult {
	result := model.OperationResult{OperationID: op.ID, ResourceID: op.ResourceID, FinishedAt: time.Now().UTC()}
	if err := a.execute(ctx, item, op); err != nil {
		result.Detail = err.Error()
		return result
	}
	result.Success = true
	return result
}

func (a *Adapter) execute(ctx context.Context, item model.Resource, op model.Operation) error {
	d, err := a.declaration(item)
	if err != nil {
		return err
	}
	if op.ResourceID != item.ID || op.Provider != item.Provider || op.Package != item.Package {
		return errors.New("git-checkout: operation identity mismatch")
	}
	if a.State == nil {
		return errors.New("git-checkout: state store is required")
	}
	snapshot, err := a.State.Snapshot()
	if err != nil {
		return err
	}
	if snapshot.ActiveJournal == nil {
		return errors.New("git-checkout: active journal is required")
	}
	owned := snapshot.Ownership[item.ID]
	if op.Kind == model.OperationPrune {
		if !sameStrings(op.Removes, []string{item.Package}) {
			return errors.New("git-checkout: prune removal authority mismatch")
		}
		return a.prune(ctx, d, owned.Paths)
	}
	if len(op.Removes) != 0 {
		return errors.New("git-checkout: non-prune operation cannot remove packages")
	}
	current, err := a.inspect(ctx, d) // close the plan/apply race
	if err != nil {
		return err
	}
	switch op.Kind {
	case model.OperationAdopt:
		if !current.exists || !current.git || !current.clean || current.remote != d.remote || current.head != d.commit {
			return errors.New("git-checkout: checkout changed before adoption")
		}
		return nil
	case model.OperationInstall, model.OperationRestore:
		if a.observation(item, d, current).Healthy {
			return nil
		}
		if current.exists && current.git {
			return errors.New("git-checkout: existing Git checkout cannot be replaced as partial")
		}
		staging, err := a.stage(ctx, d)
		if err != nil {
			return err
		}
		committed := false
		defer func() {
			if !committed {
				_ = a.removeStaging(staging)
			}
		}()
		if current.exists {
			if err := a.backupAndRemove(ctx, snapshot.ActiveJournal.ID, d); err != nil {
				return err
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := a.commitStaging(d, staging); err != nil {
			return err
		}
		committed = true
		return nil
	case model.OperationUpgrade:
		if !current.exists || !current.git || current.remote != d.remote {
			return errors.New("git-checkout: checkout changed before update")
		}
		return a.update(ctx, d)
	default:
		return fmt.Errorf("git-checkout: unsupported operation %q", op.Kind)
	}
}

func (a *Adapter) Verify(ctx context.Context, item model.Resource) (model.Observation, error) {
	d, err := a.declaration(item)
	if err != nil {
		return model.Observation{}, err
	}
	current, err := a.inspect(ctx, d)
	if err != nil {
		return model.Observation{}, err
	}
	got := a.observation(item, d, current)
	if !got.Present || !got.Healthy {
		return model.Observation{}, errors.New("git-checkout: desired pinned checkout is not exact")
	}
	return got, nil
}

func (a *Adapter) pruneOwnership(item model.Resource) (model.Ownership, bool, error) {
	if a.State == nil {
		return model.Ownership{}, false, nil
	}
	snapshot, err := a.State.Snapshot()
	if err != nil {
		return model.Ownership{}, false, err
	}
	if snapshot.ActiveJournal == nil {
		return model.Ownership{}, false, nil
	}
	for _, op := range snapshot.ActiveJournal.Plan.Operations {
		if op.ResourceID == item.ID && op.Kind == model.OperationPrune && op.Provider == item.Provider && op.Package == item.Package && sameStrings(op.Removes, []string{item.Package}) {
			return snapshot.Ownership[item.ID], true, nil
		}
	}
	return model.Ownership{}, false, nil
}

func (a *Adapter) inspectOwned(d declaration, item model.Resource, owned model.Ownership) (model.Observation, error) {
	if owned.ResourceID != item.ID || owned.Provider != item.Provider || owned.Package != item.Package {
		return model.Observation{}, errors.New("git-checkout: prune journal does not match ownership")
	}
	present, healthy := false, true
	for path, expected := range owned.Paths {
		current, err := a.ownedReceipt(d, path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return model.Observation{}, err
		}
		present = true
		healthy = healthy && current == expected
	}
	return model.Observation{Present: present, Healthy: healthy, Provider: item.Provider, Package: item.Package, Paths: owned.Paths}, nil
}

func (a *Adapter) observation(item model.Resource, d declaration, current checkout) model.Observation {
	healthy := current.exists && current.git && current.clean && current.remote == d.remote && current.head == d.commit
	for _, path := range d.verify {
		_, ok := current.paths[filepath.Join(a.Home, filepath.FromSlash(d.destination), filepath.FromSlash(path))]
		healthy = healthy && ok
	}
	paths := map[string]string{}
	if healthy {
		for path, receipt := range current.paths {
			paths[path] = receipt
		}
	}
	return model.Observation{Present: current.exists, Healthy: healthy, Provider: item.Provider, Package: item.Package, Version: current.head, Paths: paths}
}

func operation(item model.Resource, kind model.OperationKind) model.Operation {
	op := model.Operation{ID: "git-checkout-" + string(kind) + "-" + string(item.ID), ResourceID: item.ID, Kind: kind, Provider: item.Provider, Package: item.Package, Detail: string(kind) + " pinned Git checkout"}
	if kind == model.OperationPrune {
		op.Removes = []string{item.Package}
	}
	return op
}

func (a *Adapter) declaration(item model.Resource) (declaration, error) {
	if a.Runner == nil || !allowedGit(a.Git) || !filepath.IsAbs(a.Home) || filepath.Clean(a.Home) != a.Home {
		return declaration{}, errors.New("git-checkout: runner, Homebrew Git, and clean absolute home are required")
	}
	if item.Type != model.ResourceGitCheckout || item.Provider != "git" || item.VersionPolicy != model.VersionPinned || len(item.Commands) != 0 {
		return declaration{}, errors.New("git-checkout: exact pinned git resource without commands is required")
	}
	spec, ok := fixed[item.ID]
	if !ok {
		return declaration{}, fmt.Errorf("git-checkout: unsupported resource %q", item.ID)
	}
	if len(item.Metadata) != 5 {
		return declaration{}, errors.New("git-checkout: declaration contains unsupported metadata")
	}
	d := declaration{remote: item.Metadata[MetadataRemote], ref: item.Metadata[MetadataRef], commit: item.Metadata[MetadataCommit], destination: item.Metadata[MetadataDestination]}
	if d.remote != spec.remote || d.destination != spec.destination || item.Metadata[MetadataVerifyFiles] != strings.Join(spec.verify, ",") {
		return declaration{}, errors.New("git-checkout: declaration differs from compiled resource scope")
	}
	if !refPattern.MatchString(d.ref) || strings.Contains(d.ref, "..") || !commitPattern.MatchString(d.commit) {
		return declaration{}, errors.New("git-checkout: invalid pinned ref or commit")
	}
	d.verify = append([]string(nil), spec.verify...)
	return d, nil
}

func allowedGit(path string) bool {
	return path == "/opt/homebrew/bin/git" || path == "/usr/local/bin/git" || path == "/home/linuxbrew/.linuxbrew/bin/git"
}

func (a *Adapter) inspect(ctx context.Context, d declaration) (checkout, error) {
	destination := filepath.Join(a.Home, filepath.FromSlash(d.destination))
	home, err := a.openHome()
	if err != nil {
		return checkout{}, err
	}
	exists, err := home.validateDirectory(d.destination, true)
	if err != nil {
		home.close()
		return checkout{}, err
	}
	if !exists {
		home.close()
		return checkout{}, nil
	}
	current := checkout{exists: true, clean: false, paths: map[string]string{}}
	gitRelative := filepath.Join(filepath.FromSlash(d.destination), ".git")
	gitInfo, err := home.root.Lstat(gitRelative)
	if errors.Is(err, os.ErrNotExist) {
		home.close()
		return current, nil
	}
	if err != nil || !gitInfo.IsDir() || gitInfo.Mode()&os.ModeSymlink != 0 {
		home.close()
		return checkout{}, errors.New("git-checkout: .git must be a real directory")
	}
	for _, control := range []string{"config", "HEAD", "index"} {
		info, err := home.root.Lstat(filepath.Join(gitRelative, control))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			home.close()
			return checkout{}, fmt.Errorf("git-checkout: unsafe .git control file %s", control)
		}
	}
	for _, control := range []string{"hooks", "objects", "refs"} {
		info, err := home.root.Lstat(filepath.Join(gitRelative, control))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			home.close()
			return checkout{}, fmt.Errorf("git-checkout: unsafe .git control directory %s", control)
		}
	}
	if err := fs.WalkDir(home.root.FS(), gitRelative, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			return fmt.Errorf("git-checkout: unsafe .git object %s", path)
		}
		return nil
	}); err != nil {
		home.close()
		return checkout{}, err
	}
	if err := home.verify(); err != nil {
		home.close()
		return checkout{}, err
	}
	home.close()
	current.git = true
	snapshot, err := a.snapshotGitDir(d)
	if err != nil {
		return current, err
	}
	defer snapshot.cleanup()
	config, err := a.gitSnapshot(ctx, snapshot, destination, "config", "--local", "--null", "--list")
	if err != nil {
		return current, fmt.Errorf("git-checkout: inspect local config: %w", err)
	}
	if err := validateLocalConfig(config, d.remote); err != nil {
		return current, err
	}
	if afterSnapshotValidated != nil {
		afterSnapshotValidated()
	}
	remote, err := a.gitSnapshot(ctx, snapshot, destination, "remote", "get-url", "origin")
	if err != nil {
		return current, fmt.Errorf("git-checkout: inspect origin: %w", err)
	}
	head, err := a.gitSnapshot(ctx, snapshot, destination, "rev-parse", "HEAD")
	if err != nil {
		return current, fmt.Errorf("git-checkout: inspect HEAD: %w", err)
	}
	status, err := a.gitSnapshot(ctx, snapshot, destination, "status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return current, fmt.Errorf("git-checkout: inspect status: %w", err)
	}
	tracked, err := a.gitSnapshot(ctx, snapshot, destination, "ls-files", "-z")
	if err != nil {
		return current, fmt.Errorf("git-checkout: inspect tracked files: %w", err)
	}
	current.remote, current.head, current.clean = strings.TrimSpace(string(remote)), strings.TrimSpace(string(head)), len(status) == 0
	for _, rel := range splitNUL(tracked) {
		if !safeRelative(rel) {
			return checkout{}, fmt.Errorf("git-checkout: unsafe tracked path %q", rel)
		}
		absolute := filepath.Join(destination, filepath.FromSlash(rel))
		receipt, err := pathReceipt(absolute)
		if err != nil {
			return checkout{}, fmt.Errorf("git-checkout: tracked path %q: %w", rel, err)
		}
		current.paths[absolute] = receipt
	}
	if err := snapshot.verifySource(); err != nil {
		return checkout{}, err
	}
	return current, nil
}

func (a *Adapter) git(ctx context.Context, dir string, args ...string) ([]byte, error) {
	home, relative, directoryInfo, err := a.validatedGitDir(dir)
	if err != nil {
		return nil, err
	}
	defer home.close()
	common := []string{"-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "credential.helper=", "-c", "protocol.file.allow=never", "-c", "protocol.ext.allow=never", "-c", "submodule.recurse=false"}
	request := execx.Request{
		Path: a.Git,
		Dir:  dir,
		Args: append(common, args...),
		Env: map[string]string{
			"GIT_ATTR_NOSYSTEM":   "1",
			"GIT_CONFIG_GLOBAL":   "/dev/null",
			"GIT_CONFIG_NOSYSTEM": "1",
			"GIT_TERMINAL_PROMPT": "0",
		},
	}
	_ = relative
	result, err := a.Runner.Run(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(result.Stderr)))
	}
	if err := home.verify(); err != nil {
		return nil, err
	}
	if _, err := home.validateDirectory(relative, false); err != nil {
		return nil, err
	}
	current, err := home.root.Lstat(relative)
	if err != nil || current.Mode() != directoryInfo.Mode() || !os.SameFile(current, directoryInfo) {
		return nil, errors.New("git-checkout: Git directory inode changed during command")
	}
	return result.Stdout, nil
}

type gitSnapshot struct {
	adapter *Adapter
	decl    declaration
	path    string
	info    os.FileInfo
	source  map[string]os.FileInfo
}

func (a *Adapter) snapshotGitDir(d declaration) (*gitSnapshot, error) {
	home, err := a.openHome()
	if err != nil {
		return nil, err
	}
	defer home.close()
	if _, err := home.validateDirectory(d.destination, false); err != nil {
		return nil, err
	}
	gitRelative := filepath.Join(filepath.FromSlash(d.destination), ".git")
	sourceRoot, err := home.root.OpenRoot(gitRelative)
	if err != nil {
		return nil, err
	}
	defer sourceRoot.Close()
	temporary, err := os.MkdirTemp("", "tpod-git-snapshot-")
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(temporary, 0o700); err != nil {
		_ = os.RemoveAll(temporary)
		return nil, err
	}
	snapshot := &gitSnapshot{adapter: a, decl: d, path: temporary, source: map[string]os.FileInfo{}}
	snapshot.info, err = os.Lstat(temporary)
	if err != nil {
		snapshot.cleanup()
		return nil, err
	}
	err = fs.WalkDir(sourceRoot.FS(), ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			return fmt.Errorf("git-checkout: unsafe .git snapshot source %s", path)
		}
		snapshot.source[path] = info
		if path == "." {
			return nil
		}
		destination := filepath.Join(temporary, path)
		if info.IsDir() {
			return os.Mkdir(destination, info.Mode().Perm())
		}
		input, err := sourceRoot.OpenFile(path, os.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
		if err != nil {
			return err
		}
		output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, info.Mode().Perm())
		if err != nil {
			input.Close()
			return err
		}
		_, copyErr := io.Copy(output, input)
		syncErr := output.Sync()
		return errors.Join(copyErr, syncErr, input.Close(), output.Close())
	})
	if err != nil {
		snapshot.cleanup()
		return nil, err
	}
	if err := snapshot.verifySource(); err != nil {
		snapshot.cleanup()
		return nil, err
	}
	if err := syncPhysicalTree(temporary); err != nil {
		snapshot.cleanup()
		return nil, err
	}
	return snapshot, nil
}

func (s *gitSnapshot) verifySource() error {
	home, err := s.adapter.openHome()
	if err != nil {
		return err
	}
	defer home.close()
	if _, err := home.validateDirectory(s.decl.destination, false); err != nil {
		return err
	}
	gitRelative := filepath.Join(filepath.FromSlash(s.decl.destination), ".git")
	root, err := home.root.OpenRoot(gitRelative)
	if err != nil {
		return err
	}
	defer root.Close()
	seen := 0
	err = fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		expected, ok := s.source[path]
		if !ok {
			return errors.New("git-checkout: original .git changed after snapshot")
		}
		current, err := entry.Info()
		if err != nil {
			return err
		}
		if current.Mode() != expected.Mode() || !os.SameFile(current, expected) {
			return errors.New("git-checkout: original .git inode changed after snapshot")
		}
		seen++
		return nil
	})
	if err != nil || seen != len(s.source) {
		return errors.New("git-checkout: original .git changed after snapshot")
	}
	return home.verify()
}

func (s *gitSnapshot) verify() error {
	current, err := os.Lstat(s.path)
	if err != nil || current.Mode() != s.info.Mode() || !os.SameFile(current, s.info) {
		return errors.New("git-checkout: private Git snapshot identity changed")
	}
	return nil
}

func (s *gitSnapshot) cleanup() { _ = os.RemoveAll(s.path) }

func (a *Adapter) gitSnapshot(ctx context.Context, snapshot *gitSnapshot, worktree string, args ...string) ([]byte, error) {
	if err := snapshot.verify(); err != nil {
		return nil, err
	}
	home, relative, worktreeInfo, err := a.validatedGitDir(worktree)
	if err != nil {
		return nil, err
	}
	defer home.close()
	common := []string{"--git-dir=" + snapshot.path, "--work-tree=" + worktree, "-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "credential.helper=", "-c", "protocol.file.allow=never", "-c", "protocol.ext.allow=never", "-c", "submodule.recurse=false"}
	request := execx.Request{Path: a.Git, Dir: a.Home, Args: append(common, args...), Env: gitEnvironment()}
	result, err := a.Runner.Run(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(result.Stderr)))
	}
	if err := snapshot.verify(); err != nil {
		return nil, err
	}
	if err := home.verify(); err != nil {
		return nil, err
	}
	if _, err := home.validateDirectory(relative, false); err != nil {
		return nil, err
	}
	current, err := home.root.Lstat(relative)
	if err != nil || current.Mode() != worktreeInfo.Mode() || !os.SameFile(current, worktreeInfo) {
		return nil, errors.New("git-checkout: worktree inode changed during command")
	}
	return result.Stdout, nil
}

func (a *Adapter) materializeGitSnapshot(d declaration, snapshot *gitSnapshot) (stagingCapability, error) {
	token := make([]byte, 8)
	if _, err := rand.Read(token); err != nil {
		return stagingCapability{}, err
	}
	cap := stagingCapability{token: hex.EncodeToString(token)}
	cap.path = filepath.Join(filepath.FromSlash(d.destination), ".git.tpod-new-"+cap.token)
	home, err := a.openHome()
	if err != nil {
		return cap, err
	}
	defer home.close()
	if _, err := home.validateDirectory(d.destination, false); err != nil {
		return cap, err
	}
	if err := home.root.Mkdir(cap.path, 0o700); err != nil {
		return cap, err
	}
	info, err := home.root.Lstat(cap.path)
	if err != nil {
		return cap, err
	}
	a.mu.Lock()
	if a.staging == nil {
		a.staging = make(map[string]stagingRecord)
	}
	a.staging[cap.token] = stagingRecord{path: cap.path, info: info}
	a.mu.Unlock()
	if err := copyPhysicalTreeToRoot(snapshot.path, home.root, cap.path); err != nil {
		_ = a.removeStaging(cap)
		return cap, err
	}
	if err := a.verifyStaging(cap); err != nil {
		return cap, err
	}
	return cap, nil
}

func (a *Adapter) commitGitSnapshot(d declaration, snapshot *gitSnapshot, cap stagingCapability) error {
	if err := a.verifyStaging(cap); err != nil {
		return err
	}
	if err := snapshot.verifySource(); err != nil {
		return err
	}
	home, err := a.openHome()
	if err != nil {
		return err
	}
	defer home.close()
	destination := filepath.FromSlash(d.destination)
	gitPath := filepath.Join(destination, ".git")
	current, err := home.root.Lstat(gitPath)
	expected := snapshot.source["."]
	if err != nil || current.Mode() != expected.Mode() || !os.SameFile(current, expected) {
		return errors.New("git-checkout: original .git inode changed before metadata commit")
	}
	oldPath := filepath.Join(destination, ".git.tpod-old-"+cap.token)
	if _, err := home.root.Lstat(oldPath); !errors.Is(err, os.ErrNotExist) {
		return errors.New("git-checkout: metadata recovery path already exists")
	}
	if err := home.root.Rename(gitPath, oldPath); err != nil {
		return err
	}
	if err := home.root.Rename(cap.path, gitPath); err != nil {
		_ = home.root.Rename(oldPath, gitPath)
		return err
	}
	a.mu.Lock()
	delete(a.staging, cap.token)
	a.mu.Unlock()
	if err := home.root.RemoveAll(oldPath); err != nil {
		return err
	}
	if err := home.verify(); err != nil {
		return err
	}
	return syncRootDirectory(home.root, destination)
}

func copyPhysicalTreeToRoot(source string, destination *os.Root, destinationBase string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			return errors.New("git-checkout: unsafe private snapshot object")
		}
		target := filepath.Join(destinationBase, relative)
		if info.IsDir() {
			return destination.Mkdir(target, info.Mode().Perm())
		}
		input, err := os.OpenFile(path, os.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
		if err != nil {
			return err
		}
		output, err := destination.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, info.Mode().Perm())
		if err != nil {
			input.Close()
			return err
		}
		_, copyErr := io.Copy(output, input)
		err = errors.Join(copyErr, output.Sync(), input.Close(), output.Close())
		return err
	})
}

func gitEnvironment() map[string]string {
	return map[string]string{"GIT_ATTR_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": "/dev/null", "GIT_CONFIG_NOSYSTEM": "1", "GIT_TERMINAL_PROMPT": "0"}
}

func syncPhysicalTree(root string) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || !entry.IsDir() {
			return err
		}
		directory, err := os.Open(path)
		if err != nil {
			return err
		}
		return errors.Join(directory.Sync(), directory.Close())
	})
}

func (a *Adapter) stage(ctx context.Context, d declaration) (cap stagingCapability, retErr error) {
	home, err := a.openHome()
	if err != nil {
		return cap, err
	}
	defer home.close()
	parent := filepath.Dir(filepath.FromSlash(d.destination))
	if err := home.ensureDirectory(parent); err != nil {
		return cap, err
	}
	token := make([]byte, 8)
	if _, err := rand.Read(token); err != nil {
		return cap, err
	}
	cap.token = hex.EncodeToString(token)
	cap.path = filepath.FromSlash(d.destination) + ".tpod-" + cap.token
	staging := cap.path
	if err := home.root.Mkdir(staging, 0o700); err != nil {
		return cap, err
	}
	info, err := home.root.Lstat(staging)
	if err != nil {
		return cap, err
	}
	a.mu.Lock()
	if a.staging == nil {
		a.staging = make(map[string]stagingRecord)
	}
	a.staging[cap.token] = stagingRecord{path: cap.path, info: info}
	a.mu.Unlock()
	defer func() {
		if retErr != nil {
			_ = a.removeStaging(cap)
		}
	}()
	if err := home.verify(); err != nil {
		return cap, err
	}
	physical := filepath.Join(a.Home, staging)
	if err := a.verifyStaging(cap); err != nil {
		return cap, err
	}
	if _, err := a.git(ctx, physical, "init", "--quiet"); err != nil {
		return cap, err
	}
	if err := a.verifyStaging(cap); err != nil {
		return cap, err
	}
	if _, err := a.git(ctx, physical, "remote", "add", "origin", d.remote); err != nil {
		return cap, err
	}
	if err := a.verifyStaging(cap); err != nil {
		return cap, err
	}
	if _, err := a.git(ctx, physical, "fetch", "--no-tags", "--depth=1", "origin", d.commit); err != nil {
		return cap, err
	}
	if err := a.verifyStaging(cap); err != nil {
		return cap, err
	}
	fetched, err := a.git(ctx, physical, "rev-parse", "FETCH_HEAD")
	if err != nil {
		return cap, err
	}
	if strings.TrimSpace(string(fetched)) != d.commit {
		return cap, errors.New("git-checkout: fetched ref does not match signed commit")
	}
	if err := a.verifyStaging(cap); err != nil {
		return cap, err
	}
	if _, err := a.git(ctx, physical, "checkout", "--detach", "--force", d.commit); err != nil {
		return cap, err
	}
	if err := ctx.Err(); err != nil {
		return cap, err
	}
	if err := a.verifyStaging(cap); err != nil {
		return cap, err
	}
	if err := syncRootDirectory(home.root, staging); err != nil {
		return cap, err
	}
	return cap, nil
}

func (a *Adapter) commitStaging(d declaration, staging stagingCapability) error {
	if err := a.verifyStaging(staging); err != nil {
		return err
	}
	home, err := a.openHome()
	if err != nil {
		return err
	}
	defer home.close()
	if _, err := home.validateDirectory(staging.path, false); err != nil {
		return err
	}
	if _, err := home.root.Lstat(filepath.FromSlash(d.destination)); !errors.Is(err, os.ErrNotExist) {
		return errors.New("git-checkout: destination appeared before install")
	}
	if err := home.root.Rename(staging.path, filepath.FromSlash(d.destination)); err != nil {
		return err
	}
	if err := home.verify(); err != nil {
		return err
	}
	a.mu.Lock()
	delete(a.staging, staging.token)
	a.mu.Unlock()
	return syncRootDirectory(home.root, filepath.Dir(filepath.FromSlash(d.destination)))
}

func (a *Adapter) removeStaging(staging stagingCapability) error {
	if err := a.verifyStaging(staging); err != nil {
		return err
	}
	home, err := a.openHome()
	if err != nil {
		return err
	}
	defer home.close()
	if _, err := home.validateDirectory(staging.path, true); err != nil {
		return err
	}
	if err := home.root.RemoveAll(staging.path); err != nil {
		return err
	}
	if err := home.verify(); err != nil {
		return err
	}
	a.mu.Lock()
	delete(a.staging, staging.token)
	a.mu.Unlock()
	return syncRootDirectory(home.root, filepath.Dir(staging.path))
}

func (a *Adapter) verifyStaging(cap stagingCapability) error {
	a.mu.Lock()
	record, ok := a.staging[cap.token]
	a.mu.Unlock()
	if !ok || cap.token == "" || cap.path != record.path {
		return errors.New("git-checkout: invalid or replayed staging capability")
	}
	home, err := a.openHome()
	if err != nil {
		return err
	}
	defer home.close()
	if _, err := home.validateDirectory(cap.path, false); err != nil {
		return err
	}
	current, err := home.root.Lstat(cap.path)
	if err != nil || current.Mode() != record.info.Mode() || !os.SameFile(current, record.info) {
		return errors.New("git-checkout: staging inode changed")
	}
	return home.verify()
}

func (h *homeRoot) ensureDirectory(relative string) error {
	if relative == "." {
		return h.verify()
	}
	relative = filepath.Clean(relative)
	if !safeRelative(filepath.ToSlash(relative)) {
		return errors.New("git-checkout: unsafe parent directory")
	}
	current := ""
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := h.root.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			if err := h.root.Mkdir(current, 0o700); err != nil {
				return err
			}
			info, err = h.root.Lstat(current)
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("git-checkout: parent %q is not a real directory", current)
		}
	}
	return h.verify()
}

func syncRootDirectory(root *os.Root, relative string) error {
	if relative == "" {
		relative = "."
	}
	directory, err := root.Open(relative)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func (a *Adapter) update(ctx context.Context, d declaration) error {
	destination := filepath.Join(a.Home, filepath.FromSlash(d.destination))
	before, err := a.inspect(ctx, d)
	if err != nil || !before.git || before.remote != d.remote {
		return errors.New("git-checkout: checkout changed before fetch")
	}
	snapshot, err := a.snapshotGitDir(d)
	if err != nil {
		return err
	}
	defer snapshot.cleanup()
	config, err := a.gitSnapshot(ctx, snapshot, destination, "config", "--local", "--null", "--list")
	if err != nil {
		return err
	}
	if err := validateLocalConfig(config, d.remote); err != nil {
		return err
	}
	if afterSnapshotValidated != nil {
		afterSnapshotValidated()
	}
	if _, err := a.gitSnapshot(ctx, snapshot, destination, "fetch", "--no-tags", "--depth=1", "origin", d.commit); err != nil {
		return err
	}
	fetched, err := a.gitSnapshot(ctx, snapshot, destination, "rev-parse", "FETCH_HEAD")
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(fetched)) != d.commit {
		return errors.New("git-checkout: fetched ref does not match signed commit")
	}
	// Fetch mutates only Git metadata. Reinspect the worktree immediately before
	// checkout, and omit --force so a concurrent user edit fails closed.
	if err := snapshot.verifySource(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if before.clean {
		if _, err := a.gitSnapshot(ctx, snapshot, destination, "checkout", "--detach", d.commit); err != nil {
			return err
		}
	} else {
		// A previous attempt may have updated the worktree but failed before
		// committing its private Git metadata. Accept only that exact state.
		if _, err := a.gitSnapshot(ctx, snapshot, destination, "reset", "--mixed", d.commit); err != nil {
			return err
		}
	}
	status, err := a.gitSnapshot(ctx, snapshot, destination, "status", "--porcelain", "--untracked-files=no")
	if err != nil || len(status) != 0 {
		if !before.clean {
			return errors.New("git-checkout: dirty worktree is neither current nor signed desired state")
		}
		return errors.New("git-checkout: desired worktree verification failed")
	}
	if !before.clean {
		if _, err := a.gitSnapshot(ctx, snapshot, destination, "checkout", "--detach", d.commit); err != nil {
			return err
		}
	}
	if err := snapshot.verifySource(); err != nil {
		return err
	}
	cap, err := a.materializeGitSnapshot(d, snapshot)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = a.removeStaging(cap)
		}
	}()
	if beforeGitMetadataCommit != nil {
		if err := beforeGitMetadataCommit(); err != nil {
			return err
		}
	}
	if err := a.commitGitSnapshot(d, snapshot, cap); err != nil {
		return err
	}
	committed = true
	return nil
}

func (a *Adapter) backupAndRemove(ctx context.Context, journal string, d declaration) error {
	destination := filepath.Join(a.Home, filepath.FromSlash(d.destination))
	home, err := a.openHome()
	if err != nil {
		return err
	}
	if _, err := home.validateDirectory(d.destination, false); err != nil {
		home.close()
		return err
	}
	checkout, err := home.root.OpenRoot(filepath.FromSlash(d.destination))
	if err != nil {
		home.close()
		return err
	}
	var files []string
	var directories []string
	err = fs.WalkDir(checkout.FS(), ".", func(relative string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		path := filepath.Join(destination, relative)
		if info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
			directories = append(directories, path)
			return nil
		}
		if !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("git-checkout: unsafe partial object %s", path)
		}
		files = append(files, path)
		return nil
	})
	_ = checkout.Close()
	if verifyErr := home.verify(); err == nil {
		err = verifyErr
	}
	home.close()
	if err != nil {
		return err
	}
	receipts := make(map[string]string, len(files))
	for _, path := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := a.Backup.Save(journal, path); err != nil {
			return fmt.Errorf("git-checkout: backup partial checkout: %w", err)
		}
		receipt, err := pathReceipt(path)
		if err != nil {
			return err
		}
		receipts[path] = receipt
	}
	return a.removeRecorded(destination, files, directories, func(path string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		current, err := pathReceipt(path)
		if err != nil || current != receipts[path] {
			return errors.New("git-checkout: partial checkout changed immediately before replacement")
		}
		return nil
	})
}

func (a *Adapter) prune(ctx context.Context, d declaration, owned map[string]string) error {
	if err := a.validateOwned(d, owned); err != nil {
		return err
	}
	destination := filepath.Join(a.Home, filepath.FromSlash(d.destination))
	current, err := a.inspect(ctx, d)
	if err != nil {
		return err
	}
	if !current.exists {
		return nil
	}
	for absolute, receipt := range owned {
		fresh, err := a.ownedReceipt(d, absolute)
		if err != nil || fresh != receipt {
			return fmt.Errorf("git-checkout conflict: tracked path changed before prune: %s", absolute)
		}
	}
	files := make([]string, 0, len(owned))
	for absolute := range owned {
		files = append(files, absolute)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return a.removeRecorded(destination, files, nil, func(path string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		expected := owned[path]
		got, err := a.ownedReceipt(d, path)
		if err != nil || got != expected {
			return errors.New("git-checkout: path changed immediately before removal")
		}
		return nil
	})
}

func (a *Adapter) validateOwned(d declaration, owned map[string]string) error {
	destination := filepath.Join(a.Home, filepath.FromSlash(d.destination))
	for path, receipt := range owned {
		relative, err := filepath.Rel(destination, path)
		if err != nil || !safeRelative(filepath.ToSlash(relative)) || (len(receipt) != 69 || (!strings.HasPrefix(receipt, "file:") && !strings.HasPrefix(receipt, "link:"))) {
			return errors.New("git-checkout: invalid ownership path receipt")
		}
	}
	return nil
}

func (a *Adapter) ownedReceipt(d declaration, path string) (string, error) {
	home, err := a.openHome()
	if err != nil {
		return "", err
	}
	defer home.close()
	if _, err := home.validateDirectory(d.destination, false); err != nil {
		return "", err
	}
	relative, err := filepath.Rel(a.Home, path)
	if err != nil || !safeRelative(filepath.ToSlash(relative)) {
		return "", errors.New("git-checkout: owned path escapes home")
	}
	if err := home.verify(); err != nil {
		return "", err
	}
	return rootPathReceipt(home.root, relative)
}

func rootPathReceipt(root *os.Root, path string) (string, error) {
	info, err := root.Lstat(path)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := root.Readlink(path)
		if err != nil {
			return "", err
		}
		sum := sha256.Sum256([]byte(target))
		return "link:" + hex.EncodeToString(sum[:]), nil
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("not a regular file or symlink")
	}
	file, err := root.OpenFile(path, os.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return "", errors.New("path changed before hashing")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	current, err := root.Lstat(path)
	if err != nil || !os.SameFile(info, current) {
		return "", errors.New("path changed during hashing")
	}
	return "file:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func pathReceipt(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	prefix := "file:"
	var data []byte
	if info.Mode()&os.ModeSymlink != 0 {
		prefix = "link:"
		target, err := os.Readlink(path)
		if err != nil {
			return "", err
		}
		data = []byte(target)
	} else if info.Mode().IsRegular() {
		file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
		if err != nil {
			return "", err
		}
		defer file.Close()
		opened, err := file.Stat()
		if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
			return "", errors.New("path changed before hashing")
		}
		hash := sha256.New()
		if _, err := io.Copy(hash, file); err != nil {
			return "", err
		}
		current, err := os.Lstat(path)
		if err != nil || !os.SameFile(info, current) {
			return "", errors.New("path changed during hashing")
		}
		return prefix + hex.EncodeToString(hash.Sum(nil)), nil
	} else {
		return "", errors.New("not a regular file or symlink")
	}
	sum := sha256.Sum256(data)
	return prefix + hex.EncodeToString(sum[:]), nil
}

func (a *Adapter) removeRecorded(root string, files, extraDirs []string, before func(string) error) error {
	relativeRoot, err := filepath.Rel(a.Home, root)
	if err != nil || !safeRelative(filepath.ToSlash(relativeRoot)) {
		return errors.New("git-checkout: removal root escapes home")
	}
	home, err := a.openHome()
	if err != nil {
		return err
	}
	defer home.close()
	if _, err := home.validateDirectory(relativeRoot, false); err != nil {
		return err
	}
	rootInfo, err := home.root.Lstat(relativeRoot)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New("git-checkout: removal root is not a real directory")
	}
	anchored, err := home.root.OpenRoot(relativeRoot)
	if err != nil {
		return err
	}
	defer anchored.Close()
	sort.Slice(files, func(i, j int) bool { return len(files[i]) > len(files[j]) })
	dirSet := map[string]struct{}{}
	for _, dir := range extraDirs {
		relative, err := filepath.Rel(root, dir)
		if err != nil || (relative != "." && !safeRelative(filepath.ToSlash(relative))) {
			return errors.New("git-checkout: directory removal path escapes checkout")
		}
		if relative != "." {
			dirSet[relative] = struct{}{}
		}
	}
	for _, path := range files {
		relative, err := filepath.Rel(root, path)
		if err != nil || !safeRelative(filepath.ToSlash(relative)) {
			return errors.New("git-checkout: removal path escapes checkout")
		}
		if before != nil {
			if err := before(path); err != nil {
				return err
			}
		}
		if err := anchored.Remove(relative); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		for parent := filepath.Dir(relative); parent != "."; parent = filepath.Dir(parent) {
			dirSet[parent] = struct{}{}
		}
	}
	dirs := make([]string, 0, len(dirSet))
	for dir := range dirSet {
		dirs = append(dirs, dir)
	}
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
	for _, dir := range dirs {
		if err := anchored.Remove(dir); err != nil && !errors.Is(err, os.ErrNotExist) {
			if errors.Is(err, syscall.ENOTEMPTY) || errors.Is(err, syscall.EEXIST) {
				continue
			}
			return err
		}
	}
	if err := syncRootDirectory(anchored, "."); err != nil {
		return err
	}
	currentRoot, err := home.root.Lstat(relativeRoot)
	if err == nil && os.SameFile(rootInfo, currentRoot) {
		directory, openErr := anchored.Open(".")
		if openErr == nil {
			entries, readErr := directory.ReadDir(1)
			_ = directory.Close()
			if readErr == io.EOF && len(entries) == 0 {
				_ = anchored.Close()
				if latest, statErr := home.root.Lstat(relativeRoot); statErr == nil && os.SameFile(rootInfo, latest) {
					if err := home.root.Remove(relativeRoot); err != nil && !errors.Is(err, syscall.ENOTEMPTY) && !errors.Is(err, syscall.EEXIST) {
						return err
					}
				}
			}
		}
	}
	if err := home.verify(); err != nil {
		return err
	}
	return syncRootDirectory(home.root, filepath.Dir(relativeRoot))
}
func splitNUL(data []byte) []string {
	raw := strings.Split(string(data), "\x00")
	out := raw[:0]
	for _, value := range raw {
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}
func safeRelative(path string) bool {
	return path != "" && path != "." && !filepath.IsAbs(path) && filepath.Clean(filepath.FromSlash(path)) == filepath.FromSlash(path) && path != ".." && !strings.HasPrefix(path, "../") && !strings.Contains(path, "\\") && !strings.ContainsRune(path, 0)
}
func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
