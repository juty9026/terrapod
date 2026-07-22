// Package legacy recognizes and removes only package sources explicitly named
// by the signed resource catalog.
package legacy

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/provider"
)

type Kind string

const (
	APT      Kind = "apt"
	Mise     Kind = "mise"
	Homebrew Kind = "homebrew"
	Vendor   Kind = "vendor"
)

var knownVendorKinds = map[string]struct{}{
	"antigravity-native": {},
	"claude-native":      {},
	"codex-standalone":   {},
}

type Declaration struct {
	Kind          Kind
	Package       string
	ReceiptKind   string
	UninstallKind string
}

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
	knownKeys := map[string]struct{}{
		"legacy.apt.package":      {},
		"legacy.mise.package":     {},
		"legacy.homebrew.package": {},
		"legacy.vendor.receipt":   {},
		"legacy.vendor.uninstall": {},
	}
	for key := range resource.Metadata {
		if strings.HasPrefix(key, "legacy.") {
			if _, ok := knownKeys[key]; !ok {
				return nil, fmt.Errorf("legacy: resource %q has unknown legacy metadata %q", resource.ID, key)
			}
		}
	}
	declarations := make([]Declaration, 0, 4)
	for _, item := range []struct {
		key  string
		kind Kind
	}{
		{"legacy.apt.package", APT},
		{"legacy.mise.package", Mise},
		{"legacy.homebrew.package", Homebrew},
	} {
		if pkg, ok := resource.Metadata[item.key]; ok {
			if !safeIdentifier(pkg) {
				return nil, fmt.Errorf("legacy: resource %q has unsafe %s %q", resource.ID, item.key, pkg)
			}
			declarations = append(declarations, Declaration{Kind: item.kind, Package: pkg})
		}
	}
	receipt, hasReceipt := resource.Metadata["legacy.vendor.receipt"]
	uninstall, hasUninstall := resource.Metadata["legacy.vendor.uninstall"]
	if hasReceipt != hasUninstall {
		return nil, fmt.Errorf("legacy: resource %q must pair vendor receipt and uninstall kinds", resource.ID)
	}
	if hasReceipt {
		if _, ok := knownVendorKinds[receipt]; !ok {
			return nil, fmt.Errorf("legacy: resource %q has unknown vendor receipt kind %q", resource.ID, receipt)
		}
		if _, ok := knownVendorKinds[uninstall]; !ok || receipt != uninstall {
			return nil, fmt.Errorf("legacy: resource %q has unsupported vendor uninstall kind %q", resource.ID, uninstall)
		}
		declarations = append(declarations, Declaration{Kind: Vendor, Package: resource.Package, ReceiptKind: receipt, UninstallKind: uninstall})
	}
	return declarations, nil
}

func (c *Coordinator) Detect(ctx context.Context, resource model.Resource, desired model.Observation) (Inventory, error) {
	inventory := Inventory{Resource: cloneResource(resource), Desired: cloneModelObservation(desired)}
	declarations, err := ParseDeclarations(resource)
	if err != nil {
		return inventory, err
	}
	for _, declaration := range declarations {
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

func (c *Coordinator) RemovalOperations(inventory Inventory) []RemovalOperation {
	operations := make([]RemovalOperation, 0, len(inventory.Legacy))
	for _, observation := range inventory.Legacy {
		if !observation.Present {
			continue
		}
		operations = append(operations, RemovalOperation{
			resource:    cloneResource(inventory.Resource),
			declaration: Declaration{Kind: observation.Kind, Package: observation.Package, ReceiptKind: observation.ReceiptKind, UninstallKind: observation.UninstallKind},
			observation: cloneObservation(observation),
		})
	}
	return operations
}

func (c *Coordinator) Remove(ctx context.Context, operation RemovalOperation) error {
	if operation.resource.ID == "" || !operation.observation.Present || operation.declaration.Kind != operation.observation.Kind || operation.declaration.Package != operation.observation.Package {
		return errors.New("legacy: invalid removal operation")
	}
	handler, ok := c.handlers[operation.declaration.Kind]
	if !ok {
		return &ErrUnsupportedSource{Kind: operation.declaration.Kind}
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
	trustedPrefixes := standardPrefixes(desired.Provider)
	trustedPaths := map[string]string{}
	for command, path := range desired.Paths {
		trustedPaths[command] = path
	}
	for _, observation := range legacy {
		trustedPrefixes = append(trustedPrefixes, observation.Prefixes...)
		for command, path := range observation.Paths {
			trustedPaths[command] = path
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
		if exact := trustedPaths[command]; exact != "" {
			resolvedExact, exactErr := resolveClean(c.paths, exact)
			trusted = exactErr == nil && resolved == resolvedExact
		}
		for _, prefix := range trustedPrefixes {
			resolvedPrefix, prefixErr := resolveClean(c.paths, prefix)
			if prefixErr == nil && pathWithin(resolved, resolvedPrefix) {
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

// ProviderHandler adapts an APT, mise, or Homebrew provider without exposing
// executable command strings through catalog data.
type ProviderHandler struct {
	Adapter  provider.Provider
	Prefixes []string
	Paths    map[string]string
}

func (h ProviderHandler) Inspect(ctx context.Context, desired model.Resource, declaration Declaration) (Receipt, error) {
	if isNil(h.Adapter) {
		return Receipt{}, errors.New("legacy: provider adapter is required")
	}
	legacy := legacyResource(desired, declaration, h.Adapter.Name())
	observation, err := h.Adapter.Inspect(ctx, legacy)
	if err != nil {
		return Receipt{}, err
	}
	paths := clonePaths(h.Paths)
	for command, path := range observation.Paths {
		paths[command] = path
	}
	return Receipt{Present: observation.Present, Prefixes: append([]string(nil), h.Prefixes...), Paths: paths}, nil
}

func (h ProviderHandler) SimulateRemoval(ctx context.Context, desired model.Resource, declaration Declaration) (provider.ChangeSet, error) {
	return h.Adapter.Simulate(ctx, pruneOperation(desired, declaration, h.Adapter.Name()))
}

func (h ProviderHandler) Remove(ctx context.Context, desired model.Resource, declaration Declaration) error {
	return h.Adapter.Execute(ctx, pruneOperation(desired, declaration, h.Adapter.Name()))
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

func standardPrefixes(providerName string) []string {
	if providerName == "homebrew-formula" || providerName == "homebrew-cask" {
		return []string{"/opt/homebrew", "/usr/local", "/home/linuxbrew/.linuxbrew"}
	}
	return nil
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

func safeIdentifier(value string) bool {
	if value == "" || strings.HasPrefix(value, "-") || strings.HasPrefix(value, "/") {
		return false
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	for _, char := range []byte(value) {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || strings.ContainsRune("._+@:/~-", rune(char)) {
			continue
		}
		return false
	}
	return true
}

func cloneResource(resource model.Resource) model.Resource {
	resource.Profiles = append([]model.Profile(nil), resource.Profiles...)
	resource.DependsOn = append([]model.ResourceID(nil), resource.DependsOn...)
	resource.Commands = append([]string(nil), resource.Commands...)
	resource.Metadata = make(map[string]string, len(resource.Metadata))
	for key, value := range resource.Metadata {
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
