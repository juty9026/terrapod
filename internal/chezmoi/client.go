package chezmoi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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

type fileIdentity struct {
	path string
	info os.FileInfo
}

var inspectCommands = map[string]struct{}{
	"diff": {}, "status": {}, "managed": {}, "cat": {}, "data": {}, "execute-template": {},
}

func (c Client) Managed(ctx context.Context) ([]string, error) {
	result, err := c.run(ctx, []string{"managed", "--nul-path-separator", "--path-style", "relative"})
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, encoded := range strings.Split(string(result.Stdout), "\x00") {
		if encoded != "" {
			paths = append(paths, encoded)
		}
	}
	for i, path := range paths {
		absolute, _, err := safeTarget(c.Destination, path)
		if err != nil {
			return nil, fmt.Errorf("chezmoi: unsafe managed target %q: %w", path, err)
		}
		paths[i] = absolute
	}
	return paths, nil
}

func (c Client) TargetState(ctx context.Context) ([]Target, error) {
	result, err := c.run(ctx, []string{"dump", "--format", "json"})
	if err != nil {
		return nil, err
	}
	var entries map[string]struct {
		Type     string `json:"type"`
		Contents string `json:"contents"`
		Target   string `json:"target"`
	}
	if err := json.Unmarshal(result.Stdout, &entries); err != nil {
		return nil, fmt.Errorf("chezmoi: decode target state: %w", err)
	}
	targets := make([]Target, 0, len(entries))
	for path, entry := range entries {
		if entry.Type != "file" && entry.Type != "symlink" {
			continue
		}
		absolute, _, err := safeTarget(c.Destination, path)
		if err != nil {
			return nil, fmt.Errorf("chezmoi: unsafe target %q: %w", path, err)
		}
		desired := []byte(entry.Contents)
		if entry.Type == "symlink" && entry.Target != "" {
			desired = []byte(entry.Target)
		}
		sum := sha256.Sum256(desired)
		targets = append(targets, Target{Path: absolute, Kind: entry.Type, Desired: desired, Digest: hex.EncodeToString(sum[:])})
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].Path < targets[j].Path })
	return targets, nil
}

func (c Client) Diff(ctx context.Context, targets []string) ([]byte, error) {
	args := []string{"diff", "--"}
	var identities []fileIdentity
	for _, target := range targets {
		absolute, targetIdentities, err := safeTarget(c.Destination, target)
		if err != nil {
			return nil, fmt.Errorf("chezmoi: unsafe diff target %q: %w", target, err)
		}
		identities = append(identities, targetIdentities...)
		args = append(args, absolute)
	}
	result, err := c.runWithIdentities(ctx, args, identities)
	if err != nil {
		return nil, err
	}
	return result.Stdout, nil
}

func (c Client) ApplyTargets(ctx context.Context, targets []string) error {
	if len(targets) == 0 {
		return nil
	}
	args := []string{"apply", "--"}
	var identities []fileIdentity
	for _, target := range targets {
		absolute, targetIdentities, err := safeTarget(c.Destination, target)
		if err != nil {
			return fmt.Errorf("chezmoi: unsafe apply target %q: %w", target, err)
		}
		identities = append(identities, targetIdentities...)
		args = append(args, absolute)
	}
	_, err := c.runWithIdentities(ctx, args, identities)
	return err
}

func (c Client) InspectCommand(ctx context.Context, command string, args []string) (execx.Result, error) {
	if _, ok := inspectCommands[command]; !ok {
		return execx.Result{}, fmt.Errorf("chezmoi: command %q is not read-only", command)
	}
	if err := validateInspectArgs(args); err != nil {
		return execx.Result{}, err
	}
	return c.run(ctx, append([]string{command}, args...))
}

func (c Client) run(ctx context.Context, command []string) (execx.Result, error) {
	return c.runWithIdentities(ctx, command, nil)
}

