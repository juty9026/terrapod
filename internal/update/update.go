package update

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/juty9026/terrapod/internal/catalog"
	"github.com/juty9026/terrapod/internal/config"
	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/planner"
	"github.com/juty9026/terrapod/internal/provider"
	"github.com/juty9026/terrapod/internal/reconcile"
	"github.com/juty9026/terrapod/internal/release"
	"github.com/juty9026/terrapod/internal/state"
)

type ReleaseSource interface {
	LatestStable(context.Context) (release.VerifiedRelease, error)
}

type ReleaseStager interface {
	Stage(context.Context, release.VerifiedRelease, release.Platform) (release.Staged, error)
	Activate(string) error
}

type Inputs struct {
	Catalog    catalog.Verified
	Config     model.Config
	Historical map[string]model.Catalog
	Profile    model.Profile
}

type Dependencies struct {
	Releases ReleaseSource
	Stager   ReleaseStager
	Platform release.Platform

	Refreshers []provider.MetadataRefresher
	Planner    *planner.Planner
	Engine     *reconcile.Engine
	State      *state.Store
	LockDir    string

	LoadStaged     func(context.Context, release.Staged) (Inputs, error)
	VerifyActive   func(context.Context, string) (release.Staged, release.VerifiedRelease, Inputs, error)
	CurrentVersion func() (string, error)
	SelfCheck      func(context.Context, string, string, string) error
	PrintPlan      func(model.Plan) error
	WriteConfig    func(model.Config) error
	BuildTrusted   func(release.VerifiedRelease) (release.PersistedTrust, error)
	ReleaseDigest  func(release.VerifiedRelease) (string, error)
	PersistTrusted func(release.PersistedTrust) error
	LoadTrusted    func() (release.PersistedTrust, error)
	Exec           func(string, []string, []string) error
	Environment    []string
	HandoffToken   func() string
}

type Result struct {
	Summary   reconcile.Summary
	JournalID string
	Handoff   bool
}

