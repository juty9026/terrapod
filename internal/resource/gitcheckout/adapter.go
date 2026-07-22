package gitcheckout

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
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

type Runner interface {
	Run(context.Context, execx.Request) (execx.Result, error)
}

type Adapter struct {
	Runner Runner
	Git    string
	Home   string
	State  *state.Store
	Backup recovery.Backup
}

type declaration struct {
	remote, ref, commit, destination string
	verify                           []string
}
type checkout struct {
	exists, git, clean bool
	remote, head       string
	paths              map[string]string
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
		return a.inspectOwned(item, owned)
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
		current, err := pathReceipt(path)
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
		if current.exists && current.git {
			return errors.New("git-checkout: existing Git checkout cannot be replaced as partial")
		}
		if current.exists {
			if err := a.backupAndRemove(snapshot.ActiveJournal.ID, d); err != nil {
				return err
			}
		}
		return a.install(ctx, d)
	case model.OperationUpgrade:
		if !current.exists || !current.git || !current.clean || current.remote != d.remote {
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

func (a *Adapter) inspectOwned(item model.Resource, owned model.Ownership) (model.Observation, error) {
	if owned.ResourceID != item.ID || owned.Provider != item.Provider || owned.Package != item.Package {
		return model.Observation{}, errors.New("git-checkout: prune journal does not match ownership")
	}
	present, healthy := false, true
	for path, expected := range owned.Paths {
		current, err := pathReceipt(path)
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
	info, err := os.Lstat(destination)
	if errors.Is(err, os.ErrNotExist) {
		return checkout{}, nil
	}
	if err != nil {
		return checkout{}, err
	}
	current := checkout{exists: true, clean: false, paths: map[string]string{}}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return current, nil
	}
	gitInfo, err := os.Lstat(filepath.Join(destination, ".git"))
	if err != nil || (!gitInfo.IsDir() && !gitInfo.Mode().IsRegular()) || gitInfo.Mode()&os.ModeSymlink != 0 {
		return current, nil
	}
	current.git = true
	remote, err := a.git(ctx, destination, "remote", "get-url", "origin")
	if err != nil {
		return current, fmt.Errorf("git-checkout: inspect origin: %w", err)
	}
	head, err := a.git(ctx, destination, "rev-parse", "HEAD")
	if err != nil {
		return current, fmt.Errorf("git-checkout: inspect HEAD: %w", err)
	}
	status, err := a.git(ctx, destination, "status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return current, fmt.Errorf("git-checkout: inspect status: %w", err)
	}
	tracked, err := a.git(ctx, destination, "ls-files", "-z")
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
	return current, nil
}

func (a *Adapter) git(ctx context.Context, dir string, args ...string) ([]byte, error) {
	common := []string{"-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "protocol.file.allow=never", "-c", "protocol.ext.allow=never", "-c", "submodule.recurse=false"}
	request := execx.Request{
		Path: a.Git,
		Dir:  dir,
		Args: append(common, args...),
		Env: map[string]string{
			"GIT_CONFIG_GLOBAL":   "/dev/null",
			"GIT_CONFIG_NOSYSTEM": "1",
			"GIT_TERMINAL_PROMPT": "0",
		},
	}
	result, err := a.Runner.Run(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(result.Stderr)))
	}
	return result.Stdout, nil
}

func (a *Adapter) install(ctx context.Context, d declaration) (retErr error) {
	destination := filepath.Join(a.Home, filepath.FromSlash(d.destination))
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	token := make([]byte, 8)
	if _, err := rand.Read(token); err != nil {
		return err
	}
	staging := destination + ".tpod-" + hex.EncodeToString(token)
	if err := os.Mkdir(staging, 0o700); err != nil {
		return err
	}
	defer func() {
		if retErr != nil {
			_ = removeTreeFiles(staging)
		}
	}()
	if _, err := a.git(ctx, staging, "init", "--quiet"); err != nil {
		return err
	}
	if _, err := a.git(ctx, staging, "remote", "add", "origin", d.remote); err != nil {
		return err
	}
	if _, err := a.git(ctx, staging, "fetch", "--no-tags", "--depth=1", "origin", d.ref); err != nil {
		return err
	}
	fetched, err := a.git(ctx, staging, "rev-parse", "FETCH_HEAD")
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(fetched)) != d.commit {
		return errors.New("git-checkout: fetched ref does not match signed commit")
	}
	if _, err := a.git(ctx, staging, "checkout", "--detach", "--force", d.commit); err != nil {
		return err
	}
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		return errors.New("git-checkout: destination appeared before install")
	}
	if err := os.Rename(staging, destination); err != nil {
		return err
	}
	return nil
}

