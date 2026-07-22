package legacy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
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
type pathEvaluator interface{ EvalSymlinks(string) (string, error) }

func WithMise(path, dataRoot string, runner Runner) Option {
	return func(c *Coordinator) error {
		handler, err := newMiseHandler(path, dataRoot, runner, c.paths)
		if err != nil {
			return err
		}
		return withHandler(Mise, handler)(c)
	}
}

func WithHomebrew(trustedPrefix string, runner Runner) Option {
	return func(c *Coordinator) error {
		if !cleanAbsolute(trustedPrefix) {
			return fmt.Errorf("legacy: trusted Homebrew prefix must be clean and absolute: %q", trustedPrefix)
		}
		if strings.HasPrefix(trustedPrefix, "/tmp/") || strings.HasPrefix(trustedPrefix, "/private/tmp/") || trustedPrefix == "/tmp" || trustedPrefix == "/private/tmp" {
			return fmt.Errorf("legacy: temporary Homebrew prefix is not trusted: %q", trustedPrefix)
		}
		brewPath := filepath.Join(trustedPrefix, "bin", "brew")
		if _, err := c.paths.EvalSymlinks(brewPath); errors.Is(err, os.ErrNotExist) {
			return withHandler(Homebrew, absentHandler{})(c)
		}
		handler, err := newHomebrewHandler(brewPath, runner, c.paths)
		if err != nil {
			return err
		}
		return withHandler(Homebrew, handler)(c)
	}
}

type miseHandler struct {
	path, resolvedDataRoot string
	runner                 Runner
	paths                  PathResolver
	mu                     sync.Mutex
	versions               map[string][]string
}

