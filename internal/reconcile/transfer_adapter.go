package reconcile

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/provider"
	"github.com/juty9026/terrapod/internal/provider/legacy"
	"github.com/juty9026/terrapod/internal/resource"
)

type LegacyCoordinator interface {
	Detect(context.Context, model.Profile, model.Resource, model.Observation) (legacy.Inventory, error)
	RemovalOperations(legacy.Inventory) ([]legacy.RemovalOperation, error)
	Remove(context.Context, legacy.RemovalOperation) error
	PreflightRemovals(context.Context, legacy.Inventory) (provider.ChangeSet, error)
}

// ProviderTransferAdapter composes a desired package provider with the opaque,
// re-inspecting legacy removal capability.
type ProviderTransferAdapter struct {
	desired *resource.ProviderAdapter
	legacy  LegacyCoordinator
	profile model.Profile
}

func NewProviderTransferAdapter(desired *resource.ProviderAdapter, coordinator LegacyCoordinator, profile model.Profile) (*ProviderTransferAdapter, error) {
	if desired == nil || isNil(coordinator) {
		return nil, errors.New("reconcile: desired provider and legacy coordinator are required")
	}
	if !profile.Supported() {
		return nil, fmt.Errorf("reconcile: unsupported transfer profile %q", profile)
	}
	return &ProviderTransferAdapter{desired: desired, legacy: coordinator, profile: profile}, nil
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
	desired := op
	if desired.Kind == model.OperationTransfer {
		desired.Kind = model.OperationInstall
		desired.Removes = nil
	}
	changes, err := a.desired.Simulate(ctx, item, desired)
	if err != nil {
		return provider.ChangeSet{}, err
	}
	if op.Kind == model.OperationTransfer {
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
		legacyChanges, preflightErr := a.legacy.PreflightRemovals(ctx, inventory)
		if preflightErr != nil {
			return provider.ChangeSet{}, preflightErr
		}
		changes.Removes = legacyChanges.Removes
	}
	return changes, nil
}
func (a *ProviderTransferAdapter) InstallDesired(ctx context.Context, item model.Resource, op model.Operation) model.OperationResult {
	desired := op
	desired.Kind = model.OperationInstall
	desired.Removes = nil
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
	want := make(map[string]struct{}, len(op.Removes))
	for _, id := range op.Removes {
		want[id] = struct{}{}
	}
	observed := inventory.Legacy()
	if err := authorizedLegacySubset(observed, op.Removes); err != nil {
		return failedPhase(op, err.Error())
	}
	operations, err := a.legacy.RemovalOperations(inventory)
	if err != nil {
		return failedPhase(op, err.Error())
	}
	for _, removal := range operations {
		if err := ctx.Err(); err != nil {
			return failedPhase(op, err.Error())
		}
		if err := a.legacy.Remove(ctx, removal); err != nil {
			return failedPhase(op, err.Error())
		}
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