func (a *Adapter) update(ctx context.Context, d declaration) error {
	destination := filepath.Join(a.Home, filepath.FromSlash(d.destination))
	if _, err := a.git(ctx, destination, "fetch", "--no-tags", "--depth=1", "origin", d.ref); err != nil {
		return err
	}
	fetched, err := a.git(ctx, destination, "rev-parse", "FETCH_HEAD")
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(fetched)) != d.commit {
		return errors.New("git-checkout: fetched ref does not match signed commit")
	}
	// Fetch mutates only Git metadata. Reinspect the worktree immediately before
	// checkout, and omit --force so a concurrent user edit fails closed.
	current, err := a.inspect(ctx, d)
	if err != nil {
		return err
	}
	if !current.exists || !current.git || !current.clean || current.remote != d.remote {
		return errors.New("git-checkout: checkout changed immediately before update")
	}
	_, err = a.git(ctx, destination, "checkout", "--detach", d.commit)
	return err
}

func (a *Adapter) backupAndRemove(journal string, d declaration) error {
	destination := filepath.Join(a.Home, filepath.FromSlash(d.destination))
	var files []string
	var directories []string
	err := filepath.WalkDir(destination, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == destination {
			return nil
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
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
	if err != nil {
		return err
	}
	receipts := make(map[string]string, len(files))
	for _, path := range files {
		if err := a.Backup.Save(journal, path); err != nil {
			return fmt.Errorf("git-checkout: backup partial checkout: %w", err)
		}
		receipt, err := pathReceipt(path)
		if err != nil {
			return err
		}
		receipts[path] = receipt
	}
	return removeRecorded(destination, files, directories, func(path string) error {
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
		fresh, err := pathReceipt(absolute)
		if err != nil || fresh != receipt {
			return fmt.Errorf("git-checkout conflict: tracked path changed before prune: %s", absolute)
		}
	}
	files := make([]string, 0, len(owned))
	for absolute := range owned {
		files = append(files, absolute)
	}
	return removeRecorded(destination, files, nil, func(path string) error {
		expected := owned[path]
		got, err := pathReceipt(path)
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

func removeRecorded(root string, files, extraDirs []string, before func(string) error) error {
	rootInfo, err := os.Lstat(root)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New("git-checkout: removal root is not a real directory")
	}
	anchored, err := os.OpenRoot(root)
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
	currentRoot, err := os.Lstat(root)
	if err == nil && os.SameFile(rootInfo, currentRoot) {
		directory, openErr := anchored.Open(".")
		if openErr == nil {
			entries, readErr := directory.ReadDir(1)
			_ = directory.Close()
			if readErr == io.EOF && len(entries) == 0 {
				_ = anchored.Close()
				if latest, statErr := os.Lstat(root); statErr == nil && os.SameFile(rootInfo, latest) {
					if err := os.Remove(root); err != nil && !errors.Is(err, syscall.ENOTEMPTY) && !errors.Is(err, syscall.EEXIST) {
						return err
					}
				}
			}
		}
	}
	return nil
}

func removeTreeFiles(root string) error {
	var files []string
	var directories []string
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err == nil && path != root {
			if entry.IsDir() {
				directories = append(directories, path)
			} else {
				files = append(files, path)
			}
		}
		return nil
	})
	return removeRecorded(root, files, directories, nil)
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
