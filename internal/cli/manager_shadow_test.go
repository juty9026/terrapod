package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/juty9026/terrapod/internal/model"
)

type shadowManifest struct {
	Version      int                   `json:"version"`
	MutationSets map[string][]mutation `json:"mutationSets"`
	Scripts      []shadowScript        `json:"scripts"`
}
type mutation struct {
	ResourceID, Kind, Provider, Package, Destination string
	Evidence                                         string
}
type shadowScript struct {
	Path      string
	Condition string         `json:"condition"`
	Renders   []shadowRender `json:"renders"`
}
type shadowRender struct {
	Key      string
	SHA256   string   `json:"sha256"`
	Rendered bool     `json:"rendered"`
	Sets     []string `json:"mutationSets"`
}

var brewLine = regexp.MustCompile(`(?m)^brew "([^"]+)"`)
var caskLine = regexp.MustCompile(`(?m)^cask "([^"]+)"`)

func TestManagerShadowParityUsesRenderedMutationEvidence(t *testing.T) {
	repo := filepath.Clean(filepath.Join("..", ".."))
	manifest := loadShadowManifest(t, filepath.Join("testdata", "legacy_mutations.json"))
	catalog := loadShadowCatalog(t, filepath.Join(repo, "catalog", "v1", "resources.json"))
	byID, byProviderPackage := indexShadowCatalog(t, catalog)
	scripts, err := filepath.Glob(filepath.Join(repo, ".chezmoiscripts", "*.tmpl"))
	if err != nil {
		t.Fatal(err)
	}
	wantPaths := make([]string, len(manifest.Scripts))
	for i, script := range manifest.Scripts {
		wantPaths[i] = script.Path
		if script.Condition == "" || len(script.Renders) != 6 {
			t.Fatalf("%s reviewed condition/renders are incomplete", script.Path)
		}
	}
	sort.Strings(wantPaths)
	gotPaths := make([]string, len(scripts))
	for i, path := range scripts {
		gotPaths[i] = filepath.Base(path)
	}
	sort.Strings(gotPaths)
	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatalf("script inventory changed: got=%v want=%v", gotPaths, wantPaths)
	}

	renderCount := 0
	for _, profile := range []model.Profile{model.ProfileMacOSTerminal, model.ProfileVPSShell} {
		for _, preset := range []string{"minimal", "development", "workstation"} {
			cfg, flat := shadowConfig(profile, preset)
			enabled := make(map[string]struct{})
			for _, item := range enabledResources(catalog, cfg) {
				enabled[string(item.ID)] = struct{}{}
			}
			for _, script := range manifest.Scripts {
				output := renderShadowScript(t, repo, script.Path, flat)
				render := exactRender(t, script, string(profile), preset)
				renderCount++
				digest := fmt.Sprintf("%x", sha256.Sum256(output))
				if digest != render.SHA256 {
					t.Fatalf("%s %s/%s exact rendered bytes changed: got sha256=%s want=%s", script.Path, profile, preset, digest, render.SHA256)
				}
				if (len(bytes.TrimSpace(output)) != 0) != render.Rendered {
					t.Fatalf("%s %s/%s render condition changed", script.Path, profile, preset)
				}
				want := renderMutations(t, manifest, render)
				got := extractMutations(t, string(output), cfg, byProviderPackage)
				literalWant := literalMutations(want)
				sortMutations(literalWant)
				sortMutations(got)
				if !reflect.DeepEqual(got, literalWant) {
					t.Fatalf("%s %s/%s mutations changed:\ngot  %#v\nwant %#v", script.Path, profile, preset, got, want)
				}
				for _, change := range want {
					owner, ok := byID[change.ResourceID]
					if !ok || owner.Provider != change.Provider || owner.Package != change.Package {
						t.Fatalf("mutation has no exact catalog owner: %#v", change)
					}
					if _, ok := enabled[change.ResourceID]; !ok {
						t.Fatalf("mutation owner %s is disabled for %s/%s", change.ResourceID, profile, preset)
					}
				}
			}
		}
	}
	if renderCount != 78 {
		t.Fatalf("reviewed render count = %d, want 78", renderCount)
	}
}

func loadShadowManifest(t *testing.T, path string) shadowManifest {
	t.Helper()
	var value shadowManifest
	data, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(data, &value) != nil || value.Version != 2 {
		t.Fatalf("load manifest: %v", err)
	}
	return value
}
func loadShadowCatalog(t *testing.T, path string) model.Catalog {
	t.Helper()
	var value model.Catalog
	data, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(data, &value) != nil {
		t.Fatalf("load catalog: %v", err)
	}
	return value
}
func indexShadowCatalog(t *testing.T, catalog model.Catalog) (map[string]model.Resource, map[string]model.Resource) {
	t.Helper()
	byID, byPair := map[string]model.Resource{}, map[string]model.Resource{}
	for _, item := range catalog.Resources {
		if _, exists := byID[string(item.ID)]; exists {
			t.Fatalf("duplicate ID %s", item.ID)
		}
		byID[string(item.ID)] = item
		key := item.Provider + "\x00" + item.Package
		if _, exists := byPair[key]; exists {
			t.Fatalf("duplicate provider/package %s/%s", item.Provider, item.Package)
		}
		byPair[key] = item
	}
	return byID, byPair
}

