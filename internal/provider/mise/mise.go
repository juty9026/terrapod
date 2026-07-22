package mise

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"

	"github.com/juty9026/terrapod/internal/execx"
	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/provider"
)

var standardPaths = map[string]struct{}{
	"/opt/homebrew/bin/mise": {}, "/usr/local/bin/mise": {}, "/home/linuxbrew/.linuxbrew/bin/mise": {},
}

var toolCommands = map[string][]string{
	"bun": {"bun"}, "node": {"node"}, "python": {"python"}, "uv": {"uv", "uvx"},
}

var selectorPattern = regexp.MustCompile(`^(latest|[0-9]+(?:\.[0-9]+)*)$`)

type Runner interface {
	Run(context.Context, execx.Request) (execx.Result, error)
}

type inventory struct {
	resource       model.Resource
	present, ready bool
	version        string
}

type Adapter struct {
	path, dataRoot string
	standard       bool
	runner         Runner
	mu             sync.RWMutex
	inventories    map[string]inventory
}

func New(path, dataRoot string, runner Runner) (*Adapter, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, fmt.Errorf("mise: executable path must be clean and absolute: %q", path)
	}
	if !filepath.IsAbs(dataRoot) || filepath.Clean(dataRoot) != dataRoot {
		return nil, fmt.Errorf("mise: data root must be clean and absolute: %q", dataRoot)
	}
	if isNilInterface(runner) {
		return nil, errors.New("mise: runner is required")
	}
	_, standard := standardPaths[path]
	return &Adapter{path: path, dataRoot: dataRoot, standard: standard, runner: runner, inventories: make(map[string]inventory)}, nil
}

func (a *Adapter) Name() string { return "mise" }

func (a *Adapter) Inspect(ctx context.Context, resource model.Resource) (model.Observation, error) {
	if err := validateResource(resource); err != nil {
		return model.Observation{}, err
	}
	if !a.standard {
		return model.Observation{Present: true, Provider: a.Name(), Package: resource.Package, Paths: map[string]string{"mise": a.path}, Detail: fmt.Sprintf("legacy mise executable at nonstandard path %q", a.path)}, nil
	}
	result, err := a.run(ctx, "ls", "--json", resource.Package)
	if err != nil {
		return model.Observation{}, fmt.Errorf("mise: inspect %q: %w", resource.Package, err)
	}
	version, present, err := parseInventory(result.Stdout, resource.Package, resource.Metadata["version"])
	if err != nil {
		return model.Observation{}, fmt.Errorf("mise: parse inventory for %q: %w", resource.Package, err)
	}
	ready := present && selectorMatches(resource.Metadata["version"], version)
	a.mu.Lock()
	a.inventories[resource.Package] = inventory{resource: cloneResource(resource), present: present, ready: ready, version: version}
	a.mu.Unlock()
	detail := ""
	if present && !ready {
		detail = fmt.Sprintf("installed version %q does not match selector %q", version, resource.Metadata["version"])
	}
	return model.Observation{Present: present, Provider: a.Name(), Package: resource.Package, Version: version, Paths: map[string]string{}, Healthy: ready, Detail: detail}, nil
}

func (a *Adapter) Simulate(_ context.Context, operation model.Operation) (provider.ChangeSet, error) {
	if err := validateOperation(operation); err != nil {
		return provider.ChangeSet{}, err
	}
	if !a.standard {
		return provider.ChangeSet{}, fmt.Errorf("mise: refusing desired operation through nonstandard path %q", a.path)
	}
	state, ok := a.inventory(operation.Package)
	if !ok {
		return provider.ChangeSet{}, fmt.Errorf("mise: %q must be inspected before simulation", operation.Package)
	}
	switch operation.Kind {
	case model.OperationInstall, model.OperationAdopt:
		if state.ready {
			return provider.ChangeSet{}, nil
		}
		return provider.ChangeSet{Installs: []string{operation.Package}}, nil
	case model.OperationUpgrade:
		return provider.ChangeSet{Upgrades: []string{operation.Package}}, nil
	case model.OperationPrune:
		if !state.present {
			return provider.ChangeSet{}, nil
		}
		return provider.ChangeSet{Removes: []string{operation.Package}}, nil
	default:
		return provider.ChangeSet{}, fmt.Errorf("mise: unsupported operation %q", operation.Kind)
	}
}

func (a *Adapter) Execute(ctx context.Context, operation model.Operation) error {
	changes, err := a.Simulate(ctx, operation)
	if err != nil {
		return err
	}
	if len(changes.Installs)+len(changes.Upgrades)+len(changes.Removes) == 0 {
		return nil
	}
	state, _ := a.inventory(operation.Package)
	var args []string
	switch operation.Kind {
	case model.OperationInstall, model.OperationAdopt, model.OperationUpgrade:
		args = []string{"install", "--yes", operation.Package + "@" + state.resource.Metadata["version"]}
	case model.OperationPrune:
		if state.version == "" {
			return fmt.Errorf("mise: no exact installed version for %q", operation.Package)
		}
		args = []string{"uninstall", "--yes", operation.Package + "@" + state.version}
	default:
		return fmt.Errorf("mise: unsupported operation %q", operation.Kind)
	}
	if _, err := a.run(ctx, args...); err != nil {
		return fmt.Errorf("mise: execute %s for %q: %w", operation.Kind, operation.Package, err)
	}
	return nil
}

