// Package legacy recognizes and removes only package sources explicitly named
// by the signed resource catalog.
package legacy

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

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

type Handler interface {
	Inspect(context.Context, model.Resource, Declaration) (Receipt, error)
	SimulateRemoval(context.Context, model.Resource, Declaration) (provider.ChangeSet, error)
	Remove(context.Context, model.Resource, Declaration) error
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
	Resource model.Resource
	Profile  model.Profile
	Desired  model.Observation
	Legacy   []Observation
}

type RemovalOperation struct {
	resource    model.Resource
	declaration Declaration
	observation Observation
}

type ErrUnknownProvenance struct {
	ResourceID model.ResourceID
	Command    string
	Path       string
}

type ErrUnsupportedSource struct {
	Kind Kind
}

func (e *ErrUnsupportedSource) Error() string {
	return fmt.Sprintf("legacy: no typed handler for %q", e.Kind)
}

func (e *ErrUnknownProvenance) Error() string {
	return fmt.Sprintf("legacy: command %q for resource %q has unknown provenance at %q", e.Command, e.ResourceID, e.Path)
}

type Coordinator struct {
	handlers map[Kind]Handler
	paths    PathResolver
}

func New(handlers map[Kind]Handler, paths PathResolver) (*Coordinator, error) {
	if isNil(paths) {
		return nil, errors.New("legacy: path resolver is required")
	}
	copyHandlers := make(map[Kind]Handler, len(handlers))
	for kind, handler := range handlers {
		if kind != APT && kind != Mise && kind != Homebrew && kind != Vendor {
			return nil, fmt.Errorf("legacy: unsupported handler kind %q", kind)
		}
		if isNil(handler) {
			return nil, fmt.Errorf("legacy: nil handler for %q", kind)
		}
		copyHandlers[kind] = handler
	}
	return &Coordinator{handlers: copyHandlers, paths: paths}, nil
}

func ParseDeclarations(resource model.Resource) ([]Declaration, error) {
	return legacydecl.Parse(resource)
}

func (c *Coordinator) Detect(ctx context.Context, profile model.Profile, resource model.Resource, desired model.Observation) (Inventory, error) {
	inventory := Inventory{Resource: cloneResource(resource), Profile: profile, Desired: cloneModelObservation(desired)}
	declarations, err := ParseDeclarations(resource)
	if err != nil {
		return inventory, err
	}
	for _, declaration := range declarations {
		if declaration.Profile != "" && declaration.Profile != profile {
			continue
		}
		handler, ok := c.handlers[declaration.Kind]
		if !ok {
			return inventory, &ErrUnsupportedSource{Kind: declaration.Kind}
		}
		receipt, err := handler.Inspect(ctx, resource, declaration)
		if err != nil {
			return inventory, fmt.Errorf("legacy: inspect %s source for %q: %w", declaration.Kind, resource.ID, err)
		}
		if !receipt.Present {
			continue
		}
		observation, err := observationFromReceipt(declaration, receipt)
		if err != nil {
			return inventory, fmt.Errorf("legacy: invalid %s receipt for %q: %w", declaration.Kind, resource.ID, err)
		}
		inventory.Legacy = append(inventory.Legacy, observation)
	}
	if err := c.validateCommandProvenance(resource, desired, inventory.Legacy); err != nil {
		return Inventory{}, err
	}
	return inventory, nil
}

func (c *Coordinator) RemovalOperations(inventory Inventory) ([]RemovalOperation, error) {
	declarations, err := ParseDeclarations(inventory.Resource)
	if err != nil {
		return nil, err
	}
	authorized := make(map[string]Declaration, len(declarations))
	for _, declaration := range declarations {
		if declaration.Profile != "" && declaration.Profile != inventory.Profile {
			continue
		}
		key := declarationKey(declaration)
		if _, duplicate := authorized[key]; duplicate {
			return nil, errors.New("legacy: ambiguous duplicate declaration")
		}
		authorized[key] = declaration
	}
	operations := make([]RemovalOperation, 0, len(inventory.Legacy))
	seen := make(map[string]struct{}, len(inventory.Legacy))
	for _, observation := range inventory.Legacy {
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
		operations = append(operations, RemovalOperation{
			resource:    cloneResource(inventory.Resource),
			declaration: declaration,
			observation: cloneObservation(observation),
		})
	}
	return operations, nil
}

