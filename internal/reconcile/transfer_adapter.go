package reconcile

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/provider"
	"github.com/juty9026/terrapod/internal/provider/legacy"
	"github.com/juty9026/terrapod/internal/resource"
)

type LegacyCoordinator interface {
	Detect(context.Context, model.Profile, model.Resource, model.Observation) (legacy.Inventory, error)
	PreflightRemovals(context.Context, legacy.Inventory) (legacy.Preflight, provider.ChangeSet, error)
	RemovePreflight(context.Context, legacy.Preflight, legacy.Inventory) error
	CancelPreflight(legacy.Preflight) error
}

// ProviderTransferAdapter composes a desired package provider with the opaque,
// re-inspecting legacy removal capability.
type ProviderTransferAdapter struct {
	desired    *resource.ProviderAdapter
	legacy     LegacyCoordinator
	profile    model.Profile
	simulateMu sync.Mutex
	mu         sync.Mutex
	preflights map[string]transferPreflight
}
type transferPreflight struct {
	operation  model.Operation
	capability legacy.Preflight
}

func NewProviderTransferAdapter(desired *resource.ProviderAdapter, coordinator LegacyCoordinator, profile model.Profile) (*ProviderTransferAdapter, error) {
	if desired == nil || isNil(coordinator) {
		return nil, errors.New("reconcile: desired provider and legacy coordinator are required")
	}
	if !profile.Supported() {
		return nil, fmt.Errorf("reconcile: unsupported transfer profile %q", profile)
	}
	return &ProviderTransferAdapter{desired: desired, legacy: coordinator, profile: profile, preflights: make(map[string]transferPreflight)}, nil
}
func (a *ProviderTransferAdapter) Inspect(ctx context.Context, item model.Resource) (model.Observation, error) {
	return a.desired.Inspect(ctx, item)
}
func (a *ProviderTransferAdapter) Plan(ctx context.Context, item model.Resource, o model.Observation, owned model.Ownership) ([]model.Operation, error) {
	return a.desired.Plan(ctx, item, o, owned)
}
func (a *ProviderTransferAdapter) Execute(ctx context.Context, op model.Operation) model.OperationResult {
	if op.Kind == model.OperationTransfer {
		return failedPhase(op, "transfer requires engine-controlled phases")
	}
	return a.desired.Execute(ctx, op)
}
func (a *ProviderTransferAdapter) Verify(ctx context.Context, item model.Resource) (model.Observation, error) {
	return a.desired.Verify(ctx, item)
}
func (a *ProviderTransferAdapter) Simulate(ctx context.Context, item model.Resource, op model.Operation) (provider.ChangeSet, error) {
	a.simulateMu.Lock()
	defer a.simulateMu.Unlock()
	a.mu.Lock()
	old, hadOld := a.preflights[op.ID]
	delete(a.preflights, op.ID)
	a.mu.Unlock()
	if hadOld {
		if err := a.legacy.CancelPreflight(old.capability); err != nil {
			return provider.ChangeSet{}, err
		}
	}
	desired := op
	if desired.Kind == model.OperationTransfer {
		desired.Kind = model.OperationInstall
		desired.Removes = nil
		desired.RequiresPrivilege = item.Provider == "apt"
	}
	changes, err := a.desired.Simulate(ctx, item, desired)
	if err != nil {
		return provider.ChangeSet{}, err
	}
	if op.Kind == model.OperationTransfer {
		if len(changes.Removes) != 0 {
			return provider.ChangeSet{}, errors.New("reconcile: desired transfer simulation proposed removals")
		}
		observation, inspectErr := a.desired.Inspect(ctx, item)
		if inspectErr != nil {
			return provider.ChangeSet{}, inspectErr
		}
		inventory, detectErr := a.legacy.Detect(ctx, a.profile, item, observation)
		if detectErr != nil {
			return provider.ChangeSet{}, detectErr
		}
		if err := authorizedLegacySubset(inventory.Legacy(), op.Removes); err != nil {
			return provider.ChangeSet{}, err
		}
		capability, legacyChanges, preflightErr := a.legacy.PreflightRemovals(ctx, inventory)
		if preflightErr != nil {
			return provider.ChangeSet{}, preflightErr
		}
		changes.Removes = legacyChanges.Removes
		a.mu.Lock()
		a.preflights[op.ID] = transferPreflight{operation: op, capability: capability}
		a.mu.Unlock()
	}
	return changes, nil
}