func (a *Adapter) Verify(ctx context.Context, resource model.Resource) (model.Observation, error) {
	observation, err := a.Inspect(ctx, resource)
	if err != nil {
		return model.Observation{}, err
	}
	if !observation.Present || !observation.Healthy {
		return observation, fmt.Errorf("mise: verification failed for %q: %s", resource.Package, observation.Detail)
	}
	qualified := resource.Package + "@" + observation.Version
	where, err := a.run(ctx, "where", qualified)
	if err != nil {
		return observation, fmt.Errorf("mise: locate %q: %w", qualified, err)
	}
	path := strings.TrimSuffix(string(where.Stdout), "\n")
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || !pathBeneath(path, a.dataRoot) {
		return observation, fmt.Errorf("mise: location %q is not beneath trusted mise data root %q", path, a.dataRoot)
	}
	observation.Paths[resource.Package] = path
	for _, command := range toolCommands[resource.Package] {
		if _, err := a.run(ctx, "exec", "--yes", qualified, "--", command, "--version"); err != nil {
			return observation, fmt.Errorf("mise: verify command %q for %q: %w", command, qualified, err)
		}
	}
	return observation, nil
}

func (a *Adapter) run(ctx context.Context, args ...string) (execx.Result, error) {
	return a.runner.Run(ctx, execx.Request{Path: a.path, Args: args})
}

func (a *Adapter) inventory(tool string) (inventory, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	state, ok := a.inventories[tool]
	return state, ok
}

type lsEntry struct {
	Version   string `json:"version"`
	Installed bool   `json:"installed"`
}

func parseInventory(output []byte, target, selector string) (string, bool, error) {
	decoder := json.NewDecoder(bytes.NewReader(output))
	var document map[string]json.RawMessage
	if err := decoder.Decode(&document); err != nil {
		return "", false, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return "", false, errors.New("multiple JSON values")
	}
	if len(document) != 1 {
		return "", false, errors.New("inventory must contain exactly the target tool")
	}
	raw, ok := document[target]
	if !ok {
		return "", false, fmt.Errorf("inventory does not contain exact target %q", target)
	}
	var entries []lsEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return "", false, err
	}
	versions := make([]string, 0, 1)
	for _, entry := range entries {
		if !entry.Installed {
			continue
		}
		if entry.Version == "" || strings.TrimSpace(entry.Version) != entry.Version {
			return "", false, errors.New("invalid installed version")
		}
		if selectorMatches(selector, entry.Version) {
			versions = append(versions, entry.Version)
		}
	}
	if len(versions) > 1 {
		return "", false, fmt.Errorf("multiple installed versions match selector %q", selector)
	}
	if len(versions) == 1 {
		return versions[0], true, nil
	}
	for _, entry := range entries {
		if entry.Installed {
			if len(entries) > 1 {
				return "", false, errors.New("ambiguous installed inventory")
			}
			return entry.Version, true, nil
		}
	}
	return "", false, nil
}

func selectorMatches(selector, version string) bool {
	if selector == "latest" {
		return version != ""
	}
	return version == selector || strings.HasPrefix(version, selector+".")
}

func validateResource(resource model.Resource) error {
	if resource.Provider != "mise" {
		return fmt.Errorf("mise: resource provider %q does not match mise", resource.Provider)
	}
	wantCommands, ok := toolCommands[resource.Package]
	if !ok {
		return fmt.Errorf("mise: unsupported tool %q", resource.Package)
	}
	if resource.VersionPolicy != model.VersionPinned {
		return errors.New("mise: tool must use pinned version policy")
	}
	selector := resource.Metadata["version"]
	if !selectorPattern.MatchString(selector) {
		return fmt.Errorf("mise: invalid required version selector %q", selector)
	}
	if !reflect.DeepEqual(resource.Commands, wantCommands) {
		return fmt.Errorf("mise: commands for %q do not match compiled authority", resource.Package)
	}
	return nil
}

func validateOperation(operation model.Operation) error {
	if operation.Provider != "mise" {
		return fmt.Errorf("mise: operation provider %q does not match mise", operation.Provider)
	}
	if operation.RequiresPrivilege {
		return errors.New("mise: privilege is forbidden")
	}
	if _, ok := toolCommands[operation.Package]; !ok {
		return fmt.Errorf("mise: unsupported tool %q", operation.Package)
	}
	return nil
}

func pathBeneath(path, root string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
func isNilInterface(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	}
	return false
}
func cloneResource(resource model.Resource) model.Resource {
	resource.Commands = append([]string(nil), resource.Commands...)
	metadata := resource.Metadata
	resource.Metadata = make(map[string]string, len(metadata))
	for k, v := range metadata {
		resource.Metadata[k] = v
	}
	return resource
}
