package chezmoi

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"syscall"

	"github.com/juty9026/terrapod/internal/execx"
)

type Runner interface {
	Run(context.Context, execx.Request) (execx.Result, error)
}

type Client struct {
	Runner      Runner
	Binary      string
	Source      string
	Config      string
	Destination string
}

type Target struct {
	Path    string
	Kind    string
	Desired []byte
	Digest  string
}

type ExpectedTarget struct {
	Path   string
	Kind   string
	Digest string
}

type fileIdentity struct {
	path string
	info os.FileInfo
}

type invocation struct {
	root        string
	source      string
	config      string
	cliConfig   string
	cache       string
	state       string
	destination string
	binary      string
	identities  []fileIdentity
}

type dumpEntry struct {
	path string
	raw  json.RawMessage
}

func (c Client) Managed(ctx context.Context) ([]string, error) {
	result, err := c.runRead(ctx, "managed", []string{"managed", "--nul-path-separator", "--path-style", "relative"})
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, encoded := range strings.Split(string(result.Stdout), "\x00") {
		if encoded == "" {
			continue
		}
		absolute, _, err := safeTarget(c.Destination, encoded)
		if err != nil {
			return nil, fmt.Errorf("chezmoi: unsafe managed target %q: %w", encoded, err)
		}
		paths = append(paths, absolute)
	}
	return paths, nil
}

func (c Client) TargetState(ctx context.Context) ([]Target, error) {
	result, err := c.runRead(ctx, "dump", []string{"dump", "--format", "json"})
	if err != nil {
		return nil, err
	}
	entries, err := decodeDump(result.Stdout)
	if err != nil {
		return nil, fmt.Errorf("chezmoi: decode target state: %w", err)
	}
	targets := make([]Target, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, encoded := range entries {
		path, raw := encoded.path, encoded.raw
		var entry struct {
			Type     string  `json:"type"`
			Contents *string `json:"contents"`
			Linkname *string `json:"linkname"`
		}
		if err := json.Unmarshal(raw, &entry); err != nil {
			return nil, fmt.Errorf("chezmoi: decode target %q: %w", path, err)
		}
		if entry.Type == "dir" {
			continue
		}
		if entry.Type != "file" && entry.Type != "symlink" {
			return nil, fmt.Errorf("chezmoi: target %q has unsupported type %q", path, entry.Type)
		}
		absolute, _, err := safeTarget(c.Destination, path)
		if err != nil {
			return nil, fmt.Errorf("chezmoi: unsafe target %q: %w", path, err)
		}
		if _, ok := seen[absolute]; ok {
			return nil, fmt.Errorf("chezmoi: duplicate target %q", absolute)
		}
		seen[absolute] = struct{}{}
		var desired []byte
		switch entry.Type {
		case "file":
			if entry.Contents == nil || entry.Linkname != nil {
				return nil, fmt.Errorf("chezmoi: file target %q has invalid fields", path)
			}
			desired = []byte(*entry.Contents)
		case "symlink":
			if entry.Linkname == nil || entry.Contents != nil || *entry.Linkname == "" || strings.IndexByte(*entry.Linkname, 0) >= 0 {
				return nil, fmt.Errorf("chezmoi: symlink target %q has invalid fields", path)
			}
			desired = []byte(*entry.Linkname)
		}
		sum := sha256.Sum256(desired)
		targets = append(targets, Target{Path: absolute, Kind: entry.Type, Desired: desired, Digest: hex.EncodeToString(sum[:])})
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].Path < targets[j].Path })
	return targets, nil
}

func decodeDump(data []byte) ([]dumpEntry, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '{' {
		return nil, errors.New("target state must be an object")
	}
	var entries []dumpEntry
	seen := make(map[string]struct{})
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		path, ok := token.(string)
		if !ok {
			return nil, errors.New("target path must be a string")
		}
		if _, ok := seen[path]; ok {
			return nil, fmt.Errorf("duplicate encoded target %q", path)
		}
		seen[path] = struct{}{}
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return nil, err
		}
		entries = append(entries, dumpEntry{path: path, raw: raw})
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	if token, err := decoder.Token(); err != io.EOF {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("unexpected trailing JSON token %v", token)
	}
	return entries, nil
}