func (c Client) runWithIdentities(ctx context.Context, command []string, targetIdentities []fileIdentity) (execx.Result, error) {
	if ctx == nil {
		return execx.Result{}, errors.New("chezmoi: nil context")
	}
	if isNil(c.Runner) {
		return execx.Result{}, errors.New("chezmoi: nil runner")
	}
	binary, identities, err := c.validate()
	if err != nil {
		return execx.Result{}, err
	}
	args := []string{"--source", c.Source, "--override-data-file", c.Config, "--exclude", "scripts", "--destination", c.Destination}
	args = append(args, command...)
	identities = append(identities, targetIdentities...)
	result, runErr := c.Runner.Run(ctx, execx.Request{Path: binary, Args: args})
	identityErr := verifyIdentities(identities)
	if runErr != nil {
		if len(result.Stderr) != 0 {
			runErr = fmt.Errorf("%w: stderr: %s", runErr, strings.TrimSpace(string(result.Stderr)))
		}
		return result, errors.Join(runErr, identityErr)
	}
	if identityErr != nil {
		return result, identityErr
	}
	return result, nil
}

func (c Client) validate() (string, []fileIdentity, error) {
	for name, path := range map[string]string{"binary": c.Binary, "source": c.Source, "config": c.Config, "destination": c.Destination} {
		if path == "" || !filepath.IsAbs(path) {
			return "", nil, fmt.Errorf("chezmoi: %s path must be absolute: %q", name, path)
		}
		if strings.IndexByte(path, 0) >= 0 {
			return "", nil, fmt.Errorf("chezmoi: %s path contains NUL", name)
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
	if err != nil {
		return "", nil, fmt.Errorf("chezmoi: inspect source: %w", err)
	}
	if !sourceInfo.IsDir() {
		return "", nil, errors.New("chezmoi: source is not a directory")
	}
	destinationInfo, err := os.Lstat(c.Destination)
	if err != nil {
		return "", nil, fmt.Errorf("chezmoi: inspect destination: %w", err)
	}
	if !destinationInfo.IsDir() || destinationInfo.Mode()&os.ModeSymlink != 0 {
		return "", nil, errors.New("chezmoi: destination is not a real directory")
	}
	return binary, []fileIdentity{{binary, binaryInfo}, {c.Config, configInfo}, {c.Source, sourceInfo}, {c.Destination, destinationInfo}}, nil
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

func regularNonblocking(path string) (os.FileInfo, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%q is not a regular file", path)
	}
	return info, nil
}

func verifyIdentities(identities []fileIdentity) error {
	for _, identity := range identities {
		current, err := os.Stat(identity.path)
		if err != nil || !os.SameFile(identity.info, current) {
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
		return "", nil, fmt.Errorf("open destination root: %w", err)
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
		if statErr != nil {
			return "", nil, fmt.Errorf("inspect parent %q: %w", parent, statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", nil, fmt.Errorf("parent %q is not a real directory", parent)
		}
		identities = append(identities, fileIdentity{filepath.Join(home, parent), info})
	}
	return absolute, identities, nil
}

func validateInspectArgs(args []string) error {
	for _, arg := range args {
		name := arg
		if before, _, ok := strings.Cut(name, "="); ok {
			name = before
		}
		switch name {
		case "--source", "-S", "--destination", "-D", "--override-data-file", "--override-data", "--exclude", "-x", "--include", "-i", "--config", "-c", "--output", "-o", "--init", "--refresh-externals", "-R", "--persistent-state":
			return fmt.Errorf("chezmoi: passthrough argument %q can override safety constraints", arg)
		}
		for _, short := range []string{"-S", "-D", "-x", "-i", "-c", "-o", "-R"} {
			if strings.HasPrefix(arg, short) && arg != short {
				return fmt.Errorf("chezmoi: passthrough argument %q can override safety constraints", arg)
			}
		}
	}
	return nil
}
