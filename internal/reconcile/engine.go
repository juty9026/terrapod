package reconcile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/juty9026/terrapod/internal/legacydecl"
	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/provider"
	"github.com/juty9026/terrapod/internal/resource"
	"github.com/juty9026/terrapod/internal/state"
)

// Simulator is an optional execution capability required for privileged work.
// For a transfer it describes every desired-install and legacy-removal phase.
type Simulator interface {
	Simulate(context.Context, model.Resource, model.Operation) (provider.ChangeSet, error)
}

// TransferAdapter exposes the phases whose ordering must be controlled by the
// engine. Implementations must re-inspect their legacy source in these calls.
type TransferAdapter interface {
	InstallDesired(context.Context, model.Resource, model.Operation) model.OperationResult
	RemoveLegacy(context.Context, model.Resource, model.Operation) model.OperationResult
	VerifyLegacyAbsent(context.Context, model.Resource, model.Operation) error
}

type Summary struct {
	Ready       []model.ResourceID
	Unavailable map[model.ResourceID]string
}

type HistoricalResource struct {
	Resource      model.Resource
	CatalogDigest string
}
type ApplyInput struct {
	Plan                model.Plan
	CurrentResources    []model.Resource
	EnabledIDs          []model.ResourceID
	HistoricalResources map[model.ResourceID]HistoricalResource
	CatalogDigest       string
	Profile             model.Profile
}

type Engine struct {
	Registry        resource.Registry
	State           *state.Store
	LockDir         string
	Privilege       provider.Privilege
	Resources       map[model.ResourceID]model.Resource
	Enabled         []model.ResourceID
	ResourceDigests map[model.ResourceID]string
	CatalogDigest   string
	EffectiveUID    func() int
	Profile         model.Profile
}

func (e *Engine) ApplyInput(ctx context.Context, input ApplyInput) (Summary, error) {
	if !input.Profile.Supported() {
		return Summary{}, fmt.Errorf("reconcile: unsupported active profile %q", input.Profile)
	}
	copyEngine := *e
	copyEngine.Resources = make(map[model.ResourceID]model.Resource, len(input.CurrentResources)+len(input.HistoricalResources))
	for _, item := range input.CurrentResources {
		if _, duplicate := copyEngine.Resources[item.ID]; duplicate {
			return Summary{}, fmt.Errorf("reconcile: duplicate current resource %q", item.ID)
		}
		copyEngine.Resources[item.ID] = item
	}
	copyEngine.Enabled = append([]model.ResourceID(nil), input.EnabledIDs...)
	copyEngine.ResourceDigests = make(map[model.ResourceID]string, len(input.HistoricalResources))
	enabled := make(map[model.ResourceID]struct{}, len(input.EnabledIDs))
	for _, id := range input.EnabledIDs {
		enabled[id] = struct{}{}
	}
	for id, historical := range input.HistoricalResources {
		if historical.Resource.ID != id {
			return Summary{}, fmt.Errorf("reconcile: historical resource index mismatch for %q", id)
		}
		if _, current := enabled[id]; current {
			return Summary{}, fmt.Errorf("reconcile: enabled resource %q also supplied as historical", id)
		}
		copyEngine.Resources[id] = historical.Resource
		copyEngine.ResourceDigests[id] = historical.CatalogDigest
	}
	copyEngine.CatalogDigest = input.CatalogDigest
	copyEngine.Profile = input.Profile
	return copyEngine.Apply(ctx, input.Plan)
}

