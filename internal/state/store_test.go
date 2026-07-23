package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/release"
)

func TestOpenCreatesEmptySnapshot(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	got, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if got.Ownership == nil || len(got.Ownership) != 0 {
		t.Fatalf("ownership=%#v, want empty non-nil map", got.Ownership)
	}
	if got.ActiveJournal != nil {
		t.Fatalf("active journal=%#v, want nil", got.ActiveJournal)
	}
	if got.AppliedCatalogs == nil || len(got.AppliedCatalogs) != 0 {
		t.Fatalf("applied catalogs=%#v, want empty non-nil slice", got.AppliedCatalogs)
	}
	if _, err := os.Stat(filepath.Join(dir, "snapshot.json")); err != nil {
		t.Fatalf("snapshot was not created: %v", err)
	}
}

func TestOpenRejectsSnapshotWithNonPrivateMode(t *testing.T) {
	dir := t.TempDir()
	if _, err := Open(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(dir, snapshotFilename), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir); err == nil {
		t.Fatal("snapshot with mode 0644 was accepted")
	}
}

func TestPrivateJSONReadRejectsPathSwapAroundOpen(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(func(string) error)
	}{
		{name: "before open", set: func(hook func(string) error) { afterPrivateLstat = hook }},
		{name: "after open", set: func(hook func(string) error) { afterPrivateOpen = hook }},
		{name: "after decode", set: func(hook func(string) error) { afterPrivateDecode = hook }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			store, err := Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, snapshotFilename)
			backup := path + ".backup"
			called := false
			t.Cleanup(func() { afterPrivateLstat, afterPrivateOpen, afterPrivateDecode = nil, nil, nil })
			tc.set(func(observed string) error {
				if observed != path || called {
					return nil
				}
				called = true
				if err := os.Rename(path, backup); err != nil {
					return err
				}
				return os.Symlink(backup, path)
			})
			if _, err := store.Snapshot(); err == nil {
				t.Fatal("path swap was accepted")
			}
			if !called {
				t.Fatal("swap hook was not called")
			}
		})
	}
}

func TestPersistenceReadersRejectNonPrivateMode(t *testing.T) {
	for _, name := range []string{"snapshot", "journal", "update", "trust"} {
		t.Run(name, func(t *testing.T) {
			store, err := Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			journal, err := store.Begin(model.Plan{ID: "plan", Release: "1.2.3"})
			if err != nil {
				t.Fatal(err)
			}
			record := UpdateRecord{JournalID: journal.ID, PlanID: "plan", Version: "1.2.3", CatalogDigest: strings.Repeat("a", 64), ReleaseDigest: strings.Repeat("b", 64), TrustedKeys: map[string]string{}, TrustProvenance: map[string]string{}, TrustProofDigest: strings.Repeat("d", 64)}
			if err := store.PutUpdate(record); err != nil {
				t.Fatal(err)
			}
			if err := store.PutTrustProofs([]release.TrustProof{{Manifest: []byte("manifest"), Signature: []byte("signature")}}); err != nil {
				t.Fatal(err)
			}
			path := map[string]string{
				"snapshot": store.snapshotPath(),
				"journal":  store.journalPath(journal.ID),
				"update":   filepath.Join(store.dir, updateDirname, journal.ID+".json"),
				"trust":    filepath.Join(store.dir, trustedKeysFilename),
			}[name]
			if err := os.Chmod(path, 0o640); err != nil {
				t.Fatal(err)
			}
			var readErr error
			switch name {
			case "snapshot":
				_, readErr = store.Snapshot()
			case "journal":
				_, readErr = store.Journal(journal.ID)
			case "update":
				_, readErr = store.Update(journal.ID)
			case "trust":
				_, readErr = store.TrustProofs()
			}
			if readErr == nil {
				t.Fatalf("%s with mode 0640 was accepted", name)
			}
		})
	}
}

