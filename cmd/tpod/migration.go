package main

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/juty9026/terrapod/internal/catalog"
	"github.com/juty9026/terrapod/internal/chezmoi"
	"github.com/juty9026/terrapod/internal/cli"
	"github.com/juty9026/terrapod/internal/config"
	"github.com/juty9026/terrapod/internal/migrate"
	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/paths"
	"github.com/juty9026/terrapod/internal/planner"
	"github.com/juty9026/terrapod/internal/reconcile"
	"github.com/juty9026/terrapod/internal/release"
	"github.com/juty9026/terrapod/internal/resource"
	"github.com/juty9026/terrapod/internal/state"
)

type currentMigrationPrepared struct {
	config          model.Config
	conversion      *migrate.ConfigConversion
	configExists    bool
	current         catalog.Verified
	baseline        catalog.Verified
	ownership       migrate.LegacyOwnershipResult
	sourceProof     migrate.LegacySourceProof
	applyInput      reconcile.ApplyInput
	store           *state.Store
	engine          *reconcile.Engine
	staged          release.Staged
	verified        release.VerifiedRelease
	stager          release.Stager
	preflightDir    string
	preflightStore  *state.Store
	preflightEngine *reconcile.Engine
}

type productionLegacyInspector interface {
	LegacyPackages(context.Context, model.Resource, model.Observation) ([]string, error)
}

type productionGit struct{ path string }

func (g productionGit) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, g.path, args...)
	command.Dir = dir
	command.Env = []string{"HOME=" + filepath.Dir(filepath.Dir(dir)), "PATH=/usr/bin:/bin"}
	output, err := command.Output()
	if err != nil {
		return nil, err
	}
	return output, nil
}

func configureCurrentMigration(deps *cli.Dependencies, layout paths.Layout, roots map[string]ed25519.PublicKey, client chezmoi.Client) {
	var prepared *currentMigrationPrepared
	legacySource := legacySourcePath(layout)
	legacyConfig := filepath.Join(filepath.Dir(filepath.Dir(layout.ConfigFile)), "chezmoi", "chezmoi.toml")
	completion := filepath.Join(layout.StateDir, "migration-current.json")
	git := productionGit{path: fixedProviderPaths().git}

	deps.MigrateCurrent = func(ctx context.Context, printPlan func(model.Plan) error) (migrate.CurrentResult, error) {
		return migrate.RunCurrent(ctx, migrate.CurrentDependencies{
			LockDir: layout.StateDir, CompletionPath: completion,
			Prepare: func(ctx context.Context) (migrate.CurrentPrepared, error) {
				stagedVersion := os.Getenv("TPOD_MIGRATION_STAGED_VERSION")
				manifestDigest := os.Getenv("TPOD_MIGRATION_MANIFEST_DIGEST")
				if stagedVersion == "" || manifestDigest == "" {
					return migrate.CurrentPrepared{}, errors.New("migration must start through install.sh --migrate")
				}
				staged, verified, current, stager, err := loadStagedMigrationRelease(layout, roots, stagedVersion, manifestDigest)
				if err != nil {
					return migrate.CurrentPrepared{}, err
				}
				value, err := prepareCurrentMigration(ctx, layout, client, legacyConfig, legacySource, git, staged, verified, current, stager)
				if err != nil {
					return migrate.CurrentPrepared{}, err
				}
				prepared = value
				return migrate.CurrentPrepared{
					Plan: value.ownership.Plan, ApplyInput: value.applyInput,
					Binding: migrate.CurrentBinding{
						Release: value.current.Catalog.Release, ManifestDigest: manifestDigest, CatalogDigest: value.current.Digest,
					},
				}, nil
			},
			Preflight: func(ctx context.Context, _ migrate.CurrentPrepared, _ *state.Lock) error {
				if prepared == nil {
					return errors.New("migration preparation is missing")
				}
				if err := preflightLegacyWarnings(layout.StateDir, prepared.ownership.ArchiveMarkers); err != nil {
					return err
				}
				return preflightCurrentMigration(ctx, prepared)
			},
			CommitConfig: func(_ context.Context, _ migrate.CurrentPrepared) error {
				if prepared.conversion == nil {
					return nil
				}
				if prepared.configExists {
					return migrate.ApplyConfigConversionExisting(legacyConfig, layout.ConfigFile, *prepared.conversion, filepath.Join(layout.StateDir, "recovery", "legacy-config"))
				}
				return migrate.ApplyConfigConversion(legacyConfig, layout.ConfigFile, *prepared.conversion, filepath.Join(layout.StateDir, "recovery", "legacy-config"))
			},
			Activate: func(ctx context.Context, _ migrate.CurrentPrepared) error {
				launchers := [2]string{filepath.Join(layout.HomeDir, ".local", "bin", "tpod"), filepath.Join(layout.HomeDir, ".local", "bin", "terrapod")}
				if _, err := prepared.stager.RepairAndActivate(ctx, prepared.verified, release.Platform{OS: runtime.GOOS, Arch: runtime.GOARCH}, launchers); err != nil {
					return err
				}
				active, err := productionActiveCatalog(layout, roots)
				if err != nil {
					return err
				}
				if active.Digest != prepared.current.Digest {
					return errors.New("active signed catalog changed after migration preflight")
				}
				return nil
			},
			Import: func(_ context.Context, _ migrate.CurrentPrepared) error {
				store, engine, err := currentMigrationEngine(layout, client)
				if err != nil {
					return err
				}
				prepared.store = store
				prepared.engine = engine
				if err := persistLegacyOwnership(prepared.store, prepared.ownership.Receipts); err != nil {
					return err
				}
				return archiveLegacyWarnings(layout.StateDir, prepared.ownership.ArchiveMarkers)
			},
			Reconcile: func(ctx context.Context, _ migrate.CurrentPrepared, lock *state.Lock) (reconcile.Summary, error) {
				return prepared.engine.ApplyInputHeld(ctx, prepared.applyInput, lock)
			},
			Resume: func(ctx context.Context, input reconcile.ApplyInput, binding migrate.CurrentBinding, lock *state.Lock) (reconcile.Summary, error) {
				return resumeCurrentMigration(ctx, layout, roots, client, input, binding, lock)
			},
			FinalizeSource: func(ctx context.Context, input reconcile.ApplyInput, binding migrate.CurrentBinding, lock *state.Lock) error {
				if _, err := os.Lstat(legacySource); errors.Is(err, os.ErrNotExist) {
					return nil
				}
				proof := migrate.LegacySourceProof{}
				if prepared != nil {
					proof = prepared.sourceProof
				}
				if proof.Path == "" {
					var err error
					proof, err = migrate.ValidateLegacySource(ctx, legacySource, git)
					if err != nil {
						return err
					}
				}
				return migrate.RemoveLegacySource(ctx, proof, git, func(context.Context) error {
					activeBinding, err := currentMigrationBinding(layout, roots)
					if err != nil {
						return err
					}
					if activeBinding != binding {
						return errors.New("active signed release changed before source removal")
					}
					_, engine, err := currentMigrationEngine(layout, client)
					if err != nil {
						return err
					}
					summary, err := engine.PreflightInputHeld(ctx, input, lock)
					if err != nil {
						return err
					}
					if len(summary.Unavailable) != 0 {
						return errors.New("managed resources are not ready for legacy source removal")
					}
					return nil
				})
			},
		}, printPlan)
	}
}