func (e *Engine) Apply(ctx context.Context, plan model.Plan) (summary Summary, retErr error) {
	summary.Unavailable = cloneUnavailable(plan.Unavailable)
	if plan.ID == "" {
		return summary, errors.New("reconcile: plan ID is empty")
	}
	geteuid := e.EffectiveUID
	if geteuid == nil {
		geteuid = os.Geteuid
	}
	if geteuid() == 0 {
		return summary, errors.New("reconcile: must run as a non-root user")
	}
	if e.State == nil || e.LockDir == "" {
		return summary, errors.New("reconcile: state store and lock directory are required")
	}
	if e.CatalogDigest == "" {
		return summary, errors.New("reconcile: verified catalog digest is required")
	}
	if err := ctx.Err(); err != nil {
		return summary, err
	}
	enabledSet := make(map[model.ResourceID]struct{}, len(e.Enabled))
	for _, id := range e.Enabled {
		if _, duplicate := enabledSet[id]; duplicate {
			return summary, fmt.Errorf("reconcile: duplicate enabled resource %q", id)
		}
		enabledSet[id] = struct{}{}
		item, ok := e.Resources[id]
		if !ok || item.ID != id {
			return summary, fmt.Errorf("reconcile: enabled resource %q is not signed", id)
		}
	}
	lock, err := state.Acquire(e.LockDir, "tpod apply")
	if err != nil {
		return summary, fmt.Errorf("reconcile: acquire state lock: %w", err)
	}
	defer func() { retErr = errors.Join(retErr, lock.Release()) }()
	persisted, err := e.State.Snapshot()
	if err != nil {
		return summary, fmt.Errorf("reconcile: read locked snapshot: %w", err)
	}

	indexed := make(map[model.ResourceID]model.Resource, len(plan.Operations))
	adapters := make(map[model.ResourceID]resource.Adapter, len(plan.Operations))
	privileged := false
	seenOperations := make(map[string]struct{}, len(plan.Operations))
	remaining := make(map[model.ResourceID]int, len(plan.Operations))
	authorizedRemovals := make(map[model.ResourceID]map[string]struct{})
	for _, operation := range plan.Operations {
		if _, duplicate := seenOperations[operation.ID]; duplicate {
			return summary, fmt.Errorf("reconcile: duplicate operation ID %q", operation.ID)
		}
		seenOperations[operation.ID] = struct{}{}
		remaining[operation.ResourceID]++
		item, adapter, err := e.authorize(operation)
		if err != nil {
			return summary, err
		}
		indexed[operation.ResourceID], adapters[operation.ResourceID] = item, adapter
		_, current := enabledSet[item.ID]
		if current && operation.Kind == model.OperationPrune {
			return summary, fmt.Errorf("reconcile: enabled resource %q cannot be pruned", item.ID)
		}
		if !current {
			if operation.Kind != model.OperationPrune {
				return summary, fmt.Errorf("reconcile: historical resource %q only supports prune", item.ID)
			}
			if err := validateHistoricalOwnership(item, e.ResourceDigests[item.ID], persisted.Ownership[item.ID]); err != nil {
				return summary, err
			}
		}
		allowed := authorizedRemovals[item.ID]
		if allowed == nil {
			allowed = make(map[string]struct{})
			authorizedRemovals[item.ID] = allowed
		}
		if operation.Kind == model.OperationPrune && item.Package != "" {
			allowed[item.Package] = struct{}{}
		}
		declarations, err := legacydecl.Parse(item)
		if err != nil {
			return summary, fmt.Errorf("reconcile: parse legacy authority for %q: %w", item.ID, err)
		}
		for _, declaration := range declarations {
			allowed[declaration.Package] = struct{}{}
		}
		if derivedPrivilege(item, operation, declarations, e.Profile) && !operation.RequiresPrivilege {
			return summary, fmt.Errorf("reconcile: operation %q omits required privilege", operation.ID)
		}
		if err := validateRemoves(item, operation, declarations, e.Profile); err != nil {
			return summary, err
		}
	}
	for _, operation := range plan.Operations {
		item, adapter := indexed[operation.ResourceID], adapters[operation.ResourceID]
		for _, removal := range operation.Removes {
			if removal == item.Package {
				continue
			}
			if _, authorized := authorizedRemovals[item.ID][removal]; !authorized {
				return summary, fmt.Errorf("reconcile: operation %q names unauthorized removal %q", operation.ID, removal)
			}
		}
		if !operation.RequiresPrivilege && operation.Kind != model.OperationTransfer {
			continue
		}
		privileged = privileged || operation.RequiresPrivilege
		simulator, ok := adapter.(Simulator)
		if !ok {
			return summary, fmt.Errorf("reconcile: privileged operation %q has no simulator", operation.ID)
		}
		changes, err := simulator.Simulate(ctx, item, operation)
		if err != nil {
			return summary, fmt.Errorf("reconcile: simulate %q: %w", operation.ID, err)
		}
		if err := provider.ValidateChangeSet(changes, item, operation.Removes); err != nil {
			return summary, fmt.Errorf("reconcile: unsafe simulation %q: %w", operation.ID, err)
		}
		if !exactRemovals(changes.Removes, operation, item.Package) {
			return summary, fmt.Errorf("reconcile: simulation removals for %q do not match authorized operation", operation.ID)
		}
	}
	operated := make(map[model.ResourceID]struct{}, len(indexed))
	for id := range indexed {
		operated[id] = struct{}{}
	}
	if privileged {
		needed := false
		for _, operation := range plan.Operations {
			if !operation.RequiresPrivilege {
				continue
			}
			item := indexed[operation.ResourceID]
			if _, unavailable := summary.Unavailable[item.ID]; unavailable {
				continue
			}
			if _, blocked := blockedDependency(item, map[model.ResourceID]bool{}, summary.Unavailable); !blocked {
				needed = true
			}
		}
		privileged = needed
	}
	if privileged {
		if isNil(e.Privilege) {
			return summary, errors.New("reconcile: privilege acquisition is not configured")
		}
		if err := ctx.Err(); err != nil {
			return summary, err
		}
		if err := e.Privilege.Acquire(ctx); err != nil {
			return summary, fmt.Errorf("reconcile: acquire privilege: %w", err)
		}
	}
	if err := ctx.Err(); err != nil {
		return summary, err
	}
	journal, _, err := e.State.BeginOrResume(plan)
	if err != nil {
		return summary, fmt.Errorf("reconcile: begin journal: %w", err)
	}

	failed := make(map[model.ResourceID]bool)
	completed := make(map[model.ResourceID]bool)
	verifiedNoOp := make(map[model.ResourceID]bool)
	var verifyDependencies func(model.Resource) error
	verifyDependencies = func(item model.Resource) error {
		for _, id := range item.DependsOn {
			if reason, unavailable := summary.Unavailable[id]; unavailable {
				return fmt.Errorf("dependency %q is unavailable: %s", id, reason)
			}
			dependency := e.Resources[id]
			if _, hasOperation := operated[id]; hasOperation {
				if !completed[id] {
					return fmt.Errorf("dependency %q has not reconciled", id)
				}
				continue
			}
			if !verifiedNoOp[id] {
				if err := verifyDependencies(dependency); err != nil {
					summary.Unavailable[id] = err.Error()
					return err
				}
				adapter, ok := e.Registry.Lookup(dependency.Type, dependency.Provider)
				if !ok {
					summary.Unavailable[id] = "no adapter for dependency verification"
					return errors.New(summary.Unavailable[id])
				}
				if _, err := verifyDesired(ctx, adapter, dependency); err != nil {
					summary.Unavailable[id] = "dependency verification: " + err.Error()
					return err
				}
				verifiedNoOp[id] = true
			}
		}
		return nil
	}
	pendingOwnership := make(map[model.ResourceID]struct{})
	for _, operation := range plan.Operations {
		item, adapter := indexed[operation.ResourceID], adapters[operation.ResourceID]
		if failed[item.ID] {
			if err := e.record(ctx, operation, false, "blocked by earlier resource failure"); err != nil {
				return summary, err
			}
			continue
		}
		if err := verifyDependencies(item); err != nil {
			e.setUnavailable(&summary, failed, item.ID, err.Error())
			if recordErr := e.record(ctx, operation, false, err.Error()); recordErr != nil {
				return summary, recordErr
			}
			continue
		}
		if dependency, blocked := blockedDependency(item, failed, summary.Unavailable); blocked {
			e.setUnavailable(&summary, failed, item.ID, fmt.Sprintf("dependency %q is unavailable", dependency))
			if err := e.record(ctx, operation, false, summary.Unavailable[item.ID]); err != nil {
				return summary, err
			}
			continue
		}
		if reason, unavailable := summary.Unavailable[item.ID]; unavailable {
			failed[item.ID] = true
			_ = reason
			continue
		}
		observed, err := e.execute(ctx, item, operation, adapter, persisted.Ownership[item.ID])
		if err != nil {
			e.setUnavailable(&summary, failed, item.ID, err.Error())
			if recordErr := e.record(ctx, operation, false, err.Error()); recordErr != nil {
				return summary, recordErr
			}
			continue
		}
		remaining[item.ID]--
		if remaining[item.ID] == 0 && operation.Kind != model.OperationPrune {
			verifyErr := error(nil)
			if !desiredVerified(item, observed) {
				observed, verifyErr = verifyDesired(ctx, adapter, item)
			}
			if verifyErr != nil {
				e.setUnavailable(&summary, failed, item.ID, verifyErr.Error())
				if recordErr := e.record(ctx, operation, false, verifyErr.Error()); recordErr != nil {
					return summary, recordErr
				}
				continue
			}
			pendingOwnership[item.ID] = struct{}{}
			completed[item.ID] = true
		}
		if err := e.record(ctx, operation, true, "verified"); err != nil {
			return summary, err
		}
	}
	finalObservations := make(map[model.ResourceID]model.Observation)
	for _, id := range dependencyOrder(e.Enabled, e.Resources) {
		if !failed[id] {
			if _, unavailable := summary.Unavailable[id]; !unavailable {
				item := e.Resources[id]
				if dependency, blocked := blockedDependency(item, failed, summary.Unavailable); blocked {
					summary.Unavailable[id] = fmt.Sprintf("final dependency %q is unavailable", dependency)
					continue
				}
				adapter, ok := e.Registry.Lookup(item.Type, item.Provider)
				if !ok {
					summary.Unavailable[id] = "no adapter for final verification"
					continue
				}
				observed, err := verifyDesired(ctx, adapter, item)
				if err != nil {
					summary.Unavailable[id] = "final readiness verification: " + err.Error()
					continue
				}
				finalObservations[id] = observed
				summary.Ready = append(summary.Ready, id)
			}
		}
	}
	for id := range pendingOwnership {
		observed, ready := finalObservations[id]
		if !ready {
			continue
		}
		if err := ctx.Err(); err != nil {
			return summary, err
		}
		if err := e.own(e.Resources[id], observed); err != nil {
			return summary, err
		}
	}
	if err := ctx.Err(); err != nil {
		return summary, err
	}
	if err := e.State.Complete(journal.ID); err != nil {
		return summary, fmt.Errorf("reconcile: complete journal: %w", err)
	}
	sort.Slice(summary.Ready, func(i, j int) bool { return summary.Ready[i] < summary.Ready[j] })
	return summary, nil
}

