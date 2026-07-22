package catalog

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/juty9026/terrapod/internal/model"
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

func TestSeedCatalogHasCurrentConfigSchemaAndHomebrewResources(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join("..", "..", "catalog", "v1", "resources.json"))
	if err != nil {
		t.Fatal(err)
	}
	path, signatures := writeSignedCatalog(t, contents, testKeyID)
	got, err := LoadVerified(path, signatures)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Catalog.Resources) != 63 {
		t.Fatalf("Resources count = %d, want 63", len(got.Catalog.Resources))
	}
	wantFields := []model.ConfigField{
		{ID: "profile", Kind: "string", Required: true},
		{ID: "enableEditorStack", Kind: "bool", Default: false},
		{ID: "enableAiCliTools", Kind: "bool", Default: false},
		{ID: "enableDevelopmentWorkspace", Kind: "bool", Default: false},
		{ID: "enableMacosAppGroupTerminalApps", Kind: "bool", Default: false},
		{ID: "enableMacosAppGroupAutomation", Kind: "bool", Default: false},
		{ID: "enableMacosAppGroupLauncher", Kind: "bool", Default: false},
		{ID: "enableMacosAppGroupMonitoring", Kind: "bool", Default: false},
		{ID: "enableMacosAppGroupDevelopmentApps", Kind: "bool", Default: false},
	}
	if !reflect.DeepEqual(got.Catalog.Config.Fields, wantFields) {
		t.Fatalf("Config.Fields = %#v, want %#v", got.Catalog.Config.Fields, wantFields)
	}
}

func TestIntegrationCatalogRejectsScriptsAndUnknownHandlers(t *testing.T) {
	base := model.Resource{ID: "integration.test", Type: model.ResourceIntegration, Profiles: []model.Profile{model.ProfileMacOSTerminal}, VersionPolicy: model.VersionTracked, Provider: "json-fields", Package: "settings", Metadata: map[string]string{"integration.handler": "fields", "integration.path": "settings.json", "integration.format": "json", "integration.fields": `{"/font":"Jetendard"}`}}
	for _, mutate := range []func(*model.Resource){
		func(item *model.Resource) { item.Metadata["integration.script"] = "curl example.test | sh" },
		func(item *model.Resource) { item.Metadata["integration.handler"] = "catalog-hook" },
		func(item *model.Resource) { item.Metadata["integration.fields"] = "{" },
	} {
		item := base
		item.Metadata = maps.Clone(base.Metadata)
		mutate(&item)
		catalog := model.Catalog{Version: 1, Release: "test", Config: model.ConfigSchema{Version: 1}, Resources: []model.Resource{item}}
		if err := validate(catalog); err == nil {
			t.Fatalf("accepted integration metadata %#v", item.Metadata)
		}
	}
}

