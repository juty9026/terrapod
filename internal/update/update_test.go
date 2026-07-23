package update

import (
	"context"
	"crypto/ed25519"
	"errors"
	"path/filepath"
	"testing"

	"github.com/juty9026/terrapod/internal/catalog"
	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/planner"
	"github.com/juty9026/terrapod/internal/provider"
	"github.com/juty9026/terrapod/internal/reconcile"
	"github.com/juty9026/terrapod/internal/release"
	"github.com/juty9026/terrapod/internal/resource"
	"github.com/juty9026/terrapod/internal/state"
)

type sourceFixture struct {
	value release.VerifiedRelease
	err   error
}

func (f sourceFixture) LatestStable(context.Context) (release.VerifiedRelease, error) {
	return f.value, f.err
}

type stagerFixture struct {
	events      *[]string
	staged      release.Staged
	activateErr error
}

func (f *stagerFixture) Stage(context.Context, release.VerifiedRelease, release.Platform) (release.Staged, error) {
	*f.events = append(*f.events, "stage")
	return f.staged, nil
}
func (f *stagerFixture) Activate(string) error {
	*f.events = append(*f.events, "activate")
	return f.activateErr
}

type refresherFixture struct {
	name   string
	events *[]string
	err    error
}

func (f refresherFixture) Name() string { return f.name }
func (f refresherFixture) RefreshMetadata(context.Context) error {
	*f.events = append(*f.events, "refresh:"+f.name)
	return f.err
}

type adapterFixture struct {
	events  *[]string
	present bool
}

func (f *adapterFixture) Inspect(context.Context, model.Resource) (model.Observation, error) {
	*f.events = append(*f.events, "inspect")
	return model.Observation{Present: f.present, Healthy: f.present, Provider: "fixture", Package: "tool"}, nil
}
func (f *adapterFixture) Plan(_ context.Context, item model.Resource, observed model.Observation, _ model.Ownership) ([]model.Operation, error) {
	kind := model.OperationInstall
	if observed.Present {
		kind = model.OperationUpgrade
	}
	return []model.Operation{{ID: string(kind) + "-tool", ResourceID: item.ID, Kind: kind, Provider: item.Provider, Package: item.Package}}, nil
}
func (f *adapterFixture) Execute(_ context.Context, operation model.Operation) model.OperationResult {
	*f.events = append(*f.events, "execute")
	f.present = true
	return model.OperationResult{OperationID: operation.ID, ResourceID: operation.ResourceID, Success: true}
}
func (f *adapterFixture) Verify(context.Context, model.Resource) (model.Observation, error) {
	return model.Observation{Present: f.present, Healthy: f.present, Provider: "fixture", Package: "tool"}, nil
}

func TestRunPrintsAndPersistsFinalPlanBeforeActivationAndHandoff(t *testing.T) {
	deps, events := fixtureDependencies(t, "1.0.0", "2.0.0")
	result, err := Run(context.Background(), deps)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"stage", "self-check", "load", "refresh:fixture", "inspect", "print", "activate", "persist-trust", "exec"}
	if len(*events) != len(want) {
		t.Fatalf("events=%v", *events)
	}
	for i := range want {
		if (*events)[i] != want[i] {
			t.Fatalf("events=%v", *events)
		}
	}
	if !result.Handoff || result.JournalID == "" {
		t.Fatalf("result=%#v", result)
	}
	record, err := deps.State.Update(result.JournalID)
	if err != nil || !record.Activated {
		t.Fatalf("record=%#v err=%v", record, err)
	}
}

func TestRunProviderFailurePreservesActiveAndCreatesNoJournal(t *testing.T) {
	deps, events := fixtureDependencies(t, "1.0.0", "2.0.0")
	deps.Refreshers = []provider.MetadataRefresher{refresherFixture{name: "fixture", events: events, err: errors.New("offline")}}
	if _, err := Run(context.Background(), deps); err == nil {
		t.Fatal("provider failure accepted")
	}
	for _, event := range *events {
		if event == "activate" || event == "print" || event == "execute" {
			t.Fatalf("mutation event %q in %v", event, *events)
		}
	}
	snapshot, _ := deps.State.Snapshot()
	if snapshot.ActiveJournal != nil {
		t.Fatalf("journal=%#v", snapshot.ActiveJournal)
	}
}

func TestRunRejectsDowngradeBeforeStaging(t *testing.T) {
	deps, events := fixtureDependencies(t, "3.0.0", "2.0.0")
	if _, err := Run(context.Background(), deps); err == nil {
		t.Fatal("downgrade accepted")
	}
	if len(*events) != 0 {
		t.Fatalf("downgrade caused effects: %v", *events)
	}
}

func TestActivationFailureRetainsJournalWithoutTrustOrResourceMutation(t *testing.T) {
	deps, events := fixtureDependencies(t, "1.0.0", "2.0.0")
	deps.Stager.(*stagerFixture).activateErr = errors.New("swap failed")
	result, err := Run(context.Background(), deps)
	if err == nil || result.JournalID == "" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	record, readErr := deps.State.Update(result.JournalID)
	if readErr != nil || record.Activated {
		t.Fatalf("record=%#v err=%v", record, readErr)
	}
	for _, event := range *events {
		if event == "persist-trust" || event == "execute" || event == "exec" {
			t.Fatalf("post-activation effect %q in %v", event, *events)
		}
	}
}

