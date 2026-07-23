package reconcile

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/juty9026/terrapod/internal/legacydecl"
	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/provider"
	"github.com/juty9026/terrapod/internal/resource"
	"github.com/juty9026/terrapod/internal/state"
)

type fixtureAdapter struct {
	present, legacy              bool
	fail                         map[string]bool
	events                       []string
	shared                       *[]string
	simulation                   provider.ChangeSet
	verifyCalls, failVerifyAfter int
	onExecute                    func()
	onSimulate                   func()
	canceled                     []model.Operation
	observationPaths             map[string]string
	boundResources               []model.Resource
}

func TestEnginePersistsArchiveManifestPaths(t *testing.T) {
	store, err := state.Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	engine := Engine{State: store, CatalogDigest: "signed"}
	item := model.Resource{ID: "font.jetendard", Type: model.ResourceArchive, Provider: "jetendard", Package: "jetendard"}
	paths := map[string]string{"/home/me/Library/Fonts/Jetendard-Regular.ttf": "sha256:" + strings.Repeat("a", 64)}
	if err := engine.own(item, model.Observation{Paths: paths}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snapshot.Ownership[item.ID].Paths, paths) {
		t.Fatalf("Paths = %#v, want %#v", snapshot.Ownership[item.ID].Paths, paths)
	}
}

func TestEnginePreservesIntegrationPriorValues(t *testing.T) {
	store, err := state.Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	item := model.Resource{ID: "integration.test", Type: model.ResourceIntegration, Provider: "json-fields", Package: "settings"}
	want := json.RawMessage(`{"exists":true,"value":"private"}`)
	if err := store.PutOwnership(model.Ownership{ResourceID: item.ID, Provider: item.Provider, Package: item.Package, Paths: map[string]string{}, PriorValues: map[string]json.RawMessage{"settings.json#/token": want}, PriorUnknown: true}); err != nil {
		t.Fatal(err)
	}
	engine := Engine{State: store, CatalogDigest: "signed"}
	if err := engine.own(item, model.Observation{}); err != nil {
		t.Fatal(err)
	}
	owned := mustSnapshot(t, store).Ownership[item.ID]
	if !jsonEqual(owned.PriorValues["settings.json#/token"], want) || owned.CatalogDigest != "signed" || !owned.PriorUnknown {
		t.Fatalf("ownership = %#v", owned)
	}
}

func jsonEqual(left, right []byte) bool {
	var a, b any
	return json.Unmarshal(left, &a) == nil && json.Unmarshal(right, &b) == nil && reflect.DeepEqual(a, b)
}