func TestBeginOrResumeUsesExactActivePlanAndReplacesDifferentPlan(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first := model.Plan{ID: "first", Release: "v1", Operations: []model.Operation{{ID: "one", ResourceID: "core.alpha"}}, Unavailable: map[model.ResourceID]string{}}
	journal, resumed, err := store.BeginOrResume(first)
	if err != nil || resumed {
		t.Fatalf("begin=%#v resumed=%v err=%v", journal, resumed, err)
	}
	same, didResume, err := store.BeginOrResume(first)
	if err != nil || !didResume || same.ID != journal.ID {
		t.Fatalf("resume=%#v resumed=%v err=%v", same, didResume, err)
	}
	second := model.Plan{ID: "second", Release: "v2", Unavailable: map[model.ResourceID]string{}}
	replacement, didResume, err := store.BeginOrResume(second)
	if err != nil || didResume || replacement.ID == journal.ID {
		t.Fatalf("replace=%#v resumed=%v err=%v", replacement, didResume, err)
	}
	snapshot, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ActiveJournal == nil || snapshot.ActiveJournal.ID != replacement.ID || snapshot.ActiveJournal.Plan.ID != "second" {
		t.Fatalf("snapshot=%#v", snapshot)
	}
	prior, err := store.readJournal(journal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if prior.Status != "superseded" || prior.SupersededBy != replacement.ID {
		t.Fatalf("prior journal=%#v", prior)
	}
}

func TestReplacementCrashRepairsToExactlyOneAuthorizedActiveJournal(t *testing.T) {
	for _, phase := range []string{"new-journal", "snapshot", "old-superseded"} {
		t.Run(phase, func(t *testing.T) {
			dir := t.TempDir()
			store, err := Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			first := model.Plan{ID: "first"}
			second := model.Plan{ID: "second"}
			if _, _, err := store.BeginOrResume(first); err != nil {
				t.Fatal(err)
			}
			crash := func() error { return errors.New("crash") }
			switch phase {
			case "new-journal":
				afterReplacementJournal = crash
			case "snapshot":
				afterReplacementSnapshot = crash
			case "old-superseded":
				afterReplacementSuperseded = crash
			}
			t.Cleanup(func() { afterReplacementJournal, afterReplacementSnapshot, afterReplacementSuperseded = nil, nil, nil })
			_, _, _ = store.BeginOrResume(second)
			afterReplacementJournal, afterReplacementSnapshot, afterReplacementSuperseded = nil, nil, nil
			reopened, err := Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := reopened.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			activeCount := 0
			entries, _ := os.ReadDir(filepath.Join(dir, journalDirname))
			for _, entry := range entries {
				journal, err := reopened.readJournal(strings.TrimSuffix(entry.Name(), ".json"))
				if err != nil {
					t.Fatal(err)
				}
				if journal.Status == "active" {
					activeCount++
				}
			}
			if activeCount != 1 || snapshot.ActiveJournal == nil {
				t.Fatalf("activeCount=%d snapshot=%#v", activeCount, snapshot)
			}
			want := "first"
			if phase != "new-journal" {
				want = "second"
			}
			if snapshot.ActiveJournal.Plan.ID != want {
				t.Fatalf("active plan=%q want=%q", snapshot.ActiveJournal.Plan.ID, want)
			}
		})
	}
}

func TestBeginOrResumeRejectsEmptyPlanID(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.BeginOrResume(model.Plan{}); err == nil {
		t.Fatal("empty plan ID accepted")
	}
}

func TestBeginOrResumeRecoversCompletedActivePointer(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	plan := model.Plan{ID: "plan", Operations: []model.Operation{{ID: "install", ResourceID: "core.alpha"}}}
	journal, err := store.Begin(plan)
	if err != nil {
		t.Fatal(err)
	}
	afterJournalCompleted = func() error { return errors.New("crash after completed journal") }
	t.Cleanup(func() { afterJournalCompleted = nil })
	if err := store.Complete(journal.ID); err == nil {
		t.Fatal("crash hook did not fail")
	}
	afterJournalCompleted = nil
	replacement, resumed, err := store.BeginOrResume(plan)
	if err != nil || resumed || replacement.ID == journal.ID {
		t.Fatalf("recovery=%#v resumed=%v err=%v", replacement, resumed, err)
	}
}

func TestRecordUpsertsOperationResultOnResume(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	plan := model.Plan{ID: "plan", Operations: []model.Operation{{ID: "install", ResourceID: "core.alpha"}}}
	journal, err := store.Begin(plan)
	if err != nil {
		t.Fatal(err)
	}
	first := model.OperationResult{OperationID: "install", ResourceID: "core.alpha", Detail: "interrupted"}
	second := model.OperationResult{OperationID: "install", ResourceID: "core.alpha", Success: true, Detail: "verified"}
	if err := store.Record(first); err != nil {
		t.Fatal(err)
	}
	legacy, err := store.readJournal(journal.ID)
	if err != nil {
		t.Fatal(err)
	}
	legacy.Results = append(legacy.Results, first)
	if err := store.writeJournal(legacy); err != nil {
		t.Fatal(err)
	}
	if err := store.Record(second); err != nil {
		t.Fatal(err)
	}
	got, err := store.readJournal(journal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Results) != 1 || !got.Results[0].Success || got.Results[0].Detail != "verified" {
		t.Fatalf("results=%#v", got.Results)
	}
	if err := store.Record(model.OperationResult{OperationID: "forged", ResourceID: "core.alpha"}); err == nil {
		t.Fatal("unknown operation recorded")
	}
}

func TestPutAndDeleteOwnershipPersistAtomically(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ownership := model.Ownership{
		ResourceID:    "core.ripgrep",
		CatalogDigest: "sha256:digest",
		Provider:      "homebrew",
		Package:       "ripgrep",
		Paths:         map[string]string{"binary": "/opt/homebrew/bin/rg"},
		PriorValues:   map[string]json.RawMessage{"enabled": json.RawMessage("true")},
	}
	if err := store.PutOwnership(ownership); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := reopened.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if got := snapshot.Ownership[ownership.ResourceID]; got.Package != ownership.Package || got.CatalogDigest != ownership.CatalogDigest {
		t.Fatalf("ownership=%#v, want %#v", got, ownership)
	}
	assertNoTemporaryFiles(t, dir)

	if err := reopened.DeleteOwnership(ownership.ResourceID); err != nil {
		t.Fatal(err)
	}
	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err = again.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := snapshot.Ownership[ownership.ResourceID]; ok {
		t.Fatalf("ownership %q survived deletion", ownership.ResourceID)
	}
}

func TestJournalBeginRecordCompletePersists(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	plan := model.Plan{
		ID:      "plan-1",
		Release: "2026.07.22",
		Operations: []model.Operation{{
			ID:         "install-ripgrep",
			ResourceID: "core.ripgrep",
			Kind:       model.OperationInstall,
			Detail:     "install declared package",
		}},
	}
	journal, err := store.Begin(plan)
	if err != nil {
		t.Fatal(err)
	}
	if journal.ID == "" || journal.Status != "active" || !journal.StartedAt.Equal(journal.StartedAt.UTC()) {
		t.Fatalf("unexpected journal: %#v", journal)
	}
	result := model.OperationResult{
		OperationID: "install-ripgrep",
		ResourceID:  "core.ripgrep",
		Success:     true,
		Detail:      "installed version 14.1.1",
		FinishedAt:  time.Date(2026, 7, 22, 1, 2, 3, 0, time.UTC),
	}
	if err := store.Record(result); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := reopened.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ActiveJournal == nil || len(snapshot.ActiveJournal.Results) != 1 {
		t.Fatalf("interrupted journal was not reloaded: %#v", snapshot.ActiveJournal)
	}
	if got := snapshot.ActiveJournal.Results[0]; got.OperationID != result.OperationID || !got.Success {
		t.Fatalf("result=%#v, want %#v", got, result)
	}

	if err := reopened.Complete(journal.ID); err != nil {
		t.Fatal(err)
	}
	completedBytes, err := os.ReadFile(filepath.Join(dir, "journals", journal.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var completed persistedJournal
	if err := json.Unmarshal(completedBytes, &completed); err != nil {
		t.Fatal(err)
	}
	if completed.Status != "completed" {
		t.Fatalf("journal status=%q, want completed", completed.Status)
	}
	snapshot, err = reopened.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ActiveJournal != nil {
		t.Fatalf("active journal=%#v, want nil", snapshot.ActiveJournal)
	}
}

func TestSupersedePreservesHistoricalJournal(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := store.Begin(model.Plan{ID: "old-plan", Release: "old"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Supersede(journal.ID, "replacement-journal"); err != nil {
		t.Fatal(err)
	}

	b, err := os.ReadFile(filepath.Join(dir, "journals", journal.ID+".json"))
	if err != nil {
		t.Fatalf("superseded journal was not preserved: %v", err)
	}
	var got persistedJournal
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Status != "superseded" || got.SupersededBy != "replacement-journal" {
		t.Fatalf("superseded journal=%#v", got)
	}
	snapshot, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ActiveJournal != nil {
		t.Fatalf("active journal=%#v, want nil", snapshot.ActiveJournal)
	}
}

func TestStatePermissionsAndPersistedFacts(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := store.Begin(model.Plan{
		ID:      "facts-only",
		Release: "2026.07.22",
		Operations: []model.Operation{{
			ID:         "verify-ripgrep",
			ResourceID: "core.ripgrep",
			Kind:       model.OperationVerify,
			Detail:     "package is present",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{dir, filepath.Join(dir, "journals")} {
		assertMode(t, path, 0o700)
	}
	for _, path := range []string{filepath.Join(dir, "snapshot.json"), filepath.Join(dir, "journals", journal.ID+".json")} {
		assertMode(t, path, 0o600)
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		lower := strings.ToLower(string(b))
		if strings.Contains(lower, "\"command\"") || strings.Contains(lower, "\"commands\"") || strings.Contains(lower, "\"argv\"") {
			t.Fatalf("%s persisted an executable command field: %s", path, b)
		}
	}
}

func TestBeginRejectsExistingActiveJournal(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Begin(model.Plan{ID: "first"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Begin(model.Plan{ID: "second"}); err == nil {
		t.Fatal("Begin succeeded while another journal was active")
	}
}

func TestJournalIDsAreUniqueSafePathComponents(t *testing.T) {
	seen := make(map[string]struct{})
	for range 100 {
		id, err := newJournalID()
		if err != nil {
			t.Fatal(err)
		}
		if !journalIDPattern.MatchString(id) || filepath.Base(id) != id {
			t.Fatalf("unsafe journal ID %q", id)
		}
		if _, duplicate := seen[id]; duplicate {
			t.Fatalf("duplicate journal ID %q", id)
		}
		seen[id] = struct{}{}
	}
}

func TestConcurrentOwnershipUpdatesRemainValid(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	var workers sync.WaitGroup
	for i := range 20 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			id := model.ResourceID("core.resource" + string(rune('a'+i)))
			if err := store.PutOwnership(model.Ownership{ResourceID: id, Package: string(id)}); err != nil {
				t.Errorf("PutOwnership(%q): %v", id, err)
			}
		}()
	}
	workers.Wait()

	snapshot, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Ownership) != 20 {
		t.Fatalf("ownership count=%d, want 20", len(snapshot.Ownership))
	}
}

func assertNoTemporaryFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".tmp-") {
			t.Fatalf("temporary file remained after atomic update: %s", entry.Name())
		}
	}
}

func TestUpdateRecordIsBoundToActiveJournal(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	journal, err := store.Begin(model.Plan{ID: "plan", Release: "1.2.3"})
	if err != nil {
		t.Fatal(err)
	}
	record := UpdateRecord{JournalID: journal.ID, PlanID: "plan", Version: "1.2.3", CatalogDigest: strings.Repeat("a", 64), ReleaseDigest: strings.Repeat("b", 64), TrustedKeys: map[string]string{"next": strings.Repeat("0", 64)}, TrustProvenance: map[string]string{"next": strings.Repeat("c", 64)}, TrustProofDigest: strings.Repeat("d", 64)}
	if err := store.PutUpdate(record); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkUpdateActivated(journal.ID); err != nil {
		t.Fatal(err)
	}
	got, err := store.Update(journal.ID)
	if err != nil || !got.Activated || got.PlanID != "plan" {
		t.Fatalf("Update = %#v, %v", got, err)
	}
	record.PlanID = "other"
	if err := store.PutUpdate(record); err == nil {
		t.Fatal("mismatched update record accepted")
	}
}

func TestPersistedTrustStoresOnlyOrderedProofsPrivately(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	proofs := []release.TrustProof{{Manifest: []byte("manifest-one"), Signature: []byte("signature-one")}, {Manifest: []byte("manifest-two"), Signature: []byte("signature-two")}}
	if err := store.PutTrustProofs(proofs); err != nil {
		t.Fatal(err)
	}
	got, err := store.TrustProofs()
	if err != nil || !reflect.DeepEqual(got, proofs) {
		t.Fatalf("TrustProofs=%v, %v", got, err)
	}
	path := filepath.Join(store.dir, trustedKeysFilename)
	var raw map[string]json.RawMessage
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(contents, &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw) != 1 || raw["proofs"] == nil || raw["keys"] != nil || raw["provenance"] != nil {
		t.Fatalf("persisted trust schema=%s", contents)
	}
	assertMode(t, path, 0o600)
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.TrustProofs(); err == nil {
		t.Fatal("world-readable trust proofs accepted")
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode=%#o, want %#o", path, got, want)
	}
}
