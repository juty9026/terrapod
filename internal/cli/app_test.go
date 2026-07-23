package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/juty9026/terrapod/internal/catalog"
	"github.com/juty9026/terrapod/internal/config"
	"github.com/juty9026/terrapod/internal/execx"
	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/paths"
	"github.com/juty9026/terrapod/internal/planner"
	"github.com/juty9026/terrapod/internal/reconcile"
	"github.com/juty9026/terrapod/internal/resolve"
	"github.com/juty9026/terrapod/internal/resource"
	"github.com/juty9026/terrapod/internal/state"
	updatepkg "github.com/juty9026/terrapod/internal/update"
)

func TestHelpDescribesShadowCommandSurfaceWithoutDependencies(t *testing.T) {
	code, stdout, stderr := run(t, []string{"help"}, Dependencies{})
	if code != 0 || stderr != "" {
		t.Fatalf("Run(help) = %d, stderr=%q", code, stderr)
	}
	for _, want := range []string{
		"Personal Development Environment Manager",
		"plan", "status", "doctor", "diff",
		"apply      Reconcile managed resources",
		"resolve    Resolve one unavailable resource",
		"update     Install the latest signed release",
		"setup (unavailable until activation)",
		"configure (unavailable until activation)",
		"chezmoi    Run a constrained read-only chezmoi command",
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

func TestManagerCommandsRejectRootBeforeCommandDispatch(t *testing.T) {
	for _, command := range []string{"plan", "status", "doctor", "diff", "apply", "update", "resolve", "setup", "configure", "chezmoi"} {
		t.Run(command, func(t *testing.T) {
			deps := Dependencies{Geteuid: func() int { return 0 }}
			code, _, stderr := run(t, []string{command}, deps)
			if code == 0 || !strings.Contains(stderr, "must run as a non-root user") {
				t.Fatalf("Run(%s as root) = %d, stderr=%q", command, code, stderr)
			}
		})
	}
}

func TestResolveRequiresExactlyOneResourceAndDelegatesPromptIO(t *testing.T) {
	called := false
	deps := Dependencies{
		Geteuid: func() int { return 501 },
		Stdin:   strings.NewReader("yes\n"),
		Resolve: func(_ context.Context, id model.ResourceID, input io.Reader, output io.Writer) (resolve.Result, error) {
			called = true
			if id != "core.alpha" {
				t.Fatalf("id = %q", id)
			}
			answer, _ := io.ReadAll(input)
			if string(answer) != "yes\n" {
				t.Fatalf("input = %q", answer)
			}
			fmt.Fprint(output, "resolved")
			return resolve.Result{Proceeded: true, Summary: reconcile.Summary{Ready: []model.ResourceID{"core.alpha"}, Unavailable: map[model.ResourceID]string{}}}, nil
		},
	}
	code, stdout, stderr := run(t, []string{"resolve", "core.alpha"}, deps)
	if code != 0 || !called || stdout != "resolved\nReady:\n  core.alpha\nUnavailable:\n  (none)\n" || stderr != "" {
		t.Fatalf("Run(resolve) = %d, called=%v stdout=%q stderr=%q", code, called, stdout, stderr)
	}
	for _, args := range [][]string{{"resolve"}, {"resolve", "core.alpha", "core.beta"}} {
		called = false
		code, stdout, stderr = run(t, args, deps)
		if code != exitUsage || called || stdout != "" || stderr != "usage: tpod resolve <resource>\n" {
			t.Fatalf("Run(%v) = %d, called=%v stdout=%q stderr=%q", args, code, called, stdout, stderr)
		}
	}
}

func TestCommandsRejectInvalidArgumentsBeforeRendering(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"help", []string{"help", "unexpected"}},
		{"version", []string{"version", "unexpected"}},
		{"plan", []string{"plan", "unexpected"}},
		{"status", []string{"status", "unexpected"}},
		{"doctor", []string{"doctor", "unexpected"}},
		{"diff", []string{"diff", "unexpected"}},
		{"apply", []string{"apply", "unexpected"}},
		{"update", []string{"update", "unexpected"}},
		{"resolve", []string{"resolve", "unexpected", "extra"}},
		{"setup", []string{"setup", "unexpected"}},
		{"configure", []string{"configure", "unexpected"}},
		{"chezmoi", []string{"chezmoi", "unexpected"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			deps := Dependencies{Geteuid: func() int { return 501 }}
			code, stdout, stderr := run(t, test.args, deps)
			if code != 2 || stdout != "" || !strings.HasPrefix(stderr, "usage: tpod") {
				t.Fatalf("Run(%v) = %d, stdout=%q stderr=%q", test.args, code, stdout, stderr)
			}
		})
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

func TestUpdateAndHiddenContinuationDispatch(t *testing.T) {
	deps := Dependencies{Geteuid: func() int { return 501 }, Update: func(context.Context) (updatepkg.Result, error) {
		return updatepkg.Result{Handoff: true, JournalID: "journal"}, nil
	}}
	code, _, stderr := run(t, []string{"update"}, deps)
	if code != 0 || stderr != "" {
		t.Fatalf("Run(update)=%d stderr=%q", code, stderr)
	}
	var journal string
	deps.ContinueUpdate = func(_ context.Context, id string) (updatepkg.Result, error) {
		journal = id
		return updatepkg.Result{Summary: reconcile.Summary{Unavailable: map[model.ResourceID]string{}}}, nil
	}
	code, _, stderr = run(t, []string{"internal-continue-update", "--journal", "journal-id"}, deps)
	if code != 0 || stderr != "" || journal != "journal-id" {
		t.Fatalf("Run(continue)=%d journal=%q stderr=%q", code, journal, stderr)
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

func TestBuildReconciliationUsesStateBoundPlannerFactory(t *testing.T) {
	deps, _ := fixtureDependencies(t, "ready.json")
	want := deps.Planner
	deps.Planner = nil
	called := false
	deps.PlannerForState = func(store *state.Store) (*planner.Planner, error) {
		called = true
		if store == nil {
			t.Fatal("planner factory received nil state")
		}
		return want, nil
	}
	code, _, stderr := run(t, []string{"plan"}, deps)
	if code != 0 || !called || stderr != "" {
		t.Fatalf("Run(plan)=%d called=%v stderr=%q", code, called, stderr)
	}
}

func TestStatusDistinguishesReadyAndUnavailable(t *testing.T) {
	deps, fixture := fixtureDependencies(t, "drifted.json")
	store, err := deps.OpenState()
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if got := persisted.Ownership["core.alpha"].Package; got != "alpha" {
		t.Fatalf("drifted fixture ownership package = %q, want alpha", got)
	}
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

func TestRequireSameFileRejectsDifferentIdentity(t *testing.T) {
	firstPath := filepath.Join(t.TempDir(), "first")
	secondPath := filepath.Join(t.TempDir(), "second")
	if err := os.WriteFile(firstPath, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secondPath, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := os.Stat(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	second, err := os.Stat(secondPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := requireSameFile(first, first, "owner"); err != nil {
		t.Fatalf("same identity rejected: %v", err)
	}
	if err := requireSameFile(first, second, "owner"); err == nil {
		t.Fatal("different identities accepted")
	}
}

func TestDoctorFailsOnlyWhenEnabledResourceIsUnavailable(t *testing.T) {
	deps, fixture := fixtureDependencies(t, "ready.json")
	store, err := deps.OpenState()
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(persisted.Ownership) != 0 || len(persisted.AppliedCatalogs) != 1 || persisted.AppliedCatalogs[0] != "fixture-digest" {
		t.Fatalf("ready fixture snapshot = %#v", persisted)
	}
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

func TestInspectLiveLockRejectsUnsafeFilesystemEntries(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, stateDir string)
	}{
		{
			name: "lock directory symlink",
			setup: func(t *testing.T, stateDir string) {
				external := t.TempDir()
				if err := os.Symlink(external, filepath.Join(stateDir, "lock")); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "owner symlink",
			setup: func(t *testing.T, stateDir string) {
				makeLockDir(t, stateDir)
				external := filepath.Join(t.TempDir(), "owner.json")
				if err := os.WriteFile(external, validOwnerJSON(42), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(external, filepath.Join(stateDir, "lock", "owner.json")); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "owner fifo",
			setup: func(t *testing.T, stateDir string) {
				makeLockDir(t, stateDir)
				if err := syscall.Mkfifo(filepath.Join(stateDir, "lock", "owner.json"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "oversized owner",
			setup: func(t *testing.T, stateDir string) {
				makeLockDir(t, stateDir)
				writeOwnerContents(t, stateDir, bytes.Repeat([]byte("x"), 64*1024+1))
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stateDir := t.TempDir()
			test.setup(t, stateDir)
			called := false
			_, err := inspectLiveLock(stateDir, func(int) error {
				called = true
				return nil
			})
			if err == nil {
				t.Fatal("unsafe lock accepted")
			}
			if called {
				t.Fatal("process probe called for unsafe lock")
			}
		})
	}
}

func TestRunDoesNotBlockWhenOwnerChangesFromRegularFileToFIFO(t *testing.T) {
	deps, _ := fixtureDependencies(t, "ready.json")
	writeLockOwner(t, deps.Paths.StateDir, "")
	ownerPath := filepath.Join(deps.Paths.StateDir, "lock", "owner.json")
	originalHook := beforeOpenLockOwner
	t.Cleanup(func() { beforeOpenLockOwner = originalHook })
	var hookErr error
	hookReached := make(chan struct{})
	beforeOpenLockOwner = func() {
		defer close(hookReached)
		if err := os.Remove(ownerPath); err != nil {
			hookErr = err
			return
		}
		hookErr = syscall.Mkfifo(ownerPath, 0o600)
	}

	type result struct {
		code   int
		stderr string
	}
	results := make(chan result, 1)
	go func() {
		code, _, stderr := run(t, []string{"status"}, deps)
		results <- result{code: code, stderr: stderr}
	}()
	<-hookReached
	if hookErr != nil {
		t.Fatal(hookErr)
	}

	select {
	case got := <-results:
		if got.code == 0 || !strings.Contains(got.stderr, "inspect reconciliation lock") {
			t.Fatalf("Run(status) = %d, stderr=%q", got.code, got.stderr)
		}
	case <-time.After(250 * time.Millisecond):
		writer, err := os.OpenFile(ownerPath, os.O_WRONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			t.Fatalf("unblock FIFO reader: %v", err)
		}
		if err := writer.Close(); err != nil {
			t.Fatalf("close FIFO writer: %v", err)
		}
		select {
		case <-results:
		case <-time.After(time.Second):
			t.Fatal("blocked Run goroutine could not be recovered")
		}
		t.Fatal("Run blocked while opening a swapped FIFO owner")
	}
}

func TestInspectLiveLockRejectsInvalidOwnerMetadata(t *testing.T) {
	tests := []struct {
		name     string
		contents []byte
	}{
		{"unknown field", []byte(`{"pid":42,"command":"tpod apply","startedAt":"2026-07-22T00:00:00Z","nonce":"0123456789abcdef0123456789abcdef","extra":true}`)},
		{"invalid nonce", []byte(`{"pid":42,"command":"tpod apply","startedAt":"2026-07-22T00:00:00Z","nonce":"00"}`)},
		{"zero pid", []byte(`{"pid":0,"command":"tpod apply","startedAt":"2026-07-22T00:00:00Z","nonce":"0123456789abcdef0123456789abcdef"}`)},
		{"control command", []byte("{\"pid\":42,\"command\":\"tpod\\napply\",\"startedAt\":\"2026-07-22T00:00:00Z\",\"nonce\":\"0123456789abcdef0123456789abcdef\"}")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stateDir := t.TempDir()
			makeLockDir(t, stateDir)
			writeOwnerContents(t, stateDir, test.contents)
			called := false
			_, err := inspectLiveLock(stateDir, func(int) error {
				called = true
				return nil
			})
			if err == nil {
				t.Fatal("invalid lock owner accepted")
			}
			if called {
				t.Fatal("process probe called for invalid lock owner")
			}
		})
	}
}

func TestInspectLiveLockClassifiesProcessProbeResults(t *testing.T) {
	tests := []struct {
		name       string
		probeError error
		want       string
		wantError  bool
	}{
		{"live", nil, "active (PID 42, command tpod apply)", false},
		{"permission denied is live", syscall.EPERM, "active (PID 42, command tpod apply)", false},
		{"stale", syscall.ESRCH, "none (stale lock present)", false},
		{"probe failure", errors.New("probe failed"), "", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stateDir := t.TempDir()
			makeLockDir(t, stateDir)
			writeOwnerContents(t, stateDir, validOwnerJSON(42))
			got, err := inspectLiveLock(stateDir, func(pid int) error {
				if pid != 42 {
					t.Fatalf("probe PID = %d, want 42", pid)
				}
				return test.probeError
			})
			if (err != nil) != test.wantError || got != test.want {
				t.Fatalf("inspectLiveLock() = %q, %v; want %q, error=%v", got, err, test.want, test.wantError)
			}
			if strings.Contains(got, "active") && test.probeError == syscall.ESRCH {
				t.Fatalf("stale PID reported live: %q", got)
			}
		})
	}
}

func TestDiffUsesManagedFileClientAndPreservesOutput(t *testing.T) {
	called := false
	deps := Dependencies{Geteuid: func() int { return 501 }, Diff: func(context.Context) ([]byte, error) {
		called = true
		return []byte("diff --git a/.zshrc b/.zshrc\n"), nil
	}}
	code, stdout, stderr := run(t, []string{"diff"}, deps)
	if code != 0 || !called || stdout != "diff --git a/.zshrc b/.zshrc\n" || stderr != "" {
		t.Fatalf("Run(diff) = %d, called=%v stdout=%q stderr=%q", code, called, stdout, stderr)
	}

	deps.Diff = func(context.Context) ([]byte, error) { return nil, errors.New("diff failed") }
	code, _, stderr = run(t, []string{"diff"}, deps)
	if code != exitUnavailable || !strings.Contains(stderr, "diff failed") {
		t.Fatalf("failed diff = %d, stderr=%q", code, stderr)
	}
}

func TestMutationCommandsAreNotDispatched(t *testing.T) {
	for _, command := range []string{"setup", "configure"} {
		t.Run(command, func(t *testing.T) {
			deps := Dependencies{Geteuid: func() int { return 501 }}
			code, _, stderr := run(t, []string{command}, deps)
			if code == 0 || !strings.Contains(stderr, "unavailable until activation") {
				t.Fatalf("Run(%s) = %d, stderr=%q", command, code, stderr)
			}
		})
	}
}

func TestChezmoiDispatchesOnlyExplicitReadOnlyPassthrough(t *testing.T) {
	called := false
	deps := Dependencies{
		Geteuid: func() int { return 501 },
		Chezmoi: func(_ context.Context, command string, operands []string) (execx.Result, error) {
			called = true
			if command != "status" || !reflect.DeepEqual(operands, []string{".zshrc"}) {
				t.Fatalf("passthrough = %q %#v", command, operands)
			}
			return execx.Result{Stdout: []byte("clean\n"), Stderr: []byte("notice\n")}, nil
		},
	}
	code, stdout, stderr := run(t, []string{"chezmoi", "--", "status", ".zshrc"}, deps)
	if code != 0 || !called || stdout != "clean\n" || stderr != "notice\n" {
		t.Fatalf("Run(chezmoi) = %d, called=%v stdout=%q stderr=%q", code, called, stdout, stderr)
	}

	for _, args := range [][]string{{"chezmoi"}, {"chezmoi", "status"}, {"chezmoi", "--"}} {
		called = false
		code, _, stderr = run(t, args, deps)
		if code != exitUsage || called || stderr != "usage: tpod chezmoi -- <read-only-command> [args...]\n" {
			t.Fatalf("Run(%v) = %d, called=%v stderr=%q", args, code, called, stderr)
		}
	}
}

func TestComposeRegistryRegistersEveryPlan02And03Adapter(t *testing.T) {
	fixture := &resource.Fixture{}
	registry, err := ComposeRegistry(AdapterSet{
		ManagementCore:  fixture,
		HomebrewFormula: fixture,
		HomebrewCask:    fixture,
		APT:             fixture,
		Mise:            fixture,
		ManagedFiles:    fixture,
		GitCheckout:     fixture,
		Jetendard:       fixture,
		JSONFields:      fixture,
		PlistFields:     fixture,
		Karabiner:       fixture,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []struct {
		typeName model.ResourceType
		provider string
	}{
		{model.ResourceManagementCore, "terrapod"},
		{model.ResourcePackage, "homebrew-formula"},
		{model.ResourcePackage, "homebrew-cask"},
		{model.ResourcePackage, "apt"},
		{model.ResourcePackage, "mise"},
		{model.ResourceManagedFiles, "chezmoi"},
		{model.ResourceGitCheckout, "git"},
		{model.ResourceArchive, "jetendard"},
		{model.ResourceIntegration, "json-fields"},
		{model.ResourceIntegration, "plist-fields"},
		{model.ResourceIntegration, "karabiner"},
	} {
		if got, ok := registry.Lookup(key.typeName, key.provider); !ok || got != fixture {
			t.Errorf("registry lookup (%q, %q) = %#v, %v", key.typeName, key.provider, got, ok)
		}
	}
}

func TestComposeRegistryRejectsMissingAdapter(t *testing.T) {
	if _, err := ComposeRegistry(AdapterSet{}); err == nil || !strings.Contains(err.Error(), "management-core") {
		t.Fatalf("ComposeRegistry error = %v", err)
	}
}

func TestApplyBuildsNonUpgradePlanAndRendersDeterministicSummary(t *testing.T) {
	deps, fixture := fixtureDependencies(t, "ready.json")
	loadCatalog := deps.LoadCatalog
	deps.LoadCatalog = func() (catalog.Verified, error) {
		verified, err := loadCatalog()
		if err != nil {
			return catalog.Verified{}, err
		}
		verified.Catalog.Resources = append(verified.Catalog.Resources, model.Resource{ID: "optional.hidden", Type: model.ResourcePackage, Provider: "fixture", Package: "hidden", VersionPolicy: model.VersionTracked, Metadata: map[string]string{planner.EnabledByAnyConfigMetadataPrefix + "enableAi": "true"}})
		return verified, nil
	}
	fixture.Operations = map[model.ResourceID][]model.Operation{
		"core.alpha": {{ID: "upgrade", Kind: model.OperationUpgrade, Provider: "fixture"}, {ID: "install", Kind: model.OperationInstall, Provider: "fixture"}},
	}
	called := false
	deps.Apply = func(_ context.Context, input reconcile.ApplyInput) (reconcile.Summary, error) {
		called = true
		if input.CatalogDigest != "fixture-digest" || len(input.CurrentResources) != 5 || len(input.EnabledIDs) != 4 {
			t.Fatalf("apply facts: %#v", input)
		}
		for _, operation := range input.Plan.Operations {
			if operation.Kind == model.OperationUpgrade {
				t.Fatal("apply enabled upgrade")
			}
		}
		return reconcile.Summary{Ready: []model.ResourceID{"core.beta", "core.alpha"}, Unavailable: map[model.ResourceID]string{}}, nil
	}
	code, stdout, stderr := run(t, []string{"apply"}, deps)
	if code != 0 || stderr != "" || !called {
		t.Fatalf("apply=%d called=%v stderr=%q", code, called, stderr)
	}
	if stdout != "Ready:\n  core.alpha\n  core.beta\nUnavailable:\n  (none)\n" {
		t.Fatalf("stdout=%q", stdout)
	}
}

func TestApplyReturnsUnavailableWithUsefulSummaryOnResourceFailure(t *testing.T) {
	deps, _ := fixtureDependencies(t, "ready.json")
	deps.Apply = func(context.Context, reconcile.ApplyInput) (reconcile.Summary, error) {
		return reconcile.Summary{Ready: []model.ResourceID{"core.alpha"}, Unavailable: map[model.ResourceID]string{"core.beta": "verify failed"}}, errors.New("journal warning")
	}
	code, stdout, stderr := run(t, []string{"apply"}, deps)
	if code != exitUnavailable || !strings.Contains(stdout, "core.beta: verify failed") || !strings.Contains(stderr, "journal warning") {
		t.Fatalf("apply=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestApplyInputPreservesCurrentEnabledAndHistoricalAuthorities(t *testing.T) {
	current := model.Resource{ID: "core.alpha", Type: model.ResourcePackage, Provider: "new", Package: "alpha", VersionPolicy: model.VersionTracked, Metadata: map[string]string{planner.EnabledByConfigMetadataKey: "enableAlpha"}}
	historical := model.Resource{ID: "core.alpha", Type: model.ResourcePackage, Provider: "old", Package: "alpha-old", VersionPolicy: model.VersionTracked}
	r := reconciliation{catalog: model.Catalog{Resources: []model.Resource{current}}, config: model.Config{Terrapod: map[string]any{"profile": "vps-shell", "enableAlpha": false}}, plan: model.Plan{ID: "p"}, digest: "current", historical: map[string]model.Catalog{"old-digest": {Resources: []model.Resource{historical}}}, snapshot: model.Snapshot{Ownership: map[model.ResourceID]model.Ownership{"core.alpha": {ResourceID: "core.alpha", CatalogDigest: "old-digest", Provider: "old", Package: "alpha-old"}}}, profile: model.ProfileVPSShell}
	input := r.applyInput()
	if len(input.CurrentResources) != 1 || len(input.EnabledIDs) != 0 {
		t.Fatalf("current/enabled lost: %#v", input)
	}
	got, ok := input.HistoricalResources["core.alpha"]
	if !ok || got.Resource.Provider != "old" || got.CatalogDigest != "old-digest" {
		t.Fatalf("historical lost: %#v", input.HistoricalResources)
	}
	if input.Profile != model.ProfileVPSShell {
		t.Fatalf("profile=%q", input.Profile)
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

func makeLockDir(t *testing.T, stateDir string) {
	t.Helper()
	if err := os.Mkdir(filepath.Join(stateDir, "lock"), 0o700); err != nil {
		t.Fatal(err)
	}
}

func writeOwnerContents(t *testing.T, stateDir string, contents []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(stateDir, "lock", "owner.json"), contents, 0o600); err != nil {
		t.Fatal(err)
	}
}

func validOwnerJSON(pid int) []byte {
	return []byte(fmt.Sprintf(`{"pid":%d,"command":"tpod apply","startedAt":"2026-07-22T00:00:00Z","nonce":"0123456789abcdef0123456789abcdef"}`, pid))
}