func TestSeedCatalogDeclaresOnlyKnownLegacyPackageSources(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join("..", "..", "catalog", "v1", "resources.json"))
	if err != nil {
		t.Fatal(err)
	}
	var seed model.Catalog
	if err := json.Unmarshal(contents, &seed); err != nil {
		t.Fatal(err)
	}
	wantMise := map[model.ResourceID]string{
		"core.bat": "aqua:sharkdp/bat", "core.btop": "aqua:aristocratos/btop",
		"core.dust": "aqua:bootandy/dust", "core.duf": "aqua:muesli/duf",
		"core.fastfetch": "aqua:fastfetch-cli/fastfetch", "core.fd": "aqua:sharkdp/fd",
		"core.fzf": "aqua:junegunn/fzf", "core.gh": "aqua:cli/cli",
		"core.git-delta": "aqua:dandavison/delta", "core.lazygit": "aqua:jesseduffield/lazygit",
		"core.lsd": "aqua:lsd-rs/lsd", "core.neovim": "aqua:neovim/neovim",
		"core.ripgrep": "aqua:BurntSushi/ripgrep", "core.starship": "aqua:starship/starship",
		"core.zellij": "aqua:zellij-org/zellij", "core.zoxide": "aqua:ajeetdsouza/zoxide",
	}
	wantVendor := map[model.ResourceID]string{
		"optional-ai.antigravity-cli": "antigravity-native",
		"optional-ai.claude-code":     "claude-native",
		"optional-ai.codex":           "codex-standalone",
	}
	wantAPT := map[model.ResourceID]string{"core.gum": "gum", "core.mise": "mise"}
	seenMise := 0
	seenVendor := 0
	seenAPT := 0
	for _, resource := range seed.Resources {
		if resource.Provider != "homebrew-formula" && resource.Provider != "homebrew-cask" {
			continue
		}
		if got := resource.Metadata["legacy.homebrew.package"]; got != resource.Package {
			t.Fatalf("resource %q legacy Homebrew package = %q, want %q", resource.ID, got, resource.Package)
		}
		if want, ok := wantMise[resource.ID]; ok {
			seenMise++
			if got := resource.Metadata["legacy.mise.package"]; got != want {
				t.Fatalf("resource %q legacy mise package = %q, want %q", resource.ID, got, want)
			}
		}
		if resource.ID == "core.btop" && resource.Metadata["legacy.mise.profile"] != string(model.ProfileVPSShell) {
			t.Fatalf("core.btop legacy mise scope = %q", resource.Metadata["legacy.mise.profile"])
		}
		if want, ok := wantAPT[resource.ID]; ok {
			seenAPT++
			if got := resource.Metadata["legacy.apt.package"]; got != want {
				t.Fatalf("resource %q legacy APT package = %q, want %q", resource.ID, got, want)
			}
			if resource.Metadata["legacy.apt.profile"] != string(model.ProfileVPSShell) {
				t.Fatalf("resource %q legacy APT scope = %q", resource.ID, resource.Metadata["legacy.apt.profile"])
			}
		}
		if want, ok := wantVendor[resource.ID]; ok {
			seenVendor++
			if resource.Metadata["legacy.vendor.receipt"] != want || resource.Metadata["legacy.vendor.uninstall"] != want {
				t.Fatalf("resource %q vendor metadata = %#v, want kind %q", resource.ID, resource.Metadata, want)
			}
		}
	}
	if seenMise != len(wantMise) || seenVendor != len(wantVendor) || seenAPT != len(wantAPT) {
		t.Fatalf("legacy declarations seen mise=%d vendor=%d apt=%d", seenMise, seenVendor, seenAPT)
	}
}

func TestSeedCatalogMatchesBootstrapAPTAndMiseDeclarations(t *testing.T) {
	catalogContents, err := os.ReadFile(filepath.Join("..", "..", "catalog", "v1", "resources.json"))
	if err != nil {
		t.Fatal(err)
	}
	var seed model.Catalog
	if err := json.Unmarshal(catalogContents, &seed); err != nil {
		t.Fatal(err)
	}

	aptContents, err := os.ReadFile(filepath.Join("..", "..", ".chezmoiscripts", "run_onchange_before_00-bootstrap-ubuntu.sh.tmpl"))
	if err != nil {
		t.Fatal(err)
	}
	miseContents, err := os.ReadFile(filepath.Join("..", "..", "dot_config", "mise", "config.toml.tmpl"))
	if err != nil {
		t.Fatal(err)
	}

	wantAPT := extractAPTInstallList(t, string(aptContents))
	wantMise := extractMiseTools(t, string(miseContents))
	gotAPT := make(map[string]model.Resource)
	gotMise := make(map[string]model.Resource)
	seenIDs := make(map[model.ResourceID]struct{})
	for _, resource := range seed.Resources {
		if _, duplicate := seenIDs[resource.ID]; duplicate {
			t.Fatalf("duplicate catalog resource ID %q", resource.ID)
		}
		seenIDs[resource.ID] = struct{}{}
		switch resource.Provider {
		case "apt":
			if _, duplicate := gotAPT[resource.Package]; duplicate {
				t.Fatalf("duplicate APT package %q", resource.Package)
			}
			gotAPT[resource.Package] = resource
		case "mise":
			if _, duplicate := gotMise[resource.Package]; duplicate {
				t.Fatalf("duplicate mise package %q", resource.Package)
			}
			gotMise[resource.Package] = resource
		}
	}
	if len(gotAPT) != 20 || len(wantAPT) != 20 || len(gotAPT) != len(wantAPT) {
		t.Fatalf("APT catalog packages = %v, template packages = %v", sortedResourceKeys(gotAPT), wantAPT)
	}
	for _, pkg := range wantAPT {
		resource, ok := gotAPT[pkg]
		if !ok {
			t.Fatalf("missing APT resource for %q", pkg)
		}
		want := model.Resource{ID: model.ResourceID("bootstrap-apt." + pkg), Type: model.ResourcePackage, Profiles: []model.Profile{model.ProfileVPSShell}, DependsOn: []model.ResourceID{}, VersionPolicy: model.VersionTracked, Provider: "apt", Package: pkg, Commands: []string{}, Metadata: map[string]string{"bootstrapOnly": "true"}}
		if !reflect.DeepEqual(resource, want) {
			t.Fatalf("APT resource %q = %#v, want %#v", pkg, resource, want)
		}
	}
	if len(gotMise) != 4 || len(wantMise) != 4 || len(gotMise) != len(wantMise) {
		t.Fatalf("mise catalog tools = %v, template tools = %v", sortedResourceKeys(gotMise), sortedStringKeys(wantMise))
	}
	wantCommands := map[string][]string{"bun": {"bun"}, "node": {"node"}, "python": {"python"}, "uv": {"uv", "uvx"}}
	for tool, version := range wantMise {
		resource, ok := gotMise[tool]
		if !ok {
			t.Fatalf("missing mise resource for %q", tool)
		}
		want := model.Resource{ID: model.ResourceID("runtime." + tool), Type: model.ResourcePackage, Profiles: []model.Profile{model.ProfileMacOSTerminal, model.ProfileVPSShell}, DependsOn: []model.ResourceID{"core.mise"}, VersionPolicy: model.VersionPinned, Provider: "mise", Package: tool, Commands: wantCommands[tool], Metadata: map[string]string{"version": version}}
		if !reflect.DeepEqual(resource, want) {
			t.Fatalf("mise resource %q = %#v, want %#v", tool, resource, want)
		}
	}
}

