package legacy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"

	"github.com/juty9026/terrapod/internal/execx"
	"github.com/juty9026/terrapod/internal/legacydecl"
	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/provider"
)

type Runner interface {
	Run(context.Context, execx.Request) (execx.Result, error)
}
type PathEvaluator interface{ EvalSymlinks(string) (string, error) }

type MiseHandler struct {
	path, resolvedDataRoot string
	runner                 Runner
	paths                  PathEvaluator
	mu                     sync.Mutex
	versions               map[string]string
}

func NewMiseHandler(path, dataRoot string, runner Runner, paths PathEvaluator) (*MiseHandler, error) {
	standard := map[string]struct{}{"/opt/homebrew/bin/mise": {}, "/usr/local/bin/mise": {}, "/home/linuxbrew/.linuxbrew/bin/mise": {}}
	if _, ok := standard[path]; !ok {
		return nil, fmt.Errorf("legacy: unsupported mise executable %q", path)
	}
	if !cleanAbsolute(dataRoot) || isNil(runner) || isNil(paths) {
		return nil, errors.New("legacy: mise requires an absolute data root, runner, and path evaluator")
	}
	resolvedPath, err := paths.EvalSymlinks(path)
	if err != nil || !cleanAbsolute(resolvedPath) {
		return nil, fmt.Errorf("legacy: resolve mise executable %q", path)
	}
	prefix := filepath.Dir(filepath.Dir(path))
	if !pathWithin(resolvedPath, prefix) {
		return nil, fmt.Errorf("legacy: mise executable escapes provider prefix %q", prefix)
	}
	resolvedData, err := paths.EvalSymlinks(dataRoot)
	if err != nil || !cleanAbsolute(resolvedData) {
		return nil, fmt.Errorf("legacy: resolve mise data root %q", dataRoot)
	}
	return &MiseHandler{path: path, resolvedDataRoot: resolvedData, runner: runner, paths: paths, versions: make(map[string]string)}, nil
}

func (h *MiseHandler) Inspect(ctx context.Context, _ model.Resource, declaration Declaration) (Receipt, error) {
	commands, ok := legacydecl.Commands(declaration.Package)
	if declaration.Kind != Mise || !ok {
		return Receipt{}, fmt.Errorf("legacy: unsupported mise package %q", declaration.Package)
	}
	result, err := h.run(ctx, "ls", "--json", declaration.Package)
	if err != nil {
		return Receipt{}, fmt.Errorf("legacy: inspect mise package %q: %w", declaration.Package, err)
	}
	version, present, err := parseMiseInventory(result.Stdout, declaration.Package)
	if err != nil {
		return Receipt{}, err
	}
	if !present {
		h.setVersion(declaration.Package, "")
		return Receipt{}, nil
	}
	qualified := declaration.Package + "@" + version
	where, err := h.run(ctx, "where", qualified)
	if err != nil {
		return Receipt{}, fmt.Errorf("legacy: locate mise package %q: %w", declaration.Package, err)
	}
	root := strings.TrimSuffix(string(where.Stdout), "\n")
	resolvedRoot, err := h.paths.EvalSymlinks(root)
	if err != nil || !cleanAbsolute(root) || !cleanAbsolute(resolvedRoot) || !pathWithin(resolvedRoot, h.resolvedDataRoot) {
		return Receipt{}, fmt.Errorf("legacy: mise package root %q escapes data root", root)
	}
	receiptPaths := make(map[string]string, len(commands))
	for _, command := range commands {
		which, runErr := h.run(ctx, "exec", "--yes", qualified, "--", "/usr/bin/which", command)
		if runErr != nil {
			return Receipt{}, fmt.Errorf("legacy: locate mise command %q: %w", command, runErr)
		}
		commandPath := strings.TrimSuffix(string(which.Stdout), "\n")
		resolvedCommand, resolveErr := h.paths.EvalSymlinks(commandPath)
		if resolveErr != nil || !cleanAbsolute(commandPath) || !cleanAbsolute(resolvedCommand) || !pathWithin(resolvedCommand, resolvedRoot) {
			return Receipt{}, fmt.Errorf("legacy: mise command %q escapes package root", command)
		}
		receiptPaths[command] = commandPath
	}
	h.setVersion(declaration.Package, version)
	return Receipt{Present: true, Prefixes: []string{root}, Paths: receiptPaths}, nil
}
func (h *MiseHandler) SimulateRemoval(_ context.Context, _ model.Resource, declaration Declaration) (provider.ChangeSet, error) {
	if _, ok := legacydecl.Commands(declaration.Package); declaration.Kind != Mise || !ok {
		return provider.ChangeSet{}, fmt.Errorf("legacy: unsupported mise package %q", declaration.Package)
	}
	if h.version(declaration.Package) == "" {
		return provider.ChangeSet{}, errors.New("legacy: mise package must be inspected before removal")
	}
	return provider.ChangeSet{Removes: []string{declaration.Package}}, nil
}
func (h *MiseHandler) Remove(ctx context.Context, _ model.Resource, declaration Declaration) error {
	version := h.version(declaration.Package)
	if version == "" {
		return errors.New("legacy: mise package must be inspected before removal")
	}
	_, err := h.run(ctx, "uninstall", "--yes", declaration.Package+"@"+version)
	if err != nil {
		return fmt.Errorf("legacy: uninstall mise package %q: %w", declaration.Package, err)
	}
	h.setVersion(declaration.Package, "")
	return nil
}
func (h *MiseHandler) run(ctx context.Context, args ...string) (execx.Result, error) {
	return h.runner.Run(ctx, execx.Request{Path: h.path, Args: args, Env: map[string]string{"LC_ALL": "C"}})
}
func (h *MiseHandler) setVersion(pkg, version string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if version == "" {
		delete(h.versions, pkg)
	} else {
		h.versions[pkg] = version
	}
}
func (h *MiseHandler) version(pkg string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.versions[pkg]
}

