package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/juty9026/terrapod/internal/catalog"
	"github.com/juty9026/terrapod/internal/config"
	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/paths"
	"github.com/juty9026/terrapod/internal/planner"
	"github.com/juty9026/terrapod/internal/resource"
	"github.com/juty9026/terrapod/internal/state"
)

func TestHelpDescribesShadowCommandSurfaceWithoutDependencies(t *testing.T) {
	code, stdout, stderr := run(t, []string{"help"}, Dependencies{})
	if code != 0 || stderr != "" {
		t.Fatalf("Run(help) = %d, stderr=%q", code, stderr)
	}
	for _, want := range []string{
		"Personal Development Environment Manager",
		"plan", "status", "doctor", "diff",
		"apply (unavailable until activation)",
		"update (unavailable until activation)",
		"resolve (unavailable until activation)",
		"setup (unavailable until activation)",
		"configure (unavailable until activation)",
		"chezmoi (unavailable until activation)",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("help does not contain %q:\n%s", want, stdout)
		}
	}
}

func TestVersionDoesNotRequireManagerDependencies(t *testing.T) {
	code, stdout, stderr := run(t, []string{"version"}, Dependencies{})
	if code != 0 || stdout != "tpod development\n" || stderr != "" {
		t.Fatalf("Run(version) = %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestManagerCommandsRejectRootBeforeLoadingDependencies(t *testing.T) {
	deps := Dependencies{Geteuid: func() int { return 0 }}
	code, _, stderr := run(t, []string{"status"}, deps)
	if code == 0 || !strings.Contains(stderr, "must run as a non-root user") {
		t.Fatalf("Run(status as root) = %d, stderr=%q", code, stderr)
	}
}

func TestMissingConfigPrintsSetupGuidanceWithoutLoadingOtherData(t *testing.T) {
	loadedCatalog := false
	deps := Dependencies{
		Geteuid: func() int { return 501 },
		Paths:   paths.Layout{ConfigFile: "/home/me/.config/terrapod/config.json"},
		LoadConfig: func() (model.Config, error) {
			return model.Config{}, &config.ErrMissing{Path: "/home/me/.config/terrapod/config.json"}
		},
		LoadCatalog: func() (catalog.Verified, error) {
			loadedCatalog = true
			return catalog.Verified{}, nil
		},
	}
	code, _, stderr := run(t, []string{"plan"}, deps)
	if code == 0 || !strings.Contains(stderr, "config is missing") || !strings.Contains(stderr, "setup is unavailable until activation") {
		t.Fatalf("Run(plan) = %d, stderr=%q", code, stderr)
	}
	if loadedCatalog {
		t.Fatal("catalog loaded after missing config")
	}
}

func TestPlanRendersStableSectionsAndNeverExecutes(t *testing.T) {
	deps, fixture := fixtureDependencies(t, "ready.json")
	fixture.Operations = map[model.ResourceID][]model.Operation{
		"core.alpha": {{ID: "install-alpha", Kind: model.OperationInstall, Detail: "install alpha"}},
		"core.beta":  {{ID: "adopt-beta", Kind: model.OperationAdopt, Detail: "adopt beta"}},
		"core.gamma": {{ID: "transfer-gamma", Kind: model.OperationTransfer, Detail: "transfer gamma"}},
	}
	fixture.InspectErrors = map[model.ResourceID]error{"core.delta": errors.New("binary missing")}
	t.Setenv("NO_COLOR", "1")

	code, stdout, stderr := run(t, []string{"plan"}, deps)
	if code != 0 || stderr != "" {
		t.Fatalf("Run(plan) = %d, stderr=%q", code, stderr)
	}
	wantSections := []string{"Adopt", "Install", "Upgrade", "Transfer", "Prune", "Unavailable"}
	last := -1
	for _, section := range wantSections {
		index := strings.Index(stdout, section+":")
		if index <= last {
			t.Fatalf("section %q absent or out of order:\n%s", section, stdout)
		}
		last = index
	}
	if strings.Contains(stdout, "\x1b[") {
		t.Fatalf("NO_COLOR output contains ANSI: %q", stdout)
	}
	code, repeated, repeatedStderr := run(t, []string{"plan"}, deps)
	if code != 0 || repeatedStderr != "" || repeated != stdout {
		t.Fatalf("repeated plan was not deterministic: code=%d\nfirst=%q\nsecond=%q\nstderr=%q", code, stdout, repeated, repeatedStderr)
	}
	if len(fixture.ExecuteCalls) != 0 || len(fixture.VerifyCalls) != 0 {
		t.Fatalf("read-only plan called Execute/Verify: %v %v", fixture.ExecuteCalls, fixture.VerifyCalls)
	}
}

func TestStatusDistinguishesReadyAndUnavailable(t *testing.T) {
	deps, fixture := fixtureDependencies(t, "drifted.json")
	fixture.Operations = map[model.ResourceID][]model.Operation{
		"core.beta": {{ID: "install-beta", Kind: model.OperationInstall}},
	}
	fixture.InspectErrors = map[model.ResourceID]error{"core.delta": errors.New("binary missing")}

	code, stdout, stderr := run(t, []string{"status"}, deps)
	if code != 0 || stderr != "" {
		t.Fatalf("Run(status) = %d, stderr=%q", code, stderr)
	}
	for _, want := range []string{
		"core.alpha: Ready",
		"core.beta: Unavailable (pending install)",
		"core.delta: Unavailable (inspect: binary missing)",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("status does not contain %q:\n%s", want, stdout)
		}
	}
}

func TestDoctorFailsOnlyWhenEnabledResourceIsUnavailable(t *testing.T) {
	deps, fixture := fixtureDependencies(t, "ready.json")
	code, _, stderr := run(t, []string{"doctor"}, deps)
	if code != 0 || stderr != "" {
		t.Fatalf("ready doctor = %d, stderr=%q", code, stderr)
	}

	fixture.InspectErrors = map[model.ResourceID]error{"core.alpha": errors.New("not found")}
	code, stdout, _ := run(t, []string{"doctor"}, deps)
	if code == 0 || !strings.Contains(stdout, "core.alpha: Unavailable (inspect: not found)") {
		t.Fatalf("unavailable doctor = %d, stdout=%q", code, stdout)
	}
}

func TestStatusReportsLiveReconciliationLock(t *testing.T) {
	deps, _ := fixtureDependencies(t, "ready.json")
	writeLockOwner(t, deps.Paths.StateDir, "")

	code, stdout, stderr := run(t, []string{"status"}, deps)
	want := fmt.Sprintf("Reconciliation lock: active (PID %d, command tpod apply)", os.Getpid())
	if code != 0 || stderr != "" || !strings.Contains(stdout, want) {
		t.Fatalf("Run(status) = %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestStatusRejectsTrailingLockOwnerData(t *testing.T) {
	deps, _ := fixtureDependencies(t, "ready.json")
	writeLockOwner(t, deps.Paths.StateDir, "{}")

	code, _, stderr := run(t, []string{"status"}, deps)
	if code == 0 || !strings.Contains(stderr, "inspect reconciliation lock") {
		t.Fatalf("Run(status) = %d, stderr=%q", code, stderr)
	}
}

func TestStatusRejectsIncompleteLockOwner(t *testing.T) {
	deps, _ := fixtureDependencies(t, "ready.json")
	if err := os.Mkdir(filepath.Join(deps.Paths.StateDir, "lock"), 0o700); err != nil {
		t.Fatal(err)
	}
	owner := fmt.Sprintf(`{"pid":%d,"command":"tpod apply"}`, os.Getpid())
	if err := os.WriteFile(filepath.Join(deps.Paths.StateDir, "lock", "owner.json"), []byte(owner), 0o600); err != nil {
		t.Fatal(err)
	}

	code, _, stderr := run(t, []string{"status"}, deps)
	if code == 0 || !strings.Contains(stderr, "inspect reconciliation lock") {
		t.Fatalf("Run(status) = %d, stderr=%q", code, stderr)
	}
}

func TestDiffReturnsShadowModeExitCode(t *testing.T) {
	deps := Dependencies{Geteuid: func() int { return 501 }}
	code, _, stderr := run(t, []string{"diff"}, deps)
	if code != 69 || stderr != "shadow mode: managed-file adapter is not active\n" {
		t.Fatalf("Run(diff) = %d, stderr=%q", code, stderr)
	}
}

func TestMutationCommandsAreNotDispatched(t *testing.T) {
	for _, command := range []string{"apply", "update", "resolve", "setup", "configure", "chezmoi"} {
		t.Run(command, func(t *testing.T) {
			deps := Dependencies{Geteuid: func() int { return 501 }}
			code, _, stderr := run(t, []string{command}, deps)
			if code == 0 || !strings.Contains(stderr, "unavailable until activation") {
				t.Fatalf("Run(%s) = %d, stderr=%q", command, code, stderr)
			}
		})
	}
}

func fixtureDependencies(t *testing.T, fixtureName string) (Dependencies, *resource.Fixture) {
	t.Helper()
	stateDir := t.TempDir()
	contents, err := os.ReadFile(filepath.Join("..", "..", "tests", "fixtures", "shadow-state", fixtureName))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "snapshot.json"), contents, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(stateDir, "journals"), 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := state.Open(stateDir)
	if err != nil {
		t.Fatal(err)
	}

	fixture := &resource.Fixture{}
	registry := resource.NewRegistry()
	if err := registry.Register(model.ResourcePackage, "fixture", fixture); err != nil {
		t.Fatal(err)
	}
	resources := []model.Resource{
		{ID: "core.delta", Type: model.ResourcePackage, Provider: "fixture", VersionPolicy: model.VersionTracked},
		{ID: "core.beta", Type: model.ResourcePackage, Provider: "fixture", VersionPolicy: model.VersionTracked},
		{ID: "core.alpha", Type: model.ResourcePackage, Provider: "fixture", VersionPolicy: model.VersionTracked},
		{ID: "core.gamma", Type: model.ResourcePackage, Provider: "fixture", VersionPolicy: model.VersionTracked},
	}
	return Dependencies{
		Geteuid: func() int { return 501 },
		Paths:   paths.Layout{StateDir: stateDir},
		LoadConfig: func() (model.Config, error) {
			return model.Config{Version: 1, Terrapod: map[string]any{"profile": "vps-shell"}}, nil
		},
		LoadCatalog: func() (catalog.Verified, error) {
			return catalog.Verified{Catalog: model.Catalog{Version: 1, Release: "fixture-v1", Resources: resources}, Digest: "fixture-digest"}, nil
		},
		OpenState: func() (*state.Store, error) { return store, nil },
		Planner:   planner.New(registry),
	}, fixture
}

func run(t *testing.T, args []string, deps Dependencies) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	deps.Stdout = &stdout
	deps.Stderr = &stderr
	return Run(context.Background(), args, deps), stdout.String(), stderr.String()
}

func writeLockOwner(t *testing.T, stateDir, suffix string) {
	t.Helper()
	if err := os.Mkdir(filepath.Join(stateDir, "lock"), 0o700); err != nil {
		t.Fatal(err)
	}
	owner := fmt.Sprintf(`{"pid":%d,"command":"tpod apply","startedAt":"2026-07-22T00:00:00Z","nonce":"0123456789abcdef0123456789abcdef"}%s`, os.Getpid(), suffix)
	if err := os.WriteFile(filepath.Join(stateDir, "lock", "owner.json"), []byte(owner), 0o600); err != nil {
		t.Fatal(err)
	}
}
