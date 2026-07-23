package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/juty9026/terrapod/internal/chezmoi"
	"github.com/juty9026/terrapod/internal/cli"
	"github.com/juty9026/terrapod/internal/execx"
	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/paths"
	"github.com/juty9026/terrapod/internal/planner"
	"github.com/juty9026/terrapod/internal/resource"
	"github.com/juty9026/terrapod/internal/resource/managementcore"
	"github.com/juty9026/terrapod/internal/state"
)

func TestCompiledReleaseRootsRequireCanonicalCompleteLdflags(t *testing.T) {
	oldID, oldKey := releaseRootKeyID, releaseRootPublicKey
	t.Cleanup(func() { releaseRootKeyID, releaseRootPublicKey = oldID, oldKey })
	releaseRootKeyID, releaseRootPublicKey = "", ""
	if _, err := compiledReleaseRoots(); err == nil || !strings.Contains(err.Error(), "not embedded") {
		t.Fatalf("empty roots error=%v", err)
	}
	releaseRootKeyID, releaseRootPublicKey = "root", "%%"
	if _, err := compiledReleaseRoots(); err == nil {
		t.Fatal("invalid base64 accepted")
	}
	key := make([]byte, ed25519.PublicKeySize)
	releaseRootPublicKey = base64.StdEncoding.EncodeToString(key)
	roots, err := compiledReleaseRoots()
	if err != nil || len(roots["root"]) != ed25519.PublicKeySize {
		t.Fatalf("roots=%v err=%v", roots, err)
	}
}

type privilegeRunnerFunc func(context.Context, execx.Request) (execx.Result, error)

func (f privilegeRunnerFunc) Run(ctx context.Context, request execx.Request) (execx.Result, error) {
	return f(ctx, request)
}

func TestPrivilegePreflightIsNoninteractiveAndBounded(t *testing.T) {
	called := false
	err := noninteractivePrivilegeWithRunner(context.Background(), privilegeRunnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		called = true
		if request.Path != "/usr/bin/sudo" || !reflect.DeepEqual(request.Args, []string{"-n", "true"}) || request.Stdin != nil || request.Privilege {
			t.Fatalf("request=%#v", request)
		}
		return execx.Result{}, nil
	}))
	if err != nil || !called {
		t.Fatalf("called=%v err=%v", called, err)
	}
}

