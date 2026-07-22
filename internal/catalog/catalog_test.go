package catalog

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testKeyID = "test-2026"

var testSeed = []byte("0123456789abcdef0123456789abcdef")

func TestLoadVerified(t *testing.T) {
	catalogBytes := readFixture(t)
	path, signatures := writeSignedCatalog(t, catalogBytes, testKeyID)

	got, err := LoadVerified(path, signatures)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := sha256.Sum256(catalogBytes)
	if got.Digest != hex.EncodeToString(wantDigest[:]) {
		t.Fatalf("Digest = %q, want %x", got.Digest, wantDigest)
	}
	if got.KeyID != testKeyID {
		t.Fatalf("KeyID = %q, want %q", got.KeyID, testKeyID)
	}
	if got.Catalog.Release != "test-2026" || len(got.Catalog.Resources) != 1 {
		t.Fatalf("Catalog = %#v", got.Catalog)
	}
}

func TestSeedCatalogHasCurrentConfigSchemaAndNoResources(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join("..", "..", "catalog", "v1", "resources.json"))
	if err != nil {
		t.Fatal(err)
	}
	path, signatures := writeSignedCatalog(t, contents, testKeyID)
	got, err := LoadVerified(path, signatures)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Catalog.Resources) != 0 {
		t.Fatalf("Resources = %#v, want none", got.Catalog.Resources)
	}
	if len(got.Catalog.Config.Fields) != 9 {
		t.Fatalf("Config.Fields count = %d, want 9 current setup fields", len(got.Catalog.Config.Fields))
	}
}

