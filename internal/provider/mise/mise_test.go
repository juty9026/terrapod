package mise

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/juty9026/terrapod/internal/execx"
	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/provider"
)

type runnerFunc func(context.Context, execx.Request) (execx.Result, error)

func (f runnerFunc) Run(ctx context.Context, request execx.Request) (execx.Result, error) {
	return f(ctx, request)
}

func TestRefreshMetadataClearsMiseCache(t *testing.T) {
	called := false
	adapter, err := New("/opt/homebrew/bin/mise", t.TempDir(), runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		called = true
		if request.Path != "/opt/homebrew/bin/mise" || !reflect.DeepEqual(request.Args, []string{"cache", "clear"}) {
			t.Fatalf("request=%#v", request)
		}
		return execx.Result{}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.RefreshMetadata(context.Background()); err != nil || !called {
		t.Fatalf("RefreshMetadata called=%v err=%v", called, err)
	}
}

func TestConstructorRequiresCleanTrustedDataRootAndRunner(t *testing.T) {
	var nilRunner *pointerRunner
	var nilFS *pointerFS
	for _, tc := range []struct {
		name, path, root string
		runner           Runner
	}{
		{name: "relative path", path: "mise", root: "/home/me/.local/share/mise", runner: runnerFunc(noCommand)},
		{name: "relative root", path: "/opt/homebrew/bin/mise", root: "mise", runner: runnerFunc(noCommand)},
		{name: "typed nil", path: "/opt/homebrew/bin/mise", root: "/home/me/.local/share/mise", runner: nilRunner},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := New(tc.path, tc.root, tc.runner); err == nil {
				t.Fatal("New succeeded")
			}
		})
	}
	if _, err := New("/opt/homebrew/bin/mise", "/home/me/.local/share/mise", runnerFunc(noCommand), nilFS); err == nil || !strings.Contains(err.Error(), "nil") {
		t.Fatalf("typed nil filesystem error=%v", err)
	}
}

func TestNonstandardMiseIsLegacyAndNeverExecutesMutation(t *testing.T) {
	called := false
	a, err := New("/custom/bin/mise", "/home/me/.local/share/mise", runnerFunc(func(context.Context, execx.Request) (execx.Result, error) { called = true; return execx.Result{}, nil }))
	if err != nil {
		t.Fatal(err)
	}
	got, err := a.Inspect(context.Background(), miseResource("node", "24", "node"))
	if err != nil || got.Healthy || !got.Present || !strings.Contains(got.Detail, "legacy") {
		t.Fatalf("observation=%#v error=%v", got, err)
	}
	if err := a.Execute(context.Background(), miseOperation(model.OperationInstall, "node")); err == nil || !strings.Contains(err.Error(), "nonstandard") {
		t.Fatalf("Execute error=%v", err)
	}
	if called {
		t.Fatal("runner called")
	}
}

func TestInspectParsesExactTargetInventoryAndSelectorReadiness(t *testing.T) {
	for _, tc := range []struct{ name, selector, version string }{
		{name: "latest accepts installed", selector: "latest", version: "1.2.3"},
		{name: "numeric prefix", selector: "24", version: "24.7.0"},
		{name: "minor prefix", selector: "3.13", version: "3.13.5"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newAdapter(t, runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
				want := execx.Request{Path: "/opt/homebrew/bin/mise", Args: []string{"ls", "--json", "node"}}
				if !reflect.DeepEqual(request, want) {
					t.Fatalf("request=%#v want=%#v", request, want)
				}
				return execx.Result{Stdout: []byte(`{"node":[{"version":"` + tc.version + `","installed":true}]}`)}, nil
			}))
			got, err := a.Inspect(context.Background(), miseResource("node", tc.selector, "node"))
			if err != nil || !got.Present || !got.Healthy || got.Version != tc.version {
				t.Fatalf("observation=%#v error=%v", got, err)
			}
		})
	}
}

func TestNormalApplyDoesNotUpgradeMatchingOrLatestInventory(t *testing.T) {
	for _, selector := range []string{"latest", "24"} {
		a := newAdapter(t, inventoryRunner("node", "24.7.0"))
		if _, err := a.Inspect(context.Background(), miseResource("node", selector, "node")); err != nil {
			t.Fatal(err)
		}
		changes, err := a.Simulate(context.Background(), miseOperation(model.OperationInstall, "node"))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(changes, provider.ChangeSet{}) {
			t.Fatalf("changes=%#v", changes)
		}
	}
}

func TestPinnedInstallAndExplicitUpgradeUseExactSelector(t *testing.T) {
	for _, kind := range []model.OperationKind{model.OperationInstall, model.OperationUpgrade} {
		var calls []execx.Request
		a := newAdapter(t, runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
			calls = append(calls, request)
			if request.Args[0] == "ls" {
				return execx.Result{Stdout: []byte(`{"node":[]}`)}, nil
			}
			return execx.Result{}, nil
		}))
		if _, err := a.Inspect(context.Background(), miseResource("node", "24", "node")); err != nil {
			t.Fatal(err)
		}
		if err := a.Execute(context.Background(), miseOperation(kind, "node")); err != nil {
			t.Fatal(err)
		}
		want := execx.Request{Path: "/opt/homebrew/bin/mise", Args: []string{"install", "--yes", "node@24"}}
		if !reflect.DeepEqual(calls[len(calls)-1], want) {
			t.Fatalf("last call=%#v want=%#v", calls[len(calls)-1], want)
		}
	}
}

