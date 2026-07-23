package release

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
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
)

type Asset struct {
	Kind   string `json:"kind"`
	OS     string `json:"os,omitempty"`
	Arch   string `json:"arch,omitempty"`
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type Manifest struct {
	Version       string  `json:"version"`
	CatalogSchema int     `json:"catalogSchema"`
	StateSchema   int     `json:"stateSchema"`
	Assets        []Asset `json:"assets"`

	verified       bool
	manifestDigest string
}

func ParseManifest(data []byte) (Manifest, error) {
	if len(data) == 0 || len(data) > MaxManifestSize {
		return Manifest{}, fmt.Errorf("release manifest size is outside 1..%d bytes", MaxManifestSize)
	}

	var manifest Manifest
	if err := decodeStrict(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode release manifest: %w", err)
	}
	if err := validateManifest(manifest); err != nil {
		return Manifest{}, fmt.Errorf("validate release manifest: %w", err)
	}
	digest := sha256.Sum256(data)
	manifest.verified = true
	manifest.manifestDigest = hex.EncodeToString(digest[:])
	return manifest, nil
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

func validateManifest(manifest Manifest) error {
	if !stableSemVerPattern.MatchString(manifest.Version) {
		return fmt.Errorf("version %q is not stable SemVer", manifest.Version)
	}
	if manifest.CatalogSchema < CompiledMinCatalogSchema || manifest.CatalogSchema > CompiledMaxCatalogSchema {
		return fmt.Errorf("catalog schema %d is outside supported range %d..%d", manifest.CatalogSchema, CompiledMinCatalogSchema, CompiledMaxCatalogSchema)
	}
	if manifest.StateSchema < CompiledMinStateSchema || manifest.StateSchema > CompiledMaxStateSchema {
		return fmt.Errorf("state schema %d is outside supported range %d..%d", manifest.StateSchema, CompiledMinStateSchema, CompiledMaxStateSchema)
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
			if want := "tpod-" + asset.OS + "-" + asset.Arch; asset.Name != want {
				return fmt.Errorf("binary platform %s must use canonical asset name %q", platform, want)
			}
			expectedPlatforms[platform] = true
		case "source":
			if asset.OS != "" || asset.Arch != "" {
				return fmt.Errorf("source asset %q must not declare a platform", asset.Name)
			}
			if asset.Name != "terrapod-source.tar.gz" {
				return errors.New(`source asset must use canonical asset name "terrapod-source.tar.gz"`)
			}
			sourceCount++
		case "catalog":
			if asset.OS != "" || asset.Arch != "" {
				return fmt.Errorf("catalog asset %q must not declare a platform", asset.Name)
			}
			if asset.Name != "resources.json" {
				return errors.New(`catalog asset must use canonical asset name "resources.json"`)
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
