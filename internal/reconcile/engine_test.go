package reconcile

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

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
	if f.fail["simulate:"+operation.ID] {
		return provider.ChangeSet{}, errors.New("simulation failed")
	}
	return f.simulation, nil
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
	return model.Observation{Present: f.present, Healthy: f.present, Provider: item.Provider, Package: item.Package, Paths: map[string]string{"bin": "/safe/bin"}}
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
	if owned.CatalogDigest != "signed" || owned.Provider != item.Provider || owned.Paths["bin"] != "/safe/bin" {
		t.Fatalf("ownership = %#v", owned)
	}
	if got := strings.Join(adapter.events, ","); got != "inspect:core.alpha,execute:install,verify:core.alpha,verify:core.alpha" {
		t.Fatalf("events = %s", got)
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
	input := ApplyInput{Plan: model.Plan{ID: "p", Operations: []model.Operation{op(item, "install", model.OperationInstall)}}, CurrentResources: []model.Resource{item}, EnabledIDs: []model.ResourceID{item.ID}, HistoricalResources: map[model.ResourceID]HistoricalResource{}, CatalogDigest: "current"}
	if _, err := engine.ApplyInput(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	snapshot, _ := store.Snapshot()
	if snapshot.Ownership[item.ID].CatalogDigest != "current" {
		t.Fatalf("ownership=%#v", snapshot.Ownership[item.ID])
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
	want := "inspect:core.ripgrep,install-desired,verify:core.ripgrep,remove-legacy,verify-legacy-absent,verify:core.ripgrep,verify:core.ripgrep"
	if got := strings.Join(adapter.events, ","); got != want {
		t.Fatalf("events = %s, want %s", got, want)
	}
	snapshot, _ := store.Snapshot()
	if _, ok := snapshot.Ownership[item.ID]; !ok {
		t.Fatal("ownership not committed")
	}
}

func TestApplyDoesNotRemoveLegacyWhenDesiredVerificationFails(t *testing.T) {
	item := transferPkg()
	adapter := &fixtureAdapter{legacy: true, fail: map[string]bool{"verify:core.ripgrep": true}}
	engine, store := testEngine(t, map[string]*fixtureAdapter{"homebrew-formula": adapter}, item)
	operation := op(item, "transfer", model.OperationTransfer)
	operation.Removes = []string{"aqua:BurntSushi/ripgrep"}
	summary, err := engine.Apply(context.Background(), model.Plan{ID: "p", Operations: []model.Operation{operation}})
	if err != nil {
		t.Fatal(err)
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
	if err != nil {
		t.Fatal(err)
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
			if err != nil {
				t.Fatal(err)
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
	summary, err := engine.Apply(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := summary.Unavailable[item.ID]; !ok || !adapter.present {
		t.Fatalf("receipt mismatch mutated resource: summary=%#v present=%v", summary, adapter.present)
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
	if err != nil {
		t.Fatal(err)
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
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := summary.Unavailable[item.ID]; !ok {
		t.Fatal("second operation failure not reported")
	}
	snapshot, _ := store.Snapshot()
	if _, ok := snapshot.Ownership[item.ID]; ok {
		t.Fatal("ownership committed before final operation")
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