func TestLoadVerifiedRejectsUntrustedBytes(t *testing.T) {
	tests := []struct {
		name       string
		catalogMut func([]byte) []byte
		envelope   func([]byte) []byte
		keyID      string
		want       string
	}{
		{
			name: "one byte catalog mutation",
			catalogMut: func(input []byte) []byte {
				mutated := append([]byte(nil), input...)
				mutated[len(mutated)-2] ^= 1
				return mutated
			},
			want: "signature verification failed",
		},
		{name: "unknown key ID", keyID: "unknown-2026", want: "unknown signature key ID"},
		{
			name: "unknown algorithm",
			envelope: func(signature []byte) []byte {
				return envelopeJSON(t, testKeyID, "rsa", base64.StdEncoding.EncodeToString(signature), "")
			},
			want: "unsupported signature algorithm",
		},
		{
			name: "invalid base64",
			envelope: func([]byte) []byte {
				return envelopeJSON(t, testKeyID, "ed25519", "%%%", "")
			},
			want: "decode signature",
		},
		{
			name: "wrong signature length",
			envelope: func([]byte) []byte {
				return envelopeJSON(t, testKeyID, "ed25519", base64.StdEncoding.EncodeToString([]byte("short")), "")
			},
			want: "signature length",
		},
		{
			name: "unknown envelope field",
			envelope: func(signature []byte) []byte {
				return envelopeJSON(t, testKeyID, "ed25519", base64.StdEncoding.EncodeToString(signature), `,"extra":true`)
			},
			want: "decode signature envelope",
		},
		{
			name: "trailing envelope JSON",
			envelope: func(signature []byte) []byte {
				return append(envelopeJSON(t, testKeyID, "ed25519", base64.StdEncoding.EncodeToString(signature), ""), []byte(" {}")...)
			},
			want: "trailing JSON",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := readFixture(t)
			path, signatures := writeSignedCatalog(t, original, testKeyID)
			if tt.catalogMut != nil {
				if err := os.WriteFile(path, tt.catalogMut(original), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if tt.keyID != "" {
				signature := ed25519.Sign(ed25519.NewKeyFromSeed(testSeed), original)
				if err := os.WriteFile(path+".sig", envelopeJSON(t, tt.keyID, "ed25519", base64.StdEncoding.EncodeToString(signature), ""), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if tt.envelope != nil {
				signature := ed25519.Sign(ed25519.NewKeyFromSeed(testSeed), original)
				if err := os.WriteFile(path+".sig", tt.envelope(signature), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			_, err := LoadVerified(path, signatures)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("LoadVerified() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestLoadVerifiedRejectsUnknownCatalogField(t *testing.T) {
	input := catalogObject(t)
	input["unexpected"] = true
	assertCatalogError(t, input, "unknown field")
}

func TestLoadVerifiedRejectsInvalidCatalog(t *testing.T) {
	tests := []struct {
		name string
		edit func(map[string]any)
		want string
	}{
		{
			name: "duplicate ID",
			edit: func(input map[string]any) {
				resources := resourcesOf(input)
				input["resources"] = append(resources, cloneObject(t, resources[0]))
			},
			want: `duplicate resource ID "core.ripgrep"`,
		},
		{
			name: "unknown dependency",
			edit: func(input map[string]any) {
				resourcesOf(input)[0]["dependsOn"] = []any{"core.missing"}
			},
			want: `unknown dependency "core.missing"`,
		},
		{
			name: "unsupported profile",
			edit: func(input map[string]any) {
				resourcesOf(input)[0]["profiles"] = []any{"linux"}
			},
			want: `unsupported profile "linux"`,
		},
		{
			name: "unsupported provider",
			edit: func(input map[string]any) {
				resourcesOf(input)[0]["provider"] = "shell"
			},
			want: `unsupported provider "shell"`,
		},
		{
			name: "duplicate command",
			edit: func(input map[string]any) {
				resourcesOf(input)[0]["commands"] = []any{"rg", "rg"}
			},
			want: `duplicate command "rg"`,
		},
		{
			name: "dependency cycle",
			edit: func(input map[string]any) {
				first := resourcesOf(input)[0]
				first["id"] = "core.beta"
				first["dependsOn"] = []any{"core.alpha"}
				second := cloneObject(t, first)
				second["id"] = "core.alpha"
				second["dependsOn"] = []any{"core.beta"}
				input["resources"] = []any{first, second}
			},
			want: "dependency cycle: core.alpha -> core.beta -> core.alpha",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := catalogObject(t)
			tt.edit(input)
			assertCatalogError(t, input, tt.want)
		})
	}
}

func TestLoadVerifiedRejectsUnsafePackageIdentifiers(t *testing.T) {
	for _, identifier := range []string{"../ripgrep", "-rf", "rip grep", "ripgrep\nnext", "ripgrep;touch", "$(touch)", "rípgrep"} {
		t.Run(identifier, func(t *testing.T) {
			input := catalogObject(t)
			resourcesOf(input)[0]["package"] = identifier
			assertCatalogError(t, input, "unsafe package identifier")
		})
	}
}

func TestLoadVerifiedBoundsCatalogAndSignature(t *testing.T) {
	t.Run("catalog", func(t *testing.T) {
		path, signatures := writeSignedCatalog(t, readFixture(t), testKeyID)
		if err := os.WriteFile(path, make([]byte, 4*1024*1024+1), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := LoadVerified(path, signatures)
		if err == nil || !strings.Contains(err.Error(), "exceeds 4 MiB") {
			t.Fatalf("LoadVerified() error = %v", err)
		}
	})

	t.Run("signature", func(t *testing.T) {
		path, signatures := writeSignedCatalog(t, readFixture(t), testKeyID)
		if err := os.WriteFile(path+".sig", make([]byte, 4*1024*1024+1), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := LoadVerified(path, signatures)
		if err == nil || !strings.Contains(err.Error(), "exceeds 4 MiB") {
			t.Fatalf("LoadVerified() error = %v", err)
		}
	})
}

func readFixture(t *testing.T) []byte {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("testdata", "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	return contents
}

func writeSignedCatalog(t *testing.T, contents []byte, keyID string) (string, SignatureSet) {
	t.Helper()
	privateKey := ed25519.NewKeyFromSeed(testSeed)
	path := filepath.Join(t.TempDir(), "catalog.json")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	signature := ed25519.Sign(privateKey, contents)
	envelope := envelopeJSON(t, keyID, "ed25519", base64.StdEncoding.EncodeToString(signature), "")
	if err := os.WriteFile(path+".sig", envelope, 0o600); err != nil {
		t.Fatal(err)
	}
	return path, SignatureSet{PublicKeys: map[string]ed25519.PublicKey{testKeyID: privateKey.Public().(ed25519.PublicKey)}}
}

func envelopeJSON(t *testing.T, keyID, algorithm, signature, extra string) []byte {
	t.Helper()
	return []byte(`{"keyId":` + quoted(t, keyID) + `,"algorithm":` + quoted(t, algorithm) + `,"signature":` + quoted(t, signature) + extra + `}`)
}

func quoted(t *testing.T, value string) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func catalogObject(t *testing.T) map[string]any {
	t.Helper()
	var input map[string]any
	if err := json.Unmarshal(readFixture(t), &input); err != nil {
		t.Fatal(err)
	}
	return input
}

func resourcesOf(input map[string]any) []map[string]any {
	values := input["resources"].([]any)
	resources := make([]map[string]any, len(values))
	for i := range values {
		resources[i] = values[i].(map[string]any)
	}
	return resources
}

func cloneObject(t *testing.T, input map[string]any) map[string]any {
	t.Helper()
	contents, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var clone map[string]any
	if err := json.Unmarshal(contents, &clone); err != nil {
		t.Fatal(err)
	}
	return clone
}

func assertCatalogError(t *testing.T, input map[string]any, want string) {
	t.Helper()
	contents, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	path, signatures := writeSignedCatalog(t, contents, testKeyID)
	_, err = LoadVerified(path, signatures)
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("LoadVerified() error = %v, want containing %q", err, want)
	}
}
