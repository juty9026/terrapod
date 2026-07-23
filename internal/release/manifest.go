package release

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
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
	Version        string       `json:"version"`
	CatalogSchema  int          `json:"catalogSchema"`
	StateSchema    int          `json:"stateSchema"`
	TrustedKeys    []TrustedKey `json:"trustedKeys"`
	Assets         []Asset      `json:"assets"`
	verified       bool
	trustBefore    [sha256.Size]byte
	trustAfter     map[string]ed25519.PublicKey
	manifestDigest string
}

type Verifier struct {
	CompiledKeys                               map[string]ed25519.PublicKey
	PersistedProofs                            []TrustProof
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
	effectiveTrust, provenance, err := v.effectiveTrust()
	if err != nil {
		return Manifest{}, err
	}
	v.MinCatalog, v.MaxCatalog, err = effectiveSchemaRange(CompiledMinCatalogSchema, CompiledMaxCatalogSchema, v.MinCatalog, v.MaxCatalog, "catalog")
	if err != nil {
		return Manifest{}, err
	}
	v.MinState, v.MaxState, err = effectiveSchemaRange(CompiledMinStateSchema, CompiledMaxStateSchema, v.MinState, v.MaxState, "state")
	if err != nil {
		return Manifest{}, err
	}

	var envelope signatureEnvelope
	if err := decodeStrict(signature, &envelope); err != nil {
		return Manifest{}, fmt.Errorf("decode release signature: %w", err)
	}
	if envelope.Algorithm != "ed25519" {
		return Manifest{}, fmt.Errorf("unsupported signature algorithm %q", envelope.Algorithm)
	}
	publicKey, ok := effectiveTrust[envelope.KeyID]
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
	digest := sha256.Sum256(data)
	manifestDigest := hex.EncodeToString(digest[:])
	if err := v.validate(manifest, effectiveTrust, provenance, manifestDigest); err != nil {
		return Manifest{}, fmt.Errorf("validate release manifest: %w", err)
	}
	manifest.verified = true
	manifest.trustBefore = trustDigest(effectiveTrust)
	manifest.trustAfter = cloneKeys(effectiveTrust)
	manifest.manifestDigest = manifestDigest
	for _, addition := range manifest.TrustedKeys {
		decoded, _ := base64.StdEncoding.Strict().DecodeString(addition.PublicKey)
		manifest.trustAfter[addition.ID] = ed25519.PublicKey(append([]byte(nil), decoded...))
	}
	return manifest, nil
}

func (v Verifier) TrustAfter(manifest Manifest) (map[string]ed25519.PublicKey, error) {
	if !manifest.verified || manifest.trustAfter == nil {
		return nil, errors.New("release manifest was not verified")
	}
	effectiveTrust, _, err := v.effectiveTrust()
	if err != nil {
		return nil, err
	}
	if trustDigest(effectiveTrust) != manifest.trustBefore {
		return nil, errors.New("release manifest was verified by a different trust set")
	}
	return cloneKeys(manifest.trustAfter), nil
}

