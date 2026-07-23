package release

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

var testSeed = []byte("0123456789abcdef0123456789abcdef")

func TestVerifierVerifiesFixture(t *testing.T) {
	data, err := os.ReadFile("testdata/release.json")
	if err != nil {
		t.Fatal(err)
	}
	signature, err := os.ReadFile("testdata/release.json.sig")
	if err != nil {
		t.Fatal(err)
	}
	root := ed25519.NewKeyFromSeed(testSeed).Public().(ed25519.PublicKey)
	if _, err := testVerifier(root).VerifyManifest(data, signature); err != nil {
		t.Fatal(err)
	}
}

func TestVerifierVerifyManifest(t *testing.T) {
	data := validManifestJSON(t)
	privateKey := ed25519.NewKeyFromSeed(testSeed)
	verifier := testVerifier(privateKey.Public().(ed25519.PublicKey))

	manifest, err := verifier.VerifyManifest(data, signManifest(t, "root", privateKey, data))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Version != "1.2.3" || len(manifest.Assets) != 6 {
		t.Fatalf("manifest=%+v", manifest)
	}
	asset, err := manifest.BinaryAsset("linux", "arm64")
	if err != nil || asset.Name != "tpod-linux-arm64" {
		t.Fatalf("asset=%+v err=%v", asset, err)
	}
	if _, err := manifest.BinaryAsset("freebsd", "amd64"); err == nil || !strings.Contains(err.Error(), "unsupported platform") {
		t.Fatalf("unsupported platform err=%v", err)
	}
}

func TestVerifierRejectsInvalidSignatureAndUntrustedSigner(t *testing.T) {
	data := validManifestJSON(t)
	trusted := ed25519.NewKeyFromSeed(testSeed)
	untrusted := ed25519.NewKeyFromSeed([]byte("abcdef0123456789abcdef0123456789"))
	verifier := testVerifier(trusted.Public().(ed25519.PublicKey))

	modified := append([]byte(nil), data...)
	modified[len(modified)-2] ^= 1
	if _, err := verifier.VerifyManifest(modified, signManifest(t, "root", trusted, data)); err == nil {
		t.Fatal("modified manifest passed verification")
	}
	withKey := validManifest(t)
	withKey.TrustedKeys = []TrustedKey{{ID: "next", PublicKey: base64.StdEncoding.EncodeToString(untrusted.Public().(ed25519.PublicKey))}}
	keyData := encodeManifest(t, withKey)
	if _, err := verifier.VerifyManifest(keyData, signManifest(t, "outsider", untrusted, keyData)); err == nil || !strings.Contains(err.Error(), "unknown signature key") {
		t.Fatalf("untrusted signer err=%v", err)
	}
}

func TestVerifierAuthorizesOnlyAdditiveTrustedKeys(t *testing.T) {
	root := ed25519.NewKeyFromSeed(testSeed)
	next := ed25519.NewKeyFromSeed([]byte("abcdef0123456789abcdef0123456789"))
	manifest := validManifest(t)
	manifest.TrustedKeys = []TrustedKey{{ID: "next", PublicKey: base64.StdEncoding.EncodeToString(next.Public().(ed25519.PublicKey))}}
	data := encodeManifest(t, manifest)
	got, err := testVerifier(root.Public().(ed25519.PublicKey)).VerifyManifest(data, signManifest(t, "root", root, data))
	if err != nil || len(got.TrustedKeys) != 1 {
		t.Fatalf("trusted keys=%+v err=%v", got.TrustedKeys, err)
	}

	manifest.TrustedKeys = []TrustedKey{{ID: "root", PublicKey: base64.StdEncoding.EncodeToString(root.Public().(ed25519.PublicKey))}}
	data = encodeManifest(t, manifest)
	if _, err := testVerifier(root.Public().(ed25519.PublicKey)).VerifyManifest(data, signManifest(t, "root", root, data)); err == nil || !strings.Contains(err.Error(), "already trusted") {
		t.Fatalf("existing key addition err=%v", err)
	}
}

