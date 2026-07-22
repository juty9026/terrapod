// Package legacy recognizes and removes only package sources explicitly named
// by the signed resource catalog.
package legacy

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/juty9026/terrapod/internal/execx"
	"github.com/juty9026/terrapod/internal/legacydecl"
	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/provider"
)

type Kind = legacydecl.Kind

const (
	APT      = legacydecl.APT
	Mise     = legacydecl.Mise
	Homebrew = legacydecl.Homebrew
	Vendor   = legacydecl.Vendor
)

type Declaration = legacydecl.Declaration

type Receipt struct {
	Present  bool
	Prefixes []string
	Paths    map[string]string
}

type handler interface {
	inspect(context.Context, model.Resource, Declaration) (Receipt, error)
	simulateRemoval(context.Context, model.Resource, Declaration) (provider.ChangeSet, error)
	remove(context.Context, model.Resource, Declaration) error
}

type PathResolver interface {
	ResolveCommand(string) (string, error)
	EvalSymlinks(string) (string, error)
}

type Observation struct {
	Kind          Kind
	Package       string
	ReceiptKind   string
	UninstallKind string
	Present       bool
	Prefixes      []string
	Paths         map[string]string
}

type Inventory struct {
	resource model.Resource
	profile  model.Profile
	desired  model.Observation
	legacy   []Observation
	issuer   [32]byte
	valid    bool
}

func (i Inventory) Resource() model.Resource   { return cloneResource(i.resource) }
func (i Inventory) Desired() model.Observation { return cloneModelObservation(i.desired) }
func (i Inventory) Legacy() []Observation {
	result := make([]Observation, len(i.legacy))
	for index, item := range i.legacy {
		result[index] = cloneObservation(item)
	}
	return result
}

type RemovalOperation struct {
	resource    model.Resource
	declaration Declaration
	observation Observation
	issuer      [32]byte
	digest      [32]byte
}

type ErrUnknownProvenance struct {
	ResourceID model.ResourceID
	Command    string
	Path       string
}

type ErrUnsupportedSource struct {
	Kind Kind
}

type ErrConsumedOperation struct{}

func (*ErrConsumedOperation) Error() string { return "legacy: removal operation was already consumed" }

type ErrInvalidDesiredObservation struct{ Detail string }

func (e *ErrInvalidDesiredObservation) Error() string {
	return "legacy: invalid desired observation: " + e.Detail
}

func (e *ErrUnsupportedSource) Error() string {
	return fmt.Sprintf("legacy: no typed handler for %q", e.Kind)
}

func (e *ErrUnknownProvenance) Error() string {
	return fmt.Sprintf("legacy: command %q for resource %q has unknown provenance at %q", e.Command, e.ResourceID, e.Path)
}

type Coordinator struct {
	handlers  map[Kind]handler
	paths     PathResolver
	issuer    [32]byte
	mu        sync.Mutex
	closed    bool
	completed map[[32]byte]struct{}
	closers   []func() error
}

type Option func(*Coordinator) error

func withHandler(kind Kind, source handler) Option {
	return func(c *Coordinator) error {
		if isNil(source) {
			return fmt.Errorf("legacy: nil handler for %q", kind)
		}
		if _, exists := c.handlers[kind]; exists {
			return fmt.Errorf("legacy: duplicate handler for %q", kind)
		}
		c.handlers[kind] = source
		return nil
	}
}

type absentHandler struct{}

func (absentHandler) inspect(context.Context, model.Resource, Declaration) (Receipt, error) {
	return Receipt{}, nil
}
func (absentHandler) simulateRemoval(context.Context, model.Resource, Declaration) (provider.ChangeSet, error) {
	return provider.ChangeSet{}, errors.New("legacy: absent source cannot be removed")
}
func (absentHandler) remove(context.Context, model.Resource, Declaration) error {
	return errors.New("legacy: absent source cannot be removed")
}

func New(paths PathResolver, options ...Option) (*Coordinator, error) {
	if isNil(paths) {
		return nil, errors.New("legacy: path resolver is required")
	}
	c := &Coordinator{handlers: make(map[Kind]handler), paths: paths, completed: make(map[[32]byte]struct{})}
	if _, err := rand.Read(c.issuer[:]); err != nil {
		return nil, fmt.Errorf("legacy: create coordinator identity: %w", err)
	}
	for _, option := range options {
		if option == nil {
			_ = c.Close()
			return nil, errors.New("legacy: nil option")
		}
		if err := option(c); err != nil {
			_ = c.Close()
			return nil, err
		}
	}
	return c, nil
}