func Run(ctx context.Context, deps Dependencies) (result Result, retErr error) {
	if err := validate(deps); err != nil {
		return result, err
	}
	lock, err := state.Acquire(deps.LockDir, "tpod update")
	if err != nil {
		return result, fmt.Errorf("update: acquire state lock: %w", err)
	}
	defer func() { retErr = errors.Join(retErr, lock.Release()) }()

	verified, err := deps.Releases.LatestStable(ctx)
	if err != nil {
		return result, fmt.Errorf("update: fetch latest signed release: %w", err)
	}
	current, err := deps.CurrentVersion()
	if err != nil {
		return result, fmt.Errorf("update: inspect active release: %w", err)
	}
	if current != "" && compareVersion(verified.Manifest.Version, current) < 0 {
		return result, fmt.Errorf("update: refusing automatic downgrade from %s to %s", current, verified.Manifest.Version)
	}
	staged, err := deps.Stager.Stage(ctx, verified, deps.Platform)
	if err != nil {
		return result, fmt.Errorf("update: stage release: %w", err)
	}
	if staged.Version != verified.Manifest.Version {
		return result, errors.New("update: staged version differs from signed release")
	}
	releaseDigest, err := deps.ReleaseDigest(verified)
	if err != nil {
		return result, fmt.Errorf("update: bind verified manifest digest: %w", err)
	}
	if err := deps.SelfCheck(ctx, filepath.Join(staged.Path, "bin", "tpod"), staged.Path, releaseDigest); err != nil {
		return result, fmt.Errorf("update: staged binary compatibility check: %w", err)
	}
	inputs, err := deps.LoadStaged(ctx, staged)
	if err != nil {
		return result, fmt.Errorf("update: load staged signed inputs: %w", err)
	}
	if inputs.Catalog.Catalog.Release != staged.Version {
		return result, errors.New("update: catalog release differs from staged release")
	}
	if err := refreshEnabled(ctx, deps.Refreshers, enabledProviders(inputs)); err != nil {
		return result, err
	}
	plan, applyInput, err := build(ctx, deps, inputs)
	if err != nil {
		return result, err
	}
	if _, err := deps.Engine.PreflightInputHeld(ctx, applyInput, lock); err != nil {
		return result, fmt.Errorf("update: privilege preflight: %w", err)
	}
	if err := deps.PrintPlan(plan); err != nil {
		return result, fmt.Errorf("update: print final plan: %w", err)
	}
	journal, _, err := deps.State.BeginOrResume(plan)
	if err != nil {
		return result, fmt.Errorf("update: persist pre-activation journal: %w", err)
	}
	trust, err := deps.BuildTrusted(verified)
	if err != nil {
		return result, fmt.Errorf("update: derive signed trust additions: %w", err)
	}
	record := state.UpdateRecord{JournalID: journal.ID, PlanID: plan.ID, Version: staged.Version, CatalogDigest: inputs.Catalog.Digest, ReleaseDigest: releaseDigest, TrustedKeys: encodeKeys(trust.Keys), TrustProvenance: cloneStrings(trust.Provenance), TrustProofDigest: trust.ProofDigest}
	if err := deps.State.PutUpdate(record); err != nil {
		return result, fmt.Errorf("update: persist update record: %w", err)
	}
	result.JournalID = journal.ID

	if current == staged.Version {
		if err := deps.State.MarkUpdateActivated(journal.ID); err != nil {
			return result, fmt.Errorf("update: record same-release activation: %w", err)
		}
		if err := deps.PersistTrusted(trust); err != nil {
			return result, fmt.Errorf("update: persist trusted keys for active release: %w", err)
		}
		return continueHeld(ctx, deps, lock, journal.ID, staged, verified, inputs)
	}
	if err := deps.Stager.Activate(staged.Version); err != nil {
		return result, fmt.Errorf("update: activate release: %w", err)
	}
	if err := deps.State.MarkUpdateActivated(journal.ID); err != nil {
		return result, fmt.Errorf("update: record activation: %w", err)
	}
	if err := deps.PersistTrusted(trust); err != nil {
		return result, fmt.Errorf("update: persist trusted keys after activation: %w", err)
	}
	result.Handoff = true
	token, err := lock.HandoffToken(deps.LockDir)
	if err != nil {
		return result, fmt.Errorf("update: prepare lock handoff: %w", err)
	}
	environment := append(controlledEnvironment(deps.Environment), "TPOD_UPDATE_LOCK_NONCE="+token)
	if err := deps.Exec(filepath.Join(staged.Path, "bin", "tpod"), []string{"internal-continue-update", "--journal", journal.ID}, environment); err != nil {
		return result, fmt.Errorf("update: handoff to active binary: %w", err)
	}
	return result, nil
}

func Continue(ctx context.Context, journalID string, deps Dependencies) (result Result, retErr error) {
	if err := validate(deps); err != nil {
		return result, err
	}
	var lock *state.Lock
	var err error
	if deps.HandoffToken != nil && deps.HandoffToken() != "" {
		lock, err = state.ResumeHandoff(deps.LockDir, deps.HandoffToken())
	} else {
		lock, err = state.Acquire(deps.LockDir, "tpod internal-continue-update")
	}
	if err != nil {
		return result, fmt.Errorf("update: acquire continuation lock: %w", err)
	}
	defer func() { retErr = errors.Join(retErr, lock.Release()) }()
	record, err := deps.State.Update(journalID)
	if err != nil || !record.Activated {
		return result, fmt.Errorf("update: load activated update record: %w", err)
	}
	trust, err := deps.LoadTrusted()
	if err != nil || !reflect.DeepEqual(encodeKeys(trust.Keys), record.TrustedKeys) || !reflect.DeepEqual(trust.Provenance, record.TrustProvenance) || trust.ProofDigest != record.TrustProofDigest {
		return result, fmt.Errorf("update: persisted trusted keys differ from update record: %w", err)
	}
	staged, verified, inputs, err := deps.VerifyActive(ctx, record.Version)
	if err != nil {
		return result, fmt.Errorf("update: re-verify active release: %w", err)
	}
	return continueHeld(ctx, deps, lock, journalID, staged, verified, inputs)
}