func TestVerifierTrustAfterRetainsCompiledAndPersistedKeys(t *testing.T) {
	root := ed25519.NewKeyFromSeed(testSeed)
	persisted := ed25519.NewKeyFromSeed([]byte("abcdef0123456789abcdef0123456789"))
	addition := ed25519.NewKeyFromSeed([]byte("fedcba9876543210fedcba9876543210"))
	proof := trustProof(t, "root", root, "persisted", persisted.Public().(ed25519.PublicKey), "1.2.2")
	verifier := Verifier{
		CompiledKeys:    map[string]ed25519.PublicKey{"root": root.Public().(ed25519.PublicKey)},
		PersistedProofs: []TrustProof{proof},
	}
	manifest := validManifest(t)
	manifest.TrustedKeys = []TrustedKey{{ID: "next", PublicKey: base64.StdEncoding.EncodeToString(addition.Public().(ed25519.PublicKey))}}
	data := encodeManifest(t, manifest)
	verified, err := verifier.VerifyManifest(data, signManifest(t, "persisted", persisted, data))
	if err != nil {
		t.Fatal(err)
	}
	trust, err := verifier.TrustAfter(verified)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"root", "persisted", "next"} {
		if _, ok := trust[id]; !ok {
			t.Fatalf("TrustAfter omitted %q: %v", id, trust)
		}
	}
	delete(trust, "root")
	again, err := verifier.TrustAfter(verified)
	if err != nil || again["root"] == nil {
		t.Fatalf("TrustAfter exposed mutable trust state: keys=%v err=%v", again, err)
	}
	if _, err := verifier.TrustAfter(validManifest(t)); err == nil || !strings.Contains(err.Error(), "not verified") {
		t.Fatalf("unverified manifest TrustAfter err=%v", err)
	}
}

func TestVerifierAllowsOnlySameManifestKeyAdditionReplay(t *testing.T) {
	root := ed25519.NewKeyFromSeed(testSeed)
	next := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize))
	manifest := validManifest(t)
	manifest.TrustedKeys = []TrustedKey{{ID: "next", PublicKey: base64.StdEncoding.EncodeToString(next.Public().(ed25519.PublicKey))}}
	data := encodeManifest(t, manifest)
	verifier := Verifier{CompiledKeys: map[string]ed25519.PublicKey{"root": root.Public().(ed25519.PublicKey)}, PersistedProofs: []TrustProof{{Manifest: data, Signature: signManifest(t, "root", root, data)}}}
	if _, err := verifier.VerifyManifest(data, signManifest(t, "root", root, data)); err != nil {
		t.Fatalf("same manifest replay: %v", err)
	}
	manifest.Version = "1.2.4"
	other := encodeManifest(t, manifest)
	if _, err := verifier.VerifyManifest(other, signManifest(t, "root", root, other)); err == nil {
		t.Fatal("different manifest reused existing key ID")
	}
}

func TestVerifierRejectsAdditionForPersistedKeyID(t *testing.T) {
	root := ed25519.NewKeyFromSeed(testSeed)
	persisted := ed25519.NewKeyFromSeed([]byte("abcdef0123456789abcdef0123456789"))
	proof := trustProof(t, "root", root, "persisted", persisted.Public().(ed25519.PublicKey), "1.2.2")
	verifier := Verifier{
		CompiledKeys:    map[string]ed25519.PublicKey{"root": root.Public().(ed25519.PublicKey)},
		PersistedProofs: []TrustProof{proof},
	}
	manifest := validManifest(t)
	manifest.TrustedKeys = []TrustedKey{{ID: "persisted", PublicKey: base64.StdEncoding.EncodeToString(persisted.Public().(ed25519.PublicKey))}}
	data := encodeManifest(t, manifest)
	if _, err := verifier.VerifyManifest(data, signManifest(t, "root", root, data)); err == nil || !strings.Contains(err.Error(), "already trusted") {
		t.Fatalf("persisted key addition err=%v", err)
	}
}

func TestVerifierRequiresAndProtectsCompiledTrust(t *testing.T) {
	root := ed25519.NewKeyFromSeed(testSeed)
	other := ed25519.NewKeyFromSeed([]byte("abcdef0123456789abcdef0123456789"))
	data := validManifestJSON(t)

	withoutRoot := Verifier{PersistedProofs: []TrustProof{trustProof(t, "root", root, "persisted", other.Public().(ed25519.PublicKey), "1.2.2")}}
	if _, err := withoutRoot.VerifyManifest(data, signManifest(t, "persisted", root, data)); err == nil || !strings.Contains(err.Error(), "compiled trust root") {
		t.Fatalf("missing compiled root err=%v", err)
	}
	conflict := Verifier{CompiledKeys: map[string]ed25519.PublicKey{"root": root.Public().(ed25519.PublicKey)}, PersistedProofs: []TrustProof{trustProof(t, "root", root, "root", other.Public().(ed25519.PublicKey), "1.2.2")}}
	if _, err := conflict.VerifyManifest(data, signManifest(t, "root", root, data)); err == nil || !strings.Contains(err.Error(), "already trusted") {
		t.Fatalf("compiled root addition err=%v", err)
	}
}