func (c *Coordinator) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	var errs []error
	for _, closeFn := range c.closers {
		errs = append(errs, closeFn())
	}
	return errors.Join(errs...)
}

func ParseDeclarations(resource model.Resource) ([]Declaration, error) {
	return legacydecl.Parse(resource)
}

func (c *Coordinator) Detect(ctx context.Context, profile model.Profile, resource model.Resource, desired model.Observation) (Inventory, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return Inventory{}, errors.New("legacy: coordinator is closed")
	}
	if err := validateDesiredObservation(resource, desired, c.paths); err != nil {
		return Inventory{}, err
	}
	inventory := Inventory{resource: cloneResource(resource), profile: profile, desired: cloneModelObservation(desired), issuer: c.issuer, valid: true}
	declarations, err := ParseDeclarations(resource)
	if err != nil {
		return Inventory{}, err
	}
	for _, declaration := range declarations {
		if declaration.Profile != "" && declaration.Profile != profile {
			continue
		}
		handler, ok := c.handlers[declaration.Kind]
		if !ok {
			return Inventory{}, &ErrUnsupportedSource{Kind: declaration.Kind}
		}
		receipt, err := handler.inspect(ctx, resource, declaration)
		if err != nil {
			return Inventory{}, fmt.Errorf("legacy: inspect %s source for %q: %w", declaration.Kind, resource.ID, err)
		}
		if !receipt.Present {
			continue
		}
		observation, err := observationFromReceipt(declaration, receipt)
		if err != nil {
			return Inventory{}, fmt.Errorf("legacy: invalid %s receipt for %q: %w", declaration.Kind, resource.ID, err)
		}
		inventory.legacy = append(inventory.legacy, observation)
	}
	if err := c.validateCommandProvenance(resource, desired, inventory.legacy); err != nil {
		return Inventory{}, err
	}
	return inventory, nil
}

func (c *Coordinator) RemovalOperations(inventory Inventory) ([]RemovalOperation, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, errors.New("legacy: coordinator is closed")
	}
	if !inventory.valid || inventory.issuer != c.issuer {
		return nil, errors.New("legacy: inventory was not issued by this coordinator")
	}
	declarations, err := ParseDeclarations(inventory.resource)
	if err != nil {
		return nil, err
	}
	authorized := make(map[string]Declaration, len(declarations))
	for _, declaration := range declarations {
		if declaration.Profile != "" && declaration.Profile != inventory.profile {
			continue
		}
		key := declarationKey(declaration)
		if _, duplicate := authorized[key]; duplicate {
			return nil, errors.New("legacy: ambiguous duplicate declaration")
		}
		authorized[key] = declaration
	}
	operations := make([]RemovalOperation, 0, len(inventory.legacy))
	seen := make(map[string]struct{}, len(inventory.legacy))
	for _, observation := range inventory.legacy {
		if !observation.Present {
			continue
		}
		declaration := Declaration{Kind: observation.Kind, Package: observation.Package, ReceiptKind: observation.ReceiptKind, UninstallKind: observation.UninstallKind}
		key := declarationKey(declaration)
		if _, duplicate := seen[key]; duplicate {
			return nil, errors.New("legacy: ambiguous duplicate observation")
		}
		seen[key] = struct{}{}
		if _, ok := authorized[key]; !ok {
			return nil, fmt.Errorf("legacy: observation for %s package %q is not authorized by catalog", observation.Kind, observation.Package)
		}
		operation := RemovalOperation{
			resource:    cloneResource(inventory.resource),
			declaration: declaration,
			observation: cloneObservation(observation),
			issuer:      c.issuer,
		}
		operation.digest = operationDigest(operation)
		operations = append(operations, operation)
	}
	return operations, nil
}