func continueHeld(ctx context.Context, deps Dependencies, lock *state.Lock, journalID string, staged release.Staged, verified release.VerifiedRelease, inputs Inputs) (Result, error) {
	record, err := deps.State.Update(journalID)
	if err != nil {
		return Result{}, err
	}
	journal, err := deps.State.Journal(journalID)
	if err != nil {
		return Result{}, err
	}
	digest, digestErr := planner.Digest(journal.Plan)
	releaseDigest, releaseDigestErr := deps.ReleaseDigest(verified)
	if digestErr != nil || releaseDigestErr != nil || releaseDigest != record.ReleaseDigest || digest != record.PlanID || journal.Status != "active" || journal.Plan.ID != record.PlanID || record.Version != staged.Version || verified.Manifest.Version != record.Version || inputs.Catalog.Digest != record.CatalogDigest || inputs.Catalog.Catalog.Release != record.Version {
		return Result{}, errors.New("update: active release, catalog, plan, and journal binding differs")
	}
	normalized, _, err := config.Normalize(inputs.Config, inputs.Catalog.Catalog.Config)
	if err != nil {
		return Result{}, fmt.Errorf("update: normalize config with active schema: %w", err)
	}
	inputs.Config = normalized
	replanned, applyInput, err := build(ctx, deps, inputs)
	if err != nil {
		return Result{}, err
	}
	if err := requireAuthorizedReplan(journal.Plan, replanned); err != nil {
		return Result{}, err
	}
	if err := rejectNewUnavailable(journal.Plan, replanned); err != nil {
		return Result{}, err
	}
	required := make(map[string]bool, len(replanned.Operations))
	for _, operation := range replanned.Operations {
		required[operation.ID] = true
	}
	if deps.WriteConfig != nil {
		if err := deps.WriteConfig(normalized); err != nil {
			return Result{}, fmt.Errorf("update: write normalized config: %w", err)
		}
	}
	applyInput.Plan = journal.Plan
	applyInput.RequiredOperationIDs = required
	summary, err := deps.Engine.ApplyInputHeld(ctx, applyInput, lock)
	return Result{Summary: summary, JournalID: journalID}, err
}

func rejectNewUnavailable(original, actual model.Plan) error {
	for id, reason := range actual.Unavailable {
		if previous, existed := original.Unavailable[id]; !existed || previous != reason {
			return fmt.Errorf("update: actual-state replan made resource %q unavailable: %s", id, reason)
		}
	}
	return nil
}

func build(ctx context.Context, deps Dependencies, inputs Inputs) (model.Plan, reconcile.ApplyInput, error) {
	snapshot, err := deps.State.Snapshot()
	if err != nil {
		return model.Plan{}, reconcile.ApplyInput{}, fmt.Errorf("update: read actual state: %w", err)
	}
	plan, err := deps.Planner.Build(ctx, planner.Input{Catalog: inputs.Catalog.Catalog, CatalogDigest: inputs.Catalog.Digest, Historical: inputs.Historical, Config: inputs.Config, Profile: inputs.Profile, Snapshot: snapshot, Upgrade: true})
	if err != nil {
		return model.Plan{}, reconcile.ApplyInput{}, fmt.Errorf("update: build final plan: %w", err)
	}
	enabled := enabledResources(inputs)
	ids := make([]model.ResourceID, len(enabled))
	set := make(map[model.ResourceID]struct{}, len(enabled))
	for i, item := range enabled {
		ids[i], set[item.ID] = item.ID, struct{}{}
	}
	historical := make(map[model.ResourceID]reconcile.HistoricalResource)
	for id, owned := range snapshot.Ownership {
		if _, ok := set[id]; ok {
			continue
		}
		if old, ok := inputs.Historical[owned.CatalogDigest]; ok {
			for _, item := range old.Resources {
				if item.ID == id {
					historical[id] = reconcile.HistoricalResource{Resource: item, CatalogDigest: owned.CatalogDigest}
					break
				}
			}
		}
	}
	return plan, reconcile.ApplyInput{Plan: plan, CurrentResources: append([]model.Resource(nil), inputs.Catalog.Catalog.Resources...), EnabledIDs: ids, HistoricalResources: historical, CatalogDigest: inputs.Catalog.Digest, Profile: inputs.Profile, ForceUpgrade: true}, nil
}