func derivedPrivilege(item model.Resource, operation model.Operation, declarations []legacydecl.Declaration, profile model.Profile) bool {
	if item.Provider == "apt" {
		return true
	}
	if operation.Kind != model.OperationTransfer {
		return false
	}
	removed := make(map[string]struct{}, len(operation.Removes))
	for _, id := range operation.Removes {
		removed[id] = struct{}{}
	}
	for _, declaration := range declarations {
		if declaration.Kind == legacydecl.APT && (declaration.Profile == "" || declaration.Profile == profile) {
			if _, ok := removed[declaration.Package]; ok {
				return true
			}
		}
	}
	return false
}

func (e *Engine) authorize(operation model.Operation) (model.Resource, resource.Adapter, error) {
	if operation.ID == "" || operation.ResourceID == "" {
		return model.Resource{}, nil, errors.New("reconcile: operation identity is empty")
	}
	item, ok := e.Resources[operation.ResourceID]
	if !ok {
		return model.Resource{}, nil, fmt.Errorf("reconcile: resource %q is not signed", operation.ResourceID)
	}
	if item.ID != operation.ResourceID {
		return model.Resource{}, nil, fmt.Errorf("reconcile: signed resource index mismatch for %q", operation.ResourceID)
	}
	switch operation.Kind {
	case model.OperationAdopt, model.OperationInstall, model.OperationUpgrade, model.OperationTransfer, model.OperationPrune, model.OperationRestore, model.OperationVerify:
	default:
		return model.Resource{}, nil, fmt.Errorf("reconcile: operation %q has unsupported kind %q", operation.ID, operation.Kind)
	}
	if operation.Provider != item.Provider || operation.Package != item.Package {
		return model.Resource{}, nil, fmt.Errorf("reconcile: operation %q identity does not match signed resource", operation.ID)
	}
	adapter, ok := e.Registry.Lookup(item.Type, item.Provider)
	if !ok {
		return model.Resource{}, nil, fmt.Errorf("reconcile: no adapter for resource %q", item.ID)
	}
	return item, adapter, nil
}