func (c *Coordinator) Remove(ctx context.Context, operation RemovalOperation) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errors.New("legacy: coordinator is closed")
	}
	if _, done := c.completed[operation.digest]; done {
		return &ErrConsumedOperation{}
	}
	if operation.issuer != c.issuer || operation.digest != operationDigest(operation) {
		return errors.New("legacy: removal capability is invalid or belongs to another coordinator")
	}
	if operation.resource.ID == "" || !operation.observation.Present || operation.declaration.Kind != operation.observation.Kind || operation.declaration.Package != operation.observation.Package {
		return errors.New("legacy: invalid removal operation")
	}
	handler, ok := c.handlers[operation.declaration.Kind]
	if !ok {
		return &ErrUnsupportedSource{Kind: operation.declaration.Kind}
	}
	fresh, err := handler.inspect(ctx, operation.resource, operation.declaration)
	if err != nil {
		return fmt.Errorf("legacy: refresh receipt for %q: %w", operation.declaration.Package, err)
	}
	if !fresh.Present {
		c.completed[operation.digest] = struct{}{}
		return nil
	}
	freshObservation, err := observationFromReceipt(operation.declaration, fresh)
	if err != nil {
		return fmt.Errorf("legacy: invalid refreshed receipt: %w", err)
	}
	if !observationsEqual(freshObservation, operation.observation) {
		return &ErrStaleReceipt{Kind: operation.declaration.Kind, Package: operation.declaration.Package}
	}
	changes, err := handler.simulateRemoval(ctx, operation.resource, operation.declaration)
	if err != nil {
		return fmt.Errorf("legacy: simulate removal of %q from %s: %w", operation.declaration.Package, operation.declaration.Kind, err)
	}
	legacyResource := model.Resource{ID: operation.resource.ID, Type: model.ResourcePackage, Provider: string(operation.declaration.Kind), Package: operation.declaration.Package}
	if err := provider.ValidateChangeSet(changes, legacyResource, nil); err != nil {
		return err
	}
	if len(changes.Installs) != 0 || len(changes.Upgrades) != 0 || len(changes.Removes) != 1 || changes.Removes[0] != operation.declaration.Package {
		return fmt.Errorf("legacy: removal simulation for %q is not an exact package removal", operation.declaration.Package)
	}
	if err := handler.remove(ctx, operation.resource, operation.declaration); err != nil {
		return fmt.Errorf("legacy: remove %q from %s: %w", operation.declaration.Package, operation.declaration.Kind, err)
	}
	receipt, err := handler.inspect(ctx, operation.resource, operation.declaration)
	if err != nil {
		return fmt.Errorf("legacy: re-inventory %q from %s: %w", operation.declaration.Package, operation.declaration.Kind, err)
	}
	if receipt.Present {
		return fmt.Errorf("legacy: %q remains present in %s after removal", operation.declaration.Package, operation.declaration.Kind)
	}
	c.completed[operation.digest] = struct{}{}
	return nil
}

func operationDigest(operation RemovalOperation) [32]byte {
	payload, _ := json.Marshal(struct {
		Resource    model.Resource
		Declaration Declaration
		Observation Observation
	}{operation.resource, operation.declaration, operation.observation})
	return sha256.Sum256(payload)
}

func validateDesiredObservation(resource model.Resource, desired model.Observation, paths PathResolver) error {
	if desired.Provider != "" && desired.Provider != resource.Provider {
		return &ErrInvalidDesiredObservation{Detail: "provider mismatch"}
	}
	if desired.Package != "" && desired.Package != resource.Package {
		return &ErrInvalidDesiredObservation{Detail: "package mismatch"}
	}
	if !desired.Present {
		return nil
	}
	if !desired.Healthy {
		return &ErrInvalidDesiredObservation{Detail: "present resource is unhealthy"}
	}
	standard := []string{"/opt/homebrew", "/usr/local", "/home/linuxbrew/.linuxbrew"}
	for _, command := range resource.Commands {
		path := desired.Paths[command]
		if path == "" {
			return &ErrInvalidDesiredObservation{Detail: "missing command receipt"}
		}
		if !pathWithinAnyResolved(paths, path, standard) {
			return &ErrInvalidDesiredObservation{Detail: "command receipt is outside desired provider roots"}
		}
	}
	for command := range desired.Paths {
		found := false
		for _, declared := range resource.Commands {
			if command == declared {
				found = true
				break
			}
		}
		if !found {
			return &ErrInvalidDesiredObservation{Detail: "undeclared command receipt"}
		}
	}
	return nil
}