func prepareCurrentMigration(ctx context.Context, layout paths.Layout, client chezmoi.Client, legacyConfig, legacySource string, git migrate.GitRunner, staged release.Staged, verified release.VerifiedRelease, current catalog.Verified, stager release.Stager) (*currentMigrationPrepared, error) {
	baselinePath := filepath.Join(staged.Path, "source", "catalog", "v1", "legacy-current.json")
	baseline, warningCategories, err := migrate.LoadLegacyBaseline(baselinePath)
	if err != nil {
		return nil, fmt.Errorf("load signed legacy baseline: %w", err)
	}
	contents, err := os.ReadFile(legacyConfig)
	if err != nil {
		return nil, fmt.Errorf("read legacy config: %w", err)
	}
	configExists := false
	var converted migrate.ConfigConversion
	if _, statErr := os.Lstat(layout.ConfigFile); statErr == nil {
		loaded, loadErr := config.Load(layout.ConfigFile)
		if loadErr != nil {
			return nil, fmt.Errorf("load existing independent config: %w", loadErr)
		}
		converted, err = migrate.ConvertLegacyConfigForExisting(contents, current.Catalog.Config, loaded)
		if err != nil {
			return nil, err
		}
		configExists = true
	} else if errors.Is(statErr, os.ErrNotExist) {
		converted, err = migrate.ConvertLegacyConfig(contents, current.Catalog.Config)
		if err != nil {
			return nil, err
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect independent config: %w", statErr)
	}
	cfg := converted.Terrapod
	conversion := &converted
	profile, err := migrationProfile(cfg)
	if err != nil {
		return nil, err
	}
	proof, err := migrate.ValidateLegacySource(ctx, legacySource, git)
	if err != nil {
		return nil, err
	}
	temporary, err := os.MkdirTemp("", "terrapod-migration-preflight-")
	if err != nil {
		return nil, err
	}
	keepTemporary := false
	defer func() {
		if !keepTemporary {
			_ = os.RemoveAll(temporary)
		}
	}()
	store, err := state.Open(filepath.Join(temporary, "state"))
	if err != nil {
		return nil, err
	}
	configuredPlanner, err := productionPlanner(layout, store, client)
	if err != nil {
		return nil, err
	}
	registry := configuredPlanner.Registry()
	desired := migrationDesired(current.Catalog, cfg, profile)
	actual, err := inspectLegacyArtifacts(ctx, baseline.Catalog.Resources, registry)
	if err != nil {
		return nil, err
	}
	warnings := existingWarningMarkers(layout.StateDir, warningCategories)
	ownership, err := migrate.PlanLegacyOwnership(ctx, migrate.LegacyOwnershipInput{
		Baseline: baseline, Current: current, Registry: registry, Desired: desired, Actual: actual, WarningMarkers: warnings,
	})
	if err != nil {
		return nil, err
	}
	enabled, historical := migrationApplyResources(current.Catalog.Resources, baseline.Catalog.Resources, desired, ownership.Receipts, baseline.Digest)
	applyInput := reconcile.ApplyInput{
		Plan: ownership.Plan, CurrentResources: append([]model.Resource(nil), current.Catalog.Resources...),
		EnabledIDs: enabled, HistoricalResources: historical, CatalogDigest: current.Digest, Profile: profile,
	}
	engine := &reconcile.Engine{
		Registry: registry, State: store, LockDir: filepath.Join(temporary, "state"), Privilege: sudoPrivilege{},
		EffectiveUID: os.Geteuid,
	}
	keepTemporary = true
	return &currentMigrationPrepared{
		config: cfg, conversion: conversion, configExists: configExists, current: current, baseline: baseline, ownership: ownership,
		sourceProof: proof, applyInput: applyInput, staged: staged, verified: verified, stager: stager,
		preflightDir: temporary, preflightStore: store, preflightEngine: engine,
	}, nil
}

func loadStagedMigrationRelease(layout paths.Layout, roots map[string]ed25519.PublicKey, version, expectedManifestDigest string) (release.Staged, release.VerifiedRelease, catalog.Verified, release.Stager, error) {
	verifier := release.Verifier{CompiledKeys: roots}
	stager := release.Stager{
		ReleaseDir: layout.ReleaseDir, ActiveRelease: layout.ActiveRelease, Verifier: verifier,
		ExpectedPlatform: release.Platform{OS: runtime.GOOS, Arch: runtime.GOARCH},
	}
	staged, verified, err := stager.Load(version)
	if err != nil {
		return release.Staged{}, release.VerifiedRelease{}, catalog.Verified{}, stager, err
	}
	manifestDigest, err := verified.Manifest.Digest()
	if err != nil {
		return release.Staged{}, release.VerifiedRelease{}, catalog.Verified{}, stager, err
	}
	if manifestDigest != expectedManifestDigest {
		return release.Staged{}, release.VerifiedRelease{}, catalog.Verified{}, stager, errors.New("staged manifest digest differs from installer verification")
	}
	asset, err := verified.Manifest.CatalogAsset()
	if err != nil {
		return release.Staged{}, release.VerifiedRelease{}, catalog.Verified{}, stager, err
	}
	current, err := catalog.LoadReleaseBound(filepath.Join(staged.Path, "catalog", "resources.json"), asset.SHA256)
	if err != nil {
		return release.Staged{}, release.VerifiedRelease{}, catalog.Verified{}, stager, err
	}
	if err := validateActiveCatalogRelease(current, version); err != nil {
		return release.Staged{}, release.VerifiedRelease{}, catalog.Verified{}, stager, err
	}
	return staged, verified, current, stager, nil
}

func inspectLegacyArtifacts(ctx context.Context, resources []model.Resource, registry resource.Registry) (map[model.ResourceID]migrate.LegacyArtifact, error) {
	actual := make(map[model.ResourceID]migrate.LegacyArtifact)
	for _, item := range resources {
		adapter, ok := registry.Lookup(item.Type, item.Provider)
		if !ok {
			continue
		}
		observed, err := adapter.Inspect(ctx, item)
		if err != nil {
			return nil, fmt.Errorf("inspect legacy resource %q: %w", item.ID, err)
		}
		var packages []string
		if inspector, ok := adapter.(productionLegacyInspector); ok {
			packages, err = inspector.LegacyPackages(ctx, item, observed)
			if err != nil {
				return nil, fmt.Errorf("inspect legacy package source %q: %w", item.ID, err)
			}
		}
		present := observed.Present || len(packages) != 0
		if !present {
			continue
		}
		modified := requiresHealthyMigrationAdoption(item.Type) && observed.Present && !observed.Healthy
		actual[item.ID] = migrate.LegacyArtifact{
			Observation: observed, LegacyPackages: packages, Modified: modified,
			PriorUnknown: item.Type == model.ResourceIntegration && observed.Present,
		}
	}
	return actual, nil
}

func preflightCurrentMigration(ctx context.Context, prepared *currentMigrationPrepared) error {
	defer func() {
		_ = os.RemoveAll(prepared.preflightDir)
		prepared.preflightDir = ""
		prepared.preflightStore = nil
		prepared.preflightEngine = nil
	}()
	if prepared.preflightStore == nil || prepared.preflightEngine == nil {
		return errors.New("migration preflight state is missing")
	}
	if err := persistLegacyOwnership(prepared.preflightStore, prepared.ownership.Receipts); err != nil {
		return err
	}
	_, err := prepared.preflightEngine.PreflightInput(ctx, prepared.applyInput)
	return err
}

func currentMigrationEngine(layout paths.Layout, client chezmoi.Client) (*state.Store, *reconcile.Engine, error) {
	store, err := state.Open(layout.StateDir)
	if err != nil {
		return nil, nil, err
	}
	configuredPlanner, err := productionPlanner(layout, store, client)
	if err != nil {
		return nil, nil, err
	}
	return store, &reconcile.Engine{
		Registry: configuredPlanner.Registry(), State: store, LockDir: layout.StateDir,
		Privilege: sudoPrivilege{}, EffectiveUID: os.Geteuid,
	}, nil
}

func resumeCurrentMigration(ctx context.Context, layout paths.Layout, roots map[string]ed25519.PublicKey, client chezmoi.Client, input reconcile.ApplyInput, binding migrate.CurrentBinding, lock *state.Lock) (reconcile.Summary, error) {
	activeBinding, err := currentMigrationBinding(layout, roots)
	if err != nil {
		return reconcile.Summary{}, err
	}
	if activeBinding != binding {
		return reconcile.Summary{}, errors.New("active signed release differs from persisted migration binding")
	}
	_, engine, err := currentMigrationEngine(layout, client)
	if err != nil {
		return reconcile.Summary{}, err
	}
	return engine.ApplyInputHeld(ctx, input, lock)
}

func currentMigrationBinding(layout paths.Layout, roots map[string]ed25519.PublicKey) (migrate.CurrentBinding, error) {
	current, err := productionActiveCatalog(layout, roots)
	if err != nil {
		return migrate.CurrentBinding{}, err
	}
	stager := release.Stager{
		ReleaseDir: layout.ReleaseDir, ActiveRelease: layout.ActiveRelease,
		Verifier: release.Verifier{CompiledKeys: roots}, ExpectedPlatform: release.Platform{OS: runtime.GOOS, Arch: runtime.GOARCH},
	}
	version, err := stager.CurrentVersion()
	if err != nil {
		return migrate.CurrentBinding{}, err
	}
	_, verified, err := stager.Load(version)
	if err != nil {
		return migrate.CurrentBinding{}, err
	}
	manifestDigest, err := verified.Manifest.Digest()
	if err != nil {
		return migrate.CurrentBinding{}, err
	}
	return migrate.CurrentBinding{Release: current.Catalog.Release, ManifestDigest: manifestDigest, CatalogDigest: current.Digest}, nil
}

func persistLegacyOwnership(store *state.Store, receipts map[model.ResourceID]model.Ownership) error {
	ids := make([]model.ResourceID, 0, len(receipts))
	for id := range receipts {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		if err := store.PutOwnership(receipts[id]); err != nil {
			return err
		}
	}
	return nil
}

func migrationProfile(cfg model.Config) (model.Profile, error) {
	value, ok := cfg.Terrapod["profile"].(string)
	profile := model.Profile(value)
	if !ok || !profile.Supported() {
		return "", fmt.Errorf("migration config profile %q is unsupported", value)
	}
	return profile, nil
}

func migrationDesired(catalog model.Catalog, cfg model.Config, profile model.Profile) map[model.ResourceID]bool {
	desired := make(map[model.ResourceID]bool)
	for _, item := range catalog.Resources {
		if !migrationProfileMatches(item, profile) || !migrationConfigEnabled(item, cfg) {
			continue
		}
		desired[item.ID] = true
	}
	return desired
}

func migrationProfileMatches(item model.Resource, profile model.Profile) bool {
	if len(item.Profiles) == 0 {
		return true
	}
	for _, allowed := range item.Profiles {
		if allowed == profile {
			return true
		}
	}
	return false
}

func migrationConfigEnabled(item model.Resource, cfg model.Config) bool {
	if field, ok := item.Metadata[planner.EnabledByConfigMetadataKey]; ok {
		enabled, _ := cfg.Terrapod[field].(bool)
		return enabled
	}
	hasAny, enabledAny := false, false
	for key := range item.Metadata {
		if strings.HasPrefix(key, planner.EnabledByAnyConfigMetadataPrefix) {
			hasAny = true
			field := strings.TrimPrefix(key, planner.EnabledByAnyConfigMetadataPrefix)
			enabled, _ := cfg.Terrapod[field].(bool)
			enabledAny = enabledAny || enabled
		}
	}
	return !hasAny || enabledAny
}

func migrationApplyResources(current, baseline []model.Resource, desired map[model.ResourceID]bool, receipts map[model.ResourceID]model.Ownership, baselineDigest string) ([]model.ResourceID, map[model.ResourceID]reconcile.HistoricalResource) {
	var enabled []model.ResourceID
	for _, item := range current {
		if desired[item.ID] {
			enabled = append(enabled, item.ID)
		}
	}
	historical := make(map[model.ResourceID]reconcile.HistoricalResource)
	for _, item := range baseline {
		if _, owned := receipts[item.ID]; owned && !desired[item.ID] {
			historical[item.ID] = reconcile.HistoricalResource{Resource: item, CatalogDigest: baselineDigest}
		}
	}
	return enabled, historical
}

func existingWarningMarkers(stateDir string, categories []string) map[string]string {
	root := filepath.Join(stateDir, "install-warnings")
	result := make(map[string]string)
	for _, category := range categories {
		path := filepath.Join(root, category)
		if info, err := os.Lstat(path); err == nil && info.Mode().IsRegular() {
			result[category] = path
		}
	}
	return result
}

func archiveLegacyWarnings(stateDir string, paths []string) error {
	destination := filepath.Join(stateDir, "recovery", "install-warnings")
	if len(paths) != 0 {
		if err := requireWarningArchiveParents(stateDir); err != nil {
			return err
		}
		if err := os.MkdirAll(destination, 0o700); err != nil {
			return err
		}
		if err := requireRealMigrationDirectory(destination); err != nil {
			return err
		}
	}
	for _, source := range paths {
		info, err := os.Lstat(source)
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("legacy warning marker is unsafe: %s", source)
		}
		contents, err := os.ReadFile(source)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, filepath.Base(source))
		if err := writeExactArchive(target, contents); err != nil {
			return err
		}
		if err := os.Remove(source); err != nil {
			return err
		}
	}
	return nil
}