func mustSnapshot(t *testing.T, store *state.Store) model.Snapshot {
	t.Helper()
	snapshot, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func (f *fixtureAdapter) event(value string) {
	f.events = append(f.events, value)
	if f.shared != nil {
		*f.shared = append(*f.shared, value)
	}
}

func (f *fixtureAdapter) Inspect(_ context.Context, item model.Resource) (model.Observation, error) {
	f.event("inspect:" + string(item.ID))
	if f.fail["inspect:"+string(item.ID)] {
		return model.Observation{}, errors.New("inspect failed")
	}
	return f.observation(item), nil
}
func (f *fixtureAdapter) Plan(context.Context, model.Resource, model.Observation, model.Ownership) ([]model.Operation, error) {
	return nil, nil
}
func (f *fixtureAdapter) Execute(_ context.Context, operation model.Operation) model.OperationResult {
	f.event("execute:" + operation.ID)
	if operation.Kind == model.OperationPrune {
		f.present = false
	} else {
		f.present = true
	}
	if f.onExecute != nil {
		f.onExecute()
	}
	return f.result(operation, "execute:"+operation.ID)
}
func (f *fixtureAdapter) ExecuteResource(ctx context.Context, item model.Resource, operation model.Operation) model.OperationResult {
	f.boundResources = append(f.boundResources, item)
	return f.Execute(ctx, operation)
}
func (f *fixtureAdapter) Verify(_ context.Context, item model.Resource) (model.Observation, error) {
	f.event("verify:" + string(item.ID))
	f.verifyCalls++
	if f.failVerifyAfter > 0 && f.verifyCalls >= f.failVerifyAfter {
		return model.Observation{}, errors.New("verify failed")
	}
	if f.fail["verify:"+string(item.ID)] {
		return model.Observation{}, errors.New("verify failed")
	}
	return f.observation(item), nil
}
func (f *fixtureAdapter) Simulate(_ context.Context, _ model.Resource, operation model.Operation) (provider.ChangeSet, error) {
	f.event("simulate:" + operation.ID)
	if f.onSimulate != nil {
		f.onSimulate()
	}
	if f.fail["simulate:"+operation.ID] {
		return provider.ChangeSet{}, errors.New("simulation failed")
	}
	return f.simulation, nil
}
func (f *fixtureAdapter) CancelSimulation(operation model.Operation) error {
	f.canceled = append(f.canceled, operation)
	f.event("cancel:" + operation.ID)
	return nil
}
func (f *fixtureAdapter) InstallDesired(_ context.Context, _ model.Resource, operation model.Operation) model.OperationResult {
	f.event("install-desired")
	f.present = true
	return f.result(operation, "install")
}
func (f *fixtureAdapter) RemoveLegacy(_ context.Context, _ model.Resource, operation model.Operation) model.OperationResult {
	f.event("remove-legacy")
	if f.fail["remove-legacy"] {
		return model.OperationResult{OperationID: operation.ID, ResourceID: operation.ResourceID, Detail: "legacy removal failed"}
	}
	f.legacy = false
	return f.result(operation, "remove")
}
func (f *fixtureAdapter) VerifyLegacyAbsent(context.Context, model.Resource, model.Operation) error {
	f.event("verify-legacy-absent")
	if f.legacy {
		return errors.New("legacy remains")
	}
	return nil
}
func (f *fixtureAdapter) result(op model.Operation, phase string) model.OperationResult {
	return model.OperationResult{OperationID: op.ID, ResourceID: op.ResourceID, Success: !f.fail[phase]}
}
func (f *fixtureAdapter) observation(item model.Resource) model.Observation {
	paths := f.observationPaths
	if paths == nil {
		paths = map[string]string{"bin": "/safe/bin"}
	}
	return model.Observation{Present: f.present, Healthy: f.present, Provider: item.Provider, Package: item.Package, Paths: paths}
}

type privilegeFixture struct {
	calls int
	err   error
}

func (p *privilegeFixture) Acquire(context.Context) error { p.calls++; return p.err }

func testEngine(t *testing.T, adapters map[string]*fixtureAdapter, resources ...model.Resource) (*Engine, *state.Store) {
	t.Helper()
	dir := t.TempDir()
	store, err := state.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	registry := resource.NewRegistry()
	indexed := make(map[model.ResourceID]model.Resource)
	digests := make(map[model.ResourceID]string)
	enabled := make([]model.ResourceID, 0, len(resources))
	for _, item := range resources {
		indexed[item.ID] = item
		digests[item.ID] = "signed"
		enabled = append(enabled, item.ID)
		if err := registry.Register(item.Type, item.Provider, adapters[item.Provider]); err != nil {
			t.Fatal(err)
		}
	}
	return &Engine{Registry: registry, State: store, LockDir: dir, Resources: indexed, Enabled: enabled, ResourceDigests: digests, CatalogDigest: "signed", EffectiveUID: func() int { return 501 }}, store
}

func pkg(id, providerName string, dependencies ...model.ResourceID) model.Resource {
	return model.Resource{ID: model.ResourceID(id), Type: model.ResourcePackage, Provider: providerName, Package: id, DependsOn: dependencies, VersionPolicy: model.VersionTracked}
}
func transferPkg() model.Resource {
	return model.Resource{ID: "core.ripgrep", Type: model.ResourcePackage, Provider: "homebrew-formula", Package: "ripgrep", VersionPolicy: model.VersionTracked, Metadata: map[string]string{"legacy.mise.package": "aqua:BurntSushi/ripgrep"}}
}
func op(item model.Resource, id string, kind model.OperationKind) model.Operation {
	operation := model.Operation{ID: id, ResourceID: item.ID, Kind: kind, Provider: item.Provider, Package: item.Package}
	if kind == model.OperationPrune {
		operation.Removes = []string{item.Package}
	}
	return operation
}

func TestApplyInstallsVerifiesAndCommitsOwnership(t *testing.T) {
	item := pkg("core.alpha", "fixture")
	adapter := &fixtureAdapter{fail: map[string]bool{}}
	engine, store := testEngine(t, map[string]*fixtureAdapter{"fixture": adapter}, item)
	summary, err := engine.Apply(context.Background(), model.Plan{ID: "p", Operations: []model.Operation{op(item, "install", model.OperationInstall)}})
	if err != nil || len(summary.Ready) != 1 {
		t.Fatalf("Apply = %#v, %v", summary, err)
	}
	snapshot, _ := store.Snapshot()
	owned := snapshot.Ownership[item.ID]
	if owned.CatalogDigest != "signed" || owned.Provider != item.Provider || len(owned.Paths) != 0 {
		t.Fatalf("ownership = %#v", owned)
	}
	if got := strings.Join(adapter.events, ","); got != "inspect:core.alpha,execute:install,verify:core.alpha,verify:core.alpha" {
		t.Fatalf("events = %s", got)
	}
}

func TestDynamicObservationPathsRoundTripToHistoricalPrune(t *testing.T) {
	item := model.Resource{ID: "shell.test", Type: model.ResourceGitCheckout, Provider: "fixture", Package: "test", VersionPolicy: model.VersionPinned, Metadata: map[string]string{"git.destination": ".test"}}
	paths := map[string]string{"/home/me/.test/tracked": "file:" + strings.Repeat("a", 64)}
	adapter := &fixtureAdapter{fail: map[string]bool{}, observationPaths: paths}
	engine, store := testEngine(t, map[string]*fixtureAdapter{"fixture": adapter}, item)
	if _, err := engine.Apply(context.Background(), model.Plan{ID: "install", Operations: []model.Operation{op(item, "install", model.OperationInstall)}}); err != nil {
		t.Fatal(err)
	}
	snapshot, _ := store.Snapshot()
	if !reflect.DeepEqual(snapshot.Ownership[item.ID].Paths, paths) {
		t.Fatalf("dynamic paths persisted=%#v", snapshot.Ownership[item.ID].Paths)
	}
	engine.Enabled = nil
	if _, err := engine.Apply(context.Background(), model.Plan{ID: "prune", Operations: []model.Operation{op(item, "prune", model.OperationPrune)}}); err != nil {
		t.Fatal(err)
	}
	if adapter.present {
		t.Fatal("historical prune failed")
	}
}

func TestApplyRejectsMalformedVerifiedGitCheckoutPaths(t *testing.T) {
	item := model.Resource{ID: "shell.test", Type: model.ResourceGitCheckout, Provider: "fixture", Package: "test", VersionPolicy: model.VersionPinned, Metadata: map[string]string{"git.destination": ".test"}}
	adapter := &fixtureAdapter{fail: map[string]bool{}, observationPaths: map[string]string{"/tmp/outside": "file:" + strings.Repeat("a", 64)}}
	engine, store := testEngine(t, map[string]*fixtureAdapter{"fixture": adapter}, item)
	if _, err := engine.Apply(context.Background(), model.Plan{ID: "malformed-observation", Operations: []model.Operation{op(item, "install", model.OperationInstall)}}); err == nil || !strings.Contains(err.Error(), "verified git-checkout paths") {
		t.Fatalf("err=%v", err)
	}
	snapshot, _ := store.Snapshot()
	if _, exists := snapshot.Ownership[item.ID]; exists {
		t.Fatal("malformed Git paths became ownership")
	}
}

func TestManagedFileObservationPathsBecomeExactOwnership(t *testing.T) {
	item := model.Resource{ID: "dotfiles.home", Type: model.ResourceManagedFiles, Provider: "fixture", Package: "home", VersionPolicy: model.VersionTracked, Metadata: map[string]string{model.ManagedFilesScopeMetadataKey: "."}}
	adapter := &fixtureAdapter{fail: map[string]bool{}, observationPaths: map[string]string{"/home/me/.zshrc": "file:digest"}}
	engine, store := testEngine(t, map[string]*fixtureAdapter{"fixture": adapter}, item)
	if _, err := engine.Apply(context.Background(), model.Plan{ID: "install-managed", Operations: []model.Operation{op(item, "install-managed", model.OperationInstall)}}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if got := snapshot.Ownership[item.ID].Paths; !reflect.DeepEqual(got, adapter.observationPaths) {
		t.Fatalf("managed ownership paths = %#v", got)
	}
	if len(adapter.boundResources) != 1 || adapter.boundResources[0].Metadata[model.ManagedFilesScopeMetadataKey] != "." {
		t.Fatalf("bound resources=%#v", adapter.boundResources)
	}
	engine.Enabled = nil
	if _, err := engine.Apply(context.Background(), model.Plan{ID: "prune-managed", Operations: []model.Operation{op(item, "prune-managed", model.OperationPrune)}}); err != nil {
		t.Fatalf("dynamic managed-file receipt rejected for historical prune: %v", err)
	}
}

func TestApplyInputComposesCurrentAndHistoricalFacts(t *testing.T) {
	item := pkg("core.alpha", "fixture")
	adapter := &fixtureAdapter{fail: map[string]bool{}}
	engine, store := testEngine(t, map[string]*fixtureAdapter{"fixture": adapter}, item)
	engine.Resources = nil
	engine.Enabled = nil
	engine.ResourceDigests = nil
	engine.CatalogDigest = ""
	input := ApplyInput{Plan: model.Plan{ID: "p", Operations: []model.Operation{op(item, "install", model.OperationInstall)}}, CurrentResources: []model.Resource{item}, EnabledIDs: []model.ResourceID{item.ID}, HistoricalResources: map[model.ResourceID]HistoricalResource{}, CatalogDigest: "current", Profile: model.ProfileVPSShell}
	if _, err := engine.ApplyInput(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	snapshot, _ := store.Snapshot()
	if snapshot.Ownership[item.ID].CatalogDigest != "current" {
		t.Fatalf("ownership=%#v", snapshot.Ownership[item.ID])
	}
}

func TestPreflightInputSimulatesPrivilegeWithoutJournalOrExecution(t *testing.T) {
	item := pkg("tools.example", "fixture")
	adapter := &fixtureAdapter{fail: map[string]bool{}, simulation: provider.ChangeSet{Installs: []string{item.Package}}}
	engine, store := testEngine(t, map[string]*fixtureAdapter{"fixture": adapter}, item)
	engine.Privilege = &privilegeFixture{}
	operation := op(item, "install", model.OperationInstall)
	operation.RequiresPrivilege = true
	input := ApplyInput{Plan: model.Plan{ID: "preflight", Operations: []model.Operation{operation}}, CurrentResources: []model.Resource{item}, EnabledIDs: []model.ResourceID{item.ID}, HistoricalResources: map[model.ResourceID]HistoricalResource{}, CatalogDigest: "signed", Profile: model.ProfileVPSShell}

	if _, err := engine.PreflightInput(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(adapter.events, ","), "execute:") {
		t.Fatalf("events = %v", adapter.events)
	}
	snapshot, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ActiveJournal != nil {
		t.Fatalf("preflight created journal %#v", snapshot.ActiveJournal)
	}
}

func TestApplyInputExecutesOnlyReplannedOperationIDs(t *testing.T) {
	first := pkg("core.alpha", "first")
	second := pkg("core.beta", "second")
	a := &fixtureAdapter{fail: map[string]bool{}}
	b := &fixtureAdapter{fail: map[string]bool{}, present: true}
	engine, _ := testEngine(t, map[string]*fixtureAdapter{"first": a, "second": b}, first, second)
	plan := model.Plan{ID: "filtered", Operations: []model.Operation{op(first, "install-first", model.OperationInstall), op(second, "upgrade-second", model.OperationUpgrade)}}
	input := ApplyInput{Plan: plan, CurrentResources: []model.Resource{first, second}, EnabledIDs: []model.ResourceID{first.ID, second.ID}, HistoricalResources: map[model.ResourceID]HistoricalResource{}, CatalogDigest: "signed", Profile: model.ProfileVPSShell, ForceUpgrade: true, RequiredOperationIDs: map[string]bool{"install-first": true}}
	if _, err := engine.ApplyInput(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(b.events, ","), "execute:upgrade-second") {
		t.Fatalf("filtered operation executed: %v", b.events)
	}
}

func TestApplyFailureKeepsJournalAndRetryExecutesPendingOperations(t *testing.T) {
	first := pkg("core.alpha", "first")
	second := pkg("core.beta", "second")
	a := &fixtureAdapter{fail: map[string]bool{}}
	b := &fixtureAdapter{fail: map[string]bool{"execute:install-second": true}}
	engine, store := testEngine(t, map[string]*fixtureAdapter{"first": a, "second": b}, first, second)
	plan := model.Plan{ID: "retry", Operations: []model.Operation{op(first, "install-first", model.OperationInstall), op(second, "install-second", model.OperationInstall)}}
	summary, err := engine.Apply(context.Background(), plan)
	if err == nil || summary.Unavailable[second.ID] == "" {
		t.Fatalf("Apply summary=%#v err=%v", summary, err)
	}
	snapshot := mustSnapshot(t, store)
	if snapshot.ActiveJournal == nil || snapshot.ActiveJournal.ID == "" {
		t.Fatalf("journal completed after failure: %#v", snapshot)
	}
	firstExecs := strings.Count(strings.Join(a.events, ","), "execute:install-first")
	delete(b.fail, "execute:install-second")
	summary, err = engine.Apply(context.Background(), plan)
	if err != nil || len(summary.Unavailable) != 0 {
		t.Fatalf("retry summary=%#v err=%v", summary, err)
	}
	if strings.Count(strings.Join(a.events, ","), "execute:install-first") != firstExecs {
		t.Fatalf("successful operation repeated: %v", a.events)
	}
	if mustSnapshot(t, store).ActiveJournal != nil {
		t.Fatal("successful retry left active journal")
	}
}

func TestApplyInputHeldReusesExactLiveLockAndPreservesOwnership(t *testing.T) {
	item := pkg("core.alpha", "fixture")
	adapter := &fixtureAdapter{fail: map[string]bool{}}
	engine, store := testEngine(t, map[string]*fixtureAdapter{"fixture": adapter}, item)
	input := ApplyInput{Plan: model.Plan{ID: "held", Operations: []model.Operation{op(item, "install", model.OperationInstall)}}, CurrentResources: []model.Resource{item}, EnabledIDs: []model.ResourceID{item.ID}, HistoricalResources: map[model.ResourceID]HistoricalResource{}, CatalogDigest: "held-digest", Profile: model.ProfileVPSShell}
	lock, err := state.Acquire(engine.LockDir, "tpod resolve core.alpha")
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()

	summary, err := engine.ApplyInputHeld(context.Background(), input, lock)
	if err != nil || len(summary.Ready) != 1 {
		t.Fatalf("ApplyInputHeld = %#v, %v", summary, err)
	}
	snapshot, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Ownership[item.ID].CatalogDigest != "held-digest" || snapshot.ActiveJournal != nil {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestApplyInputHeldRejectsNilForeignAndReleasedLock(t *testing.T) {
	item := pkg("core.alpha", "fixture")
	adapter := &fixtureAdapter{fail: map[string]bool{}}
	engine, _ := testEngine(t, map[string]*fixtureAdapter{"fixture": adapter}, item)
	input := ApplyInput{Plan: model.Plan{ID: "held"}, CurrentResources: []model.Resource{item}, EnabledIDs: []model.ResourceID{item.ID}, CatalogDigest: "held-digest", Profile: model.ProfileVPSShell}
	if _, err := engine.ApplyInputHeld(context.Background(), input, nil); err == nil {
		t.Fatal("nil held lock accepted")
	}
	foreign, err := state.Acquire(t.TempDir(), "tpod resolve core.alpha")
	if err != nil {
		t.Fatal(err)
	}
	defer foreign.Release()
	if _, err := engine.ApplyInputHeld(context.Background(), input, foreign); err == nil {
		t.Fatal("foreign held lock accepted")
	}
	released, err := state.Acquire(engine.LockDir, "tpod resolve core.alpha")
	if err != nil {
		t.Fatal(err)
	}
	if err := released.Release(); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.ApplyInputHeld(context.Background(), input, released); err == nil {
		t.Fatal("released held lock accepted")
	}
}

func TestApplyTransferControlsSafePhaseOrder(t *testing.T) {
	item := transferPkg()
	adapter := &fixtureAdapter{legacy: true, fail: map[string]bool{}}
	engine, store := testEngine(t, map[string]*fixtureAdapter{"homebrew-formula": adapter}, item)
	operation := op(item, "transfer", model.OperationTransfer)
	operation.Removes = []string{"aqua:BurntSushi/ripgrep"}
	_, err := engine.Apply(context.Background(), model.Plan{ID: "p", Operations: []model.Operation{operation}})
	if err != nil {
		t.Fatal(err)
	}
	want := "simulate:transfer,inspect:core.ripgrep,install-desired,verify:core.ripgrep,remove-legacy,verify-legacy-absent,verify:core.ripgrep,verify:core.ripgrep,cancel:transfer"
	if got := strings.Join(adapter.events, ","); got != want {
		t.Fatalf("events = %s, want %s", got, want)
	}
	snapshot, _ := store.Snapshot()
	if _, ok := snapshot.Ownership[item.ID]; !ok {
		t.Fatal("ownership not committed")
	}
}

func TestApplyRevokesTransferSimulationOnEveryExitPath(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T, *Engine, *state.Store, *fixtureAdapter, *model.Resource, *model.Operation, context.CancelFunc)
	}{
		{
			name: "privilege failure",
			setup: func(_ *testing.T, engine *Engine, _ *state.Store, _ *fixtureAdapter, _ *model.Resource, operation *model.Operation, _ context.CancelFunc) {
				operation.RequiresPrivilege = true
				engine.Privilege = &privilegeFixture{err: errors.New("denied")}
			},
		},
		{
			name: "context cancellation",
			setup: func(_ *testing.T, _ *Engine, _ *state.Store, adapter *fixtureAdapter, _ *model.Resource, _ *model.Operation, cancel context.CancelFunc) {
				adapter.onSimulate = cancel
			},
		},
		{
			name: "journal begin failure",
			setup: func(t *testing.T, engine *Engine, store *state.Store, _ *fixtureAdapter, _ *model.Resource, operation *model.Operation, _ context.CancelFunc) {
				plan := model.Plan{ID: "p", Operations: []model.Operation{*operation}}
				journal, err := store.Begin(plan)
				if err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(engine.LockDir, "journals", journal.ID+".json")
				contents, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var document map[string]any
				if err := json.Unmarshal(contents, &document); err != nil {
					t.Fatal(err)
				}
				document["Status"] = "invalid"
				contents, err = json.Marshal(document)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, contents, 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "dependency failure",
			setup: func(_ *testing.T, _ *Engine, _ *state.Store, _ *fixtureAdapter, item *model.Resource, _ *model.Operation, _ context.CancelFunc) {
				item.DependsOn = []model.ResourceID{"core.missing"}
			},
		},
		{
			name: "desired install failure",
			setup: func(_ *testing.T, _ *Engine, _ *state.Store, adapter *fixtureAdapter, _ *model.Resource, _ *model.Operation, _ context.CancelFunc) {
				adapter.fail["install"] = true
			},
		},
		{name: "success", setup: func(*testing.T, *Engine, *state.Store, *fixtureAdapter, *model.Resource, *model.Operation, context.CancelFunc) {
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			item := transferPkg()
			if test.name == "privilege failure" {
				item = model.Resource{ID: "core.mise", Type: model.ResourcePackage, Provider: "homebrew-formula", Package: "mise", VersionPolicy: model.VersionTracked, Metadata: map[string]string{"legacy.apt.package": "mise", "legacy.apt.profile": "vps-shell"}}
			}
			adapter := &fixtureAdapter{legacy: true, fail: map[string]bool{}}
			engine, store := testEngine(t, map[string]*fixtureAdapter{"homebrew-formula": adapter}, item)
			engine.Profile = model.ProfileVPSShell
			operation := op(item, "transfer", model.OperationTransfer)
			if test.name == "privilege failure" {
				operation.Removes = []string{"mise"}
			} else {
				operation.Removes = []string{"aqua:BurntSushi/ripgrep"}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			test.setup(t, engine, store, adapter, &item, &operation, cancel)
			engine.Resources[item.ID] = item
			engine.Enabled = []model.ResourceID{item.ID}
			summary, err := engine.Apply(ctx, model.Plan{ID: "p", Operations: []model.Operation{operation}})
			if len(adapter.canceled) != 1 || adapter.canceled[0].ID != operation.ID {
				t.Fatalf("canceled=%#v", adapter.canceled)
			}
			events := strings.Join(adapter.events, ",")
			switch test.name {
			case "privilege failure":
				if err == nil || !strings.Contains(err.Error(), "acquire privilege") || strings.Contains(events, "install-desired") {
					t.Fatalf("err=%v events=%s", err, events)
				}
			case "context cancellation":
				if !errors.Is(err, context.Canceled) || strings.Contains(events, "install-desired") {
					t.Fatalf("err=%v events=%s", err, events)
				}
			case "journal begin failure":
				if err == nil || !strings.Contains(err.Error(), "begin journal") || strings.Contains(events, "install-desired") {
					t.Fatalf("err=%v events=%s", err, events)
				}
			case "dependency failure":
				if err == nil || summary.Unavailable[item.ID] == "" || strings.Contains(events, "install-desired") {
					t.Fatalf("summary=%#v err=%v events=%s", summary, err, events)
				}
			case "desired install failure":
				if err == nil || summary.Unavailable[item.ID] == "" || !strings.Contains(events, "install-desired") {
					t.Fatalf("summary=%#v err=%v events=%s", summary, err, events)
				}
			case "success":
				if err != nil || len(summary.Ready) != 1 || !strings.Contains(events, "remove-legacy") {
					t.Fatalf("summary=%#v err=%v events=%s", summary, err, events)
				}
			}
		})
	}
}

func TestApplyDoesNotRemoveLegacyWhenDesiredVerificationFails(t *testing.T) {
	item := transferPkg()
	adapter := &fixtureAdapter{legacy: true, fail: map[string]bool{"verify:core.ripgrep": true}}
	engine, store := testEngine(t, map[string]*fixtureAdapter{"homebrew-formula": adapter}, item)
	operation := op(item, "transfer", model.OperationTransfer)
	operation.Removes = []string{"aqua:BurntSushi/ripgrep"}
	summary, err := engine.Apply(context.Background(), model.Plan{ID: "p", Operations: []model.Operation{operation}})
	if err == nil {
		t.Fatal("failed verification returned nil error")
	}
	if !adapter.legacy || strings.Contains(strings.Join(adapter.events, ","), "remove-legacy") {
		t.Fatal("legacy removal ran after failed desired verification")
	}
	if _, ok := summary.Unavailable[item.ID]; !ok {
		t.Fatal("resource not unavailable")
	}
	snapshot, _ := store.Snapshot()
	if _, ok := snapshot.Ownership[item.ID]; ok {
		t.Fatal("failed transfer was owned")
	}
}

func TestApplyContinuesIndependentAndBlocksDependentResources(t *testing.T) {
	first, child, independent := pkg("core.first", "first"), pkg("core.child", "child", "core.first"), pkg("core.other", "other")
	a := &fixtureAdapter{fail: map[string]bool{"execute:first": true}}
	b := &fixtureAdapter{fail: map[string]bool{}}
	c := &fixtureAdapter{fail: map[string]bool{}}
	engine, _ := testEngine(t, map[string]*fixtureAdapter{"first": a, "child": b, "other": c}, first, child, independent)
	plan := model.Plan{ID: "p", Operations: []model.Operation{op(first, "first", model.OperationInstall), op(child, "child", model.OperationInstall), op(independent, "other", model.OperationInstall)}}
	summary, err := engine.Apply(context.Background(), plan)
	if err == nil {
		t.Fatal("partial failure returned nil error")
	}
	if len(b.events) != 0 || !c.present {
		t.Fatalf("child events=%v independent present=%v", b.events, c.present)
	}
	if len(summary.Unavailable) != 2 || len(summary.Ready) != 1 || summary.Ready[0] != independent.ID {
		t.Fatalf("summary=%#v", summary)
	}
}

func TestApplyPreflightsEveryPrivilegedOperationBeforeMutationAndAcquiresOnce(t *testing.T) {
	aItem, bItem := pkg("core.alpha", "a"), pkg("core.beta", "b")
	aItem.Metadata = map[string]string{"path.bin": "/safe/bin"}
	bItem.Metadata = map[string]string{"path.bin": "/safe/bin"}
	shared := []string{}
	a := &fixtureAdapter{fail: map[string]bool{}, shared: &shared}
	b := &fixtureAdapter{fail: map[string]bool{}, shared: &shared}
	engine, _ := testEngine(t, map[string]*fixtureAdapter{"a": a, "b": b}, aItem, bItem)
	p := &privilegeFixture{}
	engine.Privilege = p
	aop, bop := op(aItem, "a", model.OperationInstall), op(bItem, "b", model.OperationInstall)
	aop.RequiresPrivilege = true
	bop.RequiresPrivilege = true
	if _, err := engine.Apply(context.Background(), model.Plan{ID: "p", Operations: []model.Operation{aop, bop}}); err != nil {
		t.Fatal(err)
	}
	if p.calls != 1 || a.events[0] != "simulate:a" || b.events[0] != "simulate:b" {
		t.Fatalf("preflight calls=%d events=%v %v", p.calls, a.events, b.events)
	}
	if len(shared) < 2 || shared[0] != "simulate:a" || shared[1] != "simulate:b" {
		t.Fatalf("mutation preceded complete simulation: %v", shared)
	}

	badItem := pkg("core.bad", "bad")
	bad := &fixtureAdapter{fail: map[string]bool{"simulate:bad": true}}
	badEngine, _ := testEngine(t, map[string]*fixtureAdapter{"bad": bad}, badItem)
	badEngine.Privilege = &privilegeFixture{}
	badOp := op(badItem, "bad", model.OperationInstall)
	badOp.RequiresPrivilege = true
	if _, err := badEngine.Apply(context.Background(), model.Plan{ID: "bad", Operations: []model.Operation{badOp}}); err == nil {
		t.Fatal("simulation failure accepted")
	}
	if strings.Contains(strings.Join(bad.events, ","), "execute") {
		t.Fatal("mutation ran after failed simulation")
	}
}

func TestApplyResumeReinspectsActualStateAndPreservesReversePruneOrder(t *testing.T) {
	aItem, bItem := pkg("core.alpha", "a"), pkg("core.beta", "b")
	aItem.Metadata = map[string]string{"path.bin": "/safe/bin"}
	bItem.Metadata = map[string]string{"path.bin": "/safe/bin"}
	shared := []string{}
	a := &fixtureAdapter{present: true, fail: map[string]bool{}, shared: &shared}
	b := &fixtureAdapter{present: true, fail: map[string]bool{}, shared: &shared}
	engine, store := testEngine(t, map[string]*fixtureAdapter{"a": a, "b": b}, aItem, bItem)
	engine.Enabled = nil
	for _, item := range []model.Resource{aItem, bItem} {
		if err := store.PutOwnership(model.Ownership{ResourceID: item.ID, CatalogDigest: "signed", Provider: item.Provider, Package: item.Package, Paths: map[string]string{"bin": "/safe/bin"}}); err != nil {
			t.Fatal(err)
		}
	}
	plan := model.Plan{ID: "p", Operations: []model.Operation{op(bItem, "prune-b", model.OperationPrune), op(aItem, "prune-a", model.OperationPrune)}}
	if _, err := store.Begin(plan); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Apply(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	if a.present || b.present {
		t.Fatal("prune did not remove resources")
	}
	// Each prune starts with an actual inspection despite the active journal.
	if a.events[0] != "inspect:core.alpha" || b.events[0] != "inspect:core.beta" {
		t.Fatalf("resume events=%v %v", a.events, b.events)
	}
	if got := strings.Join(shared, ","); got != "inspect:core.beta,execute:prune-b,inspect:core.beta,inspect:core.alpha,execute:prune-a,inspect:core.alpha" {
		t.Fatalf("prune order=%s", got)
	}
}

func TestApplyReportsEnabledNoOpResourcesAndPlanUnavailable(t *testing.T) {
	ready, unavailable := pkg("core.ready", "ready"), pkg("core.unavailable", "unavailable")
	engine, _ := testEngine(t, map[string]*fixtureAdapter{"ready": {present: true, fail: map[string]bool{}}, "unavailable": {fail: map[string]bool{}}}, ready, unavailable)
	summary, err := engine.Apply(context.Background(), model.Plan{ID: "p", Unavailable: map[model.ResourceID]string{unavailable.ID: "not supported"}})
	if err != nil || len(summary.Ready) != 1 || summary.Ready[0] != ready.ID || summary.Unavailable[unavailable.ID] != "not supported" {
		t.Fatalf("summary=%#v err=%v", summary, err)
	}
}

func TestApplyRejectsCrossResourceRemovalAuthority(t *testing.T) {
	first, second := pkg("core.first", "first"), pkg("core.second", "second")
	a := &fixtureAdapter{fail: map[string]bool{}, simulation: provider.ChangeSet{Removes: []string{second.Package}}}
	b := &fixtureAdapter{present: true, fail: map[string]bool{}}
	engine, _ := testEngine(t, map[string]*fixtureAdapter{"first": a, "second": b}, first, second)
	install := op(first, "install", model.OperationInstall)
	install.RequiresPrivilege = true
	install.Removes = []string{second.Package}
	plan := model.Plan{ID: "p", Operations: []model.Operation{install, op(second, "prune", model.OperationPrune)}}
	if _, err := engine.Apply(context.Background(), plan); err == nil || !strings.Contains(err.Error(), "removes") {
		t.Fatalf("cross-resource removal err=%v", err)
	}
	if len(a.events) != 0 || len(b.events) != 0 {
		t.Fatalf("provider touched: %v %v", a.events, b.events)
	}
}

func TestApplyFailsClosedOnInitialInspectError(t *testing.T) {
	for _, kind := range []model.OperationKind{model.OperationInstall, model.OperationAdopt, model.OperationPrune} {
		t.Run(string(kind), func(t *testing.T) {
			item := pkg("core.alpha", "fixture")
			adapter := &fixtureAdapter{legacy: true, fail: map[string]bool{"inspect:core.alpha": true}}
			engine, store := testEngine(t, map[string]*fixtureAdapter{"fixture": adapter}, item)
			if kind == model.OperationPrune {
				engine.Enabled = nil
				if err := store.PutOwnership(model.Ownership{ResourceID: item.ID, CatalogDigest: "signed", Provider: item.Provider, Package: item.Package, Paths: map[string]string{}}); err != nil {
					t.Fatal(err)
				}
			}
			summary, err := engine.Apply(context.Background(), model.Plan{ID: "p", Operations: []model.Operation{op(item, "operation", kind)}})
			if err == nil {
				t.Fatal("inspect failure returned nil error")
			}
			if _, ok := summary.Unavailable[item.ID]; !ok {
				t.Fatalf("summary=%#v", summary)
			}
			for _, event := range adapter.events {
				if strings.HasPrefix(event, "execute") || event == "install-desired" || event == "remove-legacy" {
					t.Fatalf("mutation after inspect error: %v", adapter.events)
				}
			}
			snapshot, _ := store.Snapshot()
			_, owned := snapshot.Ownership[item.ID]
			if kind == model.OperationPrune && !owned {
				t.Fatal("ownership removed after failed inspect")
			}
			if kind != model.OperationPrune && owned {
				t.Fatal("ownership committed")
			}
		})
	}
}

func TestApplyRejectsUntrustedRemovalAndTypedNilPrivilegeBeforeMutation(t *testing.T) {
	item := pkg("core.alpha", "fixture")
	adapter := &fixtureAdapter{fail: map[string]bool{}, simulation: provider.ChangeSet{Removes: []string{"victim"}}}
	engine, _ := testEngine(t, map[string]*fixtureAdapter{"fixture": adapter}, item)
	operation := op(item, "install", model.OperationInstall)
	operation.RequiresPrivilege = true
	operation.Removes = []string{"victim"}
	if _, err := engine.Apply(context.Background(), model.Plan{ID: "p", Operations: []model.Operation{operation}}); err == nil || !strings.Contains(err.Error(), "removes") {
		t.Fatalf("untrusted removal err=%v", err)
	}
	if len(adapter.events) != 0 {
		t.Fatalf("simulation/mutation ran: %v", adapter.events)
	}

	operation.Removes = nil
	adapter.simulation = provider.ChangeSet{}
	var nilPrivilege *privilegeFixture
	engine.Privilege = nilPrivilege
	if _, err := engine.Apply(context.Background(), model.Plan{ID: "p2", Operations: []model.Operation{operation}}); err == nil || !strings.Contains(err.Error(), "privilege acquisition") {
		t.Fatalf("typed nil privilege err=%v", err)
	}
}

func TestApplyHonorsCanceledContextBeforeMutation(t *testing.T) {
	item := pkg("core.alpha", "fixture")
	adapter := &fixtureAdapter{fail: map[string]bool{}}
	engine, store := testEngine(t, map[string]*fixtureAdapter{"fixture": adapter}, item)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := engine.Apply(ctx, model.Plan{ID: "p", Operations: []model.Operation{op(item, "install", model.OperationInstall)}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	if len(adapter.events) != 0 {
		t.Fatalf("mutation/inspection ran: %v", adapter.events)
	}
	snapshot, _ := store.Snapshot()
	if snapshot.ActiveJournal != nil {
		t.Fatal("journal began after cancellation")
	}
}

func TestApplyDoesNotWriteStateAfterContextCanceledByMutation(t *testing.T) {
	item := pkg("core.alpha", "fixture")
	ctx, cancel := context.WithCancel(context.Background())
	adapter := &fixtureAdapter{fail: map[string]bool{}, onExecute: cancel}
	engine, store := testEngine(t, map[string]*fixtureAdapter{"fixture": adapter}, item)
	if _, err := engine.Apply(ctx, model.Plan{ID: "p", Operations: []model.Operation{op(item, "install", model.OperationInstall)}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	snapshot, _ := store.Snapshot()
	if snapshot.ActiveJournal == nil {
		t.Fatal("interrupted journal was not preserved")
	}
	if len(snapshot.ActiveJournal.Results) != 0 {
		t.Fatalf("result written after cancellation: %#v", snapshot.ActiveJournal.Results)
	}
	if _, ok := snapshot.Ownership[item.ID]; ok {
		t.Fatal("ownership written after cancellation")
	}
}

func TestApplyRejectsStaleOrMismatchedHistoricalOwnership(t *testing.T) {
	item := pkg("core.legacy", "fixture")
	item.Metadata = map[string]string{"path.bin": "/safe/bin"}
	adapter := &fixtureAdapter{present: true, fail: map[string]bool{}}
	engine, store := testEngine(t, map[string]*fixtureAdapter{"fixture": adapter}, item)
	engine.Enabled = nil
	plan := model.Plan{ID: "p", Operations: []model.Operation{op(item, "prune", model.OperationPrune)}}
	if _, err := engine.Apply(context.Background(), plan); err == nil || !strings.Contains(err.Error(), "ownership") {
		t.Fatalf("stale ownership err=%v", err)
	}
	if len(adapter.events) != 0 {
		t.Fatalf("provider touched=%v", adapter.events)
	}
	if err := store.PutOwnership(model.Ownership{ResourceID: item.ID, CatalogDigest: "wrong", Provider: item.Provider, Package: item.Package, Paths: map[string]string{}, PriorValues: map[string]json.RawMessage{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Apply(context.Background(), plan); err == nil || !strings.Contains(err.Error(), "ownership") {
		t.Fatalf("mismatch ownership err=%v", err)
	}
	if err := store.PutOwnership(model.Ownership{ResourceID: item.ID, CatalogDigest: "signed", Provider: item.Provider, Package: item.Package, Paths: map[string]string{"bin": "/different"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Apply(context.Background(), plan); err == nil || !strings.Contains(err.Error(), "ownership paths") {
		t.Fatalf("path scope err=%v", err)
	}
	if err := store.PutOwnership(model.Ownership{ResourceID: item.ID, CatalogDigest: "signed", Provider: item.Provider, Package: item.Package, Paths: map[string]string{"bin": "/safe/bin"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Apply(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	if adapter.present {
		t.Fatal("valid historical resource was not pruned")
	}
}

func TestApplyInputResumeAcceptsCompletedPruneWithoutOwnershipReceipt(t *testing.T) {
	legacyItem := pkg("legacy.tool", "legacy")
	currentItem := pkg("core.current", "current")
	legacyAdapter := &fixtureAdapter{present: true, fail: map[string]bool{}}
	currentAdapter := &fixtureAdapter{fail: map[string]bool{"execute:install-current": true}}
	engine, store := testEngine(t, map[string]*fixtureAdapter{"legacy": legacyAdapter, "current": currentAdapter}, legacyItem, currentItem)
	plan := model.Plan{ID: "migration", Operations: []model.Operation{
		op(legacyItem, "prune-legacy", model.OperationPrune),
		op(currentItem, "install-current", model.OperationInstall),
	}}
	input := ApplyInput{
		Plan: plan, CurrentResources: []model.Resource{currentItem}, EnabledIDs: []model.ResourceID{currentItem.ID},
		HistoricalResources: map[model.ResourceID]HistoricalResource{
			legacyItem.ID: {Resource: legacyItem, CatalogDigest: "legacy-digest"},
		},
		CatalogDigest: "signed", Profile: model.ProfileVPSShell,
	}
	if err := store.PutOwnership(model.Ownership{ResourceID: legacyItem.ID, CatalogDigest: "legacy-digest", Provider: legacyItem.Provider, Package: legacyItem.Package, Paths: map[string]string{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.ApplyInput(context.Background(), input); err == nil {
		t.Fatal("partial migration succeeded")
	}
	if _, owned := mustSnapshot(t, store).Ownership[legacyItem.ID]; owned {
		t.Fatal("successful prune retained ownership")
	}
	pruneExecutions := strings.Count(strings.Join(legacyAdapter.events, ","), "execute:prune-legacy")
	delete(currentAdapter.fail, "execute:install-current")
	if _, err := engine.ApplyInput(context.Background(), input); err != nil {
		t.Fatalf("resume failed: %v", err)
	}
	if strings.Count(strings.Join(legacyAdapter.events, ","), "execute:prune-legacy") != pruneExecutions {
		t.Fatalf("resume repeated prune: %v", legacyAdapter.events)
	}
}

func TestApplyInputResumeStillRejectsPendingPruneWithoutOwnership(t *testing.T) {
	legacyItem := pkg("legacy.tool", "legacy")
	currentItem := pkg("core.current", "current")
	engine, store := testEngine(t, map[string]*fixtureAdapter{
		"legacy":  {present: true, fail: map[string]bool{}},
		"current": {present: true, fail: map[string]bool{}},
	}, legacyItem, currentItem)
	plan := model.Plan{ID: "migration", Operations: []model.Operation{
		op(currentItem, "install-current", model.OperationInstall),
		op(legacyItem, "prune-legacy", model.OperationPrune),
	}}
	if _, err := store.Begin(plan); err != nil {
		t.Fatal(err)
	}
	if err := store.Record(model.OperationResult{OperationID: "install-current", ResourceID: currentItem.ID, Success: true}); err != nil {
		t.Fatal(err)
	}
	input := ApplyInput{
		Plan: plan, CurrentResources: []model.Resource{currentItem}, EnabledIDs: []model.ResourceID{currentItem.ID},
		HistoricalResources: map[model.ResourceID]HistoricalResource{
			legacyItem.ID: {Resource: legacyItem, CatalogDigest: "legacy-digest"},
		},
		CatalogDigest: "signed", Profile: model.ProfileVPSShell,
	}
	if _, err := engine.ApplyInput(context.Background(), input); err == nil || !strings.Contains(err.Error(), "ownership") {
		t.Fatalf("pending unauthorized prune err=%v", err)
	}
}

func TestVerifyInputPostconditionsChecksDesiredAndHistoricalState(t *testing.T) {
	currentItem := pkg("core.current", "current")
	legacyItem := pkg("legacy.tool", "legacy")
	currentAdapter := &fixtureAdapter{present: true, fail: map[string]bool{}}
	legacyAdapter := &fixtureAdapter{present: false, fail: map[string]bool{}}
	engine, _ := testEngine(t, map[string]*fixtureAdapter{"current": currentAdapter, "legacy": legacyAdapter}, currentItem, legacyItem)
	input := ApplyInput{
		Plan: model.Plan{ID: "migration"}, CurrentResources: []model.Resource{currentItem}, EnabledIDs: []model.ResourceID{currentItem.ID},
		HistoricalResources: map[model.ResourceID]HistoricalResource{
			legacyItem.ID: {Resource: legacyItem, CatalogDigest: "legacy-digest"},
		},
		CatalogDigest: "signed", Profile: model.ProfileVPSShell,
	}
	if summary, err := engine.VerifyInputPostconditions(context.Background(), input); err != nil || len(summary.Ready) != 1 {
		t.Fatalf("ready summary=%#v err=%v", summary, err)
	}
	currentAdapter.present = false
	if summary, err := engine.VerifyInputPostconditions(context.Background(), input); err == nil || summary.Unavailable[currentItem.ID] == "" {
		t.Fatalf("missing desired summary=%#v err=%v", summary, err)
	}
	currentAdapter.present = true
	legacyAdapter.present = true
	if summary, err := engine.VerifyInputPostconditions(context.Background(), input); err == nil || summary.Unavailable[legacyItem.ID] == "" {
		t.Fatalf("present historical summary=%#v err=%v", summary, err)
	}
}

func TestApplyRejectsMalformedGitCheckoutHistoricalOwnership(t *testing.T) {
	item := model.Resource{ID: "shell.test", Type: model.ResourceGitCheckout, Provider: "fixture", Package: "test", VersionPolicy: model.VersionPinned, Metadata: map[string]string{"git.destination": ".checkout"}}
	validReceipt := "file:" + strings.Repeat("a", 64)
	for name, paths := range map[string]map[string]string{
		"outside destination": {"/home/me/other/file": validReceipt},
		"bad receipt":         {"/home/me/.checkout/file": "file:digest"},
		"split roots":         {"/home/one/.checkout/a": validReceipt, "/home/two/.checkout/b": validReceipt},
	} {
		t.Run(name, func(t *testing.T) {
			adapter := &fixtureAdapter{present: true, fail: map[string]bool{}}
			engine, store := testEngine(t, map[string]*fixtureAdapter{"fixture": adapter}, item)
			engine.Enabled = nil
			if err := store.PutOwnership(model.Ownership{ResourceID: item.ID, CatalogDigest: "signed", Provider: item.Provider, Package: item.Package, Paths: paths}); err != nil {
				t.Fatal(err)
			}
			if _, err := engine.Apply(context.Background(), model.Plan{ID: "bad-git-ownership", Operations: []model.Operation{op(item, "prune", model.OperationPrune)}}); err == nil || !strings.Contains(err.Error(), "git-checkout ownership") {
				t.Fatalf("err=%v", err)
			}
			if len(adapter.events) != 0 {
				t.Fatalf("adapter touched=%v", adapter.events)
			}
		})
	}
}

func TestApplyRejectsPruneOfEnabledResource(t *testing.T) {
	item := pkg("core.alpha", "fixture")
	adapter := &fixtureAdapter{present: true, fail: map[string]bool{}}
	engine, _ := testEngine(t, map[string]*fixtureAdapter{"fixture": adapter}, item)
	if _, err := engine.Apply(context.Background(), model.Plan{ID: "p", Operations: []model.Operation{op(item, "prune", model.OperationPrune)}}); err == nil || !strings.Contains(err.Error(), "enabled resource") {
		t.Fatalf("err=%v", err)
	}
	if len(adapter.events) != 0 {
		t.Fatalf("provider touched=%v", adapter.events)
	}
}

func TestApplyVerifiesNoOpDependencyBeforeDependentMutation(t *testing.T) {
	dependency, child := pkg("core.dependency", "dep"), pkg("core.child", "child", "core.dependency")
	depAdapter := &fixtureAdapter{present: false, fail: map[string]bool{}}
	childAdapter := &fixtureAdapter{fail: map[string]bool{}}
	engine, _ := testEngine(t, map[string]*fixtureAdapter{"dep": depAdapter, "child": childAdapter}, dependency, child)
	summary, err := engine.Apply(context.Background(), model.Plan{ID: "p", Operations: []model.Operation{op(child, "install", model.OperationInstall)}})
	if err == nil {
		t.Fatal("dependency failure returned nil error")
	}
	if _, ok := summary.Unavailable[dependency.ID]; !ok {
		t.Fatalf("summary=%#v", summary)
	}
	if len(childAdapter.events) != 0 {
		t.Fatalf("dependent touched=%v", childAdapter.events)
	}
}

func TestApplyResumeSkipsUpgradeAndRestoreWhenAlreadyDesired(t *testing.T) {
	for _, kind := range []model.OperationKind{model.OperationUpgrade, model.OperationRestore} {
		t.Run(string(kind), func(t *testing.T) {
			item := pkg("core.alpha", "fixture")
			adapter := &fixtureAdapter{present: true, fail: map[string]bool{}}
			engine, _ := testEngine(t, map[string]*fixtureAdapter{"fixture": adapter}, item)
			if _, err := engine.Apply(context.Background(), model.Plan{ID: "p", Operations: []model.Operation{op(item, "mutate", kind)}}); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(strings.Join(adapter.events, ","), "execute:mutate") {
				t.Fatalf("mutation repeated=%v", adapter.events)
			}
		})
	}
}

func TestApplyValidatesRemovesForEveryOperationKind(t *testing.T) {
	item := pkg("core.alpha", "fixture")
	adapter := &fixtureAdapter{fail: map[string]bool{}}
	for _, kind := range []model.OperationKind{model.OperationInstall, model.OperationAdopt, model.OperationUpgrade, model.OperationRestore, model.OperationVerify} {
		t.Run(string(kind), func(t *testing.T) {
			engine, _ := testEngine(t, map[string]*fixtureAdapter{"fixture": adapter}, item)
			operation := op(item, "op", kind)
			operation.Removes = []string{item.Package}
			if _, err := engine.Apply(context.Background(), model.Plan{ID: "p", Operations: []model.Operation{operation}}); err == nil || !strings.Contains(err.Error(), "removes") {
				t.Fatalf("err=%v", err)
			}
		})
	}
	engine, _ := testEngine(t, map[string]*fixtureAdapter{"fixture": adapter}, item)
	prune := op(item, "prune", model.OperationPrune)
	prune.Removes = []string{item.Package, item.Package}
	engine.Enabled = nil
	if err := engine.State.PutOwnership(model.Ownership{ResourceID: item.ID, CatalogDigest: "signed", Provider: item.Provider, Package: item.Package, Paths: map[string]string{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Apply(context.Background(), model.Plan{ID: "p2", Operations: []model.Operation{prune}}); err == nil || !strings.Contains(err.Error(), "removes") {
		t.Fatalf("duplicate prune err=%v", err)
	}
}

func TestApplyResumeAfterProviderMutationVerifiesWithoutRepeatingInstall(t *testing.T) {
	item := pkg("core.alpha", "fixture")
	adapter := &fixtureAdapter{present: true, fail: map[string]bool{}}
	engine, store := testEngine(t, map[string]*fixtureAdapter{"fixture": adapter}, item)
	plan := model.Plan{ID: "p", Operations: []model.Operation{op(item, "install", model.OperationInstall)}}
	if _, err := store.Begin(plan); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Apply(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(adapter.events, ","), "execute:install") {
		t.Fatalf("resume repeated mutation: %v", adapter.events)
	}
	snapshot, _ := store.Snapshot()
	if _, ok := snapshot.Ownership[item.ID]; !ok {
		t.Fatal("verified mutation was not adopted")
	}
}

func TestApplyDefersOwnershipUntilLastResourceOperation(t *testing.T) {
	item := pkg("core.alpha", "fixture")
	adapter := &fixtureAdapter{fail: map[string]bool{}, failVerifyAfter: 2}
	engine, store := testEngine(t, map[string]*fixtureAdapter{"fixture": adapter}, item)
	plan := model.Plan{ID: "p", Operations: []model.Operation{op(item, "first", model.OperationInstall), op(item, "second", model.OperationVerify)}}
	summary, err := engine.Apply(context.Background(), plan)
	if err == nil {
		t.Fatal("final operation failure returned nil error")
	}
	if _, ok := summary.Unavailable[item.ID]; !ok {
		t.Fatal("second operation failure not reported")
	}
	snapshot, _ := store.Snapshot()
	if _, ok := snapshot.Ownership[item.ID]; ok {
		t.Fatal("ownership committed before final operation")
	}
}

func TestApplyDoesNotOwnWhenGlobalFinalVerificationFails(t *testing.T) {
	item := pkg("core.alpha", "fixture")
	adapter := &fixtureAdapter{fail: map[string]bool{}, failVerifyAfter: 2}
	engine, store := testEngine(t, map[string]*fixtureAdapter{"fixture": adapter}, item)
	summary, err := engine.Apply(context.Background(), model.Plan{ID: "p", Operations: []model.Operation{op(item, "install", model.OperationInstall)}})
	if err == nil {
		t.Fatal("final verification failure returned nil error")
	}
	if _, ok := summary.Unavailable[item.ID]; !ok {
		t.Fatalf("summary=%#v", summary)
	}
	snapshot, _ := store.Snapshot()
	if _, ok := snapshot.Ownership[item.ID]; ok {
		t.Fatal("ownership written before global final verification")
	}
}

func TestNoOpDependentWaitsForOperatedDependency(t *testing.T) {
	first := pkg("core.first", "first")
	middle := pkg("core.middle", "middle", first.ID)
	last := pkg("core.last", "last", middle.ID)
	shared := []string{}
	a := &fixtureAdapter{fail: map[string]bool{}, shared: &shared}
	b := &fixtureAdapter{present: true, fail: map[string]bool{}, shared: &shared}
	c := &fixtureAdapter{fail: map[string]bool{}, shared: &shared}
	engine, _ := testEngine(t, map[string]*fixtureAdapter{"first": a, "middle": b, "last": c}, first, middle, last)
	plan := model.Plan{ID: "p", Operations: []model.Operation{op(first, "install-first", model.OperationInstall), op(last, "install-last", model.OperationInstall)}}
	if _, err := engine.Apply(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	events := strings.Join(shared, ",")
	firstExec := strings.Index(events, "execute:install-first")
	middleVerify := strings.Index(events, "verify:core.middle")
	lastExec := strings.Index(events, "execute:install-last")
	if firstExec < 0 || middleVerify < firstExec || lastExec < middleVerify {
		t.Fatalf("dependency schedule=%s", events)
	}
}

func TestFinalVerificationPropagatesOperatedDependencyFailureToNoOpDependent(t *testing.T) {
	first := pkg("core.first", "first")
	dependent := pkg("core.dependent", "dependent", first.ID)
	a := &fixtureAdapter{fail: map[string]bool{"execute:install": true}}
	b := &fixtureAdapter{present: true, fail: map[string]bool{}}
	engine, _ := testEngine(t, map[string]*fixtureAdapter{"first": a, "dependent": b}, first, dependent)
	summary, err := engine.Apply(context.Background(), model.Plan{ID: "p", Operations: []model.Operation{op(first, "install", model.OperationInstall)}})
	if err == nil {
		t.Fatal("dependency failure returned nil error")
	}
	if _, ok := summary.Unavailable[dependent.ID]; !ok {
		t.Fatalf("dependent marked ready: %#v", summary)
	}
}

func TestTransferRemovalAuthorityHonorsDeclarationProfile(t *testing.T) {
	item := model.Resource{ID: "core.btop", Type: model.ResourcePackage, Provider: "homebrew-formula", Package: "btop", VersionPolicy: model.VersionTracked, Metadata: map[string]string{"legacy.mise.package": "aqua:aristocratos/btop", "legacy.mise.profile": "vps-shell"}}
	declarations, err := legacydecl.Parse(item)
	if err != nil {
		t.Fatal(err)
	}
	operation := op(item, "transfer", model.OperationTransfer)
	operation.Removes = []string{"aqua:aristocratos/btop"}
	if err := validateRemoves(item, operation, declarations, model.ProfileMacOSTerminal); err == nil {
		t.Fatal("profile-scoped legacy source authorized on macOS")
	}
	if err := validateRemoves(item, operation, declarations, model.ProfileVPSShell); err != nil {
		t.Fatal(err)
	}
	unscoped := transferPkg()
	declarations, err = legacydecl.Parse(unscoped)
	if err != nil {
		t.Fatal(err)
	}
	operation = op(unscoped, "transfer", model.OperationTransfer)
	operation.Removes = []string{"aqua:BurntSushi/ripgrep"}
	if err := validateRemoves(unscoped, operation, declarations, ""); err != nil {
		t.Fatalf("empty profile rejected unscoped declaration: %v", err)
	}
}

func TestDerivedAPTPrivilegeRejectsForgedFalse(t *testing.T) {
	aptItem := pkg("core.apt", "apt")
	adapter := &fixtureAdapter{fail: map[string]bool{}}
	engine, _ := testEngine(t, map[string]*fixtureAdapter{"apt": adapter}, aptItem)
	if _, err := engine.Apply(context.Background(), model.Plan{ID: "apt", Operations: []model.Operation{op(aptItem, "install", model.OperationInstall)}}); err == nil || !strings.Contains(err.Error(), "required privilege") {
		t.Fatalf("apt err=%v", err)
	}
	legacyItem := model.Resource{ID: "core.mise", Type: model.ResourcePackage, Provider: "homebrew-formula", Package: "mise", VersionPolicy: model.VersionTracked, Metadata: map[string]string{"legacy.apt.package": "mise", "legacy.apt.profile": "vps-shell"}}
	legacyAdapter := &fixtureAdapter{legacy: true, fail: map[string]bool{}}
	legacyEngine, _ := testEngine(t, map[string]*fixtureAdapter{"homebrew-formula": legacyAdapter}, legacyItem)
	legacyEngine.Profile = model.ProfileVPSShell
	operation := op(legacyItem, "transfer", model.OperationTransfer)
	operation.Removes = []string{"mise"}
	if _, err := legacyEngine.Apply(context.Background(), model.Plan{ID: "transfer", Operations: []model.Operation{operation}}); err == nil || !strings.Contains(err.Error(), "required privilege") {
		t.Fatalf("legacy apt err=%v", err)
	}
}

func TestApplyRejectsRootAndUnsignedOperationBeforeMutation(t *testing.T) {
	item := pkg("core.alpha", "fixture")
	adapter := &fixtureAdapter{fail: map[string]bool{}}
	engine, _ := testEngine(t, map[string]*fixtureAdapter{"fixture": adapter}, item)
	engine.EffectiveUID = func() int { return 0 }
	if _, err := engine.Apply(context.Background(), model.Plan{ID: "p", Operations: []model.Operation{op(item, "install", model.OperationInstall)}}); err == nil {
		t.Fatal("root accepted")
	}
	if len(adapter.events) != 0 {
		t.Fatal("root mutated")
	}
	engine.EffectiveUID = func() int { return 501 }
	forged := op(item, "install", model.OperationInstall)
	forged.Package = "forged"
	if _, err := engine.Apply(context.Background(), model.Plan{ID: "p", Operations: []model.Operation{forged}}); err == nil {
		t.Fatal("forged identity accepted")
	}
}