func parseMiseInventory(contents []byte, target string) (string, bool, error) {
	var document map[string][]struct {
		Version   string `json:"version"`
		Installed bool   `json:"installed"`
	}
	if err := json.Unmarshal(contents, &document); err != nil {
		return "", false, fmt.Errorf("legacy: parse mise inventory: %w", err)
	}
	entries, ok := document[target]
	if !ok || len(document) != 1 {
		return "", false, errors.New("legacy: mise inventory identity mismatch")
	}
	version := ""
	for _, entry := range entries {
		if !entry.Installed {
			continue
		}
		if !safeMiseVersion(entry.Version) || version != "" {
			return "", false, errors.New("legacy: ambiguous mise installed version")
		}
		version = entry.Version
	}
	return version, version != "", nil
}

func safeMiseVersion(version string) bool {
	if version == "" {
		return false
	}
	for _, char := range []byte(version) {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || strings.ContainsRune("._+-", rune(char)) {
			continue
		}
		return false
	}
	return true
}

type HomebrewPackageKind string

const (
	BrewFormula HomebrewPackageKind = "formula"
	BrewCask    HomebrewPackageKind = "cask"
)

type HomebrewHandler struct {
	path, prefix string
	runner       Runner
	paths        PathEvaluator
}

func NewHomebrewHandler(path string, runner Runner, paths PathEvaluator) (*HomebrewHandler, error) {
	if !cleanAbsolute(path) || isNil(runner) || isNil(paths) {
		return nil, errors.New("legacy: Homebrew requires an absolute path, runner, and path evaluator")
	}
	standard := map[string]struct{}{"/opt/homebrew/bin/brew": {}, "/usr/local/bin/brew": {}, "/home/linuxbrew/.linuxbrew/bin/brew": {}}
	if _, ok := standard[path]; ok {
		return nil, fmt.Errorf("legacy: Homebrew path %q is not nonstandard", path)
	}
	prefix := filepath.Dir(filepath.Dir(path))
	standardPrefixes := map[string]struct{}{"/opt/homebrew": {}, "/usr/local": {}, "/home/linuxbrew/.linuxbrew": {}}
	if _, standard := standardPrefixes[prefix]; standard || filepath.Dir(prefix) == "/" {
		return nil, fmt.Errorf("legacy: Homebrew prefix %q is not an allowed nonstandard prefix", prefix)
	}
	resolved, err := paths.EvalSymlinks(path)
	if err != nil || !cleanAbsolute(resolved) || !pathWithin(resolved, prefix) {
		return nil, fmt.Errorf("legacy: Homebrew executable escapes prefix %q", prefix)
	}
	return &HomebrewHandler{path: path, prefix: prefix, runner: runner, paths: paths}, nil
}
func (h *HomebrewHandler) Inspect(ctx context.Context, desired model.Resource, declaration Declaration) (Receipt, error) {
	if declaration.Kind != Homebrew || declaration.Package != desired.Package {
		return Receipt{}, errors.New("legacy: Homebrew declaration identity mismatch")
	}
	kind, err := homebrewKind(desired)
	if err != nil {
		return Receipt{}, err
	}
	result, err := h.run(ctx, "info", "--json=v2", declaration.Package)
	if err != nil {
		return Receipt{}, fmt.Errorf("legacy: inspect Homebrew package %q: %w", declaration.Package, err)
	}
	present, err := parseBrewReceipt(result.Stdout, kind, declaration.Package)
	if err != nil {
		return Receipt{}, err
	}
	if !present {
		return Receipt{}, nil
	}
	paths := make(map[string]string, len(desired.Commands))
	for _, command := range desired.Commands {
		path := filepath.Join(h.prefix, "bin", command)
		resolved, resolveErr := h.paths.EvalSymlinks(path)
		if resolveErr != nil || !cleanAbsolute(resolved) || !pathWithin(resolved, h.prefix) {
			return Receipt{}, fmt.Errorf("legacy: Homebrew command %q escapes prefix", command)
		}
		paths[command] = path
	}
	return Receipt{Present: true, Prefixes: []string{h.prefix}, Paths: paths}, nil
}
func (h *HomebrewHandler) SimulateRemoval(_ context.Context, desired model.Resource, declaration Declaration) (provider.ChangeSet, error) {
	if declaration.Kind != Homebrew {
		return provider.ChangeSet{}, errors.New("legacy: Homebrew declaration kind mismatch")
	}
	if _, err := homebrewKind(desired); err != nil {
		return provider.ChangeSet{}, err
	}
	return provider.ChangeSet{Removes: []string{declaration.Package}}, nil
}
func (h *HomebrewHandler) Remove(ctx context.Context, desired model.Resource, declaration Declaration) error {
	kind, err := homebrewKind(desired)
	if err != nil {
		return err
	}
	_, err = h.run(ctx, "uninstall", "--"+string(kind), declaration.Package)
	if err != nil {
		return fmt.Errorf("legacy: uninstall Homebrew package %q: %w", declaration.Package, err)
	}
	return nil
}
func homebrewKind(resource model.Resource) (HomebrewPackageKind, error) {
	switch resource.Provider {
	case "homebrew-formula":
		return BrewFormula, nil
	case "homebrew-cask":
		return BrewCask, nil
	default:
		return "", fmt.Errorf("legacy: unsupported desired Homebrew provider %q", resource.Provider)
	}
}
func (h *HomebrewHandler) run(ctx context.Context, args ...string) (execx.Result, error) {
	return h.runner.Run(ctx, execx.Request{Path: h.path, Args: args, Env: map[string]string{"HOMEBREW_NO_AUTO_UPDATE": "1", "LC_ALL": "C"}})
}

