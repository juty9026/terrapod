package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/juty9026/terrapod/internal/model"
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

func TestBeginOrResumeRejectsEmptyPlanID(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.BeginOrResume(model.Plan{}); err == nil {
		t.Fatal("empty plan ID accepted")
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
