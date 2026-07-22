package resolve

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"

	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/reconcile"
	"github.com/juty9026/terrapod/internal/resource"
	"github.com/juty9026/terrapod/internal/resource/managedfiles"
	"github.com/juty9026/terrapod/internal/state"
)

var afterManagedFilesMutation func() error

type managedFilesCapability interface {
	resource.Adapter
	Conflicts(context.Context, model.Resource, model.Ownership) ([]managedfiles.Conflict, error)
	ValidateConflictBaseline(model.Resource, []managedfiles.Conflict) error
	ResolveConflict(context.Context, model.Resource, string, []managedfiles.Conflict) error
}

// ManagedFiles resolves local managed-file conflicts and commits the resulting
// ownership under one exact state lock.
type ManagedFiles struct {
	StateDir     string
	Rebuild      RebuildFunc
	Engine       *reconcile.Engine
	EffectiveUID func() int
}

type ErrNotManaged struct{ ID model.ResourceID }

func (e *ErrNotManaged) Error() string {
	return fmt.Sprintf("resolve: resource %q is not a managed-files resource", e.ID)
}

func (r *ManagedFiles) Resolve(ctx context.Context, id model.ResourceID, input io.Reader, output io.Writer) (result Result, retErr error) {
	if err := validateID(id); err != nil {
		return result, err
	}
	geteuid := r.EffectiveUID
	if geteuid == nil {
		geteuid = os.Geteuid
	}
	if geteuid() == 0 {
		return result, errors.New("resolve: must run as a non-root user")
	}
	if r.StateDir == "" || r.Rebuild == nil || r.Engine == nil || r.Engine.State == nil {
		return result, errors.New("resolve: managed-files state, rebuild, and engine are required")
	}
	if input == nil || output == nil {
		return result, errors.New("resolve: prompt input and output are required")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}

	lock, err := state.Acquire(r.StateDir, "tpod resolve "+string(id))
	if err != nil {
		return result, fmt.Errorf("resolve: acquire state lock: %w", err)
	}
	defer func() { retErr = errors.Join(retErr, lock.Release()) }()
	if err := lock.ValidateHeld(r.Engine.LockDir); err != nil {
		return result, fmt.Errorf("resolve: engine does not use the held state lock: %w", err)
	}

	rebuilt, err := r.Rebuild(ctx)
	if err != nil {
		return result, fmt.Errorf("resolve: rebuild reconciliation: %w", err)
	}
	item, historical, err := selectManagedResource(id, rebuilt)
	if err != nil {
		return result, err
	}
	adapter, ok := r.Engine.Registry.Lookup(item.Type, item.Provider)
	if !ok {
		return result, fmt.Errorf("resolve: no signed adapter for resource %q", id)
	}
	capability, ok := adapter.(managedFilesCapability)
	if !ok {
		return result, fmt.Errorf("resolve: adapter for %q lacks managed-files resolution", id)
	}

	snapshot, err := r.Engine.State.Snapshot()
	if err != nil {
		return result, fmt.Errorf("resolve: read locked snapshot: %w", err)
	}
	conflicts, err := capability.Conflicts(ctx, item, snapshot.Ownership[id])
	if err != nil {
		return result, fmt.Errorf("resolve: inspect managed-file conflicts: %w", err)
	}
	if len(conflicts) != 0 {
		if err := capability.ValidateConflictBaseline(item, conflicts); err != nil {
			return result, fmt.Errorf("resolve: validate managed-file conflicts: %w", err)
		}
	}
	approved, activeResolution, err := activeManagedAuthorization(snapshot.ActiveJournal, id, capability)
	if err != nil {
		return result, err
	}
	resume := false
	var plan model.Plan
	if activeResolution {
		currentBinding := managedAuthorization(rebuilt, item, historical, approved)
		resume = snapshot.ActiveJournal.Plan.Release == rebuilt.Plan.Release &&
			reflect.DeepEqual(*snapshot.ActiveJournal.Plan.Operations[0].ManagedFileAuthorization, currentBinding) &&
			conflictSubset(conflicts, approved)
		if resume {
			plan = snapshot.ActiveJournal.Plan
		}
	}
	result.Blockers = conflictPaths(conflicts)
	if !resume {
		if len(conflicts) == 0 {
			return result, &ErrNotBlocked{ID: id}
		}
		if err := renderManagedFilesPrompt(output, id, conflicts); err != nil {
			return result, err
		}
		confirmed, err := readConfirmation(input)
		if err != nil {
			return result, err
		}
		if !confirmed {
			return result, nil
		}
		approved = conflicts
		plan = managedResolutionPlan(rebuilt, item, historical, approved)
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	journal, resumed, err := r.Engine.State.BeginOrResume(plan)
	if err != nil {
		return result, fmt.Errorf("resolve: begin managed-files journal: %w", err)
	}
	if resumed != resume {
		return result, errors.New("resolve: active managed-files journal changed under held lock")
	}
	if len(conflicts) != 0 {
		if err := capability.ResolveConflict(ctx, item, journal.ID, approved); err != nil {
			return result, fmt.Errorf("resolve: accept managed-file conflicts: %w", err)
		}
	}
	if afterManagedFilesMutation != nil {
		if err := afterManagedFilesMutation(); err != nil {
			return result, err
		}
	}
	if _, err := capability.Verify(ctx, item); err != nil {
		return result, fmt.Errorf("resolve: verify managed-files resolution: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	result.Summary, err = r.Engine.ApplyInputHeld(ctx, scopeManagedInput(rebuilt, item, historical, plan), lock)
	if err != nil {
		return result, fmt.Errorf("resolve: reconcile managed-files %q: %w", id, err)
	}
	result.Proceeded = true
	return result, nil
}

func selectManagedResource(id model.ResourceID, input reconcile.ApplyInput) (model.Resource, bool, error) {
	var current model.Resource
	for _, candidate := range input.CurrentResources {
		if candidate.ID != id {
			continue
		}
		if current.ID != "" {
			return model.Resource{}, false, errors.New("resolve: rebuilt input contains a duplicate resource")
		}
		current = candidate
	}
	enabled := false
	for _, candidate := range input.EnabledIDs {
		if candidate != id {
			continue
		}
		if enabled {
			return model.Resource{}, false, errors.New("resolve: rebuilt input contains a duplicate enabled resource")
		}
		enabled = true
	}
	if enabled {
		if current.ID == "" {
			return model.Resource{}, false, errors.New("resolve: enabled resource is absent from signed input")
		}
		if current.Type != model.ResourceManagedFiles {
			return model.Resource{}, false, &ErrNotManaged{ID: id}
		}
		return current, false, nil
	}
	historical, ok := input.HistoricalResources[id]
	if !ok {
		return model.Resource{}, false, &ErrNotManaged{ID: id}
	}
	if historical.Resource.ID != id {
		return model.Resource{}, false, errors.New("resolve: historical resource index does not match signed input")
	}
	if historical.Resource.Type != model.ResourceManagedFiles {
		return model.Resource{}, false, &ErrNotManaged{ID: id}
	}
	return historical.Resource, true, nil
}

func managedResolutionPlan(input reconcile.ApplyInput, item model.Resource, historical bool, conflicts []managedfiles.Conflict) model.Plan {
	return managedPlanFromAuthorization(managedAuthorization(input, item, historical, conflicts), input.Plan.Release)
}

func managedAuthorization(input reconcile.ApplyInput, item model.Resource, historical bool, conflicts []managedfiles.Conflict) model.ManagedFileAuthorization {
	mode := "current"
	historicalCatalog := ""
	if historical {
		mode = "historical"
		historicalCatalog = historicalDigest(item.ID, input)
	}
	return model.ManagedFileAuthorization{
		Version:          1,
		CatalogDigest:    input.CatalogDigest,
		HistoricalDigest: historicalCatalog,
		Mode:             mode,
		Resource:         item,
		Conflicts:        append([]managedfiles.Conflict(nil), conflicts...),
	}
}

func managedPlanFromAuthorization(authorization model.ManagedFileAuthorization, release string) model.Plan {
	kind, removes := model.OperationVerify, []string(nil)
	if authorization.Mode == "historical" {
		kind, removes = model.OperationPrune, []string{authorization.Resource.Package}
	}
	payload, _ := json.Marshal(struct {
		Authorization model.ManagedFileAuthorization `json:"authorization"`
		Release       string                         `json:"release"`
	}{Authorization: authorization, Release: release})
	sum := sha256.Sum256(payload)
	operation := model.Operation{
		ID:                       "resolve-managed-files-" + string(authorization.Resource.ID),
		ResourceID:               authorization.Resource.ID,
		Kind:                     kind,
		Provider:                 authorization.Resource.Provider,
		Package:                  authorization.Resource.Package,
		Removes:                  removes,
		ManagedFileAuthorization: &authorization,
	}
	return model.Plan{
		ID:          fmt.Sprintf("resolve-managed-files-%x", sum[:16]),
		Release:     release,
		Operations:  []model.Operation{operation},
		Unavailable: map[model.ResourceID]string{},
	}
}

func activeManagedAuthorization(journal *model.Journal, id model.ResourceID, capability managedFilesCapability) ([]managedfiles.Conflict, bool, error) {
	if journal == nil {
		return nil, false, nil
	}
	resolutionPlan := strings.HasPrefix(journal.Plan.ID, "resolve-managed-files-")
	if len(journal.Plan.Operations) != 1 {
		if resolutionPlan {
			return nil, true, errors.New("resolve: active managed-files journal is malformed")
		}
		return nil, false, nil
	}
	operation := journal.Plan.Operations[0]
	expectedID := "resolve-managed-files-" + string(id)
	if operation.ManagedFileAuthorization != nil && operation.ManagedFileAuthorization.Resource.ID != id && operation.ID != expectedID {
		return nil, false, nil
	}
	if operation.ID != expectedID && operation.ManagedFileAuthorization == nil && !resolutionPlan {
		return nil, false, nil
	}
	if operation.ID != expectedID || operation.ManagedFileAuthorization == nil {
		return nil, true, errors.New("resolve: active managed-files journal is malformed")
	}
	authorization := *operation.ManagedFileAuthorization
	if authorization.Version != 1 || authorization.CatalogDigest == "" || authorization.Resource.ID != id || authorization.Resource.Type != model.ResourceManagedFiles || authorization.Resource.Provider != "chezmoi" ||
		(authorization.Mode != "current" && authorization.Mode != "historical") ||
		(authorization.Mode == "current" && authorization.HistoricalDigest != "") ||
		(authorization.Mode == "historical" && authorization.HistoricalDigest == "") {
		return nil, true, errors.New("resolve: active managed-files authorization is invalid")
	}
	if err := authorization.Resource.Validate(); err != nil {
		return nil, true, fmt.Errorf("resolve: active managed-files resource is invalid: %w", err)
	}
	if err := capability.ValidateConflictBaseline(authorization.Resource, authorization.Conflicts); err != nil {
		return nil, true, fmt.Errorf("resolve: active managed-files conflict baseline is invalid: %w", err)
	}
	expected := managedPlanFromAuthorization(authorization, journal.Plan.Release)
	if !reflect.DeepEqual(journal.Plan, expected) {
		return nil, true, errors.New("resolve: active managed-files journal authorization was tampered")
	}
	return append([]managedfiles.Conflict(nil), authorization.Conflicts...), true, nil
}

func conflictSubset(current, approved []managedfiles.Conflict) bool {
	byPath := make(map[string]managedfiles.Conflict, len(approved))
	for _, conflict := range approved {
		byPath[conflict.Path] = conflict
	}
	for _, conflict := range current {
		if expected, ok := byPath[conflict.Path]; !ok || !reflect.DeepEqual(expected, conflict) {
			return false
		}
	}
	return true
}

func scopeManagedInput(input reconcile.ApplyInput, item model.Resource, historical bool, plan model.Plan) reconcile.ApplyInput {
	digest := historicalDigest(item.ID, input)
	input.Plan = plan
	input.EnabledIDs = nil
	input.HistoricalResources = make(map[model.ResourceID]reconcile.HistoricalResource)
	if historical {
		input.HistoricalResources[item.ID] = reconcile.HistoricalResource{Resource: item, CatalogDigest: digest}
	} else {
		input.EnabledIDs = []model.ResourceID{item.ID}
	}
	return input
}

func historicalDigest(id model.ResourceID, input reconcile.ApplyInput) string {
	if historical, ok := input.HistoricalResources[id]; ok {
		return historical.CatalogDigest
	}
	return ""
}

func conflictPaths(conflicts []managedfiles.Conflict) []string {
	paths := make([]string, len(conflicts))
	for index, conflict := range conflicts {
		paths[index] = conflict.Path
	}
	return paths
}

func renderManagedFilesPrompt(output io.Writer, id model.ResourceID, conflicts []managedfiles.Conflict) error {
	if _, err := fmt.Fprintln(output, "Managed-file conflicts:"); err != nil {
		return err
	}
	for _, conflict := range conflicts {
		action := "backup then accept desired"
		if conflict.Obsolete {
			action = "backup then remove obsolete owned path"
		}
		if _, err := fmt.Fprintf(output, "  %s: %s\n", conflict.Path, action); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(output, "Back up the listed paths and accept Terrapod's signed desired state for %s? [y/N]", id)
	return err
}

type ResolveFunc func(context.Context, model.ResourceID, io.Reader, io.Writer) (Result, error)

// Dispatcher routes managed-files resources to their resolver and leaves all
// other resources to the existing package resolver.
type Dispatcher struct {
	ManagedFiles *ManagedFiles
	Package      ResolveFunc
}

func (d Dispatcher) Resolve(ctx context.Context, id model.ResourceID, input io.Reader, output io.Writer) (Result, error) {
	if d.ManagedFiles != nil {
		result, err := d.ManagedFiles.Resolve(ctx, id, input, output)
		if err == nil {
			return result, nil
		}
		var notManaged *ErrNotManaged
		if !errors.As(err, &notManaged) {
			return result, err
		}
	}
	if d.Package == nil {
		return Result{}, &ErrUnknownResource{ID: id}
	}
	return d.Package(ctx, id, input, output)
}