func TestPruneUsesOnlyInventoriedExactVersionAndRejectsPrivilege(t *testing.T) {
	var calls []execx.Request
	a := newAdapter(t, runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		calls = append(calls, request)
		if request.Args[0] == "ls" {
			return execx.Result{Stdout: []byte(`{"python":[{"version":"3.13.5","installed":true}]}`)}, nil
		}
		return execx.Result{}, nil
	}))
	if _, err := a.Inspect(context.Background(), miseResource("python", "3.13", "python")); err != nil {
		t.Fatal(err)
	}
	if err := a.Execute(context.Background(), miseOperation(model.OperationPrune, "python")); err != nil {
		t.Fatal(err)
	}
	want := execx.Request{Path: "/opt/homebrew/bin/mise", Args: []string{"uninstall", "--yes", "python@3.13.5"}}
	if !reflect.DeepEqual(calls[len(calls)-1], want) {
		t.Fatalf("last call=%#v want=%#v", calls[len(calls)-1], want)
	}
	if got := countFirstArg(calls, "ls"); got != 2 {
		t.Fatalf("ls calls=%d, want fresh second inventory", got)
	}
	op := miseOperation(model.OperationInstall, "python")
	op.RequiresPrivilege = true
	if err := a.Execute(context.Background(), op); err == nil || !strings.Contains(err.Error(), "privilege") {
		t.Fatalf("Execute error=%v", err)
	}
}

func TestPruneUsesFreshChangedExactVersion(t *testing.T) {
	lsCalls := 0
	var uninstall execx.Request
	a := newAdapter(t, runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		if request.Args[0] == "ls" {
			lsCalls++
			version := "3.13.5"
			if lsCalls == 2 {
				version = "3.13.6"
			}
			return execx.Result{Stdout: []byte(`{"python":[{"version":"` + version + `","installed":true}]}`)}, nil
		}
		uninstall = request
		return execx.Result{}, nil
	}))
	if _, err := a.Inspect(context.Background(), miseResource("python", "3.13", "python")); err != nil {
		t.Fatal(err)
	}
	if err := a.Execute(context.Background(), miseOperation(model.OperationPrune, "python")); err != nil {
		t.Fatal(err)
	}
	want := execx.Request{Path: "/opt/homebrew/bin/mise", Args: []string{"uninstall", "--yes", "python@3.13.6"}}
	if !reflect.DeepEqual(uninstall, want) {
		t.Fatalf("uninstall=%#v want=%#v", uninstall, want)
	}
}

func TestPruneFreshlyDetectsRuntimeThatAppearedAfterInspect(t *testing.T) {
	lsCalls := 0
	var uninstall execx.Request
	a := newAdapter(t, runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		if request.Args[0] == "ls" {
			lsCalls++
			if lsCalls == 1 {
				return execx.Result{Stdout: []byte(`{"python":[]}`)}, nil
			}
			return execx.Result{Stdout: []byte(`{"python":[{"version":"3.13.7","installed":true}]}`)}, nil
		}
		uninstall = request
		return execx.Result{}, nil
	}))
	if _, err := a.Inspect(context.Background(), miseResource("python", "3.13", "python")); err != nil {
		t.Fatal(err)
	}
	if err := a.Execute(context.Background(), miseOperation(model.OperationPrune, "python")); err != nil {
		t.Fatal(err)
	}
	want := execx.Request{Path: "/opt/homebrew/bin/mise", Args: []string{"uninstall", "--yes", "python@3.13.7"}}
	if !reflect.DeepEqual(uninstall, want) {
		t.Fatalf("uninstall=%#v want=%#v", uninstall, want)
	}
}

