package resolve

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/juty9026/terrapod/internal/chezmoi"
	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/reconcile"
	"github.com/juty9026/terrapod/internal/recovery"
	"github.com/juty9026/terrapod/internal/resource"
	"github.com/juty9026/terrapod/internal/resource/managedfiles"
	"github.com/juty9026/terrapod/internal/state"
)

type managedResolveClient struct {
	targets []chezmoi.Target
}

func (c *managedResolveClient) TargetState(context.Context) ([]chezmoi.Target, error) {
	return append([]chezmoi.Target(nil), c.targets...), nil
}

func (c *managedResolveClient) Diff(context.Context, []string) ([]byte, error) { return nil, nil }

func (c *managedResolveClient) ApplyTargets(context.Context, []string) error { return nil }

func (c *managedResolveClient) ApplyTargetsChecked(_ context.Context, expected []chezmoi.ExpectedTarget, check func(string) error) error {
	byPath := make(map[string]chezmoi.Target, len(c.targets))
	for _, target := range c.targets {
		byPath[target.Path] = target
	}
	for _, target := range expected {
		if err := check(target.Path); err != nil {
			return err
		}
		desired := byPath[target.Path]
		if desired.Kind != "file" {
			return errors.New("test client only supports files")
		}
		if err := os.WriteFile(target.Path, desired.Desired, 0o600); err != nil {
			return err
		}
	}
	return nil
}

type managedResolveFixture struct {
	service  *ManagedFiles
	store    *state.Store
	home     string
	stateDir string
	recovery string
	item     model.Resource
	client   *managedResolveClient
	adapter  *managedfiles.Adapter
	input    reconcile.ApplyInput
}