func TestBuiltBinaryDispatchesThroughRealConstrainedChezmoiClient(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	xdgData := filepath.Join(root, "data")
	xdgConfig := filepath.Join(root, "config")
	source := filepath.Join(xdgData, "terrapod", "current")
	config := filepath.Join(xdgConfig, "terrapod", "config.json")
	logPath := filepath.Join(root, "argv.log")
	for _, dir := range []string{home, source, filepath.Dir(config)} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(source, "dot_test"), []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte(`{"version":1,"terrapod":{"profile":"macos-terminal"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := filepath.Join(root, "chezmoi")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + logPath + "\nprintf 'fixture-status\\n'\n"
	if err := os.WriteFile(fake, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(root, "tpod")
	build := exec.Command("go", "build", "-ldflags", "-X main.chezmoiPathOverride="+fake, "-o", binary, ".")
	build.Env = os.Environ()
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	command := exec.Command(binary, "chezmoi", "--", "status", ".zshrc")
	command.Env = append(os.Environ(), "HOME="+home, "XDG_DATA_HOME="+xdgData, "XDG_CONFIG_HOME="+xdgConfig, "XDG_STATE_HOME="+filepath.Join(root, "state"), "XDG_CACHE_HOME="+filepath.Join(root, "cache"))
	output, err := command.CombinedOutput()
	if err != nil || string(output) != "fixture-status\n" {
		t.Fatalf("tpod chezmoi: %v, output=%q", err, output)
	}
	argv, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(string(argv))
	commandIndex, excludeIndex := index(fields, "status"), index(fields, "--exclude")
	if index(fields, "--source") < 0 || index(fields, "--override-data-file") < 0 || commandIndex < 0 || excludeIndex < commandIndex || index(fields, "scripts") != excludeIndex+1 {
		t.Fatalf("unsafe argv: %q", argv)
	}
	if index(fields, "apply") >= 0 || index(fields, "update") >= 0 || index(fields, "init") >= 0 {
		t.Fatalf("mutating argv: %q", argv)
	}
}

func TestProductionPlannerComposesRealStateBoundAdapters(t *testing.T) {
	home := t.TempDir()
	layout := paths.Resolve(home, map[string]string{})
	store, err := state.Open(layout.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	client := chezmoi.Client{Runner: execx.NewRunner([]string{"HOME"}, nil, func() int { return 501 }), Binary: filepath.Join(home, "chezmoi"), Source: layout.ActiveRelease, Config: layout.ConfigFile, Destination: home}
	got, err := productionPlanner(layout, store, client)
	if err != nil || got == nil {
		t.Fatalf("productionPlanner = %#v, %v", got, err)
	}
}

func TestProductionRegistryBuildsEveryEnabledResourceForAllConfigurations(t *testing.T) {
	catalog := productionTestCatalog(t)
	fixture := &resource.Fixture{Observations: make(map[model.ResourceID]model.Observation)}
	for _, item := range catalog.Resources {
		fixture.Observations[item.ID] = model.Observation{
			Present: true, Healthy: true, Provider: item.Provider, Package: item.Package,
			Paths: map[string]string{"actual": filepath.Join(t.TempDir(), string(item.ID))},
		}
	}
	management := productionTestManagement(t, filepath.Join(t.TempDir(), "brew"), true)
	configured, err := composeProductionPlanner(productionTestAdapters(management, fixture))
	if err != nil {
		t.Fatal(err)
	}
	for _, profile := range []model.Profile{model.ProfileMacOSTerminal, model.ProfileVPSShell} {
		for _, preset := range []string{"minimal", "development", "workstation"} {
			plan, err := configured.Build(context.Background(), planner.Input{
				Catalog: catalog, CatalogDigest: "fixture-digest", Profile: profile,
				Config: productionTestConfig(profile, preset), Snapshot: model.Snapshot{Ownership: map[model.ResourceID]model.Ownership{}},
			})
			if err != nil {
				t.Fatalf("Build(%s/%s): %v", profile, preset, err)
			}
			if len(plan.Unavailable) != 0 {
				t.Fatalf("Build(%s/%s) unavailable = %#v", profile, preset, plan.Unavailable)
			}
		}
	}
}

func TestProductionRegistryReportsOnlyDeliberatelyMissingHomebrew(t *testing.T) {
	catalog := productionTestCatalog(t)
	fixture := &resource.Fixture{Observations: make(map[model.ResourceID]model.Observation)}
	for _, item := range catalog.Resources {
		fixture.Observations[item.ID] = model.Observation{Present: true, Healthy: true, Provider: item.Provider, Package: item.Package, Paths: map[string]string{}}
	}
	management := productionTestManagement(t, filepath.Join(t.TempDir(), "missing", "brew"), false)
	configured, err := composeProductionPlanner(productionTestAdapters(management, fixture))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := configured.Build(context.Background(), planner.Input{
		Catalog: catalog, CatalogDigest: "fixture-digest", Profile: model.ProfileMacOSTerminal,
		Config: productionTestConfig(model.ProfileMacOSTerminal, "minimal"), Snapshot: model.Snapshot{Ownership: map[model.ResourceID]model.Ownership{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Unavailable) != 1 || !strings.Contains(plan.Unavailable["management.homebrew"], "bootstrap or repair") {
		t.Fatalf("unavailable = %#v", plan.Unavailable)
	}
	for _, reason := range plan.Unavailable {
		if strings.Contains(reason, "adapter unavailable") {
			t.Fatalf("production registry missing adapter: %s", reason)
		}
	}
}

func productionTestCatalog(t *testing.T) model.Catalog {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "catalog", "v1", "resources.json"))
	if err != nil {
		t.Fatal(err)
	}
	var value model.Catalog
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func productionTestManagement(t *testing.T, binary string, present bool) resource.Adapter {
	t.Helper()
	if present {
		if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	adapter, err := managementcore.NewHomebrew(binary, filepath.Dir(binary))
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}

func productionTestAdapters(management resource.Adapter, fixture resource.Adapter) cli.AdapterSet {
	return cli.AdapterSet{
		ManagementCore: management, HomebrewFormula: fixture, HomebrewCask: fixture,
		APT: fixture, Mise: fixture, ManagedFiles: fixture, GitCheckout: fixture,
		Jetendard: fixture, JSONFields: fixture, PlistFields: fixture, Karabiner: fixture,
	}
}

func productionTestConfig(profile model.Profile, preset string) model.Config {
	enabled := preset != "minimal"
	groups := preset == "workstation"
	return model.Config{Version: 1, Terrapod: map[string]any{
		"profile": string(profile), "enableEditorStack": enabled, "enableAiCliTools": enabled,
		"enableDevelopmentWorkspace": enabled, "enableMacosAppGroupTerminalApps": groups,
		"enableMacosAppGroupAutomation": groups, "enableMacosAppGroupLauncher": groups,
		"enableMacosAppGroupMonitoring": groups, "enableMacosAppGroupDevelopmentApps": groups,
	}}
}

func index(values []string, target string) int {
	for i, value := range values {
		if value == target {
			return i
		}
	}
	return -1
}
