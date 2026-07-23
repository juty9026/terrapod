package catalog

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
	"os"
	pathpkg "path"
	"sort"
	"strings"

	"github.com/juty9026/terrapod/internal/legacydecl"
	"github.com/juty9026/terrapod/internal/model"
)

const maxInputSize = 4 * 1024 * 1024

const (
	enabledByConfigMetadataKey       = "enabledByConfig"
	enabledByAnyConfigMetadataPrefix = "enabledByAnyConfig."
)

type SignatureSet struct {
	PublicKeys map[string]ed25519.PublicKey
}

type Verified struct {
	Catalog model.Catalog
	Digest  string
	KeyID   string
}

type signatureEnvelope struct {
	KeyID     string `json:"keyId"`
	Algorithm string `json:"algorithm"`
	Signature string `json:"signature"`
}

var providersByType = map[model.ResourceType]map[string]struct{}{
	model.ResourcePackage: providerSet(
		"apt",
		"homebrew-cask",
		"homebrew-formula",
		"mise",
		"vendor-installer",
	),
	model.ResourceManagedFiles: providerSet("chezmoi"),
	model.ResourceGitCheckout:  providerSet("git"),
	model.ResourceArchive:      providerSet("jetendard"),
	model.ResourceIntegration:  providerSet("json-fields", "karabiner", "plist-fields"),
	model.ResourceManagementCore: providerSet(
		"terrapod",
	),
}

func LoadVerified(path string, signatures SignatureSet) (Verified, error) {
	catalogBytes, err := readBounded(path)
	if err != nil {
		return Verified{}, fmt.Errorf("read resource catalog: %w", err)
	}
	signatureBytes, err := readBounded(path + ".sig")
	if err != nil {
		return Verified{}, fmt.Errorf("read resource catalog signature: %w", err)
	}

	var envelope signatureEnvelope
	if err := decodeStrict(signatureBytes, &envelope); err != nil {
		return Verified{}, fmt.Errorf("decode signature envelope: %w", err)
	}
	if envelope.Algorithm != "ed25519" {
		return Verified{}, fmt.Errorf("unsupported signature algorithm %q", envelope.Algorithm)
	}
	publicKey, ok := signatures.PublicKeys[envelope.KeyID]
	if !ok {
		return Verified{}, fmt.Errorf("unknown signature key ID %q", envelope.KeyID)
	}
	if len(publicKey) != ed25519.PublicKeySize {
		return Verified{}, fmt.Errorf("signature key %q has length %d, want %d", envelope.KeyID, len(publicKey), ed25519.PublicKeySize)
	}
	signature, err := base64.StdEncoding.Strict().DecodeString(envelope.Signature)
	if err != nil {
		return Verified{}, fmt.Errorf("decode signature: %w", err)
	}
	if base64.StdEncoding.EncodeToString(signature) != envelope.Signature {
		return Verified{}, errors.New("non-canonical signature encoding")
	}
	if len(signature) != ed25519.SignatureSize {
		return Verified{}, fmt.Errorf("signature length is %d, want %d", len(signature), ed25519.SignatureSize)
	}
	if !ed25519.Verify(publicKey, catalogBytes, signature) {
		return Verified{}, errors.New("resource catalog signature verification failed")
	}

	var parsed model.Catalog
	if err := decodeStrict(catalogBytes, &parsed); err != nil {
		return Verified{}, fmt.Errorf("decode resource catalog: %w", err)
	}
	if err := validate(parsed); err != nil {
		return Verified{}, fmt.Errorf("validate resource catalog: %w", err)
	}

	digest := sha256.Sum256(catalogBytes)
	return Verified{
		Catalog: parsed,
		Digest:  hex.EncodeToString(digest[:]),
		KeyID:   envelope.KeyID,
	}, nil
}