func (c *Coordinator) Remove(ctx context.Context, operation RemovalOperation) error {
	if operation.resource.ID == "" || !operation.observation.Present || operation.declaration.Kind != operation.observation.Kind || operation.declaration.Package != operation.observation.Package {
		return errors.New("legacy: invalid removal operation")
	}
	handler, ok := c.handlers[operation.declaration.Kind]
	if !ok {
		return &ErrUnsupportedSource{Kind: operation.declaration.Kind}
	}
	fresh, err := handler.Inspect(ctx, operation.resource, operation.declaration)
	if err != nil {
		return fmt.Errorf("legacy: refresh receipt for %q: %w", operation.declaration.Package, err)
	}
	if !fresh.Present {
		return nil
	}
	freshObservation, err := observationFromReceipt(operation.declaration, fresh)
	if err != nil {
		return fmt.Errorf("legacy: invalid refreshed receipt: %w", err)
	}
	if !observationsEqual(freshObservation, operation.observation) {
		return &ErrStaleReceipt{Kind: operation.declaration.Kind, Package: operation.declaration.Package}
	}
	changes, err := handler.SimulateRemoval(ctx, operation.resource, operation.declaration)
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
	if err := handler.Remove(ctx, operation.resource, operation.declaration); err != nil {
		return fmt.Errorf("legacy: remove %q from %s: %w", operation.declaration.Package, operation.declaration.Kind, err)
	}
	receipt, err := handler.Inspect(ctx, operation.resource, operation.declaration)
	if err != nil {
		return fmt.Errorf("legacy: re-inventory %q from %s: %w", operation.declaration.Package, operation.declaration.Kind, err)
	}
	if receipt.Present {
		return fmt.Errorf("legacy: %q remains present in %s after removal", operation.declaration.Package, operation.declaration.Kind)
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

// APTHandler narrows the current APT adapter to the two historical bootstrap
// packages declared by the catalog.
type APTHandler struct{ adapter provider.Provider }

func NewAPTHandler(adapter provider.Provider) (*APTHandler, error) {
	if isNil(adapter) || adapter.Name() != "apt" {
		return nil, errors.New("legacy: APT provider adapter is required")
	}
	return &APTHandler{adapter: adapter}, nil
}

func (h *APTHandler) Inspect(ctx context.Context, desired model.Resource, declaration Declaration) (Receipt, error) {
	if declaration.Kind != APT || (declaration.Package != "gum" && declaration.Package != "mise") {
		return Receipt{}, fmt.Errorf("legacy: unsupported APT package %q", declaration.Package)
	}
	legacy := legacyResource(desired, declaration, h.adapter.Name())
	legacy.Metadata["bootstrapOnly"] = "true"
	observation, err := h.adapter.Inspect(ctx, legacy)
	if err != nil {
		return Receipt{}, err
	}
	paths := map[string]string{}
	if observation.Present {
		paths[declaration.Package] = "/usr/bin/" + declaration.Package
	}
	return Receipt{Present: observation.Present, Paths: paths}, nil
}

func (h *APTHandler) SimulateRemoval(ctx context.Context, desired model.Resource, declaration Declaration) (provider.ChangeSet, error) {
	return h.adapter.Simulate(ctx, pruneOperation(desired, declaration, h.adapter.Name()))
}

func (h *APTHandler) Remove(ctx context.Context, desired model.Resource, declaration Declaration) error {
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