func TestNumericSelectorRejectsPrereleaseAndMalformedVersions(t *testing.T) {
	for _, version := range []string{"24.1.0-rc.1", "24.1.0+build", "24..1", "24.x.1", ".24", "24."} {
		t.Run(version, func(t *testing.T) {
			a := newAdapter(t, inventoryRunner("node", version))
			if _, err := a.Inspect(context.Background(), miseResource("node", "24", "node")); err == nil || !strings.Contains(err.Error(), "version") {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestVerifyUsesWhereAndCompiledCommandsWithinTrustedRoot(t *testing.T) {
	root := t.TempDir()
	installPath := filepath.Join(root, "installs", "uv", "0.8.0")
	if err := os.MkdirAll(installPath, 0o700); err != nil {
		t.Fatal(err)
	}
	var calls []execx.Request
	a, err := New("/opt/homebrew/bin/mise", root, runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		calls = append(calls, request)
		switch request.Args[0] {
		case "ls":
			return execx.Result{Stdout: []byte(`{"uv":[{"version":"0.8.0","installed":true}]}`)}, nil
		case "where":
			return execx.Result{Stdout: []byte(installPath + "\n")}, nil
		case "exec":
			return execx.Result{Stdout: []byte("uv 0.8.0\n")}, nil
		}
		return execx.Result{}, errors.New("unexpected command")
	}))
	if err != nil {
		t.Fatal(err)
	}
	got, err := a.Verify(context.Background(), miseResource("uv", "latest", "uv", "uvx"))
	if err != nil || !got.Healthy {
		t.Fatalf("observation=%#v error=%v", got, err)
	}
	wantTail := []execx.Request{
		{Path: "/opt/homebrew/bin/mise", Args: []string{"where", "uv@0.8.0"}},
		{Path: "/opt/homebrew/bin/mise", Args: []string{"exec", "--yes", "uv@0.8.0", "--", "uv", "--version"}},
		{Path: "/opt/homebrew/bin/mise", Args: []string{"exec", "--yes", "uv@0.8.0", "--", "uvx", "--version"}},
	}
	if !reflect.DeepEqual(calls[1:], wantTail) {
		t.Fatalf("verification calls=%#v want=%#v", calls[1:], wantTail)
	}
}

func TestVerifyRejectsWhereOutsideTrustedRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	a, err := New("/opt/homebrew/bin/mise", root, runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		if request.Args[0] == "ls" {
			return execx.Result{Stdout: []byte(`{"bun":[{"version":"1.2.3","installed":true}]}`)}, nil
		}
		if request.Args[0] == "where" {
			return execx.Result{Stdout: []byte(outside + "\n")}, nil
		}
		return execx.Result{}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Verify(context.Background(), miseResource("bun", "latest", "bun")); err == nil || !strings.Contains(err.Error(), "trusted mise data root") {
		t.Fatalf("Verify error=%v", err)
	}
}

func TestVerifyRejectsInternalSymlinkResolvingOutside(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	link := filepath.Join(root, "installs", "bun", "1.2.3")
	if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	a, err := New("/opt/homebrew/bin/mise", root, runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		if request.Args[0] == "ls" {
			return execx.Result{Stdout: []byte(`{"bun":[{"version":"1.2.3","installed":true}]}`)}, nil
		}
		if request.Args[0] == "where" {
			return execx.Result{Stdout: []byte(link + "\n")}, nil
		}
		return execx.Result{}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Verify(context.Background(), miseResource("bun", "latest", "bun")); err == nil || !strings.Contains(err.Error(), "trusted mise data root") {
		t.Fatalf("error=%v", err)
	}
}

func TestExecutableFixtureProvesArgvAndExitSemantics(t *testing.T) {
	path, err := filepath.Abs(filepath.Join("testdata", "mise"))
	if err != nil {
		t.Fatal(err)
	}
	runner := execx.NewRunner(nil, nil, func() int { return 501 })
	result, err := runner.Run(context.Background(), execx.Request{Path: path, Args: []string{"ls", "--json", "node"}})
	if err != nil || !strings.Contains(string(result.Stdout), `"version":"24.7.0"`) {
		t.Fatalf("result=%q error=%v", result.Stdout, err)
	}
	if _, err := runner.Run(context.Background(), execx.Request{Path: path, Args: []string{"upgrade"}}); err == nil {
		t.Fatal("fixture accepted wrong argv")
	}
}

func miseResource(tool, version string, commands ...string) model.Resource {
	return model.Resource{ID: model.ResourceID("runtime." + tool), Type: model.ResourcePackage, VersionPolicy: model.VersionPinned, Provider: "mise", Package: tool, Commands: commands, Metadata: map[string]string{"version": version}}
}
func miseOperation(kind model.OperationKind, tool string) model.Operation {
	return model.Operation{Kind: kind, Provider: "mise", Package: tool}
}
func newAdapter(t *testing.T, runner Runner) *Adapter {
	t.Helper()
	a, err := New("/opt/homebrew/bin/mise", "/home/me/.local/share/mise", runner)
	if err != nil {
		t.Fatal(err)
	}
	return a
}
func inventoryRunner(tool, version string) Runner {
	return runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		if request.Args[0] != "ls" {
			return execx.Result{}, errors.New("unexpected mutation")
		}
		return execx.Result{Stdout: []byte(`{"` + tool + `":[{"version":"` + version + `","installed":true}]}`)}, nil
	})
}
func noCommand(context.Context, execx.Request) (execx.Result, error) {
	return execx.Result{}, errors.New("unexpected command")
}

type pointerRunner struct{}

func (*pointerRunner) Run(context.Context, execx.Request) (execx.Result, error) {
	return execx.Result{}, nil
}

type pointerFS struct{}

func (*pointerFS) EvalSymlinks(string) (string, error) { return "", nil }
func (*pointerFS) Stat(string) (os.FileInfo, error)    { return nil, nil }
func countFirstArg(calls []execx.Request, arg string) int {
	count := 0
	for _, call := range calls {
		if len(call.Args) > 0 && call.Args[0] == arg {
			count++
		}
	}
	return count
}
