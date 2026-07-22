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
	home := t.TempDir()
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
	a := &Adapter{Archive: &archivepkg.Adapter{HTTP: server.Client(), CacheDir: filepath.Join(t.TempDir(), "cache")}, Home: home, State: store, Recovery: filepath.Join(t.TempDir(), "recovery")}
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
