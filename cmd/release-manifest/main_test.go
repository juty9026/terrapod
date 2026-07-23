package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/juty9026/terrapod/internal/release"
)

func TestRunWritesDeterministicManifest(t *testing.T) {
	dir := t.TempDir()
	write := func(name, contents string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	assets := []string{
		"catalog,,," + write("resources.json", "catalog"),
		"binary,linux,arm64," + write("tpod-linux-arm64", "linux-arm64"),
		"source,,," + write("terrapod-source.tar.gz", "source"),
		"binary,darwin,amd64," + write("tpod-darwin-amd64", "darwin-amd64"),
		"binary,linux,amd64," + write("tpod-linux-amd64", "linux-amd64"),
		"binary,darwin,arm64," + write("tpod-darwin-arm64", "darwin-arm64"),
	}

	var output bytes.Buffer
	if err := run([]string{
		"--version", "1.2.3",
		"--catalog-schema", "1",
		"--state-schema", "1",
		"--asset", assets[0],
		"--asset", assets[1],
		"--asset", assets[2],
		"--asset", assets[3],
		"--asset", assets[4],
		"--asset", assets[5],
	}, &output); err != nil {
		t.Fatal(err)
	}

	var manifest release.Manifest
	if err := json.Unmarshal(output.Bytes(), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Version != "1.2.3" || manifest.CatalogSchema != 1 || manifest.StateSchema != 1 {
		t.Fatalf("unexpected manifest metadata: %+v", manifest)
	}
	if manifest.TrustedKeys == nil || len(manifest.TrustedKeys) != 0 {
		t.Fatalf("trustedKeys must be an empty array: %#v", manifest.TrustedKeys)
	}
	wantPlatforms := []string{"darwin/amd64", "darwin/arm64", "linux/amd64", "linux/arm64"}
	for index, want := range wantPlatforms {
		got := manifest.Assets[index].OS + "/" + manifest.Assets[index].Arch
		if got != want {
			t.Fatalf("asset %d platform = %q, want %q", index, got, want)
		}
	}
	if manifest.Assets[4].Kind != "source" || manifest.Assets[5].Kind != "catalog" {
		t.Fatalf("singleton asset order = %q, %q", manifest.Assets[4].Kind, manifest.Assets[5].Kind)
	}
	if !bytes.HasSuffix(output.Bytes(), []byte("\n")) || !strings.Contains(output.String(), "\n  \"assets\": [") {
		t.Fatalf("manifest is not indented with a trailing newline:\n%s", output.String())
	}
}

func TestRunRejectsInvalidReleaseInputs(t *testing.T) {
	file := filepath.Join(t.TempDir(), "asset")
	if err := os.WriteFile(file, []byte("asset"), 0o600); err != nil {
		t.Fatal(err)
	}
	validAssets := []string{
		"binary,darwin,amd64," + file,
		"binary,darwin,arm64," + file,
		"binary,linux,amd64," + file,
		"binary,linux,arm64," + file,
		"source,,," + file,
		"catalog,,," + file,
	}
	for _, test := range []struct {
		name    string
		version string
		assets  []string
	}{
		{name: "prerelease", version: "1.2.3-rc.1", assets: validAssets},
		{name: "missing platform", version: "1.2.3", assets: validAssets[:5]},
		{name: "duplicate platform", version: "1.2.3", assets: append(append([]string{}, validAssets...), validAssets[0])},
	} {
		t.Run(test.name, func(t *testing.T) {
			args := []string{"--version", test.version, "--catalog-schema", "1", "--state-schema", "1"}
			for _, asset := range test.assets {
				args = append(args, "--asset", asset)
			}
			if err := run(args, &bytes.Buffer{}); err == nil {
				t.Fatal("run succeeded, want error")
			}
		})
	}
}

func TestRunRendersDevelopmentCatalogForStableReleaseBeforeHashing(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.json")
	outputCatalog := filepath.Join(dir, "resources.json")
	sourceData := []byte(`{"version":1,"release":"development","config":{"version":1,"fields":[]},"resources":[]}` + "\n")
	if err := os.WriteFile(source, sourceData, 0o600); err != nil {
		t.Fatal(err)
	}
	write := func(name string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	args := []string{
		"--version", "1.2.3", "--catalog-schema", "1", "--state-schema", "1",
		"--catalog-source", source, "--catalog-output", outputCatalog,
		"--asset", "binary,darwin,amd64," + write("tpod-darwin-amd64"),
		"--asset", "binary,darwin,arm64," + write("tpod-darwin-arm64"),
		"--asset", "binary,linux,amd64," + write("tpod-linux-amd64"),
		"--asset", "binary,linux,arm64," + write("tpod-linux-arm64"),
		"--asset", "source,,," + write("terrapod-source.tar.gz"),
		"--asset", "catalog,,," + outputCatalog,
	}
	var first, second bytes.Buffer
	if err := run(args, &first); err != nil {
		t.Fatal(err)
	}
	firstCatalog, err := os.ReadFile(outputCatalog)
	if err != nil {
		t.Fatal(err)
	}
	if err := run(args, &second); err != nil {
		t.Fatal(err)
	}
	secondCatalog, _ := os.ReadFile(outputCatalog)
	if !bytes.Equal(firstCatalog, secondCatalog) || first.String() != second.String() {
		t.Fatal("rendered catalog or manifest is not deterministic")
	}
	var rendered struct {
		Release string `json:"release"`
	}
	if err := json.Unmarshal(firstCatalog, &rendered); err != nil || rendered.Release != "1.2.3" {
		t.Fatalf("rendered catalog release=%q err=%v", rendered.Release, err)
	}
	unchanged, _ := os.ReadFile(source)
	if !bytes.Equal(unchanged, sourceData) {
		t.Fatal("rendering modified the development source catalog")
	}
	var manifest release.Manifest
	if err := json.Unmarshal(first.Bytes(), &manifest); err != nil {
		t.Fatal(err)
	}
	for _, asset := range manifest.Assets {
		if asset.Kind == "catalog" {
			sum := sha256.Sum256(firstCatalog)
			if asset.SHA256 != hex.EncodeToString(sum[:]) {
				t.Fatalf("catalog digest=%q, want rendered digest", asset.SHA256)
			}
			return
		}
	}
	t.Fatal("catalog asset is missing")
}