func TestRunSameReleaseReplansAndAppliesWithoutActivationOrHandoff(t *testing.T) {
	deps, events := fixtureDependencies(t, "2.0.0", "2.0.0")
	result, err := Run(context.Background(), deps)
	if err != nil {
		t.Fatal(err)
	}
	if result.Handoff {
		t.Fatal("same release handed off")
	}
	foundExecute := false
	for _, event := range *events {
		if event == "activate" || event == "exec" {
			t.Fatalf("event %q in %v", event, *events)
		}
		foundExecute = foundExecute || event == "execute"
	}
	if !foundExecute {
		t.Fatalf("resource was not upgraded: %v", *events)
	}
}

func TestContinueRejectsCatalogBindingChangeBeforeConfigOrResourceMutation(t *testing.T) {
	deps, events := fixtureDependencies(t, "1.0.0", "2.0.0")
	started, err := Run(context.Background(), deps)
	if err != nil {
		t.Fatal(err)
	}
	writes := 0
	deps.WriteConfig = func(model.Config) error { writes++; return nil }
	originalVerify := deps.VerifyActive
	deps.VerifyActive = func(ctx context.Context, version string) (release.Staged, release.VerifiedRelease, Inputs, error) {
		staged, verified, inputs, err := originalVerify(ctx, version)
		inputs.Catalog.Digest = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		return staged, verified, inputs, err
	}
	before := len(*events)
	if _, err := Continue(context.Background(), started.JournalID, deps); err == nil {
		t.Fatal("changed catalog binding accepted")
	}
	if writes != 0 {
		t.Fatalf("config writes=%d", writes)
	}
	for _, event := range (*events)[before:] {
		if event == "execute" {
			t.Fatalf("resource mutation after binding change: %v", *events)
		}
	}
}

func fixtureDependencies(t *testing.T, current, latest string) (Dependencies, *[]string) {
	t.Helper()
	events := &[]string{}
	dir := t.TempDir()
	store, err := state.Open(filepath.Join(dir, "state"))
	if err != nil {
		t.Fatal(err)
	}
	adapter := &adapterFixture{events: events, present: current == latest}
	registry := resource.NewRegistry()
	if err := registry.Register(model.ResourcePackage, "fixture", adapter); err != nil {
		t.Fatal(err)
	}
	item := model.Resource{ID: "tools.example", Type: model.ResourcePackage, Provider: "fixture", Package: "tool", VersionPolicy: model.VersionTracked}
	cat := model.Catalog{Version: 1, Release: latest, Config: model.ConfigSchema{Version: 1, Fields: []model.ConfigField{}}, Resources: []model.Resource{item}}
	inputs := Inputs{Catalog: catalog.Verified{Catalog: cat, Digest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, Config: model.Config{Version: 1, Terrapod: map[string]any{}}, Historical: map[string]model.Catalog{}, Profile: model.ProfileVPSShell}
	staged := release.Staged{Version: latest, Path: filepath.Join(dir, "releases", latest)}
	manifest := release.Manifest{Version: latest, TrustedKeys: []release.TrustedKey{}, Assets: []release.Asset{}}
	engine := &reconcile.Engine{Registry: registry, State: store, LockDir: filepath.Join(dir, "state"), EffectiveUID: func() int { return 501 }}
	return Dependencies{
		Releases: sourceFixture{value: release.VerifiedRelease{Manifest: manifest}}, Stager: &stagerFixture{events: events, staged: staged}, Platform: release.Platform{OS: "darwin", Arch: "arm64"},
		Refreshers: []provider.MetadataRefresher{refresherFixture{name: "fixture", events: events}}, Planner: planner.New(registry), Engine: engine, State: store, LockDir: filepath.Join(dir, "state"),
		LoadStaged: func(context.Context, release.Staged) (Inputs, error) {
			*events = append(*events, "load")
			return inputs, nil
		},
		VerifyActive: func(context.Context, string) (release.Staged, release.VerifiedRelease, Inputs, error) {
			return staged, release.VerifiedRelease{Manifest: manifest}, inputs, nil
		},
		CurrentVersion: func() (string, error) { return current, nil }, SelfCheck: func(context.Context, string) error { *events = append(*events, "self-check"); return nil }, PrintPlan: func(model.Plan) error { *events = append(*events, "print"); return nil }, WriteConfig: func(model.Config) error { return nil },
		TrustAfter: func(release.Manifest) (map[string]ed25519.PublicKey, error) {
			return map[string]ed25519.PublicKey{"root": make([]byte, ed25519.PublicKeySize)}, nil
		}, PersistTrusted: func(map[string]ed25519.PublicKey) error { *events = append(*events, "persist-trust"); return nil }, Exec: func(string, []string, []string) error { *events = append(*events, "exec"); return nil },
	}, events
}