func (m Manifest) Digest() (string, error) {
	if !m.verified || !digestPattern.MatchString(m.manifestDigest) {
		return "", errors.New("release manifest was not verified")
	}
	return m.manifestDigest, nil
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

func CompareStableVersions(left, right string) (int, error) {
	if !stableSemVerPattern.MatchString(left) || !stableSemVerPattern.MatchString(right) {
		return 0, errors.New("version is not stable SemVer")
	}
	leftParts := strings.Split(left, ".")
	rightParts := strings.Split(right, ".")
	for index := range leftParts {
		if len(leftParts[index]) != len(rightParts[index]) {
			if len(leftParts[index]) < len(rightParts[index]) {
				return -1, nil
			}
			return 1, nil
		}
		if leftParts[index] < rightParts[index] {
			return -1, nil
		}
		if leftParts[index] > rightParts[index] {
			return 1, nil
		}
	}
	return 0, nil
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

func (v Verifier) validate(manifest Manifest, effectiveTrust map[string]ed25519.PublicKey, provenance map[string]string, manifestDigest string) error {
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
	if err := validateTrustedKeyAdditions(manifest.TrustedKeys, effectiveTrust, v.CompiledKeys, provenance, manifestDigest); err != nil {
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

func validateTrustedKeyAdditions(keys []TrustedKey, effectiveTrust, compiled map[string]ed25519.PublicKey, provenance map[string]string, manifestDigest string) error {
	seen := make(map[string]struct{}, len(keys))
	materialOwners := make(map[string]string, len(effectiveTrust)+len(keys))
	for id, key := range effectiveTrust {
		materialOwners[string(key)] = id
	}
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
		if existingID, exists := materialOwners[string(decoded)]; exists && existingID != key.ID {
			return fmt.Errorf("trusted key %q duplicates key material from %q", key.ID, existingID)
		}
		materialOwners[string(decoded)] = key.ID
		if existing, exists := effectiveTrust[key.ID]; exists {
			if _, root := compiled[key.ID]; root || !bytes.Equal(existing, decoded) || provenance[key.ID] != manifestDigest {
				return fmt.Errorf("trusted key ID %q is already trusted by different provenance", key.ID)
			}
		}
	}
	return nil
}

func (v Verifier) effectiveTrust() (map[string]ed25519.PublicKey, map[string]string, error) {
	if len(v.CompiledKeys) == 0 {
		return nil, nil, errors.New("release verifier requires at least one compiled trust root")
	}
	trusted := make(map[string]ed25519.PublicKey, len(v.CompiledKeys))
	for id, key := range v.CompiledKeys {
		if err := validateVerifierKey(id, key); err != nil {
			return nil, nil, fmt.Errorf("compiled trusted key: %w", err)
		}
		trusted[id] = append(ed25519.PublicKey(nil), key...)
	}
	if len(v.PersistedProofs) == 0 {
		return trusted, map[string]string{}, nil
	}
	persisted, err := VerifyProofChain(v.CompiledKeys, v.PersistedProofs)
	if err != nil {
		return nil, nil, fmt.Errorf("persisted trust proof chain: %w", err)
	}
	for id, key := range persisted.Keys {
		trusted[id] = append(ed25519.PublicKey(nil), key...)
	}
	return trusted, persisted.Provenance, nil
}

func validateVerifierKey(id string, key ed25519.PublicKey) error {
	if !keyIDPattern.MatchString(id) {
		return fmt.Errorf("invalid key ID %q", id)
	}
	if len(key) != ed25519.PublicKeySize {
		return fmt.Errorf("key %q has length %d, want %d", id, len(key), ed25519.PublicKeySize)
	}
	return nil
}

func effectiveSchemaRange(compiledMin, compiledMax, requestedMin, requestedMax int, name string) (int, int, error) {
	if compiledMin <= 0 || compiledMax < compiledMin {
		return 0, 0, fmt.Errorf("invalid compiled %s schema range %d..%d", name, compiledMin, compiledMax)
	}
	if requestedMin < 0 || requestedMax < 0 || (requestedMin != 0 && requestedMax != 0 && requestedMax < requestedMin) {
		return 0, 0, fmt.Errorf("invalid requested %s schema range %d..%d", name, requestedMin, requestedMax)
	}
	minimum, maximum := compiledMin, compiledMax
	if requestedMin > minimum {
		minimum = requestedMin
	}
	if requestedMax != 0 && requestedMax < maximum {
		maximum = requestedMax
	}
	if maximum < minimum {
		return 0, 0, fmt.Errorf("requested %s schema range has no compiled-compatible versions", name)
	}
	return minimum, maximum, nil
}

func trustDigest(keys map[string]ed25519.PublicKey) [sha256.Size]byte {
	ids := make([]string, 0, len(keys))
	for id := range keys {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	hash := sha256.New()
	for _, id := range ids {
		hash.Write([]byte(id))
		hash.Write([]byte{0})
		hash.Write(keys[id])
	}
	var digest [sha256.Size]byte
	copy(digest[:], hash.Sum(nil))
	return digest
}

func cloneKeys(keys map[string]ed25519.PublicKey) map[string]ed25519.PublicKey {
	cloned := make(map[string]ed25519.PublicKey, len(keys))
	for id, key := range keys {
		cloned[id] = append(ed25519.PublicKey(nil), key...)
	}
	return cloned
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