func TestVerifierSchemaRangeCannotWidenCompiledCompatibility(t *testing.T) {
	root := ed25519.NewKeyFromSeed(testSeed)
	manifest := validManifest(t)
	manifest.CatalogSchema = CompiledMaxCatalogSchema + 1
	manifest.StateSchema = CompiledMaxStateSchema + 1
	data := encodeManifest(t, manifest)
	verifier := Verifier{
		CompiledKeys: map[string]ed25519.PublicKey{"root": root.Public().(ed25519.PublicKey)},
		MinCatalog:   0, MaxCatalog: CompiledMaxCatalogSchema + 1,
		MinState: 0, MaxState: CompiledMaxStateSchema + 1,
	}
	if _, err := verifier.VerifyManifest(data, signManifest(t, "root", root, data)); err == nil || !strings.Contains(err.Error(), "catalog schema") {
		t.Fatalf("widened schema range err=%v", err)
	}
	minimum, maximum, err := effectiveSchemaRange(2, 3, 1, 0, "test")
	if err != nil || minimum != 2 || maximum != 3 {
		t.Fatalf("lower requested minimum widened compiled range: %d..%d err=%v", minimum, maximum, err)
	}
}

func TestVerifierRejectsInvalidManifest(t *testing.T) {
	root := ed25519.NewKeyFromSeed(testSeed)
	tests := []struct {
		name string
		edit func(*Manifest)
		want string
	}{
		{name: "prerelease", edit: func(m *Manifest) { m.Version = "1.2.3-rc.1" }, want: "stable SemVer"},
		{name: "build metadata", edit: func(m *Manifest) { m.Version = "1.2.3+build" }, want: "stable SemVer"},
		{name: "duplicate platform", edit: func(m *Manifest) { m.Assets[1].OS, m.Assets[1].Arch = "darwin", "amd64" }, want: "duplicate binary platform"},
		{name: "traversal", edit: func(m *Manifest) { m.Assets[0].Name = "../tpod" }, want: "unsafe asset name"},
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
			manifest := validManifest(t)
			tt.edit(&manifest)
			data := encodeManifest(t, manifest)
			_, err := testVerifier(root.Public().(ed25519.PublicKey)).VerifyManifest(data, signManifest(t, "root", root, data))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err=%v, want %q", err, tt.want)
			}
		})
	}
}

func TestVerifierRejectsUnknownDuplicateAndTrailingJSON(t *testing.T) {
	root := ed25519.NewKeyFromSeed(testSeed)
	verifier := testVerifier(root.Public().(ed25519.PublicKey))
	tests := []struct{ name, data string }{
		{name: "unknown", data: `{"version":"1.2.3","unknown":true}`},
		{name: "duplicate", data: `{"version":"1.2.3","version":"1.2.4"}`},
		{name: "trailing", data: string(validManifestJSON(t)) + ` {}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := []byte(tt.data)
			if _, err := verifier.VerifyManifest(data, signManifest(t, "root", root, data)); err == nil {
				t.Fatal("invalid JSON accepted")
			}
		})
	}
}

func testVerifier(root ed25519.PublicKey) Verifier {
	return Verifier{CompiledKeys: map[string]ed25519.PublicKey{"root": root}, MinCatalog: 1, MaxCatalog: 1, MinState: 1, MaxState: 1}
}

func validManifestJSON(t *testing.T) []byte {
	t.Helper()
	return encodeManifest(t, validManifest(t))
}

func validManifest(t *testing.T) Manifest {
	t.Helper()
	digests := []string{
		strings.Repeat("1", 64), strings.Repeat("2", 64), strings.Repeat("3", 64),
		strings.Repeat("4", 64), strings.Repeat("5", 64), strings.Repeat("6", 64),
	}
	return Manifest{Version: "1.2.3", CatalogSchema: 1, StateSchema: 1, TrustedKeys: []TrustedKey{}, Assets: []Asset{
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

func signManifest(t *testing.T, keyID string, privateKey ed25519.PrivateKey, data []byte) []byte {
	t.Helper()
	envelope := signatureEnvelope{KeyID: keyID, Algorithm: "ed25519", Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, data))}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