func (c Client) Diff(ctx context.Context, targets []string) ([]byte, error) {
	args, err := c.targetArgs("diff", targets)
	if err != nil {
		return nil, err
	}
	result, err := c.runRead(ctx, "diff", args)
	if err != nil {
		return nil, err
	}
	return result.Stdout, nil
}

func (c Client) ApplyTargets(ctx context.Context, targets []string) error {
	return c.applyTargets(ctx, targets, nil, nil)
}

// ApplyTargetsChecked invokes check for each destination immediately before
// the staged object is installed. It lets typed resource adapters bind the
// mutation to the exact content hash they planned without weakening staging.

func (c Client) ApplyTargetsChecked(ctx context.Context, expected []ExpectedTarget, check func(string) error) error {
	targets := make([]string, len(expected))
	byPath := make(map[string]ExpectedTarget, len(expected))
	for index, target := range expected {
		decodedDigest, digestErr := hex.DecodeString(target.Digest)
		if !filepath.IsAbs(target.Path) || target.Path != filepath.Clean(target.Path) || (target.Kind != "file" && target.Kind != "symlink") || digestErr != nil || len(decodedDigest) != sha256.Size {
			return fmt.Errorf("chezmoi: invalid expected target %#v", target)
		}
		path := filepath.Clean(target.Path)
		if _, duplicate := byPath[path]; duplicate {
			return fmt.Errorf("chezmoi: duplicate expected target %q", path)
		}
		target.Path = path
		targets[index] = path
		byPath[path] = target
	}
	return c.applyTargets(ctx, targets, byPath, check)
}

func (c Client) applyTargets(ctx context.Context, targets []string, expected map[string]ExpectedTarget, check func(string) error) error {
	if len(targets) == 0 {
		return nil
	}
	if ctx == nil {
		return errors.New("chezmoi: nil context")
	}
	absolute := make([]string, 0, len(targets))
	var parentIdentities []fileIdentity
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		path, identities, err := safeTarget(c.Destination, target)
		if err != nil {
			return fmt.Errorf("chezmoi: unsafe apply target %q: %w", target, err)
		}
		if _, ok := seen[path]; ok {
			return fmt.Errorf("chezmoi: duplicate apply target %q", path)
		}
		seen[path] = struct{}{}
		absolute = append(absolute, path)
		parentIdentities = append(parentIdentities, identities...)
	}
	inv, err := c.prepareInvocation()
	if err != nil {
		return err
	}
	defer os.RemoveAll(inv.root)
	args := c.globalArgs(inv, "apply")
	args = append(args, "apply", "--")
	staged := make([]string, 0, len(absolute))
	for _, path := range absolute {
		rel, _ := filepath.Rel(c.Destination, path)
		stagedPath := filepath.Join(inv.destination, rel)
		args = append(args, stagedPath)
		staged = append(staged, stagedPath)
	}
	result, runErr := c.Runner.Run(ctx, execx.Request{Path: inv.binary, Args: args, Env: map[string]string{"HOME": c.Destination}})
	if err := commandError(result, runErr); err != nil {
		return err
	}
	if err := verifyIdentities(append(inv.identities, parentIdentities...)); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for i := range staged {
		rel, _ := filepath.Rel(c.Destination, absolute[i])
		precondition := func() error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if check != nil {
				if err := check(absolute[i]); err != nil {
					return err
				}
			}
			if expected != nil {
				if err := verifyExpectedTarget(staged[i], expected[absolute[i]]); err != nil {
					return err
				}
			}
			return verifyIdentities(append(inv.identities, parentIdentities...))
		}
		if err := installStaged(c.Destination, rel, staged[i], check != nil, precondition); err != nil {
			return fmt.Errorf("chezmoi: install target %q: %w", absolute[i], err)
		}
	}
	return nil
}