func (e *Engine) execute(ctx context.Context, item model.Resource, operation model.Operation, adapter resource.Adapter, owned model.Ownership) (model.Observation, error) {
	if operation.Kind == model.OperationPrune {
		observed, err := adapter.Inspect(ctx, item)
		if err != nil {
			return model.Observation{}, fmt.Errorf("inspect resource before prune: %w", err)
		}
		if !observed.Present {
			if err := ctx.Err(); err != nil {
				return model.Observation{}, err
			}
			return model.Observation{}, e.State.DeleteOwnership(item.ID)
		}
		if !observed.Healthy || observed.Provider != item.Provider || observed.Package != item.Package {
			return model.Observation{}, errors.New("historical inspection does not match ownership receipt")
		}
		if err := ctx.Err(); err != nil {
			return model.Observation{}, err
		}
		result := adapter.Execute(ctx, operation)
		if err := validResult(operation, result); err != nil {
			return model.Observation{}, err
		}
		observed, err = adapter.Inspect(ctx, item)
		if err != nil {
			return model.Observation{}, fmt.Errorf("inspect pruned resource: %w", err)
		}
		if observed.Present {
			return model.Observation{}, errors.New("resource remains present after prune")
		}
		if err := ctx.Err(); err != nil {
			return model.Observation{}, err
		}
		return model.Observation{}, e.State.DeleteOwnership(item.ID)
	}
	if operation.Kind == model.OperationTransfer {
		phased, ok := adapter.(TransferAdapter)
		if !ok {
			return model.Observation{}, errors.New("transfer adapter does not support phased legacy removal")
		}
		observed, inspectErr := adapter.Inspect(ctx, item)
		if inspectErr != nil {
			return model.Observation{}, fmt.Errorf("inspect resource before transfer: %w", inspectErr)
		}
		if !desiredVerified(item, observed) {
			if err := ctx.Err(); err != nil {
				return model.Observation{}, err
			}
			result := phased.InstallDesired(ctx, item, operation)
			if err := validResult(operation, result); err != nil {
				return model.Observation{}, fmt.Errorf("install desired: %w", err)
			}
		}
		if _, err := verifyDesired(ctx, adapter, item); err != nil {
			return model.Observation{}, fmt.Errorf("verify desired before legacy removal: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return model.Observation{}, err
		}
		result := phased.RemoveLegacy(ctx, item, operation)
		if err := validResult(operation, result); err != nil {
			return model.Observation{}, fmt.Errorf("remove legacy: %w", err)
		}
		if err := phased.VerifyLegacyAbsent(ctx, item, operation); err != nil {
			return model.Observation{}, fmt.Errorf("verify legacy absent: %w", err)
		}
		observed, err := verifyDesired(ctx, adapter, item)
		if err != nil {
			return model.Observation{}, fmt.Errorf("final desired verification: %w", err)
		}
		return observed, nil
	}
	if operation.Kind == model.OperationVerify {
		return verifyDesired(ctx, adapter, item)
	}
	if operation.Kind == model.OperationInstall || operation.Kind == model.OperationAdopt || operation.Kind == model.OperationUpgrade || operation.Kind == model.OperationRestore {
		observed, err := adapter.Inspect(ctx, item)
		if err != nil {
			return model.Observation{}, fmt.Errorf("inspect resource before %s: %w", operation.Kind, err)
		}
		if desiredVerified(item, observed) {
			return observed, nil
		}
	}
	if err := ctx.Err(); err != nil {
		return model.Observation{}, err
	}
	result := adapter.Execute(ctx, operation)
	if err := validResult(operation, result); err != nil {
		return model.Observation{}, err
	}
	return verifyDesired(ctx, adapter, item)
}

func isNil(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return reflected.IsNil()
	}
	return false
}

