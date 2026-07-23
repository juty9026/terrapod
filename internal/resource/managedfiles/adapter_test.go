package managedfiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/juty9026/terrapod/internal/chezmoi"
	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/recovery"
	"github.com/juty9026/terrapod/internal/state"
)

type fakeClient struct {
	targets     []chezmoi.Target
	apply       func([]string) error
	beforeCheck func()
	diff        []byte
}

func (f *fakeClient) TargetState(context.Context) ([]chezmoi.Target, error) {
	return append([]chezmoi.Target(nil), f.targets...), nil
}
func (f *fakeClient) ApplyTargets(_ context.Context, paths []string) error {
	if f.apply != nil {
		return f.apply(paths)
	}
	return nil
}
func (f *fakeClient) ApplyTargetsChecked(_ context.Context, expected []chezmoi.ExpectedTarget, check func(string) error) error {
	if len(expected) == 0 {
		return nil
	}
	if f.beforeCheck != nil {
		f.beforeCheck()
	}
	paths := make([]string, len(expected))
	for index, target := range expected {
		paths[index] = target.Path
		if err := check(target.Path); err != nil {
			return err
		}
	}
	if f.apply != nil {
		return f.apply(paths)
	}
	return nil
}
func (f *fakeClient) Diff(context.Context, []string) ([]byte, error) {
	return append([]byte(nil), f.diff...), nil
}