func preflightLegacyWarnings(stateDir string, paths []string) error {
	destination := filepath.Join(stateDir, "recovery", "install-warnings")
	if len(paths) != 0 {
		if err := requireWarningArchiveParents(stateDir); err != nil {
			return err
		}
	}
	for _, source := range paths {
		info, err := os.Lstat(source)
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("legacy warning marker is unsafe: %s", source)
		}
		contents, err := os.ReadFile(source)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, filepath.Base(source))
		if existing, err := os.ReadFile(target); err == nil {
			if string(existing) != string(contents) {
				return fmt.Errorf("legacy warning archive conflicts at %s", target)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func requireWarningArchiveParents(stateDir string) error {
	if err := requireRealMigrationDirectory(stateDir); err != nil {
		return err
	}
	recovery := filepath.Join(stateDir, "recovery")
	if _, err := os.Lstat(recovery); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if err := requireRealMigrationDirectory(recovery); err != nil {
		return err
	}
	destination := filepath.Join(recovery, "install-warnings")
	if _, err := os.Lstat(destination); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	return requireRealMigrationDirectory(destination)
}

func requireRealMigrationDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("migration archive path is unsafe: %s", path)
	}
	return nil
}

func writeExactArchive(path string, contents []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		existing, readErr := os.ReadFile(path)
		if readErr != nil || string(existing) != string(contents) {
			return fmt.Errorf("legacy warning archive conflicts at %s", path)
		}
		return nil
	}
	if err != nil {
		return err
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(contents); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	keep = true
	return nil
}

func requiresHealthyMigrationAdoption(resourceType model.ResourceType) bool {
	switch resourceType {
	case model.ResourceManagedFiles, model.ResourceGitCheckout, model.ResourceArchive, model.ResourceIntegration:
		return true
	default:
		return false
	}
}

func legacySourcePath(layout paths.Layout) string {
	return filepath.Join(filepath.Dir(layout.DataDir), "chezmoi")
}
