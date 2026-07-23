package homebrew

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/juty9026/terrapod/internal/execx"
	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/provider"
)

type runnerFunc func(context.Context, execx.Request) (execx.Result, error)

func (f runnerFunc) Run(ctx context.Context, request execx.Request) (execx.Result, error) {
	return f(ctx, request)
}

func TestRefreshMetadataRunsTypedBrewUpdate(t *testing.T) {
	called := false
	adapter, err := New(Formula, "/opt/homebrew/bin/brew", t.TempDir(), runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		called = true
		if request.Path != "/opt/homebrew/bin/brew" || !reflect.DeepEqual(request.Args, []string{"update"}) {
			t.Fatalf("request=%#v", request)
		}
		return execx.Result{}, nil
	}), AppPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.RefreshMetadata(context.Background()); err != nil || !called {
		t.Fatalf("RefreshMetadata called=%v err=%v", called, err)
	}
}

func TestConstructorsUseExactNamesAndStandardBrewPaths(t *testing.T) {
	for _, brewPath := range []string{"/opt/homebrew/bin/brew", "/usr/local/bin/brew", "/home/linuxbrew/.linuxbrew/bin/brew"} {
		t.Run(brewPath, func(t *testing.T) {
			formula, err := New(Formula, brewPath, t.TempDir(), runnerFunc(noCommand), AppPolicy{})
			if err != nil {
				t.Fatal(err)
			}
			cask, err := New(Cask, brewPath, t.TempDir(), runnerFunc(noCommand), AppPolicy{})
			if err != nil {
				t.Fatal(err)
			}
			if formula.Name() != "homebrew-formula" || cask.Name() != "homebrew-cask" {
				t.Fatalf("names = %q, %q", formula.Name(), cask.Name())
			}
		})
	}
}

