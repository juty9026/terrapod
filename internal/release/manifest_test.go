package release

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestParseManifestVerifiesFixture(t *testing.T) {
	data, err := os.ReadFile("testdata/release.json")
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := ParseManifest(data)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Version != "1.0.0" {
		t.Fatalf("version=%q", manifest.Version)
	}
}

func TestParseManifestAcceptsStableManifest(t *testing.T) {
	data := stableManifestJSON(t)
	manifest, err := ParseManifest(data)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Version != "1.2.3" {
		t.Fatalf("version=%q", manifest.Version)
	}
	if _, err := manifest.Digest(); err != nil {
		t.Fatal(err)
	}
}

func TestParseManifestRejectsMalformedTrailingAndInvalidAssets(t *testing.T) {
	for _, data := range [][]byte{
		nil,
		[]byte(`{"version":"1.2.3"}` + "\n{}"),
		manifestWithDuplicateAsset(t),
		manifestWithWrongPlatformSet(t),
	} {
		if _, err := ParseManifest(data); err == nil {
			t.Fatalf("accepted %q", data)
		}
	}
}

func TestCompareStableVersions(t *testing.T) {
	tests := []struct {
		left, right string
		want        int
	}{
		{"1.2.3", "1.2.3", 0},
		{"1.2.4", "1.2.3", 1},
		{"2.0.0", "10.0.0", -1},
		{"100000000000000000000.0.0", "9.9.9", 1},
	}
	for _, test := range tests {
		got, err := CompareStableVersions(test.left, test.right)
		if err != nil || got != test.want {
			t.Fatalf("CompareStableVersions(%q, %q)=%d,%v want %d", test.left, test.right, got, err, test.want)
		}
	}
	for _, invalid := range []string{"v1.2.3", "1.2", "1.02.3", "1.2.3-beta"} {
		if _, err := CompareStableVersions(invalid, "1.2.3"); err == nil {
			t.Fatalf("invalid version %q accepted", invalid)
		}
	}
}

func TestParsedManifestSelectsAssets(t *testing.T) {
	manifest, err := ParseManifest(stableManifestJSON(t))
	if err != nil {
		t.Fatal(err)
	}
	asset, err := manifest.BinaryAsset("linux", "arm64")
	if err != nil || asset.Name != "tpod-linux-arm64" {
		t.Fatalf("asset=%+v err=%v", asset, err)
	}
	if _, err := manifest.BinaryAsset("freebsd", "amd64"); err == nil || !strings.Contains(err.Error(), "unsupported platform") {
		t.Fatalf("unsupported platform err=%v", err)
	}
}

func TestParseManifestRejectsInvalidManifest(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Manifest)
		want string
	}{
		{name: "prerelease", edit: func(m *Manifest) { m.Version = "1.2.3-rc.1" }, want: "stable SemVer"},
		{name: "unsafe name", edit: func(m *Manifest) { m.Assets[0].Name = "../tpod" }, want: "unsafe asset name"},
		{name: "uppercase name", edit: func(m *Manifest) { m.Assets[0].Name = "Tpod" }, want: "unsafe asset name"},
		{name: "unsupported os", edit: func(m *Manifest) { m.Assets[0].OS = "freebsd" }, want: "unsupported binary platform"},
		{name: "missing source", edit: func(m *Manifest) { m.Assets = append(m.Assets[:4], m.Assets[5]) }, want: "exactly one source"},
		{name: "wrong source digest", edit: func(m *Manifest) { m.Assets[4].SHA256 = strings.Repeat("a", 63) }, want: "sha256"},
		{name: "wrong catalog digest", edit: func(m *Manifest) { m.Assets[5].SHA256 = strings.Repeat("A", 64) }, want: "sha256"},
		{name: "zero size", edit: func(m *Manifest) { m.Assets[5].Size = 0 }, want: "size"},
		{name: "huge size", edit: func(m *Manifest) { m.Assets[5].Size = MaxAssetSize + 1 }, want: "size"},
		{name: "catalog floor", edit: func(m *Manifest) { m.CatalogSchema = 0 }, want: "catalog schema"},
		{name: "catalog ceiling", edit: func(m *Manifest) { m.CatalogSchema = 2 }, want: "catalog schema"},
		{name: "state floor", edit: func(m *Manifest) { m.StateSchema = 0 }, want: "state schema"},
		{name: "state ceiling", edit: func(m *Manifest) { m.StateSchema = 2 }, want: "state schema"},
		{name: "duplicate name", edit: func(m *Manifest) { m.Assets[1].Name = m.Assets[0].Name }, want: "duplicate asset name"},
		{name: "source platform", edit: func(m *Manifest) { m.Assets[4].OS = "linux" }, want: "must not declare a platform"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifest := stableManifest(t)
			tt.edit(&manifest)
			_, err := ParseManifest(encodeManifest(t, manifest))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err=%v, want %q", err, tt.want)
			}
		})
	}
}

