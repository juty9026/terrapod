package release

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLatestStableRequiresManifestButNoSignature(t *testing.T) {
	manifest := stableManifest(t)
	data := encodeManifest(t, manifest)
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			assets := []map[string]any{{"name": "release.json", "size": len(data), "browser_download_url": server.URL + "/release.json"}}
			for _, asset := range manifest.Assets {
				assets = append(assets, map[string]any{"name": asset.Name, "size": asset.Size, "browser_download_url": server.URL + "/" + asset.Name})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": "v1.2.3", "draft": false, "prerelease": false, "assets": assets})
		case "/release.json":
			_, _ = w.Write(data)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := Client{HTTP: server.Client(), Endpoint: server.URL + "/latest", CacheDir: realReleaseTempDir(t)}
	if _, err := client.LatestStable(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestClientLatestStableDownloadsVerifiedAssets(t *testing.T) {
	files := releaseFixtureFiles(t)
	manifest := stableManifest(t)
	for index := range manifest.Assets {
		body := files[manifest.Assets[index].Name]
		manifest.Assets[index].Size = int64(len(body))
		digest := sha256.Sum256(body)
		manifest.Assets[index].SHA256 = hex.EncodeToString(digest[:])
	}
	data := encodeManifest(t, manifest)
	var server *httptest.Server
	requests := map[string]int{}
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests[r.URL.Path]++
		switch r.URL.Path {
		case "/latest":
			assets := []map[string]any{
				{"name": "release.json", "size": len(data), "browser_download_url": server.URL + "/release.json"},
			}
			for _, asset := range manifest.Assets {
				assets = append(assets, map[string]any{"name": asset.Name, "size": asset.Size, "browser_download_url": server.URL + "/" + asset.Name})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": "v1.2.3", "draft": false, "prerelease": false, "assets": assets})
		case "/release.json":
			_, _ = w.Write(data)
		default:
			_, _ = w.Write(files[strings.TrimPrefix(r.URL.Path, "/")])
		}
	}))
	defer server.Close()

	client := Client{HTTP: server.Client(), Endpoint: server.URL + "/latest", CacheDir: realReleaseTempDir(t)}
	got, err := client.LatestStable(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Manifest.Version != "1.2.3" || len(got.assets) != 7 || len(got.Files) != 0 {
		t.Fatalf("release=%+v", got)
	}
	asset, _ := got.Manifest.BinaryAsset("linux", "amd64")
	path, err := got.localAsset(context.Background(), asset)
	if err != nil {
		t.Fatal(err)
	}
	if body, err := os.ReadFile(path); err != nil || string(body) != string(files[asset.Name]) {
		t.Fatalf("body=%q err=%v", body, err)
	}
	if requests["/"+asset.Name] != 1 {
		t.Fatalf("selected binary requests=%d", requests["/"+asset.Name])
	}
	for _, other := range manifest.Assets {
		if other.Name != asset.Name && requests["/"+other.Name] != 0 {
			t.Fatalf("unselected asset %q downloaded", other.Name)
		}
	}
}

func TestNewLocalVerifiedReleaseBindsOnlyCanonicalLocalAssets(t *testing.T) {
	manifest := stableManifest(t)
	data := encodeManifest(t, manifest)

	got, err := NewLocalVerifiedRelease(data, map[string]string{"tpod-linux-amd64": "/private/tpod"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Files["tpod-linux-amd64"] != "/private/tpod" {
		t.Fatalf("files=%v", got.Files)
	}
	if _, err := NewLocalVerifiedRelease(data, map[string]string{"../tpod": "/private/tpod"}); err == nil {
		t.Fatal("unsafe local asset name accepted")
	}
	if _, err := NewLocalVerifiedRelease(data, map[string]string{"tpod-linux-amd64": ""}); err == nil {
		t.Fatal("empty local asset path accepted")
	}
	if _, err := NewLocalVerifiedRelease(append(data, []byte("{}")...), nil); err == nil {
		t.Fatal("trailing manifest accepted")
	}
}

func TestClientRejectsUnsafeResponsesAndCleansTemps(t *testing.T) {
	tests := []struct {
		name    string
		handler http.Handler
		want    string
	}{
		{name: "non-2xx", handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) }), want: "status"},
		{name: "metadata limit", handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(strings.Repeat("x", MaxManifestSize+1)))
		}), want: "limit"},
		{name: "redirect non-https", handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "http://example.com/release", http.StatusFound)
		}), want: "HTTPS"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewTLSServer(tt.handler)
			defer server.Close()
			cache := realReleaseTempDir(t)
			_, err := (Client{HTTP: server.Client(), Endpoint: server.URL, CacheDir: cache}).LatestStable(context.Background())
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err=%v, want %q", err, tt.want)
			}
			entries, _ := os.ReadDir(cache)
			for _, entry := range entries {
				if strings.Contains(entry.Name(), ".tmp-") {
					t.Fatalf("temporary file leaked: %s", entry.Name())
				}
			}
		})
	}
}

func TestClientRejectsIncompleteExtraAndChecksumMismatch(t *testing.T) {
	asset := Asset{Name: "asset", Size: 3, SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("abc")))}
	for _, tc := range []struct{ name, body, want string }{{"incomplete", "ab", "size"}, {"extra", "abcd", "size"}, {"checksum", "abd", "checksum"}} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(tc.body)) }))
			defer server.Close()
			cache := realReleaseTempDir(t)
			client := Client{HTTP: server.Client(), CacheDir: cache}
			if _, err := client.downloadAsset(context.Background(), server.URL, asset); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func releaseFixtureFiles(t *testing.T) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	for _, name := range []string{"tpod-darwin-amd64", "tpod-darwin-arm64", "tpod-linux-amd64", "tpod-linux-arm64"} {
		files[name] = []byte("binary-" + name)
	}
	files["terrapod-source.tar.gz"] = []byte("archive")
	files["resources.json"] = []byte(`{"schema":1}`)
	return files
}

func realReleaseTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = filepath.WalkDir(dir, func(name string, entry os.DirEntry, err error) error {
			if err == nil {
				_ = os.Chmod(name, 0o700)
			}
			return nil
		})
	})
	return dir
}