func parseBrewReceipt(contents []byte, kind HomebrewPackageKind, target string) (bool, error) {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var envelope struct {
		Formulae []json.RawMessage `json:"formulae"`
		Casks    []json.RawMessage `json:"casks"`
	}
	if err := decoder.Decode(&envelope); err != nil {
		return false, fmt.Errorf("legacy: parse Homebrew receipt: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return false, errors.New("legacy: trailing Homebrew JSON")
	}
	records := envelope.Formulae
	if kind == BrewCask {
		records = envelope.Casks
		if len(envelope.Formulae) != 0 {
			return false, errors.New("legacy: unexpected formula receipt")
		}
	} else if len(envelope.Casks) != 0 {
		return false, errors.New("legacy: unexpected cask receipt")
	}
	if len(records) != 1 {
		return false, errors.New("legacy: Homebrew receipt must contain exact package")
	}
	var item struct {
		Name      string          `json:"name"`
		FullName  string          `json:"full_name"`
		Token     string          `json:"token"`
		FullToken string          `json:"full_token"`
		Installed json.RawMessage `json:"installed"`
	}
	if err := json.Unmarshal(records[0], &item); err != nil {
		return false, err
	}
	name := item.Name
	full := item.FullName
	if kind == BrewCask {
		name = item.Token
		if name == "" {
			name = item.Name
		}
		full = item.FullToken
	}
	if name != target && full != target {
		return false, errors.New("legacy: Homebrew package receipt identity mismatch")
	}
	installed := bytes.TrimSpace(item.Installed)
	if len(installed) == 0 || bytes.Equal(installed, []byte("null")) || bytes.Equal(installed, []byte("false")) || bytes.Equal(installed, []byte("[]")) {
		return false, nil
	}
	if installed[0] != '[' && installed[0] != '"' {
		return false, errors.New("legacy: invalid Homebrew installed receipt")
	}
	if installed[0] == '[' {
		var versions []struct {
			Version string `json:"version"`
		}
		if err := json.Unmarshal(installed, &versions); err != nil || len(versions) != 1 || versions[0].Version == "" {
			return false, errors.New("legacy: invalid Homebrew installed versions")
		}
	} else {
		var version string
		if err := json.Unmarshal(installed, &version); err != nil || version == "" {
			return false, errors.New("legacy: invalid Homebrew installed version")
		}
	}
	return true, nil
}