func (c *Coordinator) validateCommandProvenance(resource model.Resource, desired model.Observation, legacy []Observation) error {
	trustedPaths := map[string][]string{}
	if desired.Present {
		for command, path := range desired.Paths {
			trustedPaths[command] = append(trustedPaths[command], path)
		}
	}
	for _, observation := range legacy {
		for command, path := range observation.Paths {
			if len(observation.Prefixes) != 0 && !pathWithinAnyResolved(c.paths, path, observation.Prefixes) {
				return &ErrUnknownProvenance{ResourceID: resource.ID, Command: command, Path: path}
			}
			trustedPaths[command] = append(trustedPaths[command], path)
		}
	}
	for _, command := range resource.Commands {
		path, err := c.paths.ResolveCommand(command)
		if err != nil {
			return fmt.Errorf("legacy: resolve command %q: %w", command, err)
		}
		if path == "" {
			continue
		}
		resolved, err := resolveClean(c.paths, path)
		if err != nil {
			return &ErrUnknownProvenance{ResourceID: resource.ID, Command: command, Path: path}
		}
		trusted := false
		for _, exact := range trustedPaths[command] {
			resolvedExact, exactErr := resolveClean(c.paths, exact)
			if exactErr == nil && resolved == resolvedExact {
				trusted = true
				break
			}
		}
		if !trusted {
			return &ErrUnknownProvenance{ResourceID: resource.ID, Command: command, Path: path}
		}
	}
	return nil
}

type aptHandler struct {
	adapter provider.Provider
	runner  Runner
	paths   PathResolver
}

func WithAPT(adapter provider.Provider, runner Runner) Option {
	return func(c *Coordinator) error {
		handler, err := newAPTHandler(adapter, runner, c.paths)
		if err != nil {
			return err
		}
		return withHandler(APT, handler)(c)
	}
}
func newAPTHandler(adapter provider.Provider, runner Runner, paths PathResolver) (*aptHandler, error) {
	if isNil(adapter) || adapter.Name() != "apt" || isNil(runner) || isNil(paths) {
		return nil, errors.New("legacy: APT provider adapter, runner, and paths are required")
	}
	return &aptHandler{adapter: adapter, runner: runner, paths: paths}, nil
}

func (h *aptHandler) inspect(ctx context.Context, desired model.Resource, declaration Declaration) (Receipt, error) {
	if declaration.Kind != APT || !exactDeclaration(desired, declaration) {
		return Receipt{}, fmt.Errorf("legacy: unsupported APT package %q", declaration.Package)
	}
	legacy := legacyResource(desired, declaration, h.adapter.Name())
	legacy.Metadata["bootstrapOnly"] = "true"
	observation, err := h.adapter.Inspect(ctx, legacy)
	if err != nil {
		return Receipt{}, err
	}
	if !observation.Present {
		return Receipt{}, nil
	}
	result, err := h.runner.Run(ctx, execx.Request{Path: "/usr/bin/dpkg-query", Args: []string{"--listfiles", declaration.Package}, Env: map[string]string{"LC_ALL": "C"}})
	if err != nil {
		return Receipt{}, fmt.Errorf("legacy: query APT files for %q: %w", declaration.Package, err)
	}
	owned := make(map[string]struct{})
	for _, line := range strings.Split(strings.TrimSuffix(string(result.Stdout), "\n"), "\n") {
		if line == "" || !cleanAbsolute(line) {
			return Receipt{}, errors.New("legacy: invalid APT file receipt")
		}
		if _, duplicate := owned[line]; duplicate {
			return Receipt{}, errors.New("legacy: duplicate APT file receipt")
		}
		owned[line] = struct{}{}
	}
	paths := make(map[string]string)
	for _, command := range desired.Commands {
		candidate := "/usr/bin/" + command
		if _, ok := owned[candidate]; !ok {
			continue
		}
		resolved, resolveErr := h.paths.EvalSymlinks(candidate)
		if resolveErr != nil || !cleanAbsolute(resolved) {
			return Receipt{}, fmt.Errorf("legacy: resolve APT-owned command %q", command)
		}
		paths[command] = candidate
	}
	return Receipt{Present: true, Paths: paths}, nil
}

func (h *aptHandler) simulateRemoval(ctx context.Context, desired model.Resource, declaration Declaration) (provider.ChangeSet, error) {
	if !exactDeclaration(desired, declaration) {
		return provider.ChangeSet{}, errors.New("legacy: APT declaration is not authorized for resource")
	}
	return h.adapter.Simulate(ctx, pruneOperation(desired, declaration, h.adapter.Name()))
}