func testAdapter(t *testing.T, targets []chezmoi.Target) (*Adapter, *fakeClient, *state.Store, string, model.Resource) {
	t.Helper()
	home, stateDir := t.TempDir(), t.TempDir()
	store, err := state.Open(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeClient{targets: targets}
	a := &Adapter{Client: client, State: store, Home: home, Backup: recovery.Backup{Root: filepath.Join(stateDir, "recovery"), Base: home}}
	item := model.Resource{ID: "dotfiles.home", Type: model.ResourceManagedFiles, Provider: "chezmoi", Package: "home", VersionPolicy: model.VersionTracked, Metadata: map[string]string{model.ManagedFilesScopeMetadataKey: "."}}
	return a, client, store, home, item
}

func target(path, kind, desired string) chezmoi.Target {
	return chezmoi.Target{Path: path, Kind: kind, Desired: []byte(desired), Digest: Digest(kind, []byte(desired))}
}
func begin(t *testing.T, store *state.Store, op model.Operation) {
	t.Helper()
	if _, err := store.Begin(model.Plan{ID: "p", Operations: []model.Operation{op}}); err != nil {
		t.Fatal(err)
	}
}
func operation(item model.Resource, kind model.OperationKind) model.Operation {
	return model.Operation{ID: "managed-" + string(kind), ResourceID: item.ID, Kind: kind, Provider: item.Provider, Package: item.Package}
}

func beginConflictResolution(t *testing.T, store *state.Store, item model.Resource, approved []Conflict) string {
	t.Helper()
	authorization := &model.ManagedFileAuthorization{Version: 2, CatalogDigest: "signed", Mode: "current", Resource: item, Conflicts: approved}
	journal, err := store.Begin(model.Plan{ID: "resolve-test", Operations: []model.Operation{{
		ID:                       "resolve-managed-files-" + string(item.ID),
		ResourceID:               item.ID,
		Kind:                     model.OperationUpgrade,
		Provider:                 item.Provider,
		Package:                  item.Package,
		ManagedFileAuthorization: authorization,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	return journal.ID
}

func TestAbsentCreateAndExactOwnershipObservation(t *testing.T) {
	a, client, store, home, item := testAdapter(t, nil)
	path := filepath.Join(home, ".zshrc")
	client.targets = []chezmoi.Target{target(path, "file", "desired")}
	obs, err := a.Inspect(context.Background(), item)
	if err != nil {
		t.Fatal(err)
	}
	ops, err := a.Plan(context.Background(), item, obs, model.Ownership{})
	if err != nil || len(ops) != 1 || ops[0].Kind != model.OperationInstall {
		t.Fatalf("Plan = %#v, %v", ops, err)
	}
	client.apply = func(paths []string) error { return os.WriteFile(paths[0], []byte("desired"), 0o600) }
	begin(t, store, ops[0])
	result := a.ExecuteResource(context.Background(), item, ops[0])
	if !result.Success {
		t.Fatal(result.Detail)
	}
	verified, err := a.Verify(context.Background(), item)
	if err != nil || verified.Paths[path] != "file:"+Digest("file", []byte("desired")) {
		t.Fatalf("Verify = %#v, %v", verified, err)
	}
}

func TestSignedScopeFiltersAggregateChezmoiTargets(t *testing.T) {
	a, client, _, home, item := testAdapter(t, nil)
	item.Metadata[model.ManagedFilesScopeMetadataKey] = ".config/one"
	one, two := filepath.Join(home, ".config", "one", "a"), filepath.Join(home, ".config", "two", "b")
	client.targets = []chezmoi.Target{target(one, "file", "one"), target(two, "file", "two")}
	observation, err := a.Inspect(context.Background(), item)
	if err != nil {
		t.Fatal(err)
	}
	if len(observation.Paths) != 1 || observation.Paths[one] == "" || observation.Paths[two] != "" {
		t.Fatalf("scoped paths=%#v", observation.Paths)
	}
}

func TestHistoricalScopePrunesItsReceiptWhileCurrentOtherTargetsExist(t *testing.T) {
	a, client, store, home, item := testAdapter(t, nil)
	item.Metadata[model.ManagedFilesScopeMetadataKey] = ".old"
	old := filepath.Join(home, ".old", "managed")
	if err := os.MkdirAll(filepath.Dir(old), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	client.targets = []chezmoi.Target{target(filepath.Join(home, ".current", "managed"), "file", "current")}
	owned := model.Ownership{ResourceID: item.ID, Paths: map[string]string{old: "file:" + Digest("file", []byte("old"))}}
	if err := store.PutOwnership(owned); err != nil {
		t.Fatal(err)
	}
	obs, err := a.Inspect(context.Background(), item)
	if err != nil || !obs.Healthy {
		t.Fatalf("Inspect=%#v,%v", obs, err)
	}
	ops, err := a.Plan(context.Background(), item, obs, owned)
	if err != nil || len(ops) != 1 || ops[0].Kind != model.OperationPrune {
		t.Fatalf("Plan=%#v,%v", ops, err)
	}
}

func TestIdenticalPreexistingFileAdoptsWithoutBackup(t *testing.T) {
	a, client, _, home, item := testAdapter(t, nil)
	path := filepath.Join(home, "same")
	if err := os.WriteFile(path, []byte("desired"), 0o600); err != nil {
		t.Fatal(err)
	}
	client.targets = []chezmoi.Target{target(path, "file", "desired")}
	obs, _ := a.Inspect(context.Background(), item)
	ops, err := a.Plan(context.Background(), item, obs, model.Ownership{})
	if err != nil || len(ops) != 1 || ops[0].Kind != model.OperationAdopt {
		t.Fatalf("Plan=%#v, %v", ops, err)
	}
}

func TestDifferingPreownershipBacksUpBeforeAdopt(t *testing.T) {
	a, client, store, home, item := testAdapter(t, nil)
	path := filepath.Join(home, "different")
	if err := os.WriteFile(path, []byte("local"), 0o640); err != nil {
		t.Fatal(err)
	}
	client.targets = []chezmoi.Target{target(path, "file", "desired")}
	obs, _ := a.Inspect(context.Background(), item)
	ops, err := a.Plan(context.Background(), item, obs, model.Ownership{})
	if err != nil || ops[0].Kind != model.OperationAdopt {
		t.Fatalf("Plan=%#v,%v", ops, err)
	}
	client.apply = func(paths []string) error {
		snapshot, _ := store.Snapshot()
		backup := filepath.Join(a.Backup.Root, snapshot.ActiveJournal.ID, "different")
		got, err := os.ReadFile(backup)
		if err != nil || string(got) != "local" {
			t.Fatalf("backup before apply=%q,%v", got, err)
		}
		return os.WriteFile(paths[0], []byte("desired"), 0o600)
	}
	begin(t, store, ops[0])
	if result := a.ExecuteResource(context.Background(), item, ops[0]); !result.Success {
		t.Fatal(result.Detail)
	}
}

func TestOwnedUpdateRequiresCurrentOwnedHash(t *testing.T) {
	a, client, _, home, item := testAdapter(t, nil)
	path := filepath.Join(home, "owned")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	client.targets = []chezmoi.Target{target(path, "file", "new")}
	owned := model.Ownership{ResourceID: item.ID, Paths: map[string]string{path: "file:" + Digest("file", []byte("old"))}}
	obs, _ := a.Inspect(context.Background(), item)
	ops, err := a.Plan(context.Background(), item, obs, owned)
	if err != nil || len(ops) != 1 || ops[0].Kind != model.OperationUpgrade {
		t.Fatalf("Plan=%#v,%v", ops, err)
	}
	if err := os.WriteFile(path, []byte("local edit"), 0o600); err != nil {
		t.Fatal(err)
	}
	obs, _ = a.Inspect(context.Background(), item)
	if _, err := a.Plan(context.Background(), item, obs, owned); err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("expected conflict, got %v", err)
	}
}

func TestExecuteRechecksOwnedHashAfterPlanning(t *testing.T) {
	a, client, store, home, item := testAdapter(t, nil)
	path := filepath.Join(home, "owned")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	client.targets = []chezmoi.Target{target(path, "file", "new")}
	owned := model.Ownership{ResourceID: item.ID, Paths: map[string]string{path: "file:" + Digest("file", []byte("old"))}}
	if err := store.PutOwnership(owned); err != nil {
		t.Fatal(err)
	}
	obs, _ := a.Inspect(context.Background(), item)
	ops, err := a.Plan(context.Background(), item, obs, owned)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("late local edit"), 0o600); err != nil {
		t.Fatal(err)
	}
	called := false
	client.apply = func([]string) error { called = true; return nil }
	begin(t, store, ops[0])
	result := a.ExecuteResource(context.Background(), item, ops[0])
	if result.Success || called || !strings.Contains(result.Detail, "changed after planning") {
		t.Fatalf("Execute = %#v, apply=%v", result, called)
	}
}

func TestMixedOwnedAndNewDifferingPathBacksUpOnlyNewPath(t *testing.T) {
	a, client, store, home, item := testAdapter(t, nil)
	ownedPath, newPath := filepath.Join(home, "owned"), filepath.Join(home, "new")
	if err := os.WriteFile(ownedPath, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newPath, []byte("local"), 0o640); err != nil {
		t.Fatal(err)
	}
	client.targets = []chezmoi.Target{target(ownedPath, "file", "updated"), target(newPath, "file", "desired")}
	owned := model.Ownership{ResourceID: item.ID, Paths: map[string]string{ownedPath: "file:" + Digest("file", []byte("old"))}}
	if err := store.PutOwnership(owned); err != nil {
		t.Fatal(err)
	}
	op := operation(item, model.OperationUpgrade)
	begin(t, store, op)
	client.apply = func(paths []string) error {
		snapshot, _ := store.Snapshot()
		backup, err := os.ReadFile(filepath.Join(a.Backup.Root, snapshot.ActiveJournal.ID, "new"))
		if err != nil || string(backup) != "local" {
			t.Fatalf("backup=%q,%v", backup, err)
		}
		for _, path := range paths {
			desired := "updated"
			if path == newPath {
				desired = "desired"
			}
			if err := os.WriteFile(path, []byte(desired), 0o600); err != nil {
				return err
			}
		}
		return nil
	}
	if result := a.ExecuteResource(context.Background(), item, op); !result.Success {
		t.Fatal(result.Detail)
	}
}

func TestExecuteRechecksHashInsideStagedApplyImmediatelyBeforeMutation(t *testing.T) {
	a, client, store, home, item := testAdapter(t, nil)
	path := filepath.Join(home, "owned")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	client.targets = []chezmoi.Target{target(path, "file", "new")}
	owned := model.Ownership{ResourceID: item.ID, Paths: map[string]string{path: "file:" + Digest("file", []byte("old"))}}
	if err := store.PutOwnership(owned); err != nil {
		t.Fatal(err)
	}
	op := operation(item, model.OperationUpgrade)
	begin(t, store, op)
	client.beforeCheck = func() {
		if err := os.WriteFile(path, []byte("late edit"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	called := false
	client.apply = func([]string) error { called = true; return nil }
	result := a.ExecuteResource(context.Background(), item, op)
	if result.Success || called || !strings.Contains(result.Detail, "changed immediately before mutation") {
		t.Fatalf("Execute = %#v, apply=%v", result, called)
	}
}

func TestObsoleteUnchangedPrunesAndPreservesModified(t *testing.T) {
	for _, modified := range []bool{false, true} {
		t.Run(map[bool]string{false: "unchanged", true: "modified"}[modified], func(t *testing.T) {
			a, _, store, home, item := testAdapter(t, nil)
			path := filepath.Join(home, "obsolete")
			if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
				t.Fatal(err)
			}
			owned := model.Ownership{ResourceID: item.ID, Paths: map[string]string{path: "file:" + Digest("file", []byte("old"))}}
			if err := store.PutOwnership(owned); err != nil {
				t.Fatal(err)
			}
			if modified {
				if err := os.WriteFile(path, []byte("edit"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			obs, _ := a.Inspect(context.Background(), item)
			ops, err := a.Plan(context.Background(), item, obs, owned)
			if modified {
				if err == nil {
					t.Fatal("modified obsolete path was not a conflict")
				}
				return
			}
			if err != nil || len(ops) != 1 || ops[0].Kind != model.OperationPrune {
				t.Fatalf("Plan=%#v,%v", ops, err)
			}
			begin(t, store, ops[0])
			if result := a.ExecuteResource(context.Background(), item, ops[0]); !result.Success {
				t.Fatal(result.Detail)
			}
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("obsolete still present: %v", err)
			}
		})
	}
}

func TestMissingObsoleteReceiptStillSchedulesOwnershipRefresh(t *testing.T) {
	a, client, _, home, item := testAdapter(t, nil)
	current, obsolete := filepath.Join(home, "current"), filepath.Join(home, "obsolete")
	if err := os.WriteFile(current, []byte("desired"), 0o600); err != nil {
		t.Fatal(err)
	}
	client.targets = []chezmoi.Target{target(current, "file", "desired")}
	owned := model.Ownership{ResourceID: item.ID, Paths: map[string]string{current: "file:" + Digest("file", []byte("desired")), obsolete: "file:" + Digest("file", []byte("old"))}}
	obs, _ := a.Inspect(context.Background(), item)
	ops, err := a.Plan(context.Background(), item, obs, owned)
	if err != nil || len(ops) != 1 || ops[0].Kind != model.OperationUpgrade {
		t.Fatalf("Plan=%#v,%v", ops, err)
	}
}

func TestPruneRemovesOnlyRecordedPathsAndOnlyEmptyParents(t *testing.T) {
	a, _, store, home, item := testAdapter(t, nil)
	dir := filepath.Join(home, ".config", "app")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "managed")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	owned := model.Ownership{ResourceID: item.ID, Paths: map[string]string{path: "file:" + Digest("file", []byte("old"))}}
	if err := store.PutOwnership(owned); err != nil {
		t.Fatal(err)
	}
	op := operation(item, model.OperationPrune)
	begin(t, store, op)
	if result := a.ExecuteResource(context.Background(), item, op); !result.Success {
		t.Fatal(result.Detail)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("empty directory preserved: %v", err)
	}

	dir = filepath.Join(home, "keep")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(dir, "managed")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "user"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	owned.Paths = map[string]string{path: "file:" + Digest("file", []byte("old"))}
	if err := store.PutOwnership(owned); err != nil {
		t.Fatal(err)
	}
	if result := a.ExecuteResource(context.Background(), item, op); !result.Success {
		t.Fatal(result.Detail)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("non-empty directory removed: %v", err)
	}
}

func TestHistoricalInspectAuthorizesOnlyExactOwnedPrune(t *testing.T) {
	a, _, store, home, item := testAdapter(t, nil)
	path := filepath.Join(home, "obsolete")
	if err := os.WriteFile(path, []byte("owned"), 0o600); err != nil {
		t.Fatal(err)
	}
	owned := model.Ownership{ResourceID: item.ID, Paths: map[string]string{path: "file:" + Digest("file", []byte("owned"))}}
	if err := store.PutOwnership(owned); err != nil {
		t.Fatal(err)
	}
	observation, err := a.Inspect(context.Background(), item)
	if err != nil || !observation.Present || !observation.Healthy {
		t.Fatalf("Inspect exact historical = %#v, %v", observation, err)
	}
	if err := os.WriteFile(path, []byte("edited"), 0o600); err != nil {
		t.Fatal(err)
	}
	observation, err = a.Inspect(context.Background(), item)
	if err != nil || observation.Healthy {
		t.Fatalf("Inspect modified historical = %#v, %v", observation, err)
	}
}

func TestSymlinkReplacementIsConflict(t *testing.T) {
	a, client, _, home, item := testAdapter(t, nil)
	path := filepath.Join(home, "link")
	if err := os.Symlink("old", path); err != nil {
		t.Fatal(err)
	}
	client.targets = []chezmoi.Target{target(path, "file", "desired")}
	obs, _ := a.Inspect(context.Background(), item)
	if _, err := a.Plan(context.Background(), item, obs, model.Ownership{}); err == nil {
		t.Fatal("symlink replacement accepted")
	}
}

func TestExactOwnedSymlinkCanUpdateButLocallyEditedCannot(t *testing.T) {
	a, client, store, home, item := testAdapter(t, nil)
	path := filepath.Join(home, "link")
	if err := os.Symlink("old", path); err != nil {
		t.Fatal(err)
	}
	client.targets = []chezmoi.Target{target(path, "symlink", "new")}
	owned := model.Ownership{ResourceID: item.ID, Paths: map[string]string{path: "symlink:" + Digest("symlink", []byte("old"))}}
	if err := store.PutOwnership(owned); err != nil {
		t.Fatal(err)
	}
	obs, _ := a.Inspect(context.Background(), item)
	ops, err := a.Plan(context.Background(), item, obs, owned)
	if err != nil || len(ops) != 1 {
		t.Fatalf("Plan=%#v,%v", ops, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("local", path); err != nil {
		t.Fatal(err)
	}
	obs, _ = a.Inspect(context.Background(), item)
	if _, err := a.Plan(context.Background(), item, obs, owned); err == nil {
		t.Fatal("locally edited symlink accepted")
	}
}

func TestPruneRejectsSymlinkParentEvenWhenOutsideFileHashMatches(t *testing.T) {
	a, _, store, home, item := testAdapter(t, nil)
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "managed")
	if err := os.WriteFile(outsideFile, []byte("owned"), 0o600); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(home, "parent")
	if err := os.Symlink(outside, parent); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "managed")
	owned := model.Ownership{ResourceID: item.ID, Paths: map[string]string{path: "file:" + Digest("file", []byte("owned"))}}
	if err := store.PutOwnership(owned); err != nil {
		t.Fatal(err)
	}
	op := operation(item, model.OperationPrune)
	begin(t, store, op)
	result := a.ExecuteResource(context.Background(), item, op)
	if result.Success || !strings.Contains(result.Detail, "not a real directory") {
		t.Fatalf("Execute = %#v", result)
	}
	if _, err := os.Stat(outsideFile); err != nil {
		t.Fatalf("outside file was removed: %v", err)
	}
}

func TestPruneRejectsDeterministicHomeAndParentSwaps(t *testing.T) {
	for _, rootSwap := range []bool{true, false} {
		t.Run(map[bool]string{true: "home", false: "parent"}[rootSwap], func(t *testing.T) {
			a, _, store, home, item := testAdapter(t, nil)
			parent := filepath.Join(home, "parent")
			if err := os.Mkdir(parent, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(parent, "managed")
			if err := os.WriteFile(path, []byte("owned"), 0o600); err != nil {
				t.Fatal(err)
			}
			outside := t.TempDir()
			outsideFile := filepath.Join(outside, "managed")
			if err := os.WriteFile(outsideFile, []byte("owned"), 0o600); err != nil {
				t.Fatal(err)
			}
			moved := filepath.Join(t.TempDir(), "moved")
			owned := model.Ownership{ResourceID: item.ID, Paths: map[string]string{path: "file:" + Digest("file", []byte("owned"))}}
			if err := store.PutOwnership(owned); err != nil {
				t.Fatal(err)
			}
			op := operation(item, model.OperationPrune)
			begin(t, store, op)
			beforeManagedRemove = func() {
				beforeManagedRemove = nil
				target := parent
				if rootSwap {
					target = home
				}
				if err := os.Rename(target, moved); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, target); err != nil {
					t.Fatal(err)
				}
			}
			t.Cleanup(func() { beforeManagedRemove = nil })
			result := a.ExecuteResource(context.Background(), item, op)
			if result.Success {
				t.Fatalf("swap prune succeeded: %#v", result)
			}
			if _, err := os.Stat(outsideFile); err != nil {
				t.Fatalf("outside file removed: %v", err)
			}
		})
	}
}

func TestBackupFailureAbortsApply(t *testing.T) {
	a, client, store, home, item := testAdapter(t, nil)
	path := filepath.Join(home, "file")
	if err := os.WriteFile(path, []byte("local"), 0o600); err != nil {
		t.Fatal(err)
	}
	client.targets = []chezmoi.Target{target(path, "file", "desired")}
	a.Backup.Root = filepath.Join(home, "not-a-directory")
	if err := os.WriteFile(a.Backup.Root, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	called := false
	client.apply = func([]string) error { called = true; return nil }
	op := operation(item, model.OperationAdopt)
	begin(t, store, op)
	result := a.ExecuteResource(context.Background(), item, op)
	if result.Success || called {
		t.Fatalf("backup failure result=%#v apply=%v", result, called)
	}
}

func TestResolveConflictBacksUpThenAcceptsDesired(t *testing.T) {
	a, client, store, home, item := testAdapter(t, nil)
	path := filepath.Join(home, "conflict")
	if err := os.WriteFile(path, []byte("local edit"), 0o600); err != nil {
		t.Fatal(err)
	}
	client.targets = []chezmoi.Target{target(path, "file", "desired")}
	if err := store.PutOwnership(model.Ownership{ResourceID: item.ID, Paths: map[string]string{path: "file:" + Digest("file", []byte("old"))}}); err != nil {
		t.Fatal(err)
	}
	approved := []Conflict{{Path: path, Current: model.ManagedFilePathState{Exists: true, Kind: "file", Digest: Digest("file", []byte("local edit"))}, Desired: model.ManagedFilePathState{Exists: true, Kind: "file", Digest: Digest("file", []byte("desired"))}}}
	journal := beginConflictResolution(t, store, item, approved)
	client.apply = func(paths []string) error {
		backup, err := os.ReadFile(filepath.Join(a.Backup.Root, journal, "conflict"))
		if err != nil || string(backup) != "local edit" {
			t.Fatalf("resolve backup = %q, %v", backup, err)
		}
		return os.WriteFile(paths[0], []byte("desired"), 0o600)
	}
	if err := a.ResolveConflict(context.Background(), item, journal, approved); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "desired" {
		t.Fatalf("resolved target = %q, %v", got, err)
	}
}

func TestResolveConflictBacksUpAndDeletesModifiedObsoleteOwnedPath(t *testing.T) {
	a, client, store, home, item := testAdapter(t, nil)
	obsolete := filepath.Join(home, "obsolete")
	current := filepath.Join(home, "current")
	if err := os.WriteFile(obsolete, []byte("local edit"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(current, []byte("local current"), 0o600); err != nil {
		t.Fatal(err)
	}
	client.targets = []chezmoi.Target{target(current, "file", "desired")}
	owned := model.Ownership{ResourceID: item.ID, Paths: map[string]string{obsolete: "file:" + Digest("file", []byte("old"))}}
	if err := store.PutOwnership(owned); err != nil {
		t.Fatal(err)
	}
	client.apply = func(paths []string) error { return os.WriteFile(paths[0], []byte("desired"), 0o600) }
	approved, err := a.Conflicts(context.Background(), item, owned)
	if err != nil {
		t.Fatal(err)
	}
	journal := beginConflictResolution(t, store, item, approved)
	if err := a.ResolveConflict(context.Background(), item, journal, approved); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(obsolete); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("obsolete remains: %v", err)
	}
	backup, err := os.ReadFile(filepath.Join(a.Backup.Root, journal, "obsolete"))
	if err != nil || string(backup) != "local edit" {
		t.Fatalf("backup=%q,%v", backup, err)
	}
}

func TestResolveConflictRejectsNewConflictBeforeFirstMutation(t *testing.T) {
	a, client, store, home, item := testAdapter(t, nil)
	first, newlyConflicting := filepath.Join(home, "first"), filepath.Join(home, "new-conflict")
	if err := os.WriteFile(first, []byte("local"), 0o600); err != nil {
		t.Fatal(err)
	}
	client.targets = []chezmoi.Target{target(first, "file", "desired")}
	owned := model.Ownership{ResourceID: item.ID, Paths: map[string]string{
		first:            "file:" + Digest("file", []byte("old")),
		newlyConflicting: "file:" + Digest("file", []byte("old")),
	}}
	if err := store.PutOwnership(owned); err != nil {
		t.Fatal(err)
	}
	approved, err := a.Conflicts(context.Background(), item, owned)
	if err != nil {
		t.Fatal(err)
	}
	beforeManagedConflictMutation = func() {
		beforeManagedConflictMutation = nil
		if err := os.WriteFile(newlyConflicting, []byte("new edit"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { beforeManagedConflictMutation = nil })
	client.apply = func(paths []string) error {
		t.Fatalf("mutation started for %v", paths)
		return nil
	}
	journal := beginConflictResolution(t, store, item, approved)
	if err := a.ResolveConflict(context.Background(), item, journal, approved); err == nil {
		t.Fatal("new conflict was silently accepted")
	}
	got, _ := os.ReadFile(first)
	if string(got) != "local" {
		t.Fatalf("approved path changed before complete re-inventory: %q", got)
	}
}