func exactRemovals(actual []string, operation model.Operation, target string) bool {
	expected := make(map[string]struct{}, len(operation.Removes)+1)
	for _, id := range operation.Removes {
		if id == "" {
			return false
		}
		if _, duplicate := expected[id]; duplicate {
			return false
		}
		expected[id] = struct{}{}
	}
	if operation.Kind == model.OperationPrune && target != "" {
		expected[target] = struct{}{}
	}
	seen := make(map[string]struct{}, len(actual))
	for _, id := range actual {
		if id == "" {
			return false
		}
		if _, duplicate := seen[id]; duplicate {
			return false
		}
		seen[id] = struct{}{}
	}
	if operation.Kind == model.OperationTransfer {
		for id := range seen {
			if _, ok := expected[id]; !ok {
				return false
			}
		}
		return true
	}
	if len(seen) != len(expected) {
		return false
	}
	for id := range expected {
		if _, ok := seen[id]; !ok {
			return false
		}
	}
	return true
}

func validateRemoves(item model.Resource, operation model.Operation, declarations []legacydecl.Declaration, profile model.Profile) error {
	seen := make(map[string]struct{}, len(operation.Removes))
	for _, id := range operation.Removes {
		if id == "" {
			return fmt.Errorf("reconcile: operation %q removes contains an empty package", operation.ID)
		}
		if _, duplicate := seen[id]; duplicate {
			return fmt.Errorf("reconcile: operation %q removes contains duplicate %q", operation.ID, id)
		}
		seen[id] = struct{}{}
	}
	expected := make(map[string]struct{})
	switch operation.Kind {
	case model.OperationPrune:
		expected[item.Package] = struct{}{}
	case model.OperationTransfer:
		for _, declaration := range declarations {
			if declaration.Profile != "" && declaration.Profile != profile {
				continue
			}
			expected[declaration.Package] = struct{}{}
		}
		if len(seen) == 0 {
			return fmt.Errorf("reconcile: operation %q removes has no legacy source", operation.ID)
		}
		for id := range seen {
			if _, ok := expected[id]; !ok {
				return fmt.Errorf("reconcile: operation %q removes is not authorized by its legacy declarations", operation.ID)
			}
		}
		return nil
	case model.OperationInstall, model.OperationAdopt, model.OperationUpgrade, model.OperationRestore, model.OperationVerify:
	}
	if len(seen) != len(expected) {
		return fmt.Errorf("reconcile: operation %q removes does not match %s semantics", operation.ID, operation.Kind)
	}
	for id := range expected {
		if _, ok := seen[id]; !ok {
			return fmt.Errorf("reconcile: operation %q removes does not match %s semantics", operation.ID, operation.Kind)
		}
	}
	return nil
}