func extractAPTInstallList(t *testing.T, script string) []string {
	t.Helper()
	packages, err := parseAPTInstallList(script)
	if err != nil {
		t.Fatal(err)
	}
	return packages
}

func parseAPTInstallList(script string) ([]string, error) {
	start := strings.Index(script, "apt-get install -y \\\n")
	if start < 0 {
		return nil, fmt.Errorf("APT install declaration not found")
	}
	block := script[start+len("apt-get install -y \\\n"):]
	end := strings.Index(block, "; then")
	if end < 0 {
		return nil, fmt.Errorf("APT install declaration terminator not found")
	}
	lines := strings.Split(block[:end], "\n")
	packages := make([]string, 0, len(lines))
	seen := make(map[string]int, len(lines))
	for index, line := range lines {
		pkg := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line), "\\"))
		if pkg == "" || strings.ContainsAny(pkg, " \t\"'$") {
			return nil, fmt.Errorf("invalid APT package declaration at line %d: %q", index+1, line)
		}
		if firstLine, duplicate := seen[pkg]; duplicate {
			return nil, fmt.Errorf("duplicate APT package %q at line %d (first declared at line %d)", pkg, index+1, firstLine)
		}
		seen[pkg] = index + 1
		packages = append(packages, pkg)
	}
	return packages, nil
}

func TestParseAPTInstallListRejectsDuplicatePackageDeclaration(t *testing.T) {
	script := "apt-get install -y \\\n  curl \\\n  curl; then\n"
	_, err := parseAPTInstallList(script)
	if err == nil || !strings.Contains(err.Error(), "duplicate") || !strings.Contains(err.Error(), "curl") || !strings.Contains(err.Error(), "line") {
		t.Fatalf("error = %v", err)
	}
}

func extractMiseTools(t *testing.T, config string) map[string]string {
	t.Helper()
	tools, err := parseMiseTools(config)
	if err != nil {
		t.Fatal(err)
	}
	return tools
}

func parseMiseTools(config string) (map[string]string, error) {
	start := strings.Index(config, "[tools]\n")
	if start < 0 {
		return nil, fmt.Errorf("mise [tools] declaration not found")
	}
	block := config[start+len("[tools]\n"):]
	if end := strings.Index(block, "\n["); end >= 0 {
		block = block[:end]
	}
	tools := make(map[string]string)
	seen := make(map[string]int)
	for index, line := range strings.Split(strings.TrimSpace(block), "\n") {
		parts := strings.Split(line, " = ")
		if len(parts) != 2 || len(parts[1]) < 2 || parts[1][0] != '"' || parts[1][len(parts[1])-1] != '"' {
			return nil, fmt.Errorf("invalid mise tool declaration at line %d: %q", index+1, line)
		}
		if firstLine, duplicate := seen[parts[0]]; duplicate {
			return nil, fmt.Errorf("duplicate mise tool %q at line %d (first declared at line %d): %q", parts[0], index+1, firstLine, line)
		}
		seen[parts[0]] = index + 1
		tools[parts[0]] = strings.Trim(parts[1], "\"")
	}
	return tools, nil
}