func (a *ProviderTransferAdapter) CancelSimulation(op model.Operation) error {
	a.mu.Lock()
	preflight, ok := a.preflights[op.ID]
	if ok && reflect.DeepEqual(preflight.operation, op) {
		delete(a.preflights, op.ID)
	} else {
		ok = false
	}
	a.mu.Unlock()
	if !ok {
		return nil
	}
	return a.legacy.CancelPreflight(preflight.capability)
}
func (a *ProviderTransferAdapter) InstallDesired(ctx context.Context, item model.Resource, op model.Operation) model.OperationResult {
	desired := op
	desired.Kind = model.OperationInstall
	desired.Removes = nil
	desired.RequiresPrivilege = item.Provider == "apt"
	return a.desired.Execute(ctx, desired)
}
func (a *ProviderTransferAdapter) RemoveLegacy(ctx context.Context, item model.Resource, op model.Operation) model.OperationResult {
	desired, err := a.desired.Verify(ctx, item)
	if err != nil {
		return failedPhase(op, err.Error())
	}
	inventory, err := a.legacy.Detect(ctx, a.profile, item, desired)
	if err != nil {
		return failedPhase(op, err.Error())
	}
	observed := inventory.Legacy()
	if err := authorizedLegacySubset(observed, op.Removes); err != nil {
		return failedPhase(op, err.Error())
	}
	a.mu.Lock()
	preflight, ok := a.preflights[op.ID]
	if ok {
		delete(a.preflights, op.ID)
	}
	a.mu.Unlock()
	if !ok || !reflect.DeepEqual(preflight.operation, op) {
		return failedPhase(op, "legacy transfer was not preflighted")
	}
	if err := ctx.Err(); err != nil {
		return failedPhase(op, err.Error())
	}
	if err := a.legacy.RemovePreflight(ctx, preflight.capability, inventory); err != nil {
		return failedPhase(op, err.Error())
	}
	return model.OperationResult{OperationID: op.ID, ResourceID: op.ResourceID, Success: true, FinishedAt: time.Now().UTC()}
}
func authorizedLegacySubset(observed []legacy.Observation, authorized []string) error {
	allowed := make(map[string]struct{}, len(authorized))
	for _, id := range authorized {
		allowed[id] = struct{}{}
	}
	seen := make(map[string]struct{})
	for _, receipt := range observed {
		if _, ok := allowed[receipt.Package]; !ok {
			return errors.New("reconcile: legacy inventory contains unauthorized source")
		}
		seen[receipt.Package] = struct{}{}
	}
	if len(seen) > len(allowed) {
		return errors.New("reconcile: legacy inventory contains unauthorized source")
	}
	return nil
}
func (a *ProviderTransferAdapter) VerifyLegacyAbsent(ctx context.Context, item model.Resource, _ model.Operation) error {
	desired, err := a.desired.Verify(ctx, item)
	if err != nil {
		return err
	}
	inventory, err := a.legacy.Detect(ctx, a.profile, item, desired)
	if err != nil {
		return err
	}
	if len(inventory.Legacy()) != 0 {
		return errors.New("reconcile: legacy source remains present")
	}
	return nil
}
func failedPhase(op model.Operation, detail string) model.OperationResult {
	return model.OperationResult{OperationID: op.ID, ResourceID: op.ResourceID, Detail: detail, FinishedAt: time.Now().UTC()}
}

var _ resource.Adapter = (*ProviderTransferAdapter)(nil)
var _ TransferAdapter = (*ProviderTransferAdapter)(nil)
var _ Simulator = (*ProviderTransferAdapter)(nil)
var _ SimulationLifecycle = (*ProviderTransferAdapter)(nil)
