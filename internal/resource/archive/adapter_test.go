package archive

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func zipBytes(t *testing.T, entries map[string]string, symlink string) []byte {
	t.Helper()
	var output bytes.Buffer
	w := zip.NewWriter(&output)
	for name, body := range entries {
		part, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(part, body); err != nil {
			t.Fatal(err)
		}
	}
	if symlink != "" {
		header := &zip.FileHeader{Name: symlink}
		header.SetMode(os.ModeSymlink | 0o777)
		part, err := w.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.WriteString(part, "../../outside")
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func tarBytes(t *testing.T, entries map[string]string, special *tar.Header) []byte {
	t.Helper()
	var output bytes.Buffer
	w := tar.NewWriter(&output)
	for name, body := range entries {
		header := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}
		if err := w.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		_, _ = io.WriteString(w, body)
	}
	if special != nil {
		if err := w.WriteHeader(special); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func serve(t *testing.T, body []byte) (*httptest.Server, Asset) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) }))
	digest := sha256.Sum256(body)
	return server, Asset{URL: server.URL, SHA256: fmt.Sprintf("%x", digest)}
}

func TestFetchRejectsChecksumMismatchAndOversize(t *testing.T) {
	server, asset := serve(t, []byte("archive"))
	defer server.Close()
	asset.SHA256 = strings.Repeat("0", 64)
	a := Adapter{HTTP: server.Client(), CacheDir: t.TempDir()}
	if _, err := a.Fetch(context.Background(), asset); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("err=%v", err)
	}
	asset.SHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte("archive")))
	a.Limits = Limits{DownloadBytes: 4}
	if _, err := a.Fetch(context.Background(), asset); err == nil || !strings.Contains(err.Error(), "download limit") {
		t.Fatalf("err=%v", err)
	}
}

func TestExtractRejectsUnsafeZipAndTarEntries(t *testing.T) {
	tests := []struct {
		name, format string
		body         []byte
	}{
		{"zip traversal", "zip", zipBytes(t, map[string]string{"../outside": "x"}, "")},
		{"zip absolute", "zip", zipBytes(t, map[string]string{"/outside": "x"}, "")},
		{"zip backslash", "zip", zipBytes(t, map[string]string{`safe\\..\\outside`: "x"}, "")},
		{"zip symlink", "zip", zipBytes(t, nil, "link")},
		{"tar traversal", "tar", tarBytes(t, map[string]string{"../outside": "x"}, nil)},
		{"tar absolute", "tar", tarBytes(t, map[string]string{"/outside": "x"}, nil)},
		{"tar symlink", "tar", tarBytes(t, nil, &tar.Header{Name: "link", Linkname: "../../outside", Typeflag: tar.TypeSymlink})},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server, asset := serve(t, tc.body)
			defer server.Close()
			asset.Format = tc.format
			destination := filepath.Join(t.TempDir(), "out")
			a := Adapter{HTTP: server.Client(), CacheDir: t.TempDir()}
			if _, err := a.FetchAndExtract(context.Background(), asset, destination); err == nil {
				t.Fatal("expected unsafe archive rejection")
			}
			if _, err := os.Lstat(destination); !os.IsNotExist(err) {
				t.Fatalf("partial destination remains: %v", err)
			}
		})
	}
}

func TestExtractRejectsDuplicateAndPartialArchives(t *testing.T) {
	var output bytes.Buffer
	w := zip.NewWriter(&output)
	for _, name := range []string{"font.ttf", "font.ttf"} {
		part, _ := w.Create(name)
		_, _ = io.WriteString(part, "font")
	}
	_ = w.Close()
	server, asset := serve(t, output.Bytes())
	defer server.Close()
	asset.Format = "zip"
	destination := filepath.Join(t.TempDir(), "out")
	a := Adapter{HTTP: server.Client(), CacheDir: t.TempDir()}
	if _, err := a.FetchAndExtract(context.Background(), asset, destination); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("err=%v", err)
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatalf("destination=%v", err)
	}
}

func TestExtractAtomicallyReplacesDirectory(t *testing.T) {
	body := zipBytes(t, map[string]string{"fonts/font.ttf": "new"}, "")
	server, asset := serve(t, body)
	defer server.Close()
	asset.Format = "zip"
	parent := t.TempDir()
	destination := filepath.Join(parent, "out")
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "old"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := Adapter{HTTP: server.Client(), CacheDir: t.TempDir()}
	manifest, err := a.FetchAndExtract(context.Background(), asset, destination)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Files) != 1 || manifest.Files[0].Path != "fonts/font.ttf" {
		t.Fatalf("manifest=%#v", manifest)
	}
	if got, err := os.ReadFile(filepath.Join(destination, "fonts", "font.ttf")); err != nil || string(got) != "new" {
		t.Fatalf("got=%q err=%v", got, err)
	}
	if _, err := os.Stat(filepath.Join(destination, "old")); !os.IsNotExist(err) {
		t.Fatalf("old remains: %v", err)
	}
}

func TestTarRejectsEntryCountBeforeSpooling(t *testing.T) {
	var output bytes.Buffer
	w := tar.NewWriter(&output)
	for index := 0; index < 4097; index++ {
		if err := w.WriteHeader(&tar.Header{Name: fmt.Sprintf("directory-%04d/", index), Mode: 0o700, Typeflag: tar.TypeDir}); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	server, asset := serve(t, output.Bytes())
	defer server.Close()
	asset.Format = "tar"
	a := Adapter{HTTP: server.Client(), CacheDir: t.TempDir()}
	if _, err := a.FetchAndExtract(context.Background(), asset, filepath.Join(t.TempDir(), "out")); err == nil || !strings.Contains(err.Error(), "entry count") {
		t.Fatalf("err=%v", err)
	}
}

func TestTarRejectsDeclaredExpandedSizeBeforeReadingBody(t *testing.T) {
	var output bytes.Buffer
	w := tar.NewWriter(&output)
	if err := w.WriteHeader(&tar.Header{Name: "huge.ttf", Mode: 0o600, Typeflag: tar.TypeReg, Size: defaultExpandedBytes + 1}); err != nil {
		t.Fatal(err)
	}
	// Deliberately leave the body truncated: size validation must fail from the
	// header before attempting to spool hundreds of MiB.
	server, asset := serve(t, output.Bytes())
	defer server.Close()
	asset.Format = "tar"
	a := Adapter{HTTP: server.Client(), CacheDir: t.TempDir(), Limits: Limits{EntryBytes: defaultExpandedBytes + 1}}
	if _, err := a.FetchAndExtract(context.Background(), asset, filepath.Join(t.TempDir(), "out")); err == nil || !strings.Contains(err.Error(), "expanded size") {
		t.Fatalf("err=%v", err)
	}
}
