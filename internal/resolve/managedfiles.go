package resolve

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"

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
	ResolveConflict(context.Context, model.Resource, string) error
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
	plan := managedResolutionPlan(rebuilt, item, historical)
	resume := snapshot.ActiveJournal != nil && reflect.DeepEqual(snapshot.ActiveJournal.Plan, plan)
	conflicts, err := capability.Conflicts(ctx, item, snapshot.Ownership[id])
	if err != nil {
		return result, fmt.Errorf("resolve: inspect managed-file conflicts: %w", err)
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
		if err := capability.ResolveConflict(ctx, item, journal.ID); err != nil {
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

func managedResolutionPlan(input reconcile.ApplyInput, item model.Resource, historical bool) model.Plan {
	mode, kind, removes := "current", model.OperationVerify, []string(nil)
	if historical {
		mode, kind, removes = "historical", model.OperationPrune, []string{item.Package}
	}
	sum := sha256.Sum256([]byte(string(item.ID) + "\x00" + input.CatalogDigest + "\x00" + mode))
	operation := model.Operation{
		ID:         "resolve-managed-files-" + string(item.ID),
		ResourceID: item.ID,
		Kind:       kind,
		Provider:   item.Provider,
		Package:    item.Package,
		Removes:    removes,
	}
	return model.Plan{
		ID:          fmt.Sprintf("resolve-managed-files-%x", sum[:16]),
		Release:     input.Plan.Release,
		Operations:  []model.Operation{operation},
		Unavailable: map[model.ResourceID]string{},
	}
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