func TestParseManifestRequiresExactCanonicalAssetNames(t *testing.T) {
	manifest := stableManifest(t)
	want := []string{
		"tpod-darwin-amd64",
		"tpod-darwin-arm64",
		"tpod-linux-amd64",
		"tpod-linux-arm64",
		"terrapod-source.tar.gz",
		"resources.json",
	}
	for index, asset := range manifest.Assets {
		if asset.Name != want[index] {
			t.Fatalf("asset[%d].Name=%q, want %q", index, asset.Name, want[index])
		}
	}

	for index := range manifest.Assets {
		t.Run(want[index], func(t *testing.T) {
			renamed := manifest
			renamed.Assets = append([]Asset(nil), manifest.Assets...)
			renamed.Assets[index].Name = "renamed-" + want[index]
			if _, err := ParseManifest(encodeManifest(t, renamed)); err == nil ||
				!strings.Contains(err.Error(), "canonical") {
				t.Fatalf("err=%v, want canonical asset name rejection", err)
			}
		})
	}
}

func TestParseManifestRejectsUnknownDuplicateAndTrailingJSON(t *testing.T) {
	tests := []struct{ name, data string }{
		{name: "unknown", data: `{"version":"1.2.3","unknown":true}`},
		{name: "duplicate", data: `{"version":"1.2.3","version":"1.2.4"}`},
		{name: "trailing", data: string(stableManifestJSON(t)) + ` {}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParseManifest([]byte(tt.data)); err == nil {
				t.Fatal("invalid JSON accepted")
			}
		})
	}
}

func stableManifestJSON(t *testing.T) []byte {
	t.Helper()
	return encodeManifest(t, stableManifest(t))
}

func manifestWithDuplicateAsset(t *testing.T) []byte {
	t.Helper()
	manifest := stableManifest(t)
	manifest.Assets[1].Name = manifest.Assets[0].Name
	return encodeManifest(t, manifest)
}

func manifestWithWrongPlatformSet(t *testing.T) []byte {
	t.Helper()
	manifest := stableManifest(t)
	manifest.Assets[0].OS = "freebsd"
	return encodeManifest(t, manifest)
}

func stableManifest(t *testing.T) Manifest {
	t.Helper()
	digests := []string{
		strings.Repeat("1", 64), strings.Repeat("2", 64), strings.Repeat("3", 64),
		strings.Repeat("4", 64), strings.Repeat("5", 64), strings.Repeat("6", 64),
	}
	return Manifest{Version: "1.2.3", CatalogSchema: 1, StateSchema: 1, Assets: []Asset{
		{Kind: "binary", OS: "darwin", Arch: "amd64", Name: "tpod-darwin-amd64", Size: 101, SHA256: digests[0]},
		{Kind: "binary", OS: "darwin", Arch: "arm64", Name: "tpod-darwin-arm64", Size: 102, SHA256: digests[1]},
		{Kind: "binary", OS: "linux", Arch: "amd64", Name: "tpod-linux-amd64", Size: 103, SHA256: digests[2]},
		{Kind: "binary", OS: "linux", Arch: "arm64", Name: "tpod-linux-arm64", Size: 104, SHA256: digests[3]},
		{Kind: "source", Name: "terrapod-source.tar.gz", Size: 105, SHA256: digests[4]},
		{Kind: "catalog", Name: "resources.json", Size: 106, SHA256: digests[5]},
	}}
}

func encodeManifest(t *testing.T, manifest Manifest) []byte {
	t.Helper()
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(data, '\n')
}
