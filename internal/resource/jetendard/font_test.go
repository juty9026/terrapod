package jetendard

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/juty9026/terrapod/internal/model"
	archivepkg "github.com/juty9026/terrapod/internal/resource/archive"
	"github.com/juty9026/terrapod/internal/state"
)

func fontArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var output bytes.Buffer
	w := zip.NewWriter(&output)
	for name, body := range files {
		part, err := w.Create("release/ttf/" + name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.WriteString(part, body)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func fixture(t *testing.T, body []byte) (*Adapter, model.Resource, *state.Store, string, *int) {
	t.Helper()
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests++; _, _ = w.Write(body) }))
	t.Cleanup(server.Close)
	digest := sha256.Sum256(body)
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store, err := state.Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	manifest := map[string]string{}
	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range reader.File {
		name := filepath.Base(entry.Name)
		if !fontPattern.MatchString(name) {
			continue
		}
		stream, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		contents, err := io.ReadAll(stream)
		if err != nil {
			t.Fatal(err)
		}
		_ = stream.Close()
		digest := sha256.Sum256(contents)
		manifest[name] = fmt.Sprintf("%x", digest)
	}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	item := model.Resource{ID: ResourceID, Type: model.ResourceArchive, Provider: Provider, Package: "jetendard", VersionPolicy: model.VersionPinned, Metadata: map[string]string{
		MetadataURL: server.URL, MetadataSHA256: fmt.Sprintf("%x", digest), MetadataFormat: "zip", MetadataTag: "v1.0.0", MetadataDestination: "Library/Fonts",
		MetadataFiles: string(manifestJSON),
	}}
	cacheRoot, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	recoveryRoot, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := &Adapter{Archive: &archivepkg.Adapter{HTTP: server.Client(), CacheDir: filepath.Join(cacheRoot, "cache")}, Home: home, State: store, Recovery: filepath.Join(recoveryRoot, "recovery")}
	return a, item, store, filepath.Join(home, "Library", "Fonts"), &requests
}