func newMiseHandler(path, dataRoot string, runner Runner, paths PathResolver) (*miseHandler, error) {
	standard := map[string]struct{}{"/opt/homebrew/bin/mise": {}, "/usr/local/bin/mise": {}, "/home/linuxbrew/.linuxbrew/bin/mise": {}}
	if _, ok := standard[path]; !ok {
		return nil, fmt.Errorf("legacy: unsupported mise executable %q", path)
	}
	if !cleanAbsolute(dataRoot) || isNil(runner) || isNil(paths) {
		return nil, errors.New("legacy: mise requires an absolute data root, runner, and path evaluator")
	}
	if unsafeMiseRoot(dataRoot) {
		return nil, fmt.Errorf("legacy: unsafe mise data root %q", dataRoot)
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
	if unsafeMiseRoot(resolvedData) {
		return nil, fmt.Errorf("legacy: unsafe resolved mise data root %q", resolvedData)
	}
	return &miseHandler{path: path, resolvedDataRoot: resolvedData, runner: runner, paths: paths, versions: make(map[string][]string)}, nil
}

func unsafeMiseRoot(path string) bool {
	unsafeRoots := map[string]struct{}{"/": {}, "/usr": {}, "/usr/local": {}, "/opt": {}, "/home": {}, "/Users": {}, "/var": {}, "/tmp": {}, "/private/tmp": {}}
	_, unsafe := unsafeRoots[path]
	return unsafe || strings.HasPrefix(path, "/tmp/") || strings.HasPrefix(path, "/private/tmp/")
}

func (h *miseHandler) inspect(ctx context.Context, desired model.Resource, declaration Declaration) (Receipt, error) {
	if !exactDeclaration(desired, declaration) {
		return Receipt{}, errors.New("legacy: mise declaration is not authorized for resource")
	}
	commands, ok := legacydecl.Commands(declaration.Package)
	if declaration.Kind != Mise || !ok {
		return Receipt{}, fmt.Errorf("legacy: unsupported mise package %q", declaration.Package)
	}
	result, err := h.run(ctx, "ls", "--json", declaration.Package)
	if err != nil {
		return Receipt{}, fmt.Errorf("legacy: inspect mise package %q: %w", declaration.Package, err)
	}
	versions, present, err := parseMiseInventory(result.Stdout, declaration.Package)
	if err != nil {
		return Receipt{}, err
	}
	if !present {
		h.setVersions(declaration.Package, nil)
		return Receipt{}, nil
	}
	prefixes := make([]string, 0, len(versions))
	candidates := make(map[string][]string, len(commands))
	for _, version := range versions {
		qualified := declaration.Package + "@" + version
		where, runErr := h.run(ctx, "where", qualified)
		if runErr != nil {
			return Receipt{}, fmt.Errorf("legacy: locate mise package %q: %w", declaration.Package, runErr)
		}
		root := strings.TrimSuffix(string(where.Stdout), "\n")
		resolvedRoot, resolveErr := h.paths.EvalSymlinks(root)
		if resolveErr != nil || !cleanAbsolute(root) || !cleanAbsolute(resolvedRoot) || !pathWithin(resolvedRoot, h.resolvedDataRoot) {
			return Receipt{}, fmt.Errorf("legacy: mise package root %q escapes data root", root)
		}
		prefixes = append(prefixes, root)
		for _, command := range commands {
			which, whichErr := h.run(ctx, "which", command, "--tool="+qualified)
			if whichErr != nil {
				return Receipt{}, fmt.Errorf("legacy: locate mise command %q: %w", command, whichErr)
			}
			commandPath := strings.TrimSuffix(string(which.Stdout), "\n")
			resolvedCommand, pathErr := h.paths.EvalSymlinks(commandPath)
			if pathErr != nil || !cleanAbsolute(commandPath) || !cleanAbsolute(resolvedCommand) || !pathWithin(resolvedCommand, resolvedRoot) {
				return Receipt{}, fmt.Errorf("legacy: mise command %q escapes package root", command)
			}
			candidates[command] = append(candidates[command], commandPath)
		}
	}
	receiptPaths := make(map[string]string, len(commands))
	for _, command := range commands {
		active, _ := h.paths.ResolveCommand(command)
		for _, candidate := range candidates[command] {
			if active == "" && len(candidates[command]) == 1 {
				receiptPaths[command] = candidate
				break
			}
			resolvedActive, aerr := h.paths.EvalSymlinks(active)
			resolvedCandidate, cerr := h.paths.EvalSymlinks(candidate)
			if aerr == nil && cerr == nil && resolvedActive == resolvedCandidate {
				receiptPaths[command] = candidate
				break
			}
		}
	}
	h.setVersions(declaration.Package, versions)
	return Receipt{Present: true, Prefixes: prefixes, Paths: receiptPaths}, nil
}
func (h *miseHandler) simulateRemoval(_ context.Context, desired model.Resource, declaration Declaration) (provider.ChangeSet, error) {
	if !exactDeclaration(desired, declaration) {
		return provider.ChangeSet{}, errors.New("legacy: mise declaration is not authorized for resource")
	}
	if _, ok := legacydecl.Commands(declaration.Package); declaration.Kind != Mise || !ok {
		return provider.ChangeSet{}, fmt.Errorf("legacy: unsupported mise package %q", declaration.Package)
	}
	if len(h.packageVersions(declaration.Package)) == 0 {
		return provider.ChangeSet{}, errors.New("legacy: mise package must be inspected before removal")
	}
	return provider.ChangeSet{Removes: []string{declaration.Package}}, nil
}
func (h *miseHandler) remove(ctx context.Context, desired model.Resource, declaration Declaration) error {
	if !exactDeclaration(desired, declaration) {
		return errors.New("legacy: mise declaration is not authorized for resource")
	}
	versions := h.packageVersions(declaration.Package)
	if len(versions) == 0 {
		return errors.New("legacy: mise package must be inspected before removal")
	}
	for _, version := range versions {
		if _, err := h.run(ctx, "uninstall", "--yes", declaration.Package+"@"+version); err != nil {
			return fmt.Errorf("legacy: uninstall mise package %q version %q: %w", declaration.Package, version, err)
		}
	}
	h.setVersions(declaration.Package, nil)
	return nil
}
func (h *miseHandler) run(ctx context.Context, args ...string) (execx.Result, error) {
	return h.runner.Run(ctx, execx.Request{Path: h.path, Args: args, Env: map[string]string{"LC_ALL": "C"}})
}
func (h *miseHandler) setVersions(pkg string, versions []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(versions) == 0 {
		delete(h.versions, pkg)
	} else {
		h.versions[pkg] = append([]string(nil), versions...)
	}
}
func (h *miseHandler) packageVersions(pkg string) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.versions[pkg]...)
}