func verifyExpectedTarget(path string, expected ExpectedTarget) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("chezmoi: inspect staged target: %w", err)
	}
	var kind string
	var contents []byte
	if info.Mode()&os.ModeSymlink != 0 {
		kind = "symlink"
		target, err := os.Readlink(path)
		if err != nil {
			return err
		}
		contents = []byte(target)
	} else if info.Mode().IsRegular() {
		kind = "file"
		fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
		if err != nil {
			return err
		}
		file := os.NewFile(uintptr(fd), path)
		opened, statErr := file.Stat()
		if statErr != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
			file.Close()
			return errors.New("chezmoi: staged target changed before verification")
		}
		contents, err = io.ReadAll(file)
		closeErr := file.Close()
		if err = errors.Join(err, closeErr); err != nil {
			return err
		}
		current, statErr := os.Lstat(path)
		if statErr != nil || !os.SameFile(info, current) || current.Size() != info.Size() || !current.ModTime().Equal(info.ModTime()) {
			return errors.New("chezmoi: staged target changed during verification")
		}
	} else {
		return errors.New("chezmoi: staged target is not a regular file or symlink")
	}
	sum := sha256.Sum256(contents)
	if kind != expected.Kind || hex.EncodeToString(sum[:]) != expected.Digest {
		return fmt.Errorf("chezmoi: staged target %q does not match expected kind and digest", expected.Path)
	}
	return nil
}

func (c Client) InspectCommand(ctx context.Context, command string, operands []string) (execx.Result, error) {
	switch command {
	case "diff", "status", "managed", "cat":
		if command == "cat" && len(operands) == 0 {
			return execx.Result{}, errors.New("chezmoi: cat requires a target")
		}
		args, err := c.targetArgs(command, operands)
		if err != nil {
			return execx.Result{}, err
		}
		return c.runRead(ctx, command, args)
	case "data":
		if len(operands) != 0 {
			return execx.Result{}, errors.New("chezmoi: data accepts no passthrough arguments")
		}
		return c.runRead(ctx, command, []string{"data"})
	case "execute-template":
		if len(operands) == 0 {
			return execx.Result{}, errors.New("chezmoi: execute-template requires literal text")
		}
		for _, literal := range operands {
			if strings.HasPrefix(literal, "-") || strings.Contains(literal, "{{") || strings.Contains(literal, "}}") || strings.IndexByte(literal, 0) >= 0 {
				return execx.Result{}, errors.New("chezmoi: execute-template permits literal text only")
			}
		}
		return c.runRead(ctx, command, append([]string{"execute-template", "--"}, operands...))
	default:
		return execx.Result{}, fmt.Errorf("chezmoi: command %q is not read-only", command)
	}
}

func (c Client) targetArgs(command string, targets []string) ([]string, error) {
	args := []string{command, "--"}
	for _, target := range targets {
		if strings.HasPrefix(target, "-") {
			return nil, fmt.Errorf("chezmoi: %s accepts target operands only", command)
		}
		absolute, _, err := safeTarget(c.Destination, target)
		if err != nil {
			return nil, fmt.Errorf("chezmoi: unsafe %s target %q: %w", command, target, err)
		}
		args = append(args, absolute)
	}
	return args, nil
}

func (c Client) runRead(ctx context.Context, command string, args []string) (execx.Result, error) {
	if ctx == nil {
		return execx.Result{}, errors.New("chezmoi: nil context")
	}
	inv, err := c.prepareInvocation()
	if err != nil {
		return execx.Result{}, err
	}
	defer os.RemoveAll(inv.root)
	inv.destination = c.Destination
	requestArgs := c.globalArgs(inv, command)
	requestArgs = append(requestArgs, args...)
	result, runErr := c.Runner.Run(ctx, execx.Request{Path: inv.binary, Args: requestArgs, Env: map[string]string{"HOME": c.Destination}})
	if err := commandError(result, runErr); err != nil {
		return result, err
	}
	if err := verifyIdentities(inv.identities); err != nil {
		return result, err
	}
	return result, nil
}

