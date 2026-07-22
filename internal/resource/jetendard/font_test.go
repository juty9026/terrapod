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
	item := model.Resource{ID: ResourceID, Type: model.ResourceArchive, Provider: Provider, Package: "jetendard", VersionPolicy: model.VersionPinned, Metadata: map[string]string{
		MetadataURL: server.URL, MetadataSHA256: fmt.Sprintf("%x", digest), MetadataFormat: "zip", MetadataTag: "v1.0.0", MetadataDestination: "Library/Fonts",
	}}
	a := &Adapter{Archive: &archivepkg.Adapter{HTTP: server.Client(), CacheDir: filepath.Join(t.TempDir(), "cache")}, Home: home, State: store, Recovery: filepath.Join(t.TempDir(), "recovery")}
	return a, item, store, filepath.Join(home, "Library", "Fonts"), &requests
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
	if err != nil || len(entries) != 0 {
		t.Fatalf("recovery entries=%v err=%v", entries, err)
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