func enabledResources(inputs Inputs) []model.Resource {
	var result []model.Resource
	for _, item := range inputs.Catalog.Catalog.Resources {
		profile := len(item.Profiles) == 0
		for _, p := range item.Profiles {
			profile = profile || p == inputs.Profile
		}
		if !profile {
			continue
		}
		if field, ok := item.Metadata[planner.EnabledByConfigMetadataKey]; ok {
			enabled, _ := inputs.Config.Terrapod[field].(bool)
			if !enabled {
				continue
			}
		}
		anyGate, enabledAny := false, false
		for key := range item.Metadata {
			if strings.HasPrefix(key, planner.EnabledByAnyConfigMetadataPrefix) {
				anyGate = true
				value, _ := inputs.Config.Terrapod[strings.TrimPrefix(key, planner.EnabledByAnyConfigMetadataPrefix)].(bool)
				enabledAny = enabledAny || value
			}
		}
		if anyGate && !enabledAny {
			continue
		}
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func enabledProviders(inputs Inputs) map[string]struct{} {
	result := map[string]struct{}{}
	for _, item := range enabledResources(inputs) {
		result[item.Provider] = struct{}{}
	}
	return result
}

func refreshEnabled(ctx context.Context, refreshers []provider.MetadataRefresher, enabled map[string]struct{}) error {
	seen := map[string]struct{}{}
	for _, refresher := range refreshers {
		if refresher == nil {
			return errors.New("update: nil metadata refresher")
		}
		name := refresher.Name()
		if _, duplicate := seen[name]; duplicate {
			return fmt.Errorf("update: duplicate metadata refresher %q", name)
		}
		seen[name] = struct{}{}
		if _, ok := enabled[name]; !ok {
			continue
		}
		if err := refresher.RefreshMetadata(ctx); err != nil {
			return fmt.Errorf("update: refresh %s metadata: %w", name, err)
		}
	}
	return nil
}

func requireAuthorizedReplan(original, actual model.Plan) error {
	authorized := make(map[string]model.Operation, len(original.Operations))
	for _, operation := range original.Operations {
		authorized[operation.ID] = operation
	}
	for _, operation := range actual.Operations {
		if expected, ok := authorized[operation.ID]; !ok || !reflect.DeepEqual(expected, operation) {
			return fmt.Errorf("update: actual-state replan introduced unauthorized operation %q", operation.ID)
		}
	}
	return nil
}

func validate(deps Dependencies) error {
	if deps.Releases == nil || deps.Stager == nil || deps.Planner == nil || deps.Engine == nil || deps.State == nil || deps.LockDir == "" || deps.LoadStaged == nil || deps.VerifyActive == nil || deps.CurrentVersion == nil || deps.SelfCheck == nil || deps.PrintPlan == nil || deps.BuildTrusted == nil || deps.ReleaseDigest == nil || deps.PersistTrusted == nil || deps.LoadTrusted == nil || deps.Exec == nil {
		return errors.New("update: incomplete dependencies")
	}
	return nil
}

func encodeKeys(keys map[string]ed25519.PublicKey) map[string]string {
	result := make(map[string]string, len(keys))
	for id, key := range keys {
		result[id] = fmt.Sprintf("%x", []byte(key))
	}
	return result
}

func cloneStrings(values map[string]string) map[string]string {
	result := make(map[string]string, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func controlledEnvironment(input []string) []string {
	allowed := map[string]bool{"HOME": true, "PATH": true, "TMPDIR": true, "XDG_CONFIG_HOME": true, "XDG_STATE_HOME": true, "XDG_DATA_HOME": true, "XDG_CACHE_HOME": true, "LANG": true, "LC_ALL": true}
	var result []string
	for _, value := range input {
		name, _, ok := strings.Cut(value, "=")
		if ok && allowed[name] {
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}

func compareVersion(left, right string) int {
	a, aok := versionTuple(left)
	b, bok := versionTuple(right)
	if !aok || !bok {
		return strings.Compare(left, right)
	}
	for i := range a {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}
func versionTuple(value string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