func (c Client) globalArgs(inv invocation, command string) []string {
	args := []string{"--source", inv.source, "--override-data-file", inv.config, "--destination", inv.destination, "--config", inv.cliConfig, "--config-format=toml", "--cache", inv.cache, "--persistent-state", inv.state, "--refresh-externals=never", "--no-tty", "--color=false", "--progress=false"}
	switch command {
	case "managed", "status", "diff", "dump", "apply":
		args = append(args, "--exclude", "scripts")
	}
	if command == "diff" {
		args = append(args, "--no-pager", "--use-builtin-diff")
	}
	return args
}

func (c Client) prepareInvocation() (inv invocation, err error) {
	if isNil(c.Runner) {
		return inv, errors.New("chezmoi: nil runner")
	}
	binary, identities, err := c.validate()
	if err != nil {
		return inv, err
	}
	root, err := os.MkdirTemp("", "terrapod-chezmoi-")
	if err != nil {
		return inv, fmt.Errorf("chezmoi: create private invocation: %w", err)
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(root)
		}
	}()
	if err := os.Chmod(root, 0o700); err != nil {
		return inv, err
	}
	source := filepath.Join(root, "source")
	config := filepath.Join(root, "config.json")
	cliConfig := filepath.Join(root, "chezmoi.toml")
	cache := filepath.Join(root, "cache")
	state := filepath.Join(root, "state.boltdb")
	destination := filepath.Join(root, "destination")
	resolvedSource, err := filepath.EvalSymlinks(c.Source)
	if err != nil {
		return inv, fmt.Errorf("chezmoi: resolve source: %w", err)
	}
	sourceIdentities, err := copySourceTree(resolvedSource, source)
	if err != nil {
		return inv, fmt.Errorf("chezmoi: snapshot source: %w", err)
	}
	identities = append(identities, sourceIdentities...)
	if err := copyRegularFile(c.Config, config, 0o600); err != nil {
		return inv, fmt.Errorf("chezmoi: snapshot config: %w", err)
	}
	if err := os.WriteFile(cliConfig, nil, 0o600); err != nil {
		return inv, fmt.Errorf("chezmoi: create private CLI config: %w", err)
	}
	if err := os.Mkdir(cache, 0o700); err != nil {
		return inv, fmt.Errorf("chezmoi: create private cache: %w", err)
	}
	if err := os.Mkdir(destination, 0o700); err != nil {
		return inv, fmt.Errorf("chezmoi: create staging destination: %w", err)
	}
	keep = true
	return invocation{root: root, source: source, config: config, cliConfig: cliConfig, cache: cache, state: state, destination: destination, binary: binary, identities: identities}, nil
}

func (c Client) validate() (string, []fileIdentity, error) {
	for name, path := range map[string]string{"binary": c.Binary, "source": c.Source, "config": c.Config, "destination": c.Destination} {
		if path == "" || !filepath.IsAbs(path) || strings.IndexByte(path, 0) >= 0 {
			return "", nil, fmt.Errorf("chezmoi: %s path must be clean absolute", name)
		}
	}
	binary, err := filepath.EvalSymlinks(c.Binary)
	if err != nil {
		return "", nil, fmt.Errorf("chezmoi: resolve binary: %w", err)
	}
	binaryInfo, err := regularNonblocking(binary)
	if err != nil {
		return "", nil, fmt.Errorf("chezmoi: unsafe binary: %w", err)
	}
	configInfo, err := regularNonblocking(c.Config)
	if err != nil {
		return "", nil, fmt.Errorf("chezmoi: unsafe config: %w", err)
	}
	sourceInfo, err := os.Stat(c.Source)
	if err != nil || !sourceInfo.IsDir() {
		return "", nil, errors.New("chezmoi: source is not a directory")
	}
	destinationInfo, err := os.Lstat(c.Destination)
	if err != nil || !destinationInfo.IsDir() || destinationInfo.Mode()&os.ModeSymlink != 0 {
		return "", nil, errors.New("chezmoi: destination is not a real directory")
	}
	return binary, []fileIdentity{{binary, binaryInfo}, {c.Config, configInfo}, {c.Source, sourceInfo}, {c.Destination, destinationInfo}}, nil
}