func shadowConfig(profile model.Profile, preset string) (model.Config, map[string]any) {
	enabled := preset != "minimal"
	groups := preset == "workstation"
	values := map[string]any{
		"profile": string(profile), "enableEditorStack": enabled, "enableAiCliTools": enabled,
		"enableDevelopmentWorkspace": enabled, "enableMacosAppGroupTerminalApps": groups,
		"enableMacosAppGroupAutomation": groups, "enableMacosAppGroupLauncher": groups,
		"enableMacosAppGroupMonitoring": groups, "enableMacosAppGroupDevelopmentApps": groups,
	}
	flat := map[string]any{}
	for key, value := range values {
		flat[key] = value
	}
	osName := "linux"
	if profile == model.ProfileMacOSTerminal {
		osName = "darwin"
	}
	flat["chezmoi"] = map[string]any{"os": osName, "osRelease": map[string]string{"id": "ubuntu", "versionID": "24.04"}}
	return model.Config{Version: 1, Terrapod: values}, flat
}

func renderShadowScript(t *testing.T, repo, name string, data map[string]any) []byte {
	t.Helper()
	temp := t.TempDir()
	configPath, dataPath := filepath.Join(temp, "chezmoi.toml"), filepath.Join(temp, "data.json")
	encoded, _ := json.Marshal(data)
	if err := os.WriteFile(configPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dataPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("chezmoi", "--config", configPath, "--source", repo, "--override-data-file", dataPath, "execute-template", "--file", filepath.Join(repo, ".chezmoiscripts", name))
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("render %s: %v: %s", name, err, output)
	}
	return output
}

func exactRender(t *testing.T, script shadowScript, profile, preset string) shadowRender {
	t.Helper()
	var found *shadowRender
	for i := range script.Renders {
		candidate := &script.Renders[i]
		if candidate.Key == profile+"/"+preset {
			if found != nil {
				t.Fatalf("duplicate render %s/%s for %s", profile, preset, script.Path)
			}
			found = candidate
		}
	}
	if found == nil {
		t.Fatalf("missing render %s/%s for %s", profile, preset, script.Path)
	}
	return *found
}

func renderMutations(t *testing.T, manifest shadowManifest, render shadowRender) []mutation {
	t.Helper()
	var result []mutation
	for _, name := range render.Sets {
		set, ok := manifest.MutationSets[name]
		if !ok {
			t.Fatalf("unknown mutation set %q in render %q", name, render.Key)
		}
		result = append(result, set...)
	}
	return result
}

func extractMutations(t *testing.T, output string, cfg model.Config, byPair map[string]model.Resource) []mutation {
	t.Helper()
	var result []mutation
	add := func(provider, pkg, kind, destination string) {
		item, ok := byPair[provider+"\x00"+pkg]
		if !ok {
			t.Fatalf("rendered mutation has no catalog pair %s/%s", provider, pkg)
		}
		result = append(result, mutation{ResourceID: string(item.ID), Kind: kind, Provider: provider, Package: pkg, Destination: destination, Evidence: "literal"})
	}
	if strings.Contains(output, "raw.githubusercontent.com/Homebrew/install/HEAD/install.sh") {
		add("terrapod", "homebrew", "install", "brew")
	}
	for _, match := range caskLine.FindAllStringSubmatch(output, -1) {
		destination := caskDestination(match[1])
		add("homebrew-cask", match[1], "install", destination)
	}
	if marker := "apt-get install -y "; strings.Contains(output, marker) {
		block := strings.SplitN(strings.SplitN(output, marker, 2)[1], "; then", 2)[0]
		for _, field := range strings.Fields(strings.ReplaceAll(block, "\\", "")) {
			add("apt", field, "install", "dpkg")
		}
		if strings.Contains(output, "chsh -s \"$zsh_path\"") {
			add("apt", "zsh", "configure", "login-shell")
		}
	}
	if strings.Contains(output, "ohmyzsh/ohmyzsh/master/tools/install.sh") {
		add("git", "oh-my-zsh", "install", ".oh-my-zsh")
	}
	if strings.Contains(output, "github.com/zdharma-continuum/zinit") {
		add("git", "zinit", "install", ".local/share/zinit/zinit.git")
	}
	if strings.Contains(output, "github.com/scmbreeze/scm_breeze.git") {
		add("git", "scm-breeze", "install", ".scm_breeze")
	}
	if strings.Contains(output, `python3 "$font_helper" install`) {
		add("jetendard", "jetendard", "install", "Library/Fonts")
	}
	if strings.Contains(output, `python3 "$settings_helper" apply`) {
		add("json-fields", "jetendard-zed", "configure", ".config/zed/settings.json")
		add("json-fields", "jetendard-orca", "configure", "Library/Application Support/orca/profiles/*/orca-data.json")
	}
	automation, _ := cfg.Terrapod["enableMacosAppGroupAutomation"].(bool)
	if automation && strings.Contains(output, `open -a "Karabiner-Elements"`) {
		add("karabiner", "karabiner-opener", "action", "Karabiner-Elements")
	}
	for _, match := range brewLine.FindAllStringSubmatch(output, -1) {
		add("homebrew-formula", match[1], "install", byPair["homebrew-formula\x00"+match[1]].Commands[0])
	}
	return result
}

func literalMutations(values []mutation) []mutation {
	var result []mutation
	for _, value := range values {
		if value.Evidence == "literal" {
			result = append(result, value)
		}
	}
	return result
}

func caskDestination(pkg string) string {
	values := map[string]string{"ghostty": "Ghostty.app", "hammerspoon": "Hammerspoon.app", "karabiner-elements": "Karabiner-Elements.app", "scroll-reverser": "Scroll Reverser.app", "raycast": "Raycast.app", "1password-cli": "op", "istat-menus": "iStat Menus.app", "zed": "Zed.app", "stablyai/orca/orca": "Orca.app", "antigravity-cli": "agy", "claude-code": "claude", "codex": "codex"}
	return values[pkg]
}
func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
func sortMutations(values []mutation) {
	sort.Slice(values, func(i, j int) bool {
		left := values[i].ResourceID + values[i].Kind + values[i].Destination
		right := values[j].ResourceID + values[j].Kind + values[j].Destination
		return left < right
	})
}
