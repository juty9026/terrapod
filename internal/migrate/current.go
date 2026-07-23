package migrate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/reconcile"
	"github.com/juty9026/terrapod/internal/state"
)

type CurrentResult struct {
	Summary         reconcile.Summary
	AlreadyComplete bool
}

type CurrentPrepared struct {
	Plan model.Plan
}

type CurrentDependencies struct {
	LockDir        string
	CompletionPath string
	Prepare        func(context.Context) (CurrentPrepared, error)
	Preflight      func(context.Context, CurrentPrepared, *state.Lock) error
	CommitConfig   func(context.Context, CurrentPrepared) error
	Activate       func(context.Context, CurrentPrepared) error
	Import         func(context.Context, CurrentPrepared) error
	Reconcile      func(context.Context, CurrentPrepared, *state.Lock) (reconcile.Summary, error)
	Resume         func(context.Context, model.Plan, *state.Lock) (reconcile.Summary, error)
	FinalizeSource func(context.Context) error
}

type migrationMarker struct {
	Version int        `json:"version"`
	Phase   string     `json:"phase"`
	Plan    model.Plan `json:"plan,omitempty"`
}

func RunCurrent(ctx context.Context, deps CurrentDependencies, printPlan func(model.Plan) error) (result CurrentResult, retErr error) {
	if err := validateCurrentDependencies(deps, printPlan); err != nil {
		return result, err
	}
	lock, err := state.Acquire(deps.LockDir, "tpod migrate-current")
	if err != nil {
		return result, fmt.Errorf("migration: acquire state lock: %w", err)
	}
	defer func() { retErr = errors.Join(retErr, lock.Release()) }()

	marker, err := readMigrationMarker(deps.CompletionPath)
	if err != nil {
		return result, err
	}
	if marker.Phase == "complete" {
		result.AlreadyComplete = true
		return result, nil
	}
	if marker.Phase == "reconciled" {
		if err := deps.FinalizeSource(ctx); err != nil {
			return result, fmt.Errorf("migration: finalize legacy source: %w", err)
		}
		if err := writeMigrationMarker(deps.CompletionPath, migrationMarker{Version: 1, Phase: "complete"}); err != nil {
			return result, err
		}
		return result, nil
	}
	if marker.Phase == "applying" {
		result.Summary, err = deps.Resume(ctx, marker.Plan, lock)
		if err != nil {
			return result, fmt.Errorf("migration: resume imported resources: %w", err)
		}
		if len(result.Summary.Unavailable) != 0 {
			return result, errors.New("migration reconciliation has unavailable resources")
		}
		if err := writeMigrationMarker(deps.CompletionPath, migrationMarker{Version: 1, Phase: "reconciled"}); err != nil {
			return result, err
		}
		if err := deps.FinalizeSource(ctx); err != nil {
			return result, fmt.Errorf("migration: finalize legacy source: %w", err)
		}
		if err := writeMigrationMarker(deps.CompletionPath, migrationMarker{Version: 1, Phase: "complete"}); err != nil {
			return result, err
		}
		return result, nil
	}

	prepared, err := deps.Prepare(ctx)
	if err != nil {
		return result, fmt.Errorf("migration preflight: %w", err)
	}
	if err := deps.Preflight(ctx, prepared, lock); err != nil {
		return result, fmt.Errorf("migration preflight: %w", err)
	}
	if err := printPlan(prepared.Plan); err != nil {
		return result, fmt.Errorf("migration: print complete plan: %w", err)
	}
	if len(prepared.Plan.Unavailable) != 0 {
		result.Summary.Unavailable = cloneUnavailable(prepared.Plan.Unavailable)
		return result, errors.New("migration preflight has unavailable resources")
	}
	if err := deps.CommitConfig(ctx, prepared); err != nil {
		return result, fmt.Errorf("migration: commit config conversion: %w", err)
	}
	if err := deps.Activate(ctx, prepared); err != nil {
		return result, fmt.Errorf("migration: activate signed manager: %w", err)
	}
	if err := deps.Import(ctx, prepared); err != nil {
		return result, fmt.Errorf("migration: import ownership: %w", err)
	}
	if err := writeMigrationMarker(deps.CompletionPath, migrationMarker{Version: 1, Phase: "applying", Plan: prepared.Plan}); err != nil {
		return result, err
	}
	result.Summary, err = deps.Reconcile(ctx, prepared, lock)
	if err != nil {
		return result, fmt.Errorf("migration: reconcile imported resources: %w", err)
	}
	if len(result.Summary.Unavailable) != 0 {
		return result, errors.New("migration reconciliation has unavailable resources")
	}
	if err := writeMigrationMarker(deps.CompletionPath, migrationMarker{Version: 1, Phase: "reconciled"}); err != nil {
		return result, err
	}
	if err := deps.FinalizeSource(ctx); err != nil {
		return result, fmt.Errorf("migration: finalize legacy source: %w", err)
	}
	if err := writeMigrationMarker(deps.CompletionPath, migrationMarker{Version: 1, Phase: "complete"}); err != nil {
		return result, err
	}
	return result, nil
}

func validateCurrentDependencies(deps CurrentDependencies, printPlan func(model.Plan) error) error {
	if deps.LockDir == "" || deps.CompletionPath == "" || deps.Prepare == nil || deps.Preflight == nil || deps.CommitConfig == nil ||
		deps.Activate == nil || deps.Import == nil || deps.Reconcile == nil || deps.Resume == nil || deps.FinalizeSource == nil || printPlan == nil {
		return errors.New("migration: dependencies are incomplete")
	}
	return nil
}

func readMigrationMarker(path string) (migrationMarker, error) {
	info, statErr := os.Lstat(path)
	if errors.Is(statErr, os.ErrNotExist) {
		return migrationMarker{}, nil
	}
	if statErr != nil {
		return migrationMarker{}, fmt.Errorf("migration: inspect completion marker: %w", statErr)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return migrationMarker{}, errors.New("migration: completion marker is unsafe")
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return migrationMarker{}, fmt.Errorf("migration: read completion marker: %w", err)
	}
	var marker migrationMarker
	if err := json.Unmarshal(contents, &marker); err != nil || marker.Version != 1 ||
		(marker.Phase != "applying" && marker.Phase != "reconciled" && marker.Phase != "complete") ||
		(marker.Phase == "applying" && marker.Plan.ID == "") {
		return migrationMarker{}, errors.New("migration: invalid completion marker")
	}
	return marker, nil
}

func writeMigrationMarker(path string, marker migrationMarker) error {
	if marker.Version != 1 || (marker.Phase != "applying" && marker.Phase != "reconciled" && marker.Phase != "complete") ||
		(marker.Phase == "applying" && marker.Plan.ID == "") {
		return errors.New("migration: invalid completion phase")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("migration: create marker directory: %w", err)
	}
	contents, _ := json.Marshal(marker)
	contents = append(contents, '\n')
	temporary, err := os.CreateTemp(filepath.Dir(path), ".migration-*")
	if err != nil {
		return fmt.Errorf("migration: create marker: %w", err)
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("migration: publish completion marker: %w", err)
	}
	return nil
}

func cloneUnavailable(input map[model.ResourceID]string) map[model.ResourceID]string {
	output := make(map[model.ResourceID]string, len(input))
	for id, reason := range input {
		output[id] = reason
	}
	return output
}
