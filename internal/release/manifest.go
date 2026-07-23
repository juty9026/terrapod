package release

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
)

const (
	CompiledMinCatalogSchema = 1
	CompiledMaxCatalogSchema = 1
	CompiledMinStateSchema   = 1
	CompiledMaxStateSchema   = 1

	MaxManifestSize = 1 << 20
	MaxAssetSize    = 8 << 30
)

var (
	stableSemVerPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	assetNamePattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,254}$`)
	digestPattern       = regexp.MustCompile(`^[0-9a-f]{64}$`)
	keyIDPattern        = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
)

type Asset struct {
	Kind   string `json:"kind"`
	OS     string `json:"os,omitempty"`
	Arch   string `json:"arch,omitempty"`
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type TrustedKey struct {
	ID        string `json:"id"`
	PublicKey string `json:"publicKey"`
}

type Manifest struct {
	Version       string       `json:"version"`
	CatalogSchema int          `json:"catalogSchema"`
	StateSchema   int          `json:"stateSchema"`
	TrustedKeys   []TrustedKey `json:"trustedKeys"`
	Assets        []Asset      `json:"assets"`
}

type Verifier struct {
	Keys                                       map[string]ed25519.PublicKey
	MinCatalog, MaxCatalog, MinState, MaxState int
}

type signatureEnvelope struct {
	KeyID     string `json:"keyId"`
	Algorithm string `json:"algorithm"`
	Signature string `json:"signature"`
}

func (v Verifier) VerifyManifest(data, signature []byte) (Manifest, error) {
	if len(data) == 0 || len(data) > MaxManifestSize {
		return Manifest{}, fmt.Errorf("release manifest size is outside 1..%d bytes", MaxManifestSize)
	}
	if len(signature) == 0 || len(signature) > MaxManifestSize {
		return Manifest{}, fmt.Errorf("release signature size is outside 1..%d bytes", MaxManifestSize)
	}
	if len(v.Keys) == 0 {
		return Manifest{}, errors.New("release verifier has no trusted keys")
	}
	v = v.withCompiledSchemaDefaults()
	if err := validateSchemaRange(v.MinCatalog, v.MaxCatalog, "catalog"); err != nil {
		return Manifest{}, err
	}
	if err := validateSchemaRange(v.MinState, v.MaxState, "state"); err != nil {
		return Manifest{}, err
	}

	var envelope signatureEnvelope
	if err := decodeStrict(signature, &envelope); err != nil {
		return Manifest{}, fmt.Errorf("decode release signature: %w", err)
	}
	if envelope.Algorithm != "ed25519" {
		return Manifest{}, fmt.Errorf("unsupported signature algorithm %q", envelope.Algorithm)
	}
	publicKey, ok := v.Keys[envelope.KeyID]
	if !ok {
		return Manifest{}, fmt.Errorf("unknown signature key ID %q", envelope.KeyID)
	}
	if len(publicKey) != ed25519.PublicKeySize {
		return Manifest{}, fmt.Errorf("signature key %q has length %d, want %d", envelope.KeyID, len(publicKey), ed25519.PublicKeySize)
	}
	decodedSignature, err := base64.StdEncoding.Strict().DecodeString(envelope.Signature)
	if err != nil {
		return Manifest{}, fmt.Errorf("decode release signature: %w", err)
	}
	if base64.StdEncoding.EncodeToString(decodedSignature) != envelope.Signature {
		return Manifest{}, errors.New("non-canonical release signature encoding")
	}
	if len(decodedSignature) != ed25519.SignatureSize {
		return Manifest{}, fmt.Errorf("release signature length is %d, want %d", len(decodedSignature), ed25519.SignatureSize)
	}
	if !ed25519.Verify(publicKey, data, decodedSignature) {
		return Manifest{}, errors.New("release manifest signature verification failed")
	}

	var manifest Manifest
	if err := decodeStrict(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode release manifest: %w", err)
	}
	if err := v.validate(manifest); err != nil {
		return Manifest{}, fmt.Errorf("validate release manifest: %w", err)
	}
	return manifest, nil
}

func (v Verifier) withCompiledSchemaDefaults() Verifier {
	if v.MinCatalog == 0 && v.MaxCatalog == 0 {
		v.MinCatalog, v.MaxCatalog = CompiledMinCatalogSchema, CompiledMaxCatalogSchema
	}
	if v.MinState == 0 && v.MaxState == 0 {
		v.MinState, v.MaxState = CompiledMinStateSchema, CompiledMaxStateSchema
	}
	return v
}

func (m Manifest) BinaryAsset(goos, goarch string) (Asset, error) {
	if !supportedPlatform(goos, goarch) {
		return Asset{}, fmt.Errorf("unsupported platform %s/%s", goos, goarch)
	}
	for _, asset := range m.Assets {
		if asset.Kind == "binary" && asset.OS == goos && asset.Arch == goarch {
			return asset, nil
		}
	}
	return Asset{}, fmt.Errorf("release manifest has no binary for %s/%s", goos, goarch)
}

func (m Manifest) SourceAsset() (Asset, error) {
	return m.singletonAsset("source")
}

func (m Manifest) CatalogAsset() (Asset, error) {
	return m.singletonAsset("catalog")
}

func (m Manifest) singletonAsset(kind string) (Asset, error) {
	var found Asset
	count := 0
	for _, asset := range m.Assets {
		if asset.Kind == kind {
			found = asset
			count++
		}
	}
	if count != 1 {
		return Asset{}, fmt.Errorf("release manifest has %d %s assets, want 1", count, kind)
	}
	return found, nil
}

func (v Verifier) validate(manifest Manifest) error {
	if !stableSemVerPattern.MatchString(manifest.Version) {
		return fmt.Errorf("version %q is not stable SemVer", manifest.Version)
	}
	if manifest.CatalogSchema < v.MinCatalog || manifest.CatalogSchema > v.MaxCatalog {
		return fmt.Errorf("catalog schema %d is outside supported range %d..%d", manifest.CatalogSchema, v.MinCatalog, v.MaxCatalog)
	}
	if manifest.StateSchema < v.MinState || manifest.StateSchema > v.MaxState {
		return fmt.Errorf("state schema %d is outside supported range %d..%d", manifest.StateSchema, v.MinState, v.MaxState)
	}
	if manifest.TrustedKeys == nil {
		return errors.New("trustedKeys is required")
	}
	if err := v.validateTrustedKeys(manifest.TrustedKeys); err != nil {
		return err
	}

	expectedPlatforms := map[string]bool{
		"darwin/amd64": false,
		"darwin/arm64": false,
		"linux/amd64":  false,
		"linux/arm64":  false,
	}
	names := make(map[string]struct{}, len(manifest.Assets))
	sourceCount, catalogCount := 0, 0
	for _, asset := range manifest.Assets {
		if !assetNamePattern.MatchString(asset.Name) {
			return fmt.Errorf("unsafe asset name %q", asset.Name)
		}
		if _, duplicate := names[asset.Name]; duplicate {
			return fmt.Errorf("duplicate asset name %q", asset.Name)
		}
		names[asset.Name] = struct{}{}
		if asset.Size <= 0 || asset.Size > MaxAssetSize {
			return fmt.Errorf("asset %q size %d is outside 1..%d", asset.Name, asset.Size, MaxAssetSize)
		}
		if !digestPattern.MatchString(asset.SHA256) {
			return fmt.Errorf("asset %q sha256 must be 64 lowercase hexadecimal characters", asset.Name)
		}

		switch asset.Kind {
		case "binary":
			platform := asset.OS + "/" + asset.Arch
			seen, supported := expectedPlatforms[platform]
			if !supported {
				return fmt.Errorf("asset %q has unsupported binary platform %s", asset.Name, platform)
			}
			if seen {
				return fmt.Errorf("duplicate binary platform %s", platform)
			}
			expectedPlatforms[platform] = true
		case "source":
			if asset.OS != "" || asset.Arch != "" {
				return fmt.Errorf("source asset %q must not declare a platform", asset.Name)
			}
			sourceCount++
		case "catalog":
			if asset.OS != "" || asset.Arch != "" {
				return fmt.Errorf("catalog asset %q must not declare a platform", asset.Name)
			}
			catalogCount++
		default:
			return fmt.Errorf("asset %q has unsupported kind %q", asset.Name, asset.Kind)
		}
	}
	for platform, present := range expectedPlatforms {
		if !present {
			return fmt.Errorf("missing binary platform %s", platform)
		}
	}
	if sourceCount != 1 {
		return fmt.Errorf("release manifest must contain exactly one source asset, got %d", sourceCount)
	}
	if catalogCount != 1 {
		return fmt.Errorf("release manifest must contain exactly one catalog asset, got %d", catalogCount)
	}
	if len(manifest.Assets) != 6 {
		return fmt.Errorf("release manifest must contain exactly six assets, got %d", len(manifest.Assets))
	}
	return nil
}

func (v Verifier) validateTrustedKeys(keys []TrustedKey) error {
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		if !keyIDPattern.MatchString(key.ID) {
			return fmt.Errorf("invalid trusted key ID %q", key.ID)
		}
		if _, duplicate := seen[key.ID]; duplicate {
			return fmt.Errorf("duplicate trusted key ID %q", key.ID)
		}
		seen[key.ID] = struct{}{}
		decoded, err := base64.StdEncoding.Strict().DecodeString(key.PublicKey)
		if err != nil || base64.StdEncoding.EncodeToString(decoded) != key.PublicKey {
			return fmt.Errorf("trusted key %q has invalid public key encoding", key.ID)
		}
		if len(decoded) != ed25519.PublicKeySize {
			return fmt.Errorf("trusted key %q has length %d, want %d", key.ID, len(decoded), ed25519.PublicKeySize)
		}
		if current, exists := v.Keys[key.ID]; exists && !bytes.Equal(current, decoded) {
			return fmt.Errorf("manifest cannot replace trusted key %q", key.ID)
		}
	}
	return nil
}

func validateSchemaRange(minimum, maximum int, name string) error {
	if minimum <= 0 || maximum < minimum {
		return fmt.Errorf("invalid %s schema range %d..%d", name, minimum, maximum)
	}
	return nil
}

func supportedPlatform(goos, goarch string) bool {
	return (goos == "darwin" || goos == "linux") && (goarch == "amd64" || goarch == "arm64")
}

func decodeStrict(contents []byte, target any) error {
	if err := rejectDuplicateJSONKeys(contents); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return fmt.Errorf("trailing JSON: %w", err)
	}
	return nil
}

func rejectDuplicateJSONKeys(contents []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.UseNumber()
	if err := scanJSONValue(decoder); err != nil {
		return err
	}
	if token, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("trailing JSON token %v", token)
		}
		return err
	}
	return nil
}

func scanJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, compound := token.(json.Delim)
	if !compound {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("JSON object key is not a string")
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("duplicate JSON key %q", key)
			}
			seen[key] = struct{}{}
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delimiter)
	}
	closing, err := decoder.Token()
	if err != nil {
		return err
	}
	want := json.Delim('}')
	if delimiter == '[' {
		want = ']'
	}
	if closing != want {
		return fmt.Errorf("unexpected JSON delimiter %q", closing)
	}
	return nil
}