// LoadReleaseBound loads a catalog whose exact SHA-256 is already bound by a
// verified signed release manifest.
func LoadReleaseBound(path, expectedDigest string) (Verified, error) {
	contents, err := readBounded(path)
	if err != nil {
		return Verified{}, fmt.Errorf("read release-bound catalog: %w", err)
	}
	digest := sha256.Sum256(contents)
	actual := hex.EncodeToString(digest[:])
	if actual != expectedDigest {
		return Verified{}, errors.New("release-bound catalog digest mismatch")
	}
	var parsed model.Catalog
	if err := decodeStrict(contents, &parsed); err != nil {
		return Verified{}, fmt.Errorf("decode release-bound catalog: %w", err)
	}
	if err := validate(parsed); err != nil {
		return Verified{}, fmt.Errorf("validate release-bound catalog: %w", err)
	}
	return Verified{Catalog: parsed, Digest: actual, KeyID: "signed-release"}, nil
}

func readBounded(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	contents, err := io.ReadAll(io.LimitReader(file, maxInputSize+1))
	if err != nil {
		return nil, err
	}
	if len(contents) > maxInputSize {
		return nil, errors.New("input exceeds 4 MiB")
	}
	return contents, nil
}

func decodeStrict(contents []byte, target any) error {
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

func validate(catalog model.Catalog) error {
	if catalog.Version != 1 {
		return fmt.Errorf("unsupported catalog version %d", catalog.Version)
	}
	if catalog.Release == "" {
		return errors.New("release is empty")
	}
	if catalog.Config.Version != 1 {
		return fmt.Errorf("unsupported config schema version %d", catalog.Config.Version)
	}
	configKinds := make(map[string]string, len(catalog.Config.Fields))
	for _, field := range catalog.Config.Fields {
		configKinds[field.ID] = field.Kind
	}

	resources := append([]model.Resource(nil), catalog.Resources...)
	sort.SliceStable(resources, func(i, j int) bool { return resources[i].ID < resources[j].ID })
	byID := make(map[model.ResourceID]model.Resource, len(resources))
	for _, resource := range resources {
		if _, exists := byID[resource.ID]; exists {
			return fmt.Errorf("duplicate resource ID %q", resource.ID)
		}
		if err := resource.Validate(); err != nil {
			return err
		}
		allowedProviders, ok := providersByType[resource.Type]
		if !ok {
			return fmt.Errorf("resource %q has unsupported type %q", resource.ID, resource.Type)
		}
		if _, ok := allowedProviders[resource.Provider]; !ok {
			return fmt.Errorf("resource %q has unsupported provider %q for type %q", resource.ID, resource.Provider, resource.Type)
		}
		if !safePackageIdentifier(resource.Package) {
			return fmt.Errorf("resource %q has unsafe package identifier %q", resource.ID, resource.Package)
		}
		if err := validateConfigGateMetadata(resource, configKinds); err != nil {
			return err
		}
		if err := validateLegacyMetadata(resource); err != nil {
			return err
		}
		if err := validateIntegrationMetadata(resource); err != nil {
			return err
		}

		profiles := append([]model.Profile(nil), resource.Profiles...)
		sort.Slice(profiles, func(i, j int) bool { return profiles[i] < profiles[j] })
		for _, profile := range profiles {
			if !profile.Supported() {
				return fmt.Errorf("resource %q has unsupported profile %q", resource.ID, profile)
			}
		}

		commands := append([]string(nil), resource.Commands...)
		sort.Strings(commands)
		for i := 1; i < len(commands); i++ {
			if commands[i] == commands[i-1] {
				return fmt.Errorf("resource %q has duplicate command %q", resource.ID, commands[i])
			}
		}
		byID[resource.ID] = resource
	}

	for _, resource := range resources {
		dependencies := append([]model.ResourceID(nil), resource.DependsOn...)
		sort.Slice(dependencies, func(i, j int) bool { return dependencies[i] < dependencies[j] })
		for _, dependency := range dependencies {
			if _, ok := byID[dependency]; !ok {
				return fmt.Errorf("resource %q has unknown dependency %q", resource.ID, dependency)
			}
		}
	}
	if err := validateManagedFileScopes(resources); err != nil {
		return err
	}
	if cycle := findCycle(resources, byID); len(cycle) != 0 {
		parts := make([]string, len(cycle))
		for i := range cycle {
			parts[i] = string(cycle[i])
		}
		return fmt.Errorf("dependency cycle: %s", strings.Join(parts, " -> "))
	}
	return nil
}

func validateIntegrationMetadata(resource model.Resource) error {
	if resource.Type != model.ResourceIntegration {
		return nil
	}
	handler := resource.Metadata["integration.handler"]
	allowed := false
	switch resource.Provider {
	case "json-fields":
		allowed = handler == "fields" || handler == "jetendard-zed" || handler == "jetendard-orca"
	case "plist-fields":
		allowed = handler == "fields"
	case "karabiner":
		allowed = handler == "karabiner-opener"
	}
	if !allowed {
		return fmt.Errorf("resource %q has unsupported compiled integration handler %q", resource.ID, handler)
	}
	allowedKeys := map[string]struct{}{
		"integration.handler": {}, "integration.path": {}, "integration.pathGlob": {},
		"integration.format": {}, "integration.fields": {}, "enabledByConfig": {},
	}
	for key := range resource.Metadata {
		if _, ok := allowedKeys[key]; !ok {
			return fmt.Errorf("resource %q has unsupported integration metadata %q", resource.ID, key)
		}
	}
	if resource.Provider == "karabiner" {
		for _, key := range []string{"integration.path", "integration.pathGlob", "integration.format", "integration.fields"} {
			if resource.Metadata[key] != "" {
				return fmt.Errorf("resource %q Karabiner action cannot own fields", resource.ID)
			}
		}
		return nil
	}
	path, pathGlob := resource.Metadata["integration.path"], resource.Metadata["integration.pathGlob"]
	if (path == "") == (pathGlob == "") {
		return fmt.Errorf("resource %q must declare exactly one integration path or pathGlob", resource.ID)
	}
	for _, value := range []string{path, pathGlob} {
		if value == "" {
			continue
		}
		clean := pathpkg.Clean(value)
		if clean != value || strings.HasPrefix(value, "/") || value == ".." || strings.HasPrefix(value, "../") || strings.Contains(value, "\\") {
			return fmt.Errorf("resource %q has unsafe integration path %q", resource.ID, value)
		}
	}
	format := resource.Metadata["integration.format"]
	if resource.Provider == "json-fields" && format != "json" && format != "jsonc" {
		return fmt.Errorf("resource %q has invalid JSON integration format %q", resource.ID, format)
	}
	if resource.Provider == "plist-fields" && format != "plist" {
		return fmt.Errorf("resource %q has invalid plist integration format %q", resource.ID, format)
	}
	var fields map[string]json.RawMessage
	if err := decodeStrict([]byte(resource.Metadata["integration.fields"]), &fields); err != nil || len(fields) == 0 {
		return fmt.Errorf("resource %q has malformed integration fields", resource.ID)
	}
	for pointer := range fields {
		if !validJSONPointer(pointer) {
			return fmt.Errorf("resource %q has invalid integration field path %q", resource.ID, pointer)
		}
	}
	return nil
}

func validJSONPointer(pointer string) bool {
	if !strings.HasPrefix(pointer, "/") || pointer == "/" || strings.Contains(pointer, "//") {
		return false
	}
	for i := 0; i < len(pointer); i++ {
		if pointer[i] == '~' {
			if i+1 >= len(pointer) || (pointer[i+1] != '0' && pointer[i+1] != '1') {
				return false
			}
			i++
		}
	}
	return true
}

func validateManagedFileScopes(resources []model.Resource) error {
	type scoped struct {
		id    model.ResourceID
		scope string
	}
	var managed []scoped
	for _, resource := range resources {
		if resource.Type != model.ResourceManagedFiles {
			continue
		}
		scope, ok := resource.Metadata[model.ManagedFilesScopeMetadataKey]
		if !ok || scope == "" {
			return fmt.Errorf("resource %q requires %s metadata", resource.ID, model.ManagedFilesScopeMetadataKey)
		}
		if scope != pathpkg.Clean(scope) || strings.HasPrefix(scope, "/") || scope == ".." || strings.HasPrefix(scope, "../") || strings.Contains(scope, "\\") || strings.IndexByte(scope, 0) >= 0 {
			return fmt.Errorf("resource %q has unsafe managed-file scope %q", resource.ID, scope)
		}
		managed = append(managed, scoped{resource.ID, scope})
	}
	for i := 0; i < len(managed); i++ {
		for j := i + 1; j < len(managed); j++ {
			if scopesOverlap(managed[i].scope, managed[j].scope) {
				return fmt.Errorf("resources %q and %q have overlapping managed-file scopes", managed[i].id, managed[j].id)
			}
		}
	}
	return nil
}

func scopesOverlap(left, right string) bool {
	return left == "." || right == "." || left == right || strings.HasPrefix(left, right+"/") || strings.HasPrefix(right, left+"/")
}

func validateLegacyMetadata(resource model.Resource) error {
	_, err := legacydecl.Parse(resource)
	return err
}

func validateConfigGateMetadata(resource model.Resource, configKinds map[string]string) error {
	singleField, hasSingle := resource.Metadata[enabledByConfigMetadataKey]
	hasAny := false
	anyKeys := make([]string, 0)
	for key := range resource.Metadata {
		if strings.HasPrefix(key, enabledByAnyConfigMetadataPrefix) {
			hasAny = true
			anyKeys = append(anyKeys, key)
		}
	}
	if hasSingle && hasAny {
		return fmt.Errorf("resource %q mixes %q and %q metadata", resource.ID, enabledByConfigMetadataKey, "enabledByAnyConfig.*")
	}
	if hasSingle {
		if err := validateBoolConfigReference(resource.ID, singleField, configKinds); err != nil {
			return err
		}
	}
	sort.Strings(anyKeys)
	for _, key := range anyKeys {
		if resource.Metadata[key] != "true" {
			return fmt.Errorf("resource %q metadata %q must have value %q", resource.ID, key, "true")
		}
		field := strings.TrimPrefix(key, enabledByAnyConfigMetadataPrefix)
		if err := validateBoolConfigReference(resource.ID, field, configKinds); err != nil {
			return err
		}
	}
	return nil
}

func validateBoolConfigReference(resourceID model.ResourceID, field string, configKinds map[string]string) error {
	kind, ok := configKinds[field]
	if !ok {
		return fmt.Errorf("resource %q references unknown config field %q", resourceID, field)
	}
	if kind != "bool" {
		return fmt.Errorf("resource %q references non-bool config field %q", resourceID, field)
	}
	return nil
}

func findCycle(resources []model.Resource, byID map[model.ResourceID]model.Resource) []model.ResourceID {
	const (
		unvisited = iota
		visiting
		visited
	)
	state := make(map[model.ResourceID]int, len(resources))
	stack := make([]model.ResourceID, 0, len(resources))
	stackIndex := make(map[model.ResourceID]int, len(resources))

	var visit func(model.ResourceID) []model.ResourceID
	visit = func(id model.ResourceID) []model.ResourceID {
		state[id] = visiting
		stackIndex[id] = len(stack)
		stack = append(stack, id)
		dependencies := append([]model.ResourceID(nil), byID[id].DependsOn...)
		sort.Slice(dependencies, func(i, j int) bool { return dependencies[i] < dependencies[j] })
		for _, dependency := range dependencies {
			switch state[dependency] {
			case unvisited:
				if cycle := visit(dependency); len(cycle) != 0 {
					return cycle
				}
			case visiting:
				start := stackIndex[dependency]
				cycle := append([]model.ResourceID(nil), stack[start:]...)
				return append(cycle, dependency)
			}
		}
		stack = stack[:len(stack)-1]
		delete(stackIndex, id)
		state[id] = visited
		return nil
	}

	for _, resource := range resources {
		if state[resource.ID] == unvisited {
			if cycle := visit(resource.ID); len(cycle) != 0 {
				return cycle
			}
		}
	}
	return nil
}

func safePackageIdentifier(identifier string) bool {
	if identifier == "" || strings.HasPrefix(identifier, "-") || strings.HasPrefix(identifier, "/") {
		return false
	}
	for _, r := range []byte(identifier) {
		if !asciiLetterOrDigit(r) && !strings.ContainsRune("._+@/-", rune(r)) {
			return false
		}
	}
	for _, segment := range strings.Split(identifier, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func asciiLetterOrDigit(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9'
}

func providerSet(providers ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(providers))
	for _, provider := range providers {
		result[provider] = struct{}{}
	}
	return result
}