func TestConstructorRejectsTypedNilDependencies(t *testing.T) {
	var nilRunner *pointerRunner
	var nilFS *pointerFS
	var nilInspector *pointerInspector
	var nilRunning *pointerRunning
	tests := []struct {
		name   string
		runner Runner
		policy AppPolicy
		want   string
	}{
		{name: "runner", runner: nilRunner, want: "required"},
		{name: "filesystem", runner: runnerFunc(noCommand), policy: AppPolicy{FS: nilFS}, want: "nil"},
		{name: "inspector", runner: runnerFunc(noCommand), policy: AppPolicy{Inspector: nilInspector}, want: "nil"},
		{name: "running checker", runner: runnerFunc(noCommand), policy: AppPolicy{Running: nilRunning}, want: "nil"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := New(Cask, "/opt/homebrew/bin/brew", t.TempDir(), tc.runner, tc.policy); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestFormulaInspectUsesFixedInventoryCommandsAndVerifiesProvidedCommands(t *testing.T) {
	var calls []execx.Request
	runner := runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		calls = append(calls, request)
		switch strings.Join(request.Args, " ") {
		case "info --json=v2 ripgrep":
			return execx.Result{Stdout: []byte(`{"formulae":[{"name":"ripgrep","full_name":"ripgrep","installed":[{"version":"14.1.1"}]}],"casks":[]}`)}, nil
		case "list --versions ripgrep":
			return execx.Result{Stdout: []byte("ripgrep 14.1.1\n")}, nil
		default:
			return execx.Result{}, errors.New("unexpected command")
		}
	})
	fs := fakeFS{files: map[string]os.FileInfo{"/opt/homebrew/bin/rg": fakeInfo{name: "rg", mode: 0o755}}}
	adapter, err := New(Formula, "/opt/homebrew/bin/brew", t.TempDir(), runner, AppPolicy{FS: fs})
	if err != nil {
		t.Fatal(err)
	}
	resource := formulaResource("ripgrep")
	resource.Commands = []string{"rg"}

	got, err := adapter.Inspect(context.Background(), resource)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Present || !got.Healthy || got.Version != "14.1.1" || got.Paths["rg"] != "/opt/homebrew/bin/rg" {
		t.Fatalf("observation = %#v", got)
	}
	want := []execx.Request{
		{Path: "/opt/homebrew/bin/brew", Args: []string{"info", "--json=v2", "ripgrep"}},
		{Path: "/opt/homebrew/bin/brew", Args: []string{"list", "--versions", "ripgrep"}},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
}

func TestInspectRejectsPackageMismatchAndMalformedJSON(t *testing.T) {
	for _, tc := range []struct {
		name string
		json string
		want string
	}{
		{name: "mismatch", json: `{"formulae":[{"name":"wget","full_name":"wget","installed":[]}],"casks":[]}`, want: "mismatch"},
		{name: "malformed", json: `{"formulae":`, want: "unexpected EOF"},
		{name: "multiple", json: `{"formulae":[{"name":"ripgrep"},{"name":"wget"}],"casks":[]}`, want: "exactly one"},
		{name: "unknown top-level field", json: `{"formulae":[{"name":"ripgrep","full_name":"ripgrep","installed":[]}],"casks":[],"unknown":true}`, want: "unknown"},
		{name: "unknown formula field", json: `{"formulae":[{"name":"ripgrep","full_name":"ripgrep","installed":[],"unknown":true}],"casks":[]}`, want: "unknown"},
		{name: "trailing value", json: `{"formulae":[{"name":"ripgrep","full_name":"ripgrep","installed":[]}],"casks":[]} {}`, want: "trailing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			adapter, err := New(Formula, "/opt/homebrew/bin/brew", t.TempDir(), runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
				return execx.Result{Stdout: []byte(tc.json)}, nil
			}), AppPolicy{})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := adapter.Inspect(context.Background(), formulaResource("ripgrep")); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestInspectRejectsUnknownCaskRecordField(t *testing.T) {
	adapter, err := New(Cask, "/opt/homebrew/bin/brew", t.TempDir(), runnerFunc(func(context.Context, execx.Request) (execx.Result, error) {
		return execx.Result{Stdout: []byte(`{"formulae":[],"casks":[{"token":"codex","full_token":"homebrew/cask/codex","installed":null,"unknown":true}]}`)}, nil
	}), AppPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	resource := caskResource("codex", "")
	resource.Metadata = map[string]string{}
	if _, err := adapter.Inspect(context.Background(), resource); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("error = %v", err)
	}
}

func TestInspectRejectsNullEnvelopeArrays(t *testing.T) {
	for _, tc := range []struct {
		name string
		json string
	}{
		{name: "formulae", json: `{"formulae":null,"casks":[]}`},
		{name: "casks", json: `{"formulae":[],"casks":null}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			adapter, err := New(Formula, "/opt/homebrew/bin/brew", t.TempDir(), runnerFunc(func(context.Context, execx.Request) (execx.Result, error) {
				return execx.Result{Stdout: []byte(tc.json)}, nil
			}), AppPolicy{})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := adapter.Inspect(context.Background(), formulaResource("ripgrep")); err == nil || !strings.Contains(err.Error(), "array") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestInspectRejectsWrongTapWithSameBasename(t *testing.T) {
	for _, kind := range []Kind{Formula, Cask} {
		t.Run(string(kind), func(t *testing.T) {
			adapter, err := New(kind, "/opt/homebrew/bin/brew", t.TempDir(), runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
				if request.Args[0] == "info" {
					if kind == Formula {
						return execx.Result{Stdout: []byte(`{"formulae":[{"name":"tool","full_name":"other/tap/tool","installed":[]}],"casks":[]}`)}, nil
					}
					return execx.Result{Stdout: []byte(`{"formulae":[],"casks":[{"token":"tool","full_token":"other/tap/tool","installed":null}]}`)}, nil
				}
				return execx.Result{}, errors.New("not installed")
			}), AppPolicy{})
			if err != nil {
				t.Fatal(err)
			}
			resource := formulaResource("wanted/tap/tool")
			resource.Provider = adapter.Name()
			if _, err := adapter.Inspect(context.Background(), resource); err == nil || !strings.Contains(err.Error(), "mismatch") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestQualifiedCaskUsesCanonicalInfoBeforeBasenameOnlyInventory(t *testing.T) {
	var calls [][]string
	adapter, err := New(Cask, "/opt/homebrew/bin/brew", t.TempDir(), runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		calls = append(calls, request.Args)
		switch strings.Join(request.Args, " ") {
		case "info --json=v2 stablyai/orca/orca":
			return execx.Result{Stdout: []byte(`{"formulae":[],"casks":[{"token":"orca","full_token":"stablyai/orca/orca","installed":"1.0"}]}`)}, nil
		case "list --versions stablyai/orca/orca":
			return execx.Result{Stdout: []byte("orca 1.0\n")}, nil
		case "outdated --json=v2 stablyai/orca/orca":
			return execx.Result{Stdout: []byte(`{"formulae":[],"casks":[{"name":"orca","current_version":"1.1","installed_versions":["1.0"],"pinned":false,"pinned_version":null}]}`)}, nil
		default:
			return execx.Result{}, errors.New("unexpected command")
		}
	}), AppPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	resource := caskResource("stablyai/orca/orca", "")
	resource.ID = "optional-desktop.orca"
	resource.Metadata = map[string]string{}
	if _, err := adapter.Inspect(context.Background(), resource); err != nil {
		t.Fatal(err)
	}
	changes, err := adapter.Simulate(context.Background(), model.Operation{Kind: model.OperationUpgrade, Provider: adapter.Name(), Package: resource.Package})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(changes.Upgrades, []string{resource.Package}) {
		t.Fatalf("changes = %#v", changes)
	}
	want := [][]string{
		{"info", "--json=v2", resource.Package},
		{"list", "--versions", resource.Package},
		{"info", "--json=v2", resource.Package},
		{"outdated", "--json=v2", resource.Package},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
}

func TestCLICaskInspectVerifiesProvidedCommandAtBrewPrefix(t *testing.T) {
	adapter, err := New(Cask, "/home/linuxbrew/.linuxbrew/bin/brew", t.TempDir(), runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		switch request.Args[0] {
		case "info":
			return caskInfo("codex", "1.0"), nil
		case "list":
			return execx.Result{Stdout: []byte("codex 1.0\n")}, nil
		default:
			return execx.Result{}, errors.New("unexpected command")
		}
	}), AppPolicy{FS: fakeFS{files: map[string]os.FileInfo{
		"/home/linuxbrew/.linuxbrew/bin/codex": fakeInfo{name: "codex", mode: 0o755},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	resource := caskResource("codex", "")
	resource.Metadata = map[string]string{}
	resource.Commands = []string{"codex"}

	observation, err := adapter.Inspect(context.Background(), resource)
	if err != nil {
		t.Fatal(err)
	}
	if !observation.Healthy || observation.Paths["codex"] != "/home/linuxbrew/.linuxbrew/bin/codex" {
		t.Fatalf("observation = %#v", observation)
	}
}

func TestInspectRejectsProvidedCommandSymlinkEscapingBrewPrefix(t *testing.T) {
	commandPath := "/opt/homebrew/bin/rg"
	escapedPath := "/tmp/attacker/rg"
	adapter, err := New(Formula, "/opt/homebrew/bin/brew", t.TempDir(), runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		if request.Args[0] == "info" {
			return execx.Result{Stdout: []byte(`{"formulae":[{"name":"ripgrep","full_name":"ripgrep","installed":[{"version":"14.1.1"}]}],"casks":[]}`)}, nil
		}
		return execx.Result{Stdout: []byte("ripgrep 14.1.1\n")}, nil
	}), AppPolicy{FS: fakeFS{
		files: map[string]os.FileInfo{
			commandPath: fakeInfo{name: "rg", mode: os.ModeSymlink | 0o777},
			escapedPath: fakeInfo{name: "rg", mode: 0o755},
		},
		resolved: map[string]string{commandPath: escapedPath},
	}})
	if err != nil {
		t.Fatal(err)
	}
	resource := formulaResource("ripgrep")
	resource.Commands = []string{"rg"}
	observation, err := adapter.Inspect(context.Background(), resource)
	if err != nil {
		t.Fatal(err)
	}
	if observation.Healthy || !strings.Contains(observation.Detail, "prefix") {
		t.Fatalf("observation = %#v", observation)
	}
}

func TestSimulateAndExecuteUseOnlyTargetedFormulaCommands(t *testing.T) {
	var calls []execx.Request
	adapter, err := New(Formula, "/opt/homebrew/bin/brew", t.TempDir(), runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		calls = append(calls, request)
		if reflect.DeepEqual(request.Args, []string{"outdated", "--json=v2", "ripgrep"}) {
			return execx.Result{Stdout: []byte(`{"formulae":[{"name":"ripgrep"}],"casks":[]}`)}, nil
		}
		return execx.Result{}, nil
	}), AppPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	resource := formulaResource("ripgrep")
	operations := []model.Operation{
		{Kind: model.OperationInstall, Provider: adapter.Name(), Package: "ripgrep"},
		{Kind: model.OperationUpgrade, Provider: adapter.Name(), Package: "ripgrep"},
		{Kind: model.OperationPrune, Provider: adapter.Name(), Package: "ripgrep"},
	}
	wantChanges := []provider.ChangeSet{
		{Installs: []string{"ripgrep"}},
		{Upgrades: []string{"ripgrep"}},
		{Removes: []string{"ripgrep"}},
	}
	for i, operation := range operations {
		changes, err := adapter.Simulate(context.Background(), operation)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(changes, wantChanges[i]) {
			t.Fatalf("changes[%d] = %#v", i, changes)
		}
		if err := provider.ValidateChangeSet(changes, resource, nil); err != nil {
			t.Fatal(err)
		}
		if err := adapter.Execute(context.Background(), operation); err != nil {
			t.Fatal(err)
		}
	}
	wantArgs := [][]string{
		{"install", "ripgrep"},
		{"outdated", "--json=v2", "ripgrep"}, {"upgrade", "ripgrep"},
		{"uninstall", "ripgrep"},
	}
	if got := requestArgs(calls); !reflect.DeepEqual(got, wantArgs) {
		t.Fatalf("args = %#v, want %#v", got, wantArgs)
	}
}

func TestHomebrewRejectsPrivilegedOperationsWithoutRunning(t *testing.T) {
	for _, kind := range []Kind{Formula, Cask} {
		for _, operationKind := range []model.OperationKind{model.OperationInstall, model.OperationAdopt, model.OperationUpgrade, model.OperationPrune} {
			t.Run(string(kind)+"/"+string(operationKind), func(t *testing.T) {
				calls := 0
				adapter, err := New(kind, "/opt/homebrew/bin/brew", t.TempDir(), runnerFunc(func(context.Context, execx.Request) (execx.Result, error) {
					calls++
					return execx.Result{}, nil
				}), AppPolicy{})
				if err != nil {
					t.Fatal(err)
				}
				operation := model.Operation{Kind: operationKind, Provider: adapter.Name(), Package: "ripgrep", RequiresPrivilege: true}
				if _, err := adapter.Simulate(context.Background(), operation); err == nil || !strings.Contains(err.Error(), "privilege") {
					t.Fatalf("Simulate error = %v", err)
				}
				if err := adapter.Execute(context.Background(), operation); err == nil || !strings.Contains(err.Error(), "privilege") {
					t.Fatalf("Execute error = %v", err)
				}
				if calls != 0 {
					t.Fatalf("runner called %d times", calls)
				}
			})
		}
	}
}

func TestSimulateRejectsUnknownOutdatedFields(t *testing.T) {
	adapter, err := New(Formula, "/opt/homebrew/bin/brew", t.TempDir(), runnerFunc(func(context.Context, execx.Request) (execx.Result, error) {
		return execx.Result{Stdout: []byte(`{"formulae":[{"name":"ripgrep","unknown":true}],"casks":[]}`)}, nil
	}), AppPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = adapter.Simulate(context.Background(), model.Operation{Kind: model.OperationUpgrade, Provider: adapter.Name(), Package: "ripgrep"})
	if err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("error = %v", err)
	}
}

func TestSimulateRejectsNullOutdatedEnvelopeArrays(t *testing.T) {
	adapter, err := New(Formula, "/opt/homebrew/bin/brew", t.TempDir(), runnerFunc(func(context.Context, execx.Request) (execx.Result, error) {
		return execx.Result{Stdout: []byte(`{"formulae":null,"casks":[]}`)}, nil
	}), AppPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = adapter.Simulate(context.Background(), model.Operation{Kind: model.OperationUpgrade, Provider: adapter.Name(), Package: "ripgrep"})
	if err == nil || !strings.Contains(err.Error(), "array") {
		t.Fatalf("error = %v", err)
	}
}

func TestCaskUsesTargetedFlagsWithoutZap(t *testing.T) {
	var calls []execx.Request
	adapter, err := New(Cask, "/opt/homebrew/bin/brew", t.TempDir(), runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		calls = append(calls, request)
		return execx.Result{}, nil
	}), AppPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	for _, operation := range []model.Operation{
		{Kind: model.OperationInstall, Provider: adapter.Name(), Package: "codex"},
		{Kind: model.OperationUpgrade, Provider: adapter.Name(), Package: "codex"},
		{Kind: model.OperationPrune, Provider: adapter.Name(), Package: "codex"},
	} {
		if operation.Kind == model.OperationUpgrade {
			// An empty outdated set makes Execute remain targeted but unnecessary.
			if err := adapter.Execute(context.Background(), operation); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := adapter.Execute(context.Background(), operation); err != nil {
			t.Fatal(err)
		}
	}
	want := [][]string{{"install", "--cask", "codex"}, {"upgrade", "--cask", "codex"}, {"uninstall", "--cask", "codex"}}
	if got := requestArgs(calls); !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
	for _, request := range calls {
		if strings.Contains(strings.Join(request.Args, " "), "--zap") {
			t.Fatalf("unsafe args = %v", request.Args)
		}
	}
}

func TestNonstandardBrewIsLegacyObservationAndNeverExecuted(t *testing.T) {
	calls := 0
	adapter, err := New(Formula, "/custom/bin/brew", t.TempDir(), runnerFunc(func(context.Context, execx.Request) (execx.Result, error) {
		calls++
		return execx.Result{}, nil
	}), AppPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	observation, err := adapter.Inspect(context.Background(), formulaResource("ripgrep"))
	if err != nil {
		t.Fatal(err)
	}
	if !observation.Present || observation.Healthy || !strings.Contains(observation.Detail, "nonstandard") {
		t.Fatalf("observation = %#v", observation)
	}
	if err := adapter.Execute(context.Background(), model.Operation{Kind: model.OperationInstall, Provider: adapter.Name(), Package: "ripgrep"}); err == nil {
		t.Fatal("Execute succeeded")
	}
	if calls != 0 {
		t.Fatalf("runner called %d times", calls)
	}
}

func TestCaskAdoptsIdenticalDeclaredAppWhenNotRunning(t *testing.T) {
	homeApps := filepath.Join(t.TempDir(), "Applications")
	artifact := filepath.Join(homeApps, "Vendor.app")
	if err := os.MkdirAll(artifact, 0o755); err != nil {
		t.Fatal(err)
	}
	artifact = canonicalPath(t, artifact)
	homeApps = filepath.Dir(artifact)
	var calls []execx.Request
	adapter, err := New(Cask, "/opt/homebrew/bin/brew", t.TempDir(), runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		calls = append(calls, request)
		if request.Args[0] == "info" {
			return caskInfo("vendor", "1.0"), nil
		}
		if request.Args[0] == "list" {
			return execx.Result{Stdout: []byte("vendor 1.0\n")}, nil
		}
		return execx.Result{}, nil
	}), AppPolicy{
		HomeApplications: homeApps,
		Inspector: inspectorFunc(func(context.Context, string) (AppIdentity, error) {
			return AppIdentity{BundleID: "com.example.vendor", SigningID: "Developer ID Application: Vendor"}, nil
		}),
		Running: runningFunc(func(context.Context, string) (bool, error) { return false, nil }),
	})
	if err != nil {
		t.Fatal(err)
	}
	resource := caskResource("vendor", artifact)
	if _, err := adapter.Inspect(context.Background(), resource); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Execute(context.Background(), model.Operation{ResourceID: resource.ID, Kind: model.OperationInstall, Provider: adapter.Name(), Package: "vendor"}); err != nil {
		t.Fatal(err)
	}
	if got := requestArgs(calls); !containsArgs(got, []string{"install", "--cask", "--adopt", "vendor"}) {
		t.Fatalf("args = %#v", got)
	}
}

func TestCaskReplacementRestoresOriginalOnVerificationFailure(t *testing.T) {
	homeApps := filepath.Join(t.TempDir(), "Applications")
	artifact := filepath.Join(homeApps, "Vendor.app")
	if err := os.MkdirAll(artifact, 0o755); err != nil {
		t.Fatal(err)
	}
	artifact = canonicalPath(t, artifact)
	homeApps = filepath.Dir(artifact)
	marker := filepath.Join(artifact, "original")
	if err := os.WriteFile(marker, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	recovery := t.TempDir()
	installCount := 0
	adapter, err := New(Cask, "/opt/homebrew/bin/brew", recovery, runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		switch strings.Join(request.Args, " ") {
		case "info --json=v2 vendor":
			return caskInfo("vendor", "1.0"), nil
		case "list --versions vendor":
			return execx.Result{Stdout: []byte("vendor 1.0\n")}, nil
		case "install --cask --adopt vendor":
			return execx.Result{}, errors.New("adopt unsupported")
		case "install --cask vendor":
			installCount++
			if err := os.MkdirAll(artifact, 0o755); err != nil {
				return execx.Result{}, err
			}
			return execx.Result{}, nil
		default:
			return execx.Result{}, errors.New("unexpected command: " + strings.Join(request.Args, " "))
		}
	}), AppPolicy{
		HomeApplications: homeApps,
		Inspector: inspectorFunc(func(_ context.Context, path string) (AppIdentity, error) {
			if _, err := os.Stat(filepath.Join(path, "original")); err == nil {
				return AppIdentity{BundleID: "com.example.vendor", SigningID: "Developer ID Application: Vendor"}, nil
			}
			return AppIdentity{BundleID: "com.attacker.vendor", SigningID: "Unknown"}, nil
		}),
		Running: runningFunc(func(context.Context, string) (bool, error) { return false, nil }),
	})
	if err != nil {
		t.Fatal(err)
	}
	resource := caskResource("vendor", artifact)
	if _, err := adapter.Inspect(context.Background(), resource); err != nil {
		t.Fatal(err)
	}
	err = adapter.Execute(context.Background(), model.Operation{ResourceID: resource.ID, Kind: model.OperationInstall, Provider: adapter.Name(), Package: "vendor"})
	if err == nil || !strings.Contains(err.Error(), "verify") {
		t.Fatalf("error = %v", err)
	}
	if installCount != 1 {
		t.Fatalf("install count = %d", installCount)
	}
	if contents, readErr := os.ReadFile(marker); readErr != nil || string(contents) != "original" {
		t.Fatalf("original was not restored: contents=%q error=%v", contents, readErr)
	}
}

func TestAbsentDeclaredAppInstallVerifiesIdentityBeforeSuccess(t *testing.T) {
	homeApps := canonicalDir(t, filepath.Join(t.TempDir(), "Applications"))
	artifact := filepath.Join(homeApps, "Vendor.app")
	installed := false
	adapter, err := New(Cask, "/opt/homebrew/bin/brew", t.TempDir(), runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		switch strings.Join(request.Args, " ") {
		case "info --json=v2 vendor":
			if installed {
				return caskInfo("vendor", "1.0"), nil
			}
			return execx.Result{Stdout: []byte(`{"formulae":[],"casks":[{"token":"vendor","full_token":"vendor","installed":null}]}`)}, nil
		case "list --versions vendor":
			if installed {
				return execx.Result{Stdout: []byte("vendor 1.0\n")}, nil
			}
			return execx.Result{}, errors.New("not installed")
		case "install --cask vendor":
			installed = true
			if err := os.MkdirAll(artifact, 0o755); err != nil {
				return execx.Result{}, err
			}
			return execx.Result{}, nil
		default:
			return execx.Result{}, errors.New("unexpected command")
		}
	}), AppPolicy{
		HomeApplications: homeApps,
		Inspector: inspectorFunc(func(context.Context, string) (AppIdentity, error) {
			return AppIdentity{BundleID: "wrong.bundle", SigningID: "Wrong Signer"}, nil
		}),
		Running: runningFunc(func(context.Context, string) (bool, error) { return false, nil }),
	})
	if err != nil {
		t.Fatal(err)
	}
	resource := caskResource("vendor", artifact)
	if _, err := adapter.Inspect(context.Background(), resource); err != nil {
		t.Fatal(err)
	}
	err = adapter.Execute(context.Background(), model.Operation{ResourceID: resource.ID, Kind: model.OperationInstall, Provider: adapter.Name(), Package: "vendor"})
	if err == nil || !strings.Contains(err.Error(), "verify") || !strings.Contains(err.Error(), "vendor") {
		t.Fatalf("error = %v", err)
	}
}

func TestRollbackIgnoresPreexistingLegacyFailedArtifactAndRestoresOriginal(t *testing.T) {
	homeApps := canonicalDir(t, filepath.Join(t.TempDir(), "Applications"))
	artifact := filepath.Join(homeApps, "Vendor.app")
	if err := os.MkdirAll(artifact, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(artifact, "original")
	if err := os.WriteFile(marker, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	recovery := canonicalDir(t, t.TempDir())
	legacyFailed := filepath.Join(recovery, "optional-ai.vendor", "Vendor.app.failed")
	if err := os.MkdirAll(legacyFailed, 0o755); err != nil {
		t.Fatal(err)
	}
	adapter := replacementFailureAdapter(t, homeApps, artifact, recovery, nil, nil, nil)
	resource := caskResource("vendor", artifact)
	if _, err := adapter.Inspect(context.Background(), resource); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Execute(context.Background(), model.Operation{ResourceID: resource.ID, Kind: model.OperationInstall, Provider: adapter.Name(), Package: "vendor"}); err == nil {
		t.Fatal("Execute succeeded")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("original not restored: %v", err)
	}
	if _, err := os.Stat(legacyFailed); err != nil {
		t.Fatalf("legacy failed artifact changed: %v", err)
	}
}

func TestRollbackAttemptsOriginalRestoreAfterFailedReplacementMove(t *testing.T) {
	homeApps := canonicalDir(t, filepath.Join(t.TempDir(), "Applications"))
	artifact := filepath.Join(homeApps, "Vendor.app")
	if err := os.MkdirAll(artifact, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(artifact, "original")
	if err := os.WriteFile(marker, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	recovery := canonicalDir(t, t.TempDir())
	fs := &failReplacementMoveFS{artifact: artifact, stageFailures: 1}
	adapter := replacementFailureAdapter(t, homeApps, artifact, recovery, fs, nil, nil)
	resource := caskResource("vendor", artifact)
	if _, err := adapter.Inspect(context.Background(), resource); err != nil {
		t.Fatal(err)
	}
	err := adapter.Execute(context.Background(), model.Operation{ResourceID: resource.ID, Kind: model.OperationInstall, Provider: adapter.Name(), Package: "vendor"})
	if err == nil || !strings.Contains(err.Error(), "verify replacement") {
		t.Fatalf("error = %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("original not restored after move failure: %v", err)
	}
	if len(fs.stageDestinations) != 2 || fs.stageDestinations[0] == fs.stageDestinations[1] {
		t.Fatalf("stage destinations = %#v, want two distinct retries", fs.stageDestinations)
	}
}

func TestRollbackRandomFailureLeavesOriginalUntouchedAndSkipsReplacementInstall(t *testing.T) {
	homeApps := canonicalDir(t, filepath.Join(t.TempDir(), "Applications"))
	artifact := filepath.Join(homeApps, "Vendor.app")
	if err := os.MkdirAll(artifact, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(artifact, "original")
	if err := os.WriteFile(marker, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	installs := 0
	adapter := replacementFailureAdapter(t, homeApps, artifact, canonicalDir(t, t.TempDir()), nil, errorReader{}, &installs)
	resource := caskResource("vendor", artifact)
	if _, err := adapter.Inspect(context.Background(), resource); err != nil {
		t.Fatal(err)
	}
	err := adapter.Execute(context.Background(), model.Operation{ResourceID: resource.ID, Kind: model.OperationInstall, Provider: adapter.Name(), Package: "vendor"})
	if err == nil || !strings.Contains(err.Error(), "random unavailable") {
		t.Fatalf("error = %v", err)
	}
	if installs != 0 {
		t.Fatalf("replacement installs = %d, want 0", installs)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("original changed before transaction allocation: %v", err)
	}
}

func TestRollbackPersistentMoveErrorAttemptsRestoreAndReportsBoth(t *testing.T) {
	homeApps := canonicalDir(t, filepath.Join(t.TempDir(), "Applications"))
	artifact := filepath.Join(homeApps, "Vendor.app")
	if err := os.MkdirAll(artifact, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(artifact, "original"), []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	fs := &failReplacementMoveFS{artifact: artifact, persistent: true}
	adapter := replacementFailureAdapter(t, homeApps, artifact, canonicalDir(t, t.TempDir()), fs, nil, nil)
	resource := caskResource("vendor", artifact)
	if _, err := adapter.Inspect(context.Background(), resource); err != nil {
		t.Fatal(err)
	}
	err := adapter.Execute(context.Background(), model.Operation{ResourceID: resource.ID, Kind: model.OperationInstall, Provider: adapter.Name(), Package: "vendor"})
	if err == nil || !strings.Contains(err.Error(), "stage failed replacement") || !strings.Contains(err.Error(), "restore original app") {
		t.Fatalf("error = %v", err)
	}
	if len(fs.stageDestinations) < 2 || fs.restoreAttempts != 1 {
		t.Fatalf("stage attempts = %d, restore attempts = %d", len(fs.stageDestinations), fs.restoreAttempts)
	}
}

func TestCaskAdoptionRejectsRunningOrUnsafeArtifact(t *testing.T) {
	homeApps := filepath.Join(t.TempDir(), "Applications")
	artifact := filepath.Join(homeApps, "Vendor.app")
	if err := os.MkdirAll(artifact, 0o755); err != nil {
		t.Fatal(err)
	}
	artifact = canonicalPath(t, artifact)
	homeApps = filepath.Dir(artifact)
	adapter, err := New(Cask, "/opt/homebrew/bin/brew", t.TempDir(), runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		if request.Args[0] == "info" {
			return caskInfo("vendor", "1.0"), nil
		}
		return execx.Result{Stdout: []byte("vendor 1.0\n")}, nil
	}), AppPolicy{
		HomeApplications: homeApps,
		Inspector: inspectorFunc(func(context.Context, string) (AppIdentity, error) {
			return AppIdentity{BundleID: "com.example.vendor", SigningID: "Developer ID Application: Vendor"}, nil
		}),
		Running: runningFunc(func(context.Context, string) (bool, error) { return true, nil }),
	})
	if err != nil {
		t.Fatal(err)
	}
	resource := caskResource("vendor", artifact)
	if _, err := adapter.Inspect(context.Background(), resource); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Execute(context.Background(), model.Operation{ResourceID: resource.ID, Kind: model.OperationInstall, Provider: adapter.Name(), Package: "vendor"}); err == nil || !strings.Contains(err.Error(), "running") {
		t.Fatalf("error = %v", err)
	}
	resource.Metadata["artifactPath"] = filepath.Join(homeApps, "..", "Elsewhere.app")
	if _, err := adapter.Inspect(context.Background(), resource); err == nil {
		t.Fatal("unsafe artifact accepted")
	}
}

type inspectorFunc func(context.Context, string) (AppIdentity, error)

func (f inspectorFunc) Inspect(ctx context.Context, path string) (AppIdentity, error) {
	return f(ctx, path)
}

type runningFunc func(context.Context, string) (bool, error)

func (f runningFunc) IsRunning(ctx context.Context, bundleID string) (bool, error) {
	return f(ctx, bundleID)
}

type fakeFS struct {
	files    map[string]os.FileInfo
	resolved map[string]string
}

func (f fakeFS) Lstat(path string) (os.FileInfo, error) {
	info, ok := f.files[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return info, nil
}
func (f fakeFS) EvalSymlinks(path string) (string, error) {
	if resolved, ok := f.resolved[path]; ok {
		return resolved, nil
	}
	return path, nil
}
func (fakeFS) Mkdir(string, os.FileMode) error    { return nil }
func (fakeFS) MkdirAll(string, os.FileMode) error { return nil }
func (fakeFS) Rename(string, string) error        { return nil }

type fakeInfo struct {
	name string
	mode os.FileMode
}

func (f fakeInfo) Name() string       { return f.name }
func (f fakeInfo) Size() int64        { return 0 }
func (f fakeInfo) Mode() os.FileMode  { return f.mode }
func (f fakeInfo) ModTime() time.Time { return time.Time{} }
func (f fakeInfo) IsDir() bool        { return f.mode.IsDir() }
func (f fakeInfo) Sys() any           { return nil }

func noCommand(context.Context, execx.Request) (execx.Result, error) {
	return execx.Result{}, errors.New("unexpected command")
}

func formulaResource(token string) model.Resource {
	return model.Resource{ID: model.ResourceID("core." + token), Type: model.ResourcePackage, Provider: "homebrew-formula", Package: token, VersionPolicy: model.VersionTracked, Metadata: map[string]string{}}
}

func caskResource(token, artifact string) model.Resource {
	return model.Resource{
		ID:            model.ResourceID("optional-ai." + token),
		Type:          model.ResourcePackage,
		Provider:      "homebrew-cask",
		Package:       token,
		VersionPolicy: model.VersionTracked,
		Metadata: map[string]string{
			"artifactPath": artifact,
			"bundleID":     "com.example.vendor",
			"signingID":    "Developer ID Application: Vendor",
		},
	}
}

func caskInfo(token, version string) execx.Result {
	return execx.Result{Stdout: []byte(`{"formulae":[],"casks":[{"token":"` + token + `","full_token":"` + token + `","installed":"` + version + `"}]}`)}
}

func requestArgs(requests []execx.Request) [][]string {
	result := make([][]string, len(requests))
	for i := range requests {
		result[i] = requests[i].Args
	}
	return result
}

func containsArgs(values [][]string, want []string) bool {
	for _, value := range values {
		if reflect.DeepEqual(value, want) {
			return true
		}
	}
	return false
}

func canonicalPath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func canonicalDir(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	return canonicalPath(t, path)
}

func replacementFailureAdapter(t *testing.T, homeApps, artifact, recovery string, fs FileSystem, random io.Reader, replacementInstalls *int) *Adapter {
	t.Helper()
	adapter, err := New(Cask, "/opt/homebrew/bin/brew", recovery, runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		switch strings.Join(request.Args, " ") {
		case "info --json=v2 vendor":
			return caskInfo("vendor", "1.0"), nil
		case "list --versions vendor":
			return execx.Result{Stdout: []byte("vendor 1.0\n")}, nil
		case "install --cask --adopt vendor":
			return execx.Result{}, errors.New("adopt unsupported")
		case "install --cask vendor":
			if replacementInstalls != nil {
				*replacementInstalls++
			}
			if err := os.MkdirAll(artifact, 0o755); err != nil {
				return execx.Result{}, err
			}
			return execx.Result{}, nil
		default:
			return execx.Result{}, errors.New("unexpected command")
		}
	}), AppPolicy{
		HomeApplications: homeApps,
		FS:               fs,
		Random:           random,
		Inspector: inspectorFunc(func(_ context.Context, path string) (AppIdentity, error) {
			if _, err := os.Stat(filepath.Join(path, "original")); err == nil {
				return AppIdentity{BundleID: "com.example.vendor", SigningID: "Developer ID Application: Vendor"}, nil
			}
			return AppIdentity{BundleID: "wrong.bundle", SigningID: "Wrong Signer"}, nil
		}),
		Running: runningFunc(func(context.Context, string) (bool, error) { return false, nil }),
	})
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}

type failReplacementMoveFS struct {
	artifact          string
	stageFailures     int
	persistent        bool
	stageDestinations []string
	restoreAttempts   int
}

func (f *failReplacementMoveFS) Lstat(path string) (os.FileInfo, error) { return os.Lstat(path) }
func (f *failReplacementMoveFS) EvalSymlinks(path string) (string, error) {
	return filepath.EvalSymlinks(path)
}
func (f *failReplacementMoveFS) MkdirAll(path string, mode os.FileMode) error {
	return os.MkdirAll(path, mode)
}
func (f *failReplacementMoveFS) Mkdir(path string, mode os.FileMode) error {
	return os.Mkdir(path, mode)
}
func (f *failReplacementMoveFS) Rename(oldPath, newPath string) error {
	if oldPath == f.artifact {
		if _, err := os.Stat(filepath.Join(oldPath, "original")); errors.Is(err, os.ErrNotExist) {
			f.stageDestinations = append(f.stageDestinations, newPath)
			if f.persistent || f.stageFailures > 0 {
				if f.stageFailures > 0 {
					f.stageFailures--
				}
				return errors.New("injected replacement move failure")
			}
		}
	}
	if strings.Contains(oldPath, string(filepath.Separator)+"backup"+string(filepath.Separator)) {
		f.restoreAttempts++
	}
	return os.Rename(oldPath, newPath)
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("random unavailable") }

type pointerRunner struct{}

func (*pointerRunner) Run(context.Context, execx.Request) (execx.Result, error) {
	return execx.Result{}, nil
}

type pointerFS struct{}

func (*pointerFS) Lstat(string) (os.FileInfo, error)        { return nil, os.ErrNotExist }
func (*pointerFS) EvalSymlinks(path string) (string, error) { return path, nil }
func (*pointerFS) Mkdir(string, os.FileMode) error          { return nil }
func (*pointerFS) MkdirAll(string, os.FileMode) error       { return nil }
func (*pointerFS) Rename(string, string) error              { return nil }

type pointerInspector struct{}

func (*pointerInspector) Inspect(context.Context, string) (AppIdentity, error) {
	return AppIdentity{}, nil
}

type pointerRunning struct{}

func (*pointerRunning) IsRunning(context.Context, string) (bool, error) { return false, nil }

var _ provider.Provider = (*Adapter)(nil)