func validateHistoricalOwnership(item model.Resource, digest string, owned model.Ownership) error {
	if digest == "" || owned.ResourceID != item.ID || owned.Provider != item.Provider || owned.Package != item.Package || owned.CatalogDigest != digest {
		return fmt.Errorf("reconcile: historical ownership for %q does not match signed authority", item.ID)
	}
	expected := make(map[string]string)
	for key, value := range item.Metadata {
		if strings.HasPrefix(key, "path.") {
			expected[strings.TrimPrefix(key, "path.")] = value
		}
	}
	if !reflect.DeepEqual(expected, owned.Paths) {
		return fmt.Errorf("reconcile: historical ownership paths for %q do not match signed authority", item.ID)
	}
	return nil
}

func dependencyOrder(ids []model.ResourceID, resources map[model.ResourceID]model.Resource) []model.ResourceID {
	enabled := make(map[model.ResourceID]struct{}, len(ids))
	for _, id := range ids {
		enabled[id] = struct{}{}
	}
	state := make(map[model.ResourceID]uint8)
	result := make([]model.ResourceID, 0, len(ids))
	var visit func(model.ResourceID)
	visit = func(id model.ResourceID) {
		if state[id] != 0 {
			return
		}
		state[id] = 1
		for _, dependency := range resources[id].DependsOn {
			if _, ok := enabled[dependency]; ok {
				visit(dependency)
			}
		}
		state[id] = 2
		result = append(result, id)
	}
	sorted := append([]model.ResourceID(nil), ids...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	for _, id := range sorted {
		visit(id)
	}
	return result
}

func verifyDesired(ctx context.Context, adapter resource.Adapter, item model.Resource) (model.Observation, error) {
	observed, err := adapter.Verify(ctx, item)
	if err != nil {
		return model.Observation{}, err
	}
	if !desiredVerified(item, observed) {
		return model.Observation{}, errors.New("verification did not establish signed desired identity")
	}
	return observed, nil
}

func desiredVerified(item model.Resource, observed model.Observation) bool {
	return observed.Present && observed.Healthy && observed.Provider == item.Provider && observed.Package == item.Package
}

func validResult(operation model.Operation, result model.OperationResult) error {
	if result.OperationID != operation.ID || result.ResourceID != operation.ResourceID {
		return errors.New("adapter returned mismatched operation result")
	}
	if !result.Success {
		if result.Detail != "" {
			return errors.New(result.Detail)
		}
		return errors.New("operation failed")
	}
	return nil
}

func (e *Engine) own(item model.Resource, _ model.Observation) error {
	paths := make(map[string]string)
	for key, value := range item.Metadata {
		if strings.HasPrefix(key, "path.") {
			paths[strings.TrimPrefix(key, "path.")] = value
		}
	}
	return e.State.PutOwnership(model.Ownership{ResourceID: item.ID, CatalogDigest: e.CatalogDigest, Provider: item.Provider, Package: item.Package, Paths: paths, PriorValues: make(map[string]json.RawMessage)})
}

func (e *Engine) record(ctx context.Context, operation model.Operation, success bool, detail string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return e.State.Record(model.OperationResult{OperationID: operation.ID, ResourceID: operation.ResourceID, Success: success, Detail: detail, FinishedAt: time.Now().UTC()})
}

func (e *Engine) setUnavailable(summary *Summary, failed map[model.ResourceID]bool, id model.ResourceID, reason string) {
	failed[id] = true
	summary.Unavailable[id] = reason
}

func blockedDependency(item model.Resource, failed map[model.ResourceID]bool, unavailable map[model.ResourceID]string) (model.ResourceID, bool) {
	for _, dependency := range item.DependsOn {
		if failed[dependency] {
			return dependency, true
		}
		if _, blocked := unavailable[dependency]; blocked {
			return dependency, true
		}
	}
	return "", false
}

func cloneUnavailable(input map[model.ResourceID]string) map[model.ResourceID]string {
	result := make(map[model.ResourceID]string, len(input))
	for id, reason := range input {
		result[id] = reason
	}
	return result
}