func copySourceTree(source, destination string) ([]fileIdentity, error) {
	if err := os.Mkdir(destination, 0o700); err != nil {
		return nil, err
	}
	var identities []fileIdentity
	err := filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == source {
			return nil
		}
		rel, err := filepath.Rel(source, path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return errors.New("source entry escapes root")
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 || (!entry.IsDir() && !info.Mode().IsRegular()) {
			return fmt.Errorf("unsafe source entry %q", rel)
		}
		identities = append(identities, fileIdentity{path: path, info: info})
		target := filepath.Join(destination, rel)
		if entry.IsDir() {
			return os.Mkdir(target, 0o700)
		}
		return copyRegularFile(path, target, info.Mode().Perm())
	})
	return identities, err
}

func copyRegularFile(source, destination string, mode os.FileMode) error {
	fd, err := syscall.Open(source, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	in := os.NewFile(uintptr(fd), source)
	defer in.Close()
	info, err := in.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("source is not a regular file")
	}
	out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode.Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	return errors.Join(copyErr, out.Close())
}

func installStaged(home, relative, staged string, allowSymlinkReplacement bool, check func() error) error {
	info, err := os.Lstat(staged)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
		return errors.New("staged target is not a regular file or symlink")
	}
	root, err := os.OpenRoot(home)
	if err != nil {
		return err
	}
	defer root.Close()
	parts := strings.Split(filepath.Clean(relative), string(filepath.Separator))
	if relative == "." || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errors.New("target escapes destination")
	}
	for i := 1; i < len(parts); i++ {
		parent := filepath.Join(parts[:i]...)
		parentInfo, statErr := root.Lstat(parent)
		if errors.Is(statErr, os.ErrNotExist) {
			if err := check(); err != nil {
				return err
			}
			if err := root.Mkdir(parent, 0o700); err != nil {
				return err
			}
			parentInfo, statErr = root.Lstat(parent)
		}
		if statErr != nil || !parentInfo.IsDir() || parentInfo.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsafe target parent %q", parent)
		}
	}
	if leaf, statErr := root.Lstat(relative); statErr == nil {
		if !sameReplaceableKind(leaf, info, allowSymlinkReplacement) {
			return errors.New("existing target is not a regular file")
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	token := make([]byte, 12)
	if _, err := rand.Read(token); err != nil {
		return err
	}
	temp := filepath.Join(filepath.Dir(relative), ".tpod-"+hex.EncodeToString(token))
	if err := check(); err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		linkname, err := os.Readlink(staged)
		if err != nil || linkname == "" || strings.IndexByte(linkname, 0) >= 0 {
			return errors.New("invalid staged symlink")
		}
		if err := validateLinkname(home, relative, linkname); err != nil {
			return err
		}
		if err := root.Symlink(linkname, temp); err != nil {
			return err
		}
	} else {
		fd, err := syscall.Open(staged, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
		if err != nil {
			return err
		}
		in := os.NewFile(uintptr(fd), staged)
		openedInfo, err := in.Stat()
		if err != nil || !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
			in.Close()
			return errors.New("staged target changed before copy")
		}
		out, err := root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
		if err != nil {
			in.Close()
			return err
		}
		_, copyErr := io.Copy(out, in)
		closeErr := errors.Join(in.Close(), out.Close())
		if err := errors.Join(copyErr, closeErr); err != nil {
			_ = root.Remove(temp)
			return err
		}
	}
	if err := check(); err != nil {
		_ = root.Remove(temp)
		return err
	}
	if leaf, statErr := root.Lstat(relative); statErr == nil {
		if !sameReplaceableKind(leaf, info, allowSymlinkReplacement) {
			_ = root.Remove(temp)
			return errors.New("existing target changed to an unsafe type")
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		_ = root.Remove(temp)
		return statErr
	}
	if err := root.Rename(temp, relative); err != nil {
		_ = root.Remove(temp)
		return err
	}
	return nil
}

func sameReplaceableKind(existing, staged os.FileInfo, allowSymlink bool) bool {
	if existing.Mode().IsRegular() {
		return staged.Mode().IsRegular()
	}
	return allowSymlink && existing.Mode()&os.ModeSymlink != 0 && staged.Mode()&os.ModeSymlink != 0
}

func validateLinkname(home, relative, linkname string) error {
	destination := linkname
	if !filepath.IsAbs(destination) {
		destination = filepath.Join(home, filepath.Dir(relative), destination)
	}
	destination = filepath.Clean(destination)
	rel, err := filepath.Rel(filepath.Clean(home), destination)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("staged symlink target escapes destination")
	}
	root, err := os.OpenRoot(home)
	if err != nil {
		return err
	}
	defer root.Close()
	parts := strings.Split(rel, string(filepath.Separator))
	for i := 1; i <= len(parts); i++ {
		component := filepath.Join(parts[:i]...)
		info, statErr := root.Lstat(component)
		if errors.Is(statErr, os.ErrNotExist) {
			break
		}
		if statErr != nil {
			return statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("staged symlink target traverses another symlink")
		}
		if i < len(parts) && !info.IsDir() {
			return errors.New("staged symlink target has a non-directory parent")
		}
	}
	return nil
}