func TestPlanDistinguishesInstallAdoptAndTakeover(t *testing.T) {
	body := fontArchive(t, map[string]string{"Jetendard-Regular.ttf": "regular", "Jetendard-Bold.ttf": "bold"})
	for _, tc := range []struct {
		name  string
		files map[string]string
		want  model.OperationKind
	}{
		{"absent", nil, model.OperationInstall},
		{"matching", map[string]string{"Jetendard-Regular.ttf": "regular", "Jetendard-Bold.ttf": "bold"}, model.OperationAdopt},
		{"differing", map[string]string{"Jetendard-Regular.ttf": "mine", "Jetendard-Bold.ttf": "bold"}, model.OperationRestore},
		{"mixed missing", map[string]string{"Jetendard-Regular.ttf": "regular"}, model.OperationRestore},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, item, _, fonts, requests := fixture(t, body)
			if len(tc.files) > 0 {
				if err := os.MkdirAll(fonts, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			for name, contents := range tc.files {
				if err := os.WriteFile(filepath.Join(fonts, name), []byte(contents), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			obs, err := a.Inspect(context.Background(), item)
			if err != nil {
				t.Fatal(err)
			}
			ops, err := a.Plan(context.Background(), item, obs, model.Ownership{})
			if err != nil {
				t.Fatal(err)
			}
			if len(ops) != 1 || ops[0].Kind != tc.want {
				t.Fatalf("ops=%#v", ops)
			}
			if tc.want == model.OperationRestore && !strings.Contains(ops[0].Detail, "take ownership") {
				t.Fatalf("detail=%q", ops[0].Detail)
			}
			if *requests != 0 {
				t.Fatalf("planning made %d HTTP requests", *requests)
			}
		})
	}
}

func TestTakeoverBacksUpPreExistingFontAndBackupFailureAborts(t *testing.T) {
	body := fontArchive(t, map[string]string{"Jetendard-Regular.ttf": "desired"})
	t.Run("backup", func(t *testing.T) {
		a, item, store, fonts, _ := fixture(t, body)
		if err := os.MkdirAll(fonts, 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(fonts, "Jetendard-Regular.ttf")
		if err := os.WriteFile(path, []byte("mine"), 0o600); err != nil {
			t.Fatal(err)
		}
		op := operation(item, model.OperationRestore)
		if _, err := store.Begin(model.Plan{ID: "takeover", Operations: []model.Operation{op}}); err != nil {
			t.Fatal(err)
		}
		if result := a.ExecuteResource(context.Background(), item, op); !result.Success {
			t.Fatalf("result=%#v", result)
		}
		matches, err := filepath.Glob(filepath.Join(a.Recovery, "*", "preexisting", "Jetendard-Regular.ttf"))
		if err != nil || len(matches) != 1 {
			t.Fatalf("backups=%v err=%v", matches, err)
		}
		if got, _ := os.ReadFile(matches[0]); string(got) != "mine" {
			t.Fatalf("backup=%q", got)
		}
	})
	t.Run("failure", func(t *testing.T) {
		a, item, store, fonts, _ := fixture(t, body)
		if err := os.MkdirAll(fonts, 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(fonts, "Jetendard-Regular.ttf")
		if err := os.WriteFile(path, []byte("mine"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(a.Recovery, []byte("not a directory"), 0o600); err != nil {
			t.Fatal(err)
		}
		op := operation(item, model.OperationRestore)
		if _, err := store.Begin(model.Plan{ID: "takeover-fail", Operations: []model.Operation{op}}); err != nil {
			t.Fatal(err)
		}
		if result := a.ExecuteResource(context.Background(), item, op); result.Success {
			t.Fatal("expected backup failure")
		}
		if got, _ := os.ReadFile(path); string(got) != "mine" {
			t.Fatalf("font mutated: %q", got)
		}
	})
}

func TestPlanAndApplyUsesResolvedMetadataWithoutLatestLookup(t *testing.T) {
	body := fontArchive(t, map[string]string{"Jetendard-Regular.ttf": "regular", "Jetendard-Bold.ttf": "bold"})
	a, item, store, fonts, requests := fixture(t, body)
	ops, err := a.Plan(context.Background(), item, model.Observation{}, model.Ownership{})
	if err != nil || len(ops) != 1 || ops[0].Kind != model.OperationInstall {
		t.Fatalf("ops=%#v err=%v", ops, err)
	}
	if *requests != 0 {
		t.Fatalf("planning made %d HTTP requests", *requests)
	}
	if _, err := store.Begin(model.Plan{ID: "font-install", Operations: ops}); err != nil {
		t.Fatal(err)
	}
	result := a.ExecuteResource(context.Background(), item, ops[0])
	if !result.Success {
		t.Fatalf("result=%#v", result)
	}
	observation, err := a.Verify(context.Background(), item)
	if err != nil || !observation.Healthy || len(observation.Paths) != 2 {
		t.Fatalf("obs=%#v err=%v", observation, err)
	}
	if got, _ := os.ReadFile(filepath.Join(fonts, "Jetendard-Regular.ttf")); string(got) != "regular" {
		t.Fatalf("font=%q", got)
	}
}

func TestInstalledReceiptDetectsMissingAndModifiedFonts(t *testing.T) {
	body := fontArchive(t, map[string]string{"Jetendard-Regular.ttf": "regular"})
	a, item, store, fonts, _ := fixture(t, body)
	path := filepath.Join(fonts, "Jetendard-Regular.ttf")
	if err := os.MkdirAll(fonts, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("regular"), 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("regular"))
	owned := model.Ownership{ResourceID: item.ID, Provider: item.Provider, Package: item.Package, Paths: map[string]string{path: fmt.Sprintf("sha256:%x", digest)}}
	if err := store.PutOwnership(owned); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mutate func() error
	}{{"missing", func() error { return os.Remove(path) }}, {"modified", func() error { return os.WriteFile(path, []byte("mine"), 0o600) }}} {
		t.Run(tc.name, func(t *testing.T) {
			_ = os.WriteFile(path, []byte("regular"), 0o600)
			if err := tc.mutate(); err != nil {
				t.Fatal(err)
			}
			obs, err := a.Inspect(context.Background(), item)
			if err != nil {
				t.Fatal(err)
			}
			if obs.Healthy {
				t.Fatalf("obs=%#v", obs)
			}
		})
	}
}

func TestUpgradePreservesUserFontsAndPruneRemovesOnlyReceipt(t *testing.T) {
	body := fontArchive(t, map[string]string{"Jetendard-Regular.ttf": "new"})
	a, item, store, fonts, _ := fixture(t, body)
	if err := os.MkdirAll(fonts, 0o700); err != nil {
		t.Fatal(err)
	}
	ownedPath := filepath.Join(fonts, "Jetendard-Regular.ttf")
	obsolete := filepath.Join(fonts, "Jetendard-Legacy.ttf")
	manual := filepath.Join(fonts, "Jetendard-Manual.ttf")
	for path, body := range map[string]string{ownedPath: "old", obsolete: "legacy", manual: "manual"} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := sha256.Sum256([]byte("old"))
	legacy := sha256.Sum256([]byte("legacy"))
	owned := model.Ownership{ResourceID: item.ID, Provider: item.Provider, Package: item.Package, Paths: map[string]string{ownedPath: fmt.Sprintf("sha256:%x", old), obsolete: fmt.Sprintf("sha256:%x", legacy)}}
	if err := store.PutOwnership(owned); err != nil {
		t.Fatal(err)
	}
	op := operation(item, model.OperationUpgrade)
	if _, err := store.Begin(model.Plan{ID: "font-upgrade", Operations: []model.Operation{op}}); err != nil {
		t.Fatal(err)
	}
	if result := a.ExecuteResource(context.Background(), item, op); !result.Success {
		t.Fatalf("result=%#v", result)
	}
	if _, err := os.Stat(obsolete); !os.IsNotExist(err) {
		t.Fatalf("obsolete remains: %v", err)
	}
	if got, _ := os.ReadFile(manual); string(got) != "manual" {
		t.Fatalf("manual=%q", got)
	}
	newDigest := sha256.Sum256([]byte("new"))
	receipt := model.Ownership{ResourceID: item.ID, Provider: item.Provider, Package: item.Package, Paths: map[string]string{ownedPath: fmt.Sprintf("sha256:%x", newDigest)}}
	prune := operation(item, model.OperationPrune)
	prune.Removes = []string{item.Package}
	if err := a.Prune(context.Background(), item, prune, receipt); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ownedPath); !os.IsNotExist(err) {
		t.Fatalf("owned remains: %v", err)
	}
	if got, _ := os.ReadFile(manual); string(got) != "manual" {
		t.Fatalf("manual=%q", got)
	}
	if _, err := os.Stat(fonts); err != nil {
		t.Fatalf("Fonts dir removed: %v", err)
	}
}

func TestInstallErrorRollsBackAllFontsAndCleansRecovery(t *testing.T) {
	body := fontArchive(t, map[string]string{"Jetendard-Regular.ttf": "new", "Jetendard-Bold.ttf": "new-bold"})
	a, item, store, fonts, _ := fixture(t, body)
	if err := os.MkdirAll(fonts, 0o700); err != nil {
		t.Fatal(err)
	}
	owned := model.Ownership{ResourceID: item.ID, Provider: item.Provider, Package: item.Package, Paths: map[string]string{}}
	for name, body := range map[string]string{"Jetendard-Regular.ttf": "old", "Jetendard-Bold.ttf": "old-bold"} {
		path := filepath.Join(fonts, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256([]byte(body))
		owned.Paths[path] = fmt.Sprintf("sha256:%x", digest)
	}
	if err := store.PutOwnership(owned); err != nil {
		t.Fatal(err)
	}
	calls := 0
	beforeInstallFile = func(string) error {
		calls++
		if calls == 2 {
			return fmt.Errorf("injected replacement failure")
		}
		return nil
	}
	t.Cleanup(func() { beforeInstallFile = nil })
	op := operation(item, model.OperationUpgrade)
	if _, err := store.Begin(model.Plan{ID: "font-rollback", Operations: []model.Operation{op}}); err != nil {
		t.Fatal(err)
	}
	if result := a.ExecuteResource(context.Background(), item, op); result.Success || !strings.Contains(result.Detail, "injected") {
		t.Fatalf("result=%#v", result)
	}
	for name, want := range map[string]string{"Jetendard-Regular.ttf": "old", "Jetendard-Bold.ttf": "old-bold"} {
		got, err := os.ReadFile(filepath.Join(fonts, name))
		if err != nil || string(got) != want {
			t.Fatalf("%s=%q err=%v", name, got, err)
		}
	}
	entries, err := os.ReadDir(a.Recovery)
	if err != nil && !os.IsNotExist(err) || len(entries) != 0 {
		t.Fatalf("recovery entries=%v err=%v", entries, err)
	}
}

func TestInterruptedInstallRecoversAcrossAdapterRestart(t *testing.T) {
	body := fontArchive(t, map[string]string{"Jetendard-Regular.ttf": "new", "Jetendard-Bold.ttf": "new-bold"})
	a, item, store, fonts, _ := fixture(t, body)
	if err := os.MkdirAll(fonts, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, contents := range map[string]string{"Jetendard-Regular.ttf": "old", "Jetendard-Bold.ttf": "old-bold"} {
		if err := os.WriteFile(filepath.Join(fonts, name), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	op := operation(item, model.OperationRestore)
	if _, err := store.Begin(model.Plan{ID: "crash", Operations: []model.Operation{op}}); err != nil {
		t.Fatal(err)
	}
	published := 0
	afterPublishFile = func(string) error {
		published++
		if published == 1 {
			return errSimulatedCrash
		}
		return nil
	}
	t.Cleanup(func() { afterPublishFile = nil })
	if result := a.ExecuteResource(context.Background(), item, op); result.Success || !strings.Contains(result.Detail, "simulated crash") {
		t.Fatalf("result=%#v", result)
	}
	afterPublishFile = nil
	fresh := &Adapter{Archive: a.Archive, Home: a.Home, State: a.State, Recovery: a.Recovery}
	obs, err := fresh.Inspect(context.Background(), item)
	if err != nil {
		t.Fatal(err)
	}
	if !obs.Healthy {
		t.Fatalf("obs=%#v", obs)
	}
	for name, want := range map[string]string{"Jetendard-Regular.ttf": "new", "Jetendard-Bold.ttf": "new-bold"} {
		got, err := os.ReadFile(filepath.Join(fonts, name))
		if err != nil || string(got) != want {
			t.Fatalf("%s=%q err=%v", name, got, err)
		}
	}
	if _, err := os.Stat(filepath.Join(fonts, transactionFilename)); !os.IsNotExist(err) {
		t.Fatalf("transaction remains: %v", err)
	}
}

func TestRestartAfterPublishBeforeOwnershipAdoptsWithoutRewrite(t *testing.T) {
	body := fontArchive(t, map[string]string{"Jetendard-Regular.ttf": "new"})
	a, item, store, _, requests := fixture(t, body)
	op := operation(item, model.OperationInstall)
	if _, err := store.Begin(model.Plan{ID: "receipt-boundary", Operations: []model.Operation{op}}); err != nil {
		t.Fatal(err)
	}
	if result := a.ExecuteResource(context.Background(), item, op); !result.Success {
		t.Fatalf("result=%#v", result)
	}
	fresh := &Adapter{Archive: a.Archive, Home: a.Home, State: a.State, Recovery: a.Recovery}
	obs, err := fresh.Inspect(context.Background(), item)
	if err != nil {
		t.Fatal(err)
	}
	ops, err := fresh.Plan(context.Background(), item, obs, model.Ownership{})
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 1 || ops[0].Kind != model.OperationAdopt {
		t.Fatalf("ops=%#v", ops)
	}
	before := *requests
	if result := fresh.ExecuteResource(context.Background(), item, ops[0]); !result.Success {
		t.Fatalf("adopt result=%#v", result)
	}
	if *requests != before {
		t.Fatalf("adopt downloaded asset: before=%d after=%d", before, *requests)
	}
}

func TestResolveLatestStableIsExplicitPreflight(t *testing.T) {
	digest := strings.Repeat("a", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name": "v2", "draft": false, "prerelease": false,
			"assets": []map[string]any{{"name": "Jetendard-TTF.zip", "state": "uploaded", "digest": "sha256:" + digest, "browser_download_url": "https://example.test/font.zip"}},
		})
	}))
	defer server.Close()
	resolved, err := ResolveLatest(context.Background(), server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Tag != "v2" || resolved.SHA256 != digest {
		t.Fatalf("resolved=%#v", resolved)
	}
}

func TestPruneRejectsSymlinkedFontsDirectoryWithoutDeletingExternalFile(t *testing.T) {
	body := fontArchive(t, map[string]string{"Jetendard-Regular.ttf": "same"})
	a, item, _, fonts, _ := fixture(t, body)
	if err := os.MkdirAll(fonts, 0o700); err != nil {
		t.Fatal(err)
	}
	original := fonts + ".original"
	if err := os.Rename(fonts, original); err != nil {
		t.Fatal(err)
	}
	external := t.TempDir()
	externalFont := filepath.Join(external, "Jetendard-Regular.ttf")
	if err := os.WriteFile(externalFont, []byte("same"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, fonts); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("same"))
	receipt := model.Ownership{ResourceID: item.ID, Provider: item.Provider, Package: item.Package, Paths: map[string]string{filepath.Join(fonts, "Jetendard-Regular.ttf"): fmt.Sprintf("sha256:%x", digest)}}
	op := operation(item, model.OperationPrune)
	op.Removes = []string{item.Package}
	if err := a.Prune(context.Background(), item, op, receipt); err == nil {
		t.Fatal("expected symlink rejection")
	}
	if got, err := os.ReadFile(externalFont); err != nil || string(got) != "same" {
		t.Fatalf("external=%q err=%v", got, err)
	}
}

func writeRecoveryIntent(t *testing.T, a *Adapter, item model.Resource, owned model.Ownership, op model.Operation, entries []transactionEntry) string {
	t.Helper()
	d, err := a.declaration(item)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := a.State.Snapshot()
	if err != nil || snapshot.ActiveJournal == nil {
		t.Fatalf("snapshot=%#v err=%v", snapshot, err)
	}
	fonts := filepath.Join(a.Home, "Library", "Fonts")
	directory := filepath.Join(fonts, transactionDirname)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	txn := transaction{Version: 1, Phase: "prepared", ResourceID: string(item.ID), ManifestDigest: manifestDigest(d.files), OwnershipDigest: ownershipDigest(owned), JournalID: snapshot.ActiveJournal.ID, OperationID: op.ID, OperationKind: op.Kind, Entries: entries}
	contents, err := json.Marshal(txn)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, transactionFilename), contents, 0o600); err != nil {
		t.Fatal(err)
	}
	return directory
}

func TestRecoveryRejectsForgedOrStaleIntentWithoutMutation(t *testing.T) {
	body := fontArchive(t, map[string]string{"Jetendard-Regular.ttf": "desired"})
	for _, tc := range []struct {
		name  string
		forge func(*transaction)
	}{
		{"remove outside receipt", func(txn *transaction) {
			txn.Entries = []transactionEntry{{Name: "Jetendard-Manual.ttf", Remove: true, OldExists: true, OldDigest: digestString([]byte("manual")), OldSize: int64(len("manual"))}}
		}},
		{"stale manifest", func(txn *transaction) { txn.ManifestDigest = strings.Repeat("0", 64) }},
		{"stale ownership", func(txn *transaction) { txn.OwnershipDigest = strings.Repeat("0", 64) }},
		{"stale journal", func(txn *transaction) { txn.JournalID = "stale-journal" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, item, store, fonts, _ := fixture(t, body)
			if err := os.MkdirAll(fonts, 0o700); err != nil {
				t.Fatal(err)
			}
			manual := filepath.Join(fonts, "Jetendard-Manual.ttf")
			if err := os.WriteFile(manual, []byte("manual"), 0o600); err != nil {
				t.Fatal(err)
			}
			op := operation(item, model.OperationInstall)
			if _, err := store.Begin(model.Plan{ID: "forged", Operations: []model.Operation{op}}); err != nil {
				t.Fatal(err)
			}
			d, err := a.declaration(item)
			if err != nil {
				t.Fatal(err)
			}
			snapshot, _ := store.Snapshot()
			txn := transaction{Version: 1, Phase: "prepared", ResourceID: string(item.ID), ManifestDigest: manifestDigest(d.files), OwnershipDigest: ownershipDigest(model.Ownership{}), JournalID: snapshot.ActiveJournal.ID, OperationID: op.ID, OperationKind: op.Kind, Entries: []transactionEntry{{Name: "Jetendard-Regular.ttf", NewDigest: d.files["Jetendard-Regular.ttf"], NewSize: int64(len("desired"))}}}
			tc.forge(&txn)
			directory := filepath.Join(fonts, transactionDirname)
			if err := os.Mkdir(directory, 0o700); err != nil {
				t.Fatal(err)
			}
			contents, _ := json.Marshal(txn)
			if err := os.WriteFile(filepath.Join(directory, transactionFilename), contents, 0o600); err != nil {
				t.Fatal(err)
			}
			for _, entry := range txn.Entries {
				if entry.Remove {
					if err := os.WriteFile(filepath.Join(directory, backupName(entry)), []byte("manual"), 0o600); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := os.WriteFile(filepath.Join(directory, stageName(entry)), []byte("desired"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
			}
			if _, err := a.Inspect(context.Background(), item); err == nil {
				t.Fatal("expected forged intent rejection")
			}
			if got, err := os.ReadFile(manual); err != nil || string(got) != "manual" {
				t.Fatalf("manual=%q err=%v", got, err)
			}
		})
	}
}

func TestRecoveryRejectsSymlinkedOrForgedStageAndBackupWithoutMutation(t *testing.T) {
	body := fontArchive(t, map[string]string{"Jetendard-Regular.ttf": "new"})
	for _, kind := range []string{"stage-symlink", "backup-symlink", "stage-digest", "backup-digest"} {
		t.Run(kind, func(t *testing.T) {
			a, item, store, fonts, _ := fixture(t, body)
			if err := os.MkdirAll(fonts, 0o700); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(fonts, "Jetendard-Regular.ttf")
			if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
				t.Fatal(err)
			}
			op := operation(item, model.OperationRestore)
			if _, err := store.Begin(model.Plan{ID: "symlink-artifact", Operations: []model.Operation{op}}); err != nil {
				t.Fatal(err)
			}
			entry := transactionEntry{Name: "Jetendard-Regular.ttf", NewDigest: digestString([]byte("new")), NewSize: 3, OldExists: true, OldDigest: digestString([]byte("old")), OldSize: 3}
			directory := writeRecoveryIntent(t, a, item, model.Ownership{}, op, []transactionEntry{entry})
			external := filepath.Join(t.TempDir(), "artifact")
			isBackup := strings.HasPrefix(kind, "backup")
			contents := "new"
			if isBackup {
				contents = "old"
				if err := os.WriteFile(filepath.Join(directory, stageName(entry)), []byte("new"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if strings.HasSuffix(kind, "digest") {
				contents = "forged"
			}
			if err := os.WriteFile(external, []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
			link := stageName(entry)
			if isBackup {
				link = backupName(entry)
			} else {
				if err := os.WriteFile(filepath.Join(directory, backupName(entry)), []byte("old"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if strings.HasSuffix(kind, "symlink") {
				if err := os.Symlink(external, filepath.Join(directory, link)); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(filepath.Join(directory, link), []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := a.Inspect(context.Background(), item); err == nil {
				t.Fatal("expected symlink artifact rejection")
			}
			if got, _ := os.ReadFile(target); string(got) != "old" {
				t.Fatalf("target mutated: %q", got)
			}
		})
	}
}

func TestRecoveryRejectsSymlinkedIntentWithoutMutation(t *testing.T) {
	body := fontArchive(t, map[string]string{"Jetendard-Regular.ttf": "new"})
	a, item, store, fonts, _ := fixture(t, body)
	if err := os.MkdirAll(fonts, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(fonts, "Jetendard-Regular.ttf")
	if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	op := operation(item, model.OperationRestore)
	if _, err := store.Begin(model.Plan{ID: "symlink-intent", Operations: []model.Operation{op}}); err != nil {
		t.Fatal(err)
	}
	entry := transactionEntry{Name: "Jetendard-Regular.ttf", NewDigest: digestString([]byte("new")), NewSize: 3, OldExists: true, OldDigest: digestString([]byte("old")), OldSize: 3}
	directory := writeRecoveryIntent(t, a, item, model.Ownership{}, op, []transactionEntry{entry})
	if err := os.WriteFile(filepath.Join(directory, stageName(entry)), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, backupName(entry)), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	intent := filepath.Join(directory, transactionFilename)
	contents, err := os.ReadFile(intent)
	if err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(t.TempDir(), transactionFilename)
	if err := os.WriteFile(external, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(intent); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, intent); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Inspect(context.Background(), item); err == nil {
		t.Fatal("expected symlink intent rejection")
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "old" {
		t.Fatalf("target=%q err=%v", got, err)
	}
}

func TestHomeAncestorSymlinkBlocksInstallRecoveryAndPrune(t *testing.T) {
	body := fontArchive(t, map[string]string{"Jetendard-Regular.ttf": "new"})
	for _, depth := range []string{"parent", "grandparent"} {
		for _, action := range []string{"install", "recovery", "prune"} {
			t.Run(depth+"/"+action, func(t *testing.T) {
				a, item, store, _, _ := fixture(t, body)
				base, err := filepath.EvalSymlinks(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				realParent := filepath.Join(base, "real")
				relativeHome := "home"
				if depth == "grandparent" {
					relativeHome = filepath.Join("nested", "home")
				}
				realHome := filepath.Join(realParent, relativeHome)
				if err := os.MkdirAll(filepath.Join(realHome, "Library", "Fonts"), 0o700); err != nil {
					t.Fatal(err)
				}
				alias := filepath.Join(base, "alias")
				if err := os.Symlink(realParent, alias); err != nil {
					t.Fatal(err)
				}
				a.Home = filepath.Join(alias, relativeHome)
				target := filepath.Join(realHome, "Library", "Fonts", "Jetendard-Regular.ttf")
				if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
					t.Fatal(err)
				}
				digest := digestString([]byte("old"))
				owned := model.Ownership{ResourceID: item.ID, Provider: item.Provider, Package: item.Package, Paths: map[string]string{filepath.Join(a.Home, "Library", "Fonts", "Jetendard-Regular.ttf"): digest}}
				if action == "prune" {
					op := operation(item, model.OperationPrune)
					op.Removes = []string{item.Package}
					if err := a.Prune(context.Background(), item, op, owned); err == nil {
						t.Fatal("expected ancestor rejection")
					}
				} else {
					op := operation(item, model.OperationInstall)
					if _, err := store.Begin(model.Plan{ID: "ancestor", Operations: []model.Operation{op}}); err != nil {
						t.Fatal(err)
					}
					if action == "recovery" {
						entry := transactionEntry{Name: "Jetendard-Regular.ttf", NewDigest: digestString([]byte("new")), NewSize: 3, OldExists: true, OldDigest: digest, OldSize: 3}
						directory := writeRecoveryIntent(t, a, item, model.Ownership{}, op, []transactionEntry{entry})
						if err := os.WriteFile(filepath.Join(directory, stageName(entry)), []byte("new"), 0o600); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(filepath.Join(directory, backupName(entry)), []byte("old"), 0o600); err != nil {
							t.Fatal(err)
						}
					}
					if _, err := a.Inspect(context.Background(), item); err == nil {
						t.Fatal("expected ancestor rejection")
					}
				}
				if got, err := os.ReadFile(target); err != nil || string(got) != "old" {
					t.Fatalf("target=%q err=%v", got, err)
				}
			})
		}
	}
}

func TestPublishedCleanupRecoversAfterFirstBackupDeletion(t *testing.T) {
	body := fontArchive(t, map[string]string{"Jetendard-Regular.ttf": "new", "Jetendard-Bold.ttf": "new-bold"})
	a, item, store, fonts, _ := fixture(t, body)
	if err := os.MkdirAll(fonts, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, contents := range map[string]string{"Jetendard-Regular.ttf": "old", "Jetendard-Bold.ttf": "old-bold"} {
		if err := os.WriteFile(filepath.Join(fonts, name), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	op := operation(item, model.OperationRestore)
	if _, err := store.Begin(model.Plan{ID: "cleanup-crash", Operations: []model.Operation{op}}); err != nil {
		t.Fatal(err)
	}
	deleted := 0
	afterCleanupArtifact = func(path string) error {
		if strings.HasPrefix(filepath.Base(path), "old-") {
			deleted++
			if deleted == 1 {
				return errSimulatedCrash
			}
		}
		return nil
	}
	t.Cleanup(func() { afterCleanupArtifact = nil })
	if result := a.ExecuteResource(context.Background(), item, op); result.Success || !strings.Contains(result.Detail, "simulated crash") {
		t.Fatalf("result=%#v", result)
	}
	afterCleanupArtifact = nil
	for name, want := range map[string]string{"Jetendard-Regular.ttf": "new", "Jetendard-Bold.ttf": "new-bold"} {
		got, err := os.ReadFile(filepath.Join(fonts, name))
		if err != nil || string(got) != want {
			t.Fatalf("%s=%q err=%v", name, got, err)
		}
	}
	fresh := &Adapter{Archive: a.Archive, Home: a.Home, State: a.State, Recovery: a.Recovery}
	obs, err := fresh.Inspect(context.Background(), item)
	if err != nil || !obs.Healthy {
		t.Fatalf("obs=%#v err=%v", obs, err)
	}
	ops, err := fresh.Plan(context.Background(), item, obs, model.Ownership{})
	if err != nil || len(ops) != 1 || ops[0].Kind != model.OperationAdopt {
		t.Fatalf("ops=%#v err=%v", ops, err)
	}
	if _, err := os.Stat(filepath.Join(fonts, transactionDirname)); !os.IsNotExist(err) {
		t.Fatalf("transaction remains: %v", err)
	}
}

func TestRollbackCleanupRecoversAfterFirstBackupDeletion(t *testing.T) {
	body := fontArchive(t, map[string]string{"Jetendard-Regular.ttf": "new", "Jetendard-Bold.ttf": "new-bold"})
	a, item, store, fonts, _ := fixture(t, body)
	if err := os.MkdirAll(fonts, 0o700); err != nil {
		t.Fatal(err)
	}
	owned := model.Ownership{ResourceID: item.ID, Provider: item.Provider, Package: item.Package, Paths: map[string]string{}}
	for name, contents := range map[string]string{"Jetendard-Regular.ttf": "old", "Jetendard-Bold.ttf": "old-bold"} {
		path := filepath.Join(fonts, name)
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
		owned.Paths[path] = digestString([]byte(contents))
	}
	if err := store.PutOwnership(owned); err != nil {
		t.Fatal(err)
	}
	op := operation(item, model.OperationUpgrade)
	if _, err := store.Begin(model.Plan{ID: "rollback-cleanup-crash", Operations: []model.Operation{op}}); err != nil {
		t.Fatal(err)
	}
	publishCalls := 0
	beforeInstallFile = func(string) error {
		publishCalls++
		if publishCalls == 2 {
			return fmt.Errorf("injected publish failure")
		}
		return nil
	}
	deleted := 0
	afterCleanupArtifact = func(path string) error {
		if strings.HasPrefix(filepath.Base(path), "old-") {
			deleted++
			if deleted == 1 {
				return errSimulatedCrash
			}
		}
		return nil
	}
	t.Cleanup(func() { beforeInstallFile = nil; afterCleanupArtifact = nil })
	if result := a.ExecuteResource(context.Background(), item, op); result.Success {
		t.Fatalf("result=%#v", result)
	}
	beforeInstallFile = nil
	afterCleanupArtifact = nil
	for name, want := range map[string]string{"Jetendard-Regular.ttf": "old", "Jetendard-Bold.ttf": "old-bold"} {
		got, err := os.ReadFile(filepath.Join(fonts, name))
		if err != nil || string(got) != want {
			t.Fatalf("%s=%q err=%v", name, got, err)
		}
	}
	fresh := &Adapter{Archive: a.Archive, Home: a.Home, State: a.State, Recovery: a.Recovery}
	obs, err := fresh.Inspect(context.Background(), item)
	if err != nil || !obs.Healthy {
		t.Fatalf("obs=%#v err=%v", obs, err)
	}
	if _, err := os.Stat(filepath.Join(fonts, transactionDirname)); !os.IsNotExist(err) {
		t.Fatalf("transaction remains: %v", err)
	}
}

func TestRollbackSyncFailureKeepsBackupsAndFreshRecoveryCompletes(t *testing.T) {
	body := fontArchive(t, map[string]string{"Jetendard-Regular.ttf": "new", "Jetendard-Bold.ttf": "new-bold"})
	a, item, store, fonts, _ := fixture(t, body)
	if err := os.MkdirAll(fonts, 0o700); err != nil {
		t.Fatal(err)
	}
	owned := model.Ownership{ResourceID: item.ID, Provider: item.Provider, Package: item.Package, Paths: map[string]string{}}
	for name, contents := range map[string]string{"Jetendard-Regular.ttf": "old", "Jetendard-Bold.ttf": "old-bold"} {
		path := filepath.Join(fonts, name)
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
		owned.Paths[path] = digestString([]byte(contents))
	}
	if err := store.PutOwnership(owned); err != nil {
		t.Fatal(err)
	}
	op := operation(item, model.OperationUpgrade)
	if _, err := store.Begin(model.Plan{ID: "rollback-sync", Operations: []model.Operation{op}}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	beforeInstallFile = func(string) error {
		calls++
		if calls == 2 {
			return fmt.Errorf("injected publish failure")
		}
		return nil
	}
	beforeRollbackCleanupSync = func() error { return fmt.Errorf("injected directory sync failure") }
	t.Cleanup(func() { beforeInstallFile = nil; beforeRollbackCleanupSync = nil })
	if result := a.ExecuteResource(context.Background(), item, op); result.Success || !strings.Contains(result.Detail, "sync failure") {
		t.Fatalf("result=%#v", result)
	}
	beforeInstallFile = nil
	beforeRollbackCleanupSync = nil
	directory := filepath.Join(fonts, transactionDirname)
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	backups := 0
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "old-") {
			backups++
		}
	}
	if backups != 2 {
		t.Fatalf("backups=%d entries=%v", backups, entries)
	}
	fresh := &Adapter{Archive: a.Archive, Home: a.Home, State: a.State, Recovery: a.Recovery}
	obs, err := fresh.Inspect(context.Background(), item)
	if err != nil || !obs.Healthy {
		t.Fatalf("obs=%#v err=%v", obs, err)
	}
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatalf("transaction remains: %v", err)
	}
}

func TestUpgradeNewSignedFilenameCollisionUsesDurableTakeoverBackup(t *testing.T) {
	body := fontArchive(t, map[string]string{"Jetendard-Regular.ttf": "regular", "Jetendard-Bold.ttf": "desired-bold"})
	a, item, store, fonts, _ := fixture(t, body)
	if err := os.MkdirAll(fonts, 0o700); err != nil {
		t.Fatal(err)
	}
	regular := filepath.Join(fonts, "Jetendard-Regular.ttf")
	collision := filepath.Join(fonts, "Jetendard-Bold.ttf")
	manual := filepath.Join(fonts, "Manual.ttf")
	for path, contents := range map[string]string{regular: "regular", collision: "user-bold", manual: "manual"} {
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	owned := model.Ownership{ResourceID: item.ID, Provider: item.Provider, Package: item.Package, Paths: map[string]string{regular: digestString([]byte("regular"))}}
	if err := store.PutOwnership(owned); err != nil {
		t.Fatal(err)
	}
	obs, err := a.Inspect(context.Background(), item)
	if err != nil {
		t.Fatal(err)
	}
	ops, err := a.Plan(context.Background(), item, obs, owned)
	if err != nil || len(ops) != 1 || ops[0].Kind != model.OperationRestore || !strings.Contains(ops[0].Detail, "take ownership") {
		t.Fatalf("ops=%#v err=%v", ops, err)
	}
	if _, err := store.Begin(model.Plan{ID: "new-collision", Operations: ops}); err != nil {
		t.Fatal(err)
	}
	if result := a.ExecuteResource(context.Background(), item, ops[0]); !result.Success {
		t.Fatalf("result=%#v", result)
	}
	matches, err := filepath.Glob(filepath.Join(a.Recovery, "*", "preexisting", "Jetendard-Bold.ttf"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("backups=%v err=%v", matches, err)
	}
	if got, _ := os.ReadFile(matches[0]); string(got) != "user-bold" {
		t.Fatalf("backup=%q", got)
	}
	if got, _ := os.ReadFile(collision); string(got) != "desired-bold" {
		t.Fatalf("installed=%q", got)
	}
	if got, _ := os.ReadFile(manual); string(got) != "manual" {
		t.Fatalf("manual=%q", got)
	}
}

func TestRecoveryRootAncestorSymlinkAbortsTakeoverWithoutMutation(t *testing.T) {
	body := fontArchive(t, map[string]string{"Jetendard-Regular.ttf": "desired"})
	for _, depth := range []string{"parent", "grandparent"} {
		t.Run(depth, func(t *testing.T) {
			a, item, store, fonts, _ := fixture(t, body)
			if err := os.MkdirAll(fonts, 0o700); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(fonts, "Jetendard-Regular.ttf")
			if err := os.WriteFile(target, []byte("mine"), 0o600); err != nil {
				t.Fatal(err)
			}
			base, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			real := filepath.Join(base, "real")
			relative := "recovery"
			if depth == "grandparent" {
				relative = filepath.Join("nested", "recovery")
			}
			if err := os.MkdirAll(filepath.Join(real, relative), 0o700); err != nil {
				t.Fatal(err)
			}
			alias := filepath.Join(base, "alias")
			if err := os.Symlink(real, alias); err != nil {
				t.Fatal(err)
			}
			a.Recovery = filepath.Join(alias, relative)
			op := operation(item, model.OperationRestore)
			if _, err := store.Begin(model.Plan{ID: "recovery-symlink", Operations: []model.Operation{op}}); err != nil {
				t.Fatal(err)
			}
			if result := a.ExecuteResource(context.Background(), item, op); result.Success {
				t.Fatal("expected recovery ancestor rejection")
			}
			if got, _ := os.ReadFile(target); string(got) != "mine" {
				t.Fatalf("target=%q", got)
			}
		})
	}
}