func parseMiseInventory(contents []byte, target string) ([]string, bool, error) {
	if err := rejectDuplicateJSONKeys(contents); err != nil {
		return nil, false, fmt.Errorf("legacy: parse mise inventory: %w", err)
	}
	var document map[string][]struct {
		Version   string `json:"version"`
		Installed bool   `json:"installed"`
	}
	if err := json.Unmarshal(contents, &document); err != nil {
		return nil, false, fmt.Errorf("legacy: parse mise inventory: %w", err)
	}
	entries, ok := document[target]
	if !ok || len(document) != 1 {
		return nil, false, errors.New("legacy: mise inventory identity mismatch")
	}
	versions := make([]string, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if !entry.Installed {
			continue
		}
		if !safeMiseVersion(entry.Version) {
			return nil, false, errors.New("legacy: unsafe mise installed version")
		}
		if _, duplicate := seen[entry.Version]; duplicate {
			return nil, false, errors.New("legacy: duplicate mise installed version")
		}
		seen[entry.Version] = struct{}{}
		versions = append(versions, entry.Version)
	}
	sort.Strings(versions)
	return versions, len(versions) != 0, nil
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

func exactDeclaration(resource model.Resource, declaration Declaration) bool {
	declarations, err := legacydecl.Parse(resource)
	if err != nil {
		return false
	}
	for _, candidate := range declarations {
		if declarationKey(candidate) == declarationKey(declaration) {
			return true
		}
	}
	return false
}

type homebrewPackageKind string

const (
	brewFormula homebrewPackageKind = "formula"
	brewCask    homebrewPackageKind = "cask"
)

type homebrewHandler struct {
	path, prefix string
	runner       Runner
	paths        pathEvaluator
}

func newHomebrewHandler(path string, runner Runner, paths pathEvaluator) (*homebrewHandler, error) {
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
	return &homebrewHandler{path: path, prefix: prefix, runner: runner, paths: paths}, nil
}
func (h *homebrewHandler) inspect(ctx context.Context, desired model.Resource, declaration Declaration) (Receipt, error) {
	if declaration.Kind != Homebrew || declaration.Package != desired.Package || !exactDeclaration(desired, declaration) {
		return Receipt{}, errors.New("legacy: Homebrew declaration identity mismatch")
	}
	kind, err := homebrewKind(desired)
	if err != nil {
		return Receipt{}, err
	}
	result, err := h.run(ctx, "info", "--json=v2", "--"+string(kind), declaration.Package)
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
	list, err := h.run(ctx, "list", "--verbose", "--"+string(kind), declaration.Package)
	if err != nil {
		return Receipt{}, fmt.Errorf("legacy: list Homebrew-owned files for %q: %w", declaration.Package, err)
	}
	ownedResolvedPaths := make(map[string]struct{})
	for _, line := range strings.Split(strings.TrimSuffix(string(list.Stdout), "\n"), "\n") {
		if line == "" || !cleanAbsolute(line) {
			return Receipt{}, errors.New("legacy: invalid Homebrew file receipt")
		}
		resolved, resolveErr := h.paths.EvalSymlinks(line)
		if resolveErr != nil || !cleanAbsolute(resolved) || !pathWithin(resolved, h.prefix) {
			return Receipt{}, errors.New("legacy: Homebrew file receipt escapes trusted prefix")
		}
		ownedResolvedPaths[resolved] = struct{}{}
	}
	paths := make(map[string]string, len(desired.Commands))
	for _, command := range desired.Commands {
		var candidates []string
		for _, directory := range []string{"bin", "sbin"} {
			candidate := filepath.Join(h.prefix, directory, command)
			if !cleanAbsolute(candidate) {
				return Receipt{}, errors.New("legacy: invalid Homebrew command candidate")
			}
			resolved, resolveErr := h.paths.EvalSymlinks(candidate)
			if errors.Is(resolveErr, os.ErrNotExist) {
				continue
			}
			if resolveErr != nil || !cleanAbsolute(resolved) || !pathWithin(resolved, h.prefix) {
				return Receipt{}, errors.New("legacy: Homebrew command candidate escapes trusted prefix")
			}
			if _, owned := ownedResolvedPaths[resolved]; owned {
				candidates = append(candidates, candidate)
			}
		}
		if len(candidates) > 1 {
			return Receipt{}, errors.New("legacy: ambiguous Homebrew command receipt")
		}
		if len(candidates) == 1 {
			paths[command] = candidates[0]
		}
	}
	return Receipt{Present: true, Prefixes: []string{h.prefix}, Paths: paths}, nil
}
func (h *homebrewHandler) simulateRemoval(_ context.Context, desired model.Resource, declaration Declaration) (provider.ChangeSet, error) {
	if declaration.Kind != Homebrew || !exactDeclaration(desired, declaration) {
		return provider.ChangeSet{}, errors.New("legacy: Homebrew declaration kind mismatch")
	}
	if _, err := homebrewKind(desired); err != nil {
		return provider.ChangeSet{}, err
	}
	if desired.Provider == "homebrew-cask" {
		return provider.ChangeSet{}, &ErrUnsupportedSource{Kind: Homebrew}
	}
	return provider.ChangeSet{Removes: []string{declaration.Package}}, nil
}
func (h *homebrewHandler) remove(ctx context.Context, desired model.Resource, declaration Declaration) error {
	if !exactDeclaration(desired, declaration) {
		return errors.New("legacy: Homebrew declaration is not authorized for resource")
	}
	kind, err := homebrewKind(desired)
	if err != nil {
		return err
	}
	if kind == brewCask {
		return &ErrUnsupportedSource{Kind: Homebrew}
	}
	_, err = h.run(ctx, "uninstall", "--"+string(kind), declaration.Package)
	if err != nil {
		return fmt.Errorf("legacy: uninstall Homebrew package %q: %w", declaration.Package, err)
	}
	return nil
}
func homebrewKind(resource model.Resource) (homebrewPackageKind, error) {
	switch resource.Provider {
	case "homebrew-formula":
		return brewFormula, nil
	case "homebrew-cask":
		return brewCask, nil
	default:
		return "", fmt.Errorf("legacy: unsupported desired Homebrew provider %q", resource.Provider)
	}
}
func (h *homebrewHandler) run(ctx context.Context, args ...string) (execx.Result, error) {
	return h.runner.Run(ctx, execx.Request{Path: h.path, Args: args, Env: map[string]string{"HOMEBREW_NO_AUTO_UPDATE": "1", "LC_ALL": "C"}})
}

func parseBrewReceipt(contents []byte, kind homebrewPackageKind, target string) (bool, error) {
	if err := rejectDuplicateJSONKeys(contents); err != nil {
		return false, fmt.Errorf("legacy: parse Homebrew receipt: %w", err)
	}
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
	if kind == brewCask {
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
	if kind == brewCask {
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

func rejectDuplicateJSONKeys(contents []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	if err := scanJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("trailing JSON value")
		}
		return err
	}
	return nil
}

func scanJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("invalid JSON object key")
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("duplicate JSON key %q", key)
			}
			seen[key] = struct{}{}
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
	default:
		return errors.New("invalid JSON delimiter")
	}
	closing, err := decoder.Token()
	if err != nil {
		return err
	}
	if closing != matchingJSONDelimiter(delimiter) {
		return errors.New("mismatched JSON delimiter")
	}
	return nil
}

func matchingJSONDelimiter(open json.Delim) json.Delim {
	if open == '{' {
		return '}'
	}
	return ']'
}