func regularNonblocking(path string) (os.FileInfo, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	return info, nil
}

func verifyIdentities(identities []fileIdentity) error {
	for _, identity := range identities {
		current, err := os.Stat(identity.path)
		if err != nil || !os.SameFile(identity.info, current) || (identity.info.Mode().IsRegular() && (identity.info.Size() != current.Size() || identity.info.Mode() != current.Mode() || !identity.info.ModTime().Equal(current.ModTime()))) {
			return fmt.Errorf("chezmoi: trusted path identity changed during command: %s", identity.path)
		}
	}
	return nil
}

func safeTarget(home, target string) (string, []fileIdentity, error) {
	if target == "" || strings.IndexByte(target, 0) >= 0 {
		return "", nil, errors.New("empty target or NUL")
	}
	home = filepath.Clean(home)
	absolute := target
	if !filepath.IsAbs(absolute) {
		absolute = filepath.Join(home, target)
	}
	absolute = filepath.Clean(absolute)
	relative, err := filepath.Rel(home, absolute)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", nil, errors.New("target escapes destination")
	}
	root, err := os.OpenRoot(home)
	if err != nil {
		return "", nil, err
	}
	defer root.Close()
	parts := strings.Split(relative, string(filepath.Separator))
	identities := make([]fileIdentity, 0, len(parts))
	for i := 1; i < len(parts); i++ {
		parent := filepath.Join(parts[:i]...)
		info, statErr := root.Lstat(parent)
		if errors.Is(statErr, os.ErrNotExist) {
			break
		}
		if statErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", nil, fmt.Errorf("parent %q is not a real directory", parent)
		}
		identities = append(identities, fileIdentity{filepath.Join(home, parent), info})
	}
	return absolute, identities, nil
}

func commandError(result execx.Result, err error) error {
	if err == nil {
		return nil
	}
	if len(result.Stderr) != 0 {
		return fmt.Errorf("%w: stderr: %s", err, strings.TrimSpace(string(result.Stderr)))
	}
	return err
}

func isNil(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}