func newManagedResolveFixture(t *testing.T) *managedResolveFixture {
	t.Helper()
	home, stateDir := t.TempDir(), t.TempDir()
	store, err := state.Open(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	item := model.Resource{
		ID:            "dotfiles.home",
		Type:          model.ResourceManagedFiles,
		Provider:      "chezmoi",
		Package:       "home",
		VersionPolicy: model.VersionTracked,
		Metadata:      map[string]string{model.ManagedFilesScopeMetadataKey: "."},
	}
	client := &managedResolveClient{}
	recoveryRoot := filepath.Join(stateDir, "recovery")
	adapter := &managedfiles.Adapter{
		Client: client,
		State:  store,
		Home:   home,
		Backup: recovery.Backup{Root: recoveryRoot, Base: home},
	}
	registry := resource.NewRegistry()
	if err := registry.Register(item.Type, item.Provider, adapter); err != nil {
		t.Fatal(err)
	}
	engine := &reconcile.Engine{Registry: registry, State: store, LockDir: stateDir, EffectiveUID: func() int { return 501 }}
	fixture := &managedResolveFixture{
		store: store, home: home, stateDir: stateDir, recovery: recoveryRoot, item: item, client: client, adapter: adapter,
		input: reconcile.ApplyInput{
			Plan:                model.Plan{ID: "ordinary-plan", Release: "v2", Unavailable: map[model.ResourceID]string{}},
			CurrentResources:    []model.Resource{item},
			EnabledIDs:          []model.ResourceID{item.ID},
			HistoricalResources: map[model.ResourceID]reconcile.HistoricalResource{},
			CatalogDigest:       "signed-v2",
			Profile:             model.ProfileMacOSTerminal,
		},
	}
	fixture.service = &ManagedFiles{
		StateDir: stateDir,
		Rebuild: func(context.Context) (reconcile.ApplyInput, error) {
			return fixture.input, nil
		},
		Engine:       engine,
		EffectiveUID: func() int { return 501 },
	}
	return fixture
}

func (f *managedResolveFixture) target(path, value string) chezmoi.Target {
	return chezmoi.Target{Path: path, Kind: "file", Desired: []byte(value), Digest: managedfiles.Digest("file", []byte(value))}
}

func (f *managedResolveFixture) own(t *testing.T, paths map[string]string, digest string) {
	t.Helper()
	if err := f.store.PutOwnership(model.Ownership{ResourceID: f.item.ID, CatalogDigest: digest, Provider: f.item.Provider, Package: f.item.Package, Paths: paths}); err != nil {
		t.Fatal(err)
	}
}

func TestManagedFilesResolveDeclineLeavesConflictStateAndRecoveryUntouched(t *testing.T) {
	fixture := newManagedResolveFixture(t)
	path := filepath.Join(fixture.home, ".zshrc")
	if err := os.WriteFile(path, []byte("local"), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.client.targets = []chezmoi.Target{fixture.target(path, "desired")}
	fixture.own(t, map[string]string{path: "file:" + managedfiles.Digest("file", []byte("old-managed"))}, "signed-v1")

	var output bytes.Buffer
	result, err := fixture.service.Resolve(context.Background(), fixture.item.ID, strings.NewReader("\n"), &output)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	snapshot, _ := fixture.store.Snapshot()
	if result.Proceeded || string(got) != "local" || snapshot.ActiveJournal != nil || snapshot.Ownership[fixture.item.ID].CatalogDigest != "signed-v1" {
		t.Fatalf("decline changed state: result=%#v content=%q snapshot=%#v", result, got, snapshot)
	}
	if _, err := os.Lstat(fixture.recovery); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("recovery created on decline: %v", err)
	}
	if !strings.Contains(output.String(), "backup then accept desired") || !strings.Contains(output.String(), "[y/N]") {
		t.Fatalf("prompt=%q", output.String())
	}
}

func TestManagedFilesResolveBacksUpAcceptsDesiredAndRefreshesOwnership(t *testing.T) {
	fixture := newManagedResolveFixture(t)
	path := filepath.Join(fixture.home, ".zshrc")
	if err := os.WriteFile(path, []byte("local"), 0o640); err != nil {
		t.Fatal(err)
	}
	fixture.client.targets = []chezmoi.Target{fixture.target(path, "desired")}
	fixture.own(t, map[string]string{path: "file:" + managedfiles.Digest("file", []byte("old-managed"))}, "signed-v1")

	var output bytes.Buffer
	result, err := fixture.service.Resolve(context.Background(), fixture.item.ID, strings.NewReader("yes\n"), &output)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	snapshot, _ := fixture.store.Snapshot()
	owned := snapshot.Ownership[fixture.item.ID]
	if !result.Proceeded || string(got) != "desired" || owned.CatalogDigest != "signed-v2" || owned.Paths[path] != "file:"+managedfiles.Digest("file", []byte("desired")) || snapshot.ActiveJournal != nil {
		t.Fatalf("result=%#v content=%q ownership=%#v active=%#v", result, got, owned, snapshot.ActiveJournal)
	}
	backups, err := filepath.Glob(filepath.Join(fixture.recovery, "*", ".zshrc"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups=%v err=%v", backups, err)
	}
	backup, _ := os.ReadFile(backups[0])
	if string(backup) != "local" {
		t.Fatalf("backup=%q", backup)
	}
}

func TestManagedFilesResolveRemovesObsoleteConflictAndRefreshesPaths(t *testing.T) {
	fixture := newManagedResolveFixture(t)
	current := filepath.Join(fixture.home, "current")
	obsolete := filepath.Join(fixture.home, "obsolete")
	if err := os.WriteFile(current, []byte("desired"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(obsolete, []byte("local-edit"), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.client.targets = []chezmoi.Target{fixture.target(current, "desired")}
	fixture.own(t, map[string]string{
		current:  "file:" + managedfiles.Digest("file", []byte("desired")),
		obsolete: "file:" + managedfiles.Digest("file", []byte("old")),
	}, "signed-v1")

	var output bytes.Buffer
	result, err := fixture.service.Resolve(context.Background(), fixture.item.ID, strings.NewReader("y\n"), &output)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Proceeded || !strings.Contains(output.String(), "remove obsolete owned path") {
		t.Fatalf("result=%#v prompt=%q", result, output.String())
	}
	if _, err := os.Lstat(obsolete); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("obsolete still exists: %v", err)
	}
	snapshot, _ := fixture.store.Snapshot()
	if len(snapshot.Ownership[fixture.item.ID].Paths) != 1 || snapshot.Ownership[fixture.item.ID].Paths[current] == "" {
		t.Fatalf("ownership=%#v", snapshot.Ownership[fixture.item.ID])
	}
}

func TestManagedFilesResolveCrashRetryUsesExactActiveJournalWithoutReprompt(t *testing.T) {
	fixture := newManagedResolveFixture(t)
	path := filepath.Join(fixture.home, "managed")
	if err := os.WriteFile(path, []byte("local"), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.client.targets = []chezmoi.Target{fixture.target(path, "desired")}
	fixture.own(t, map[string]string{path: "file:" + managedfiles.Digest("file", []byte("old"))}, "signed-v1")
	afterManagedFilesMutation = func() error { return errors.New("simulated crash") }
	t.Cleanup(func() { afterManagedFilesMutation = nil })

	var first bytes.Buffer
	if _, err := fixture.service.Resolve(context.Background(), fixture.item.ID, strings.NewReader("yes\n"), &first); err == nil || !strings.Contains(err.Error(), "simulated crash") {
		t.Fatalf("first Resolve error=%v", err)
	}
	snapshot, _ := fixture.store.Snapshot()
	if snapshot.ActiveJournal == nil {
		t.Fatal("crash did not preserve active journal")
	}
	afterManagedFilesMutation = nil
	var second bytes.Buffer
	result, err := fixture.service.Resolve(context.Background(), fixture.item.ID, strings.NewReader(""), &second)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Proceeded || second.Len() != 0 {
		t.Fatalf("retry result=%#v prompt=%q", result, second.String())
	}
	snapshot, _ = fixture.store.Snapshot()
	if snapshot.ActiveJournal != nil || snapshot.Ownership[fixture.item.ID].CatalogDigest != "signed-v2" {
		t.Fatalf("retry snapshot=%#v", snapshot)
	}
}

func TestManagedFilesResolveHistoricalConflictPrunesOwnership(t *testing.T) {
	fixture := newManagedResolveFixture(t)
	path := filepath.Join(fixture.home, "legacy")
	if err := os.WriteFile(path, []byte("local-edit"), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.client.targets = nil
	fixture.input.CurrentResources = nil
	fixture.input.EnabledIDs = nil
	fixture.input.HistoricalResources[fixture.item.ID] = reconcile.HistoricalResource{Resource: fixture.item, CatalogDigest: "signed-v1"}
	fixture.own(t, map[string]string{path: "file:" + managedfiles.Digest("file", []byte("old"))}, "signed-v1")

	result, err := fixture.service.Resolve(context.Background(), fixture.item.ID, strings.NewReader("yes\n"), &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _ := fixture.store.Snapshot()
	if !result.Proceeded || snapshot.ActiveJournal != nil {
		t.Fatalf("result=%#v snapshot=%#v", result, snapshot)
	}
	if _, exists := snapshot.Ownership[fixture.item.ID]; exists {
		t.Fatalf("historical ownership remains: %#v", snapshot.Ownership)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("historical path remains: %v", err)
	}
}

func TestDispatcherFallsBackToPackageResolverForNonManagedResource(t *testing.T) {
	fixture := newManagedResolveFixture(t)
	called := false
	dispatcher := Dispatcher{
		ManagedFiles: fixture.service,
		Package: func(_ context.Context, id model.ResourceID, _ io.Reader, _ io.Writer) (Result, error) {
			called = id == "core.mise"
			return Result{Proceeded: true}, nil
		},
	}
	result, err := dispatcher.Resolve(context.Background(), "core.mise", strings.NewReader("yes\n"), &bytes.Buffer{})
	if err != nil || !result.Proceeded || !called {
		t.Fatalf("result=%#v called=%t err=%v", result, called, err)
	}
}

func TestManagedFilesResolveEditedConflictAfterCrashRepromptsAndDeclineIsSafe(t *testing.T) {
	fixture := newManagedResolveFixture(t)
	path := filepath.Join(fixture.home, "managed")
	if err := os.WriteFile(path, []byte("first-edit"), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.client.targets = []chezmoi.Target{fixture.target(path, "desired")}
	fixture.own(t, map[string]string{path: "file:" + managedfiles.Digest("file", []byte("old"))}, "signed-v1")
	afterManagedFilesMutation = func() error { return errors.New("simulated crash") }
	t.Cleanup(func() { afterManagedFilesMutation = nil })
	if _, err := fixture.service.Resolve(context.Background(), fixture.item.ID, strings.NewReader("yes\n"), &bytes.Buffer{}); err == nil {
		t.Fatal("first resolution did not crash")
	}
	afterManagedFilesMutation = nil
	before, _ := fixture.store.Snapshot()
	if err := os.WriteFile(path, []byte("second-edit"), 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	result, err := fixture.service.Resolve(context.Background(), fixture.item.ID, strings.NewReader("\n"), &output)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := fixture.store.Snapshot()
	got, _ := os.ReadFile(path)
	if result.Proceeded || output.Len() == 0 || string(got) != "second-edit" || before.ActiveJournal == nil || after.ActiveJournal == nil || before.ActiveJournal.ID != after.ActiveJournal.ID {
		t.Fatalf("declined retry changed state: result=%#v before=%#v after=%#v content=%q prompt=%q", result, before.ActiveJournal, after.ActiveJournal, got, output.String())
	}

	if _, err := fixture.service.Resolve(context.Background(), fixture.item.ID, strings.NewReader("yes\n"), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(path)
	after, _ = fixture.store.Snapshot()
	if string(got) != "desired" || after.ActiveJournal != nil {
		t.Fatalf("approved retry content=%q snapshot=%#v", got, after)
	}
	backups, _ := filepath.Glob(filepath.Join(fixture.recovery, "*", "managed"))
	if len(backups) != 2 {
		t.Fatalf("superseding approval backups=%v", backups)
	}
}

func TestManagedFilesResolvePartialResumeAllowsExactConflictSubsetWithoutPrompt(t *testing.T) {
	fixture := newManagedResolveFixture(t)
	first, second := filepath.Join(fixture.home, "first"), filepath.Join(fixture.home, "second")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("local"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	fixture.client.targets = []chezmoi.Target{fixture.target(first, "desired"), fixture.target(second, "desired")}
	fixture.own(t, map[string]string{
		first:  "file:" + managedfiles.Digest("file", []byte("old")),
		second: "file:" + managedfiles.Digest("file", []byte("old")),
	}, "signed-v1")
	snapshot, _ := fixture.store.Snapshot()
	conflicts, err := fixture.adapter.Conflicts(context.Background(), fixture.item, snapshot.Ownership[fixture.item.ID])
	if err != nil {
		t.Fatal(err)
	}
	plan := managedResolutionPlan(fixture.input, fixture.item, false, conflicts)
	if _, err := fixture.store.Begin(plan); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(first, []byte("desired"), 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	result, err := fixture.service.Resolve(context.Background(), fixture.item.ID, strings.NewReader(""), &output)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(second)
	if !result.Proceeded || output.Len() != 0 || string(got) != "desired" {
		t.Fatalf("result=%#v prompt=%q second=%q", result, output.String(), got)
	}
}

func TestManagedFilesResolveTamperedJournalBaselineFailsClosed(t *testing.T) {
	fixture := newManagedResolveFixture(t)
	path := filepath.Join(fixture.home, "managed")
	if err := os.WriteFile(path, []byte("local"), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.client.targets = []chezmoi.Target{fixture.target(path, "desired")}
	fixture.own(t, map[string]string{path: "file:" + managedfiles.Digest("file", []byte("old"))}, "signed-v1")
	snapshot, _ := fixture.store.Snapshot()
	conflicts, _ := fixture.adapter.Conflicts(context.Background(), fixture.item, snapshot.Ownership[fixture.item.ID])
	plan := managedResolutionPlan(fixture.input, fixture.item, false, conflicts)
	plan.Operations[0].ManagedFileAuthorization.Conflicts[0].Current.Digest = "tampered"
	if _, err := fixture.store.Begin(plan); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	_, err := fixture.service.Resolve(context.Background(), fixture.item.ID, strings.NewReader("yes\n"), &output)
	got, _ := os.ReadFile(path)
	if err == nil || output.Len() != 0 || string(got) != "local" {
		t.Fatalf("tampered journal err=%v prompt=%q content=%q", err, output.String(), got)
	}
}

func TestManagedFilesResolveDesiredCatalogChangeRepromptsAndSupersedes(t *testing.T) {
	fixture := newManagedResolveFixture(t)
	path := filepath.Join(fixture.home, "managed")
	if err := os.WriteFile(path, []byte("local"), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.client.targets = []chezmoi.Target{fixture.target(path, "desired-v1")}
	fixture.own(t, map[string]string{path: "file:" + managedfiles.Digest("file", []byte("old"))}, "signed-v1")
	snapshot, _ := fixture.store.Snapshot()
	conflicts, _ := fixture.adapter.Conflicts(context.Background(), fixture.item, snapshot.Ownership[fixture.item.ID])
	oldPlan := managedResolutionPlan(fixture.input, fixture.item, false, conflicts)
	oldJournal, err := fixture.store.Begin(oldPlan)
	if err != nil {
		t.Fatal(err)
	}
	fixture.input.CatalogDigest = "signed-v3"
	fixture.input.Plan.Release = "v3"
	fixture.client.targets = []chezmoi.Target{fixture.target(path, "desired-v2")}
	var output bytes.Buffer
	if _, err := fixture.service.Resolve(context.Background(), fixture.item.ID, strings.NewReader("yes\n"), &output); err != nil {
		t.Fatal(err)
	}
	snapshot, _ = fixture.store.Snapshot()
	got, _ := os.ReadFile(path)
	if output.Len() == 0 || string(got) != "desired-v2" || snapshot.ActiveJournal != nil || snapshot.Ownership[fixture.item.ID].CatalogDigest != "signed-v3" {
		t.Fatalf("prompt=%q content=%q snapshot=%#v", output.String(), got, snapshot)
	}
	backups, _ := filepath.Glob(filepath.Join(fixture.recovery, "*", "managed"))
	for _, backup := range backups {
		if strings.Contains(backup, oldJournal.ID) {
			t.Fatalf("superseded plan reused old journal backup: %v", backups)
		}
	}
}