func TestParseMiseToolsRejectsDuplicateToolDeclaration(t *testing.T) {
	config := "[tools]\nnode = \"24\"\nnode = \"25\"\n\n[settings]\n"
	_, err := parseMiseTools(config)
	if err == nil || !strings.Contains(err.Error(), "duplicate") || !strings.Contains(err.Error(), "node") || !strings.Contains(err.Error(), "line") {
		t.Fatalf("error = %v", err)
	}
}

func sortedResourceKeys(resources map[string]model.Resource) []string {
	keys := make([]string, 0, len(resources))
	for key := range resources {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedStringKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
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
			name: "embedded newline in base64",
			envelope: func(signature []byte) []byte {
				encoded := base64.StdEncoding.EncodeToString(signature)
				encoded = encoded[:20] + "\n" + encoded[20:]
				return envelopeJSON(t, testKeyID, "ed25519", encoded, "")
			},
			want: "non-canonical signature encoding",
		},
		{
			name: "noncanonical trailing bits",
			envelope: func(signature []byte) []byte {
				return envelopeJSON(t, testKeyID, "ed25519", noncanonicalBase64Alias(t, signature), "")
			},
			want: "decode signature",
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
			name: "unknown legacy metadata",
			edit: func(input map[string]any) {
				resourcesOf(input)[0]["metadata"] = map[string]any{"legacy.shell.command": "rm"}
			},
			want: `unknown legacy metadata "legacy.shell.command"`,
		},
		{
			name: "unpaired vendor receipt",
			edit: func(input map[string]any) {
				resourcesOf(input)[0]["metadata"] = map[string]any{"legacy.vendor.receipt": "claude-native"}
			},
			want: "must pair legacy vendor receipt and uninstall kinds",
		},
		{
			name: "unknown vendor kind",
			edit: func(input map[string]any) {
				resourcesOf(input)[0]["metadata"] = map[string]any{"legacy.vendor.receipt": "shell", "legacy.vendor.uninstall": "shell"}
			},
			want: `unsupported legacy vendor transition "shell"/"shell"`,
		},
		{
			name: "unsafe APT transition",
			edit: func(input map[string]any) {
				resource := resourcesOf(input)[0]
				resource["id"] = "core.gum"
				resource["package"] = "gum"
				resource["metadata"] = map[string]any{"legacy.apt.package": "gum;rm"}
			},
			want: `unsupported legacy APT transition "gum;rm"`,
		},
		{
			name: "unscoped APT transition",
			edit: func(input map[string]any) {
				resource := resourcesOf(input)[0]
				resource["id"] = "core.gum"
				resource["package"] = "gum"
				resource["metadata"] = map[string]any{"legacy.apt.package": "gum"}
			},
			want: `requires legacy APT profile "vps-shell"`,
		},
		{
			name: "mismatched mise declaration",
			edit: func(input map[string]any) {
				resourcesOf(input)[0]["metadata"] = map[string]any{"legacy.mise.package": "aqua:sharkdp/fd"}
			},
			want: `unsupported legacy mise transition "aqua:sharkdp/fd"`,
		},
		{
			name: "mismatched Homebrew token",
			edit: func(input map[string]any) {
				resourcesOf(input)[0]["metadata"] = map[string]any{"legacy.homebrew.package": "fd"}
			},
			want: `unsupported legacy Homebrew transition "fd"`,
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

func TestLoadVerifiedValidatesConfigGateMetadata(t *testing.T) {
	tests := []struct {
		name string
		edit func(map[string]any)
		want string
	}{
		{
			name: "single gate unknown field",
			edit: func(input map[string]any) {
				resourcesOf(input)[0]["metadata"] = map[string]any{"enabledByConfig": "missing"}
			},
			want: `references unknown config field "missing"`,
		},
		{
			name: "single gate non-bool field",
			edit: func(input map[string]any) {
				resourcesOf(input)[0]["metadata"] = map[string]any{"enabledByConfig": "profile"}
			},
			want: `references non-bool config field "profile"`,
		},
		{
			name: "any gate unknown field",
			edit: func(input map[string]any) {
				resourcesOf(input)[0]["metadata"] = map[string]any{"enabledByAnyConfig.missing": "true"}
			},
			want: `references unknown config field "missing"`,
		},
		{
			name: "any gate non-bool field",
			edit: func(input map[string]any) {
				resourcesOf(input)[0]["metadata"] = map[string]any{"enabledByAnyConfig.profile": "true"}
			},
			want: `references non-bool config field "profile"`,
		},
		{
			name: "any gate non-true value",
			edit: func(input map[string]any) {
				resourcesOf(input)[0]["metadata"] = map[string]any{"enabledByAnyConfig.enableAiCliTools": "false"}
			},
			want: `metadata "enabledByAnyConfig.enableAiCliTools" must have value "true"`,
		},
		{
			name: "mixed gate kinds",
			edit: func(input map[string]any) {
				resourcesOf(input)[0]["metadata"] = map[string]any{
					"enabledByConfig":                      "enableAiCliTools",
					"enabledByAnyConfig.enableEditorStack": "true",
				}
			},
			want: `mixes "enabledByConfig" and "enabledByAnyConfig.*" metadata`,
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

func TestLoadVerifiedAllowsAnyConfigGateAndUnrelatedProviderMetadata(t *testing.T) {
	input := catalogObject(t)
	resourcesOf(input)[0]["metadata"] = map[string]any{
		"enabledByAnyConfig.enableAiCliTools":           "true",
		"enabledByAnyConfig.enableDevelopmentWorkspace": "true",
		"providerSpecific":                              "permitted",
	}
	contents, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	path, signatures := writeSignedCatalog(t, contents, testKeyID)
	if _, err := LoadVerified(path, signatures); err != nil {
		t.Fatal(err)
	}
}

func TestLoadVerifiedRequiresNonOverlappingManagedFileScopes(t *testing.T) {
	managed := func(id, scope string) map[string]any {
		return map[string]any{"id": id, "type": "managed-files", "profiles": []any{"macos-terminal"}, "dependsOn": []any{}, "versionPolicy": "tracked", "provider": "chezmoi", "package": id, "commands": []any{}, "metadata": map[string]any{model.ManagedFilesScopeMetadataKey: scope}}
	}
	t.Run("missing", func(t *testing.T) {
		input := catalogObject(t)
		item := managed("dotfiles.one", ".config/one")
		item["metadata"] = map[string]any{}
		input["resources"] = []any{item}
		assertCatalogError(t, input, "requires managedFiles.scope")
	})
	for name, scope := range map[string]string{"absolute": "/tmp", "escape": "../other", "unclean": "a/../b"} {
		t.Run(name, func(t *testing.T) {
			input := catalogObject(t)
			input["resources"] = []any{managed("dotfiles.one", scope)}
			assertCatalogError(t, input, "unsafe managed-file scope")
		})
	}
	t.Run("overlap", func(t *testing.T) {
		input := catalogObject(t)
		input["resources"] = []any{managed("dotfiles.one", ".config"), managed("dotfiles.two", ".config/two")}
		assertCatalogError(t, input, "overlapping managed-file scopes")
	})
}

func TestLoadVerifiedBoundsCatalogAndSignature(t *testing.T) {
	t.Run("exact catalog limit", func(t *testing.T) {
		contents := readFixture(t)
		fixtureSize := len(contents)
		contents = append(contents, make([]byte, 4*1024*1024-len(contents))...)
		for i := fixtureSize; i < len(contents); i++ {
			contents[i] = ' '
		}
		path, signatures := writeSignedCatalog(t, contents, testKeyID)
		if _, err := LoadVerified(path, signatures); err != nil {
			t.Fatalf("LoadVerified() error = %v at exact 4 MiB", err)
		}
	})

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

func noncanonicalBase64Alias(t *testing.T, contents []byte) string {
	t.Helper()
	encoded := base64.StdEncoding.EncodeToString(contents)
	if len(contents)%3 != 1 || !strings.HasSuffix(encoded, "==") {
		t.Fatalf("fixture length = %d, want base64 with two padding bytes", len(contents))
	}
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	position := len(encoded) - 3
	value := strings.IndexByte(alphabet, encoded[position])
	if value < 0 || value&0x0f != 0 {
		t.Fatalf("canonical final sextet = %q", encoded[position])
	}
	alias := []byte(encoded)
	alias[position] = alphabet[value|1]
	decoded, err := base64.StdEncoding.DecodeString(string(alias))
	if err != nil || !reflect.DeepEqual(decoded, contents) {
		t.Fatalf("constructed alias does not decode to original bytes: %v", err)
	}
	return string(alias)
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