func (h *aptHandler) remove(ctx context.Context, desired model.Resource, declaration Declaration) error {
	if !exactDeclaration(desired, declaration) {
		return errors.New("legacy: APT declaration is not authorized for resource")
	}
	return h.adapter.Execute(ctx, pruneOperation(desired, declaration, h.adapter.Name()))
}

func legacyResource(desired model.Resource, declaration Declaration, providerName string) model.Resource {
	legacy := cloneResource(desired)
	legacy.Provider = providerName
	legacy.Package = declaration.Package
	return legacy
}

func pruneOperation(desired model.Resource, declaration Declaration, providerName string) model.Operation {
	return model.Operation{ID: "legacy-prune:" + string(desired.ID) + ":" + string(declaration.Kind), ResourceID: desired.ID, Kind: model.OperationPrune, Provider: providerName, Package: declaration.Package, Removes: []string{declaration.Package}}
}

func observationFromReceipt(declaration Declaration, receipt Receipt) (Observation, error) {
	for _, prefix := range receipt.Prefixes {
		if !cleanAbsolute(prefix) {
			return Observation{}, fmt.Errorf("unclean prefix %q", prefix)
		}
	}
	for command, path := range receipt.Paths {
		if command == "" || !cleanAbsolute(path) {
			return Observation{}, fmt.Errorf("unclean command receipt %q=%q", command, path)
		}
	}
	return Observation{Kind: declaration.Kind, Package: declaration.Package, ReceiptKind: declaration.ReceiptKind, UninstallKind: declaration.UninstallKind, Present: true, Prefixes: append([]string(nil), receipt.Prefixes...), Paths: clonePaths(receipt.Paths)}, nil
}

type ErrStaleReceipt struct {
	Kind    Kind
	Package string
}

func (e *ErrStaleReceipt) Error() string {
	return fmt.Sprintf("legacy: stale %s receipt for %q", e.Kind, e.Package)
}

func declarationKey(d Declaration) string {
	return string(d.Kind) + "\x00" + d.Package + "\x00" + d.ReceiptKind + "\x00" + d.UninstallKind
}
func observationsEqual(a, b Observation) bool {
	return a.Kind == b.Kind && a.Package == b.Package && a.ReceiptKind == b.ReceiptKind && a.UninstallKind == b.UninstallKind && a.Present == b.Present && reflect.DeepEqual(sortedStrings(a.Prefixes), sortedStrings(b.Prefixes)) && reflect.DeepEqual(a.Paths, b.Paths)
}
func sortedStrings(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}
func pathWithinAnyResolved(paths PathResolver, path string, prefixes []string) bool {
	resolved, err := resolveClean(paths, path)
	if err != nil {
		return false
	}
	for _, prefix := range prefixes {
		root, e := resolveClean(paths, prefix)
		if e == nil && pathWithin(resolved, root) {
			return true
		}
	}
	return false
}

func resolveClean(paths PathResolver, path string) (string, error) {
	if !cleanAbsolute(path) {
		return "", errors.New("path is not clean and absolute")
	}
	resolved, err := paths.EvalSymlinks(path)
	if err != nil || !cleanAbsolute(resolved) {
		return "", errors.New("path cannot be safely resolved")
	}
	return resolved, nil
}

func pathWithin(path, prefix string) bool {
	relative, err := filepath.Rel(prefix, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func cleanAbsolute(path string) bool { return filepath.IsAbs(path) && filepath.Clean(path) == path }

func cloneResource(resource model.Resource) model.Resource {
	metadata := resource.Metadata
	resource.Profiles = append([]model.Profile(nil), resource.Profiles...)
	resource.DependsOn = append([]model.ResourceID(nil), resource.DependsOn...)
	resource.Commands = append([]string(nil), resource.Commands...)
	resource.Metadata = make(map[string]string, len(resource.Metadata))
	for key, value := range metadata {
		resource.Metadata[key] = value
	}
	return resource
}

func cloneModelObservation(observation model.Observation) model.Observation {
	observation.Paths = clonePaths(observation.Paths)
	return observation
}
func cloneObservation(observation Observation) Observation {
	observation.Prefixes = append([]string(nil), observation.Prefixes...)
	observation.Paths = clonePaths(observation.Paths)
	return observation
}
func clonePaths(paths map[string]string) map[string]string {
	copyPaths := make(map[string]string, len(paths))
	for key, value := range paths {
		copyPaths[key] = value
	}
	return copyPaths
}

func isNil(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
