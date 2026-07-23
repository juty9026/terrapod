package main

import (
	"bytes"
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
