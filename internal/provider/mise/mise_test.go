package mise

import (
	"context"
	"errors"
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

func TestConstructorRequiresCleanTrustedDataRootAndRunner(t *testing.T) {
	var nilRunner *pointerRunner
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
	op := miseOperation(model.OperationInstall, "python")
	op.RequiresPrivilege = true
	if err := a.Execute(context.Background(), op); err == nil || !strings.Contains(err.Error(), "privilege") {
		t.Fatalf("Execute error=%v", err)
	}
}

func TestVerifyUsesWhereAndCompiledCommandsWithinTrustedRoot(t *testing.T) {
	var calls []execx.Request
	a := newAdapter(t, runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		calls = append(calls, request)
		switch request.Args[0] {
		case "ls":
			return execx.Result{Stdout: []byte(`{"uv":[{"version":"0.8.0","installed":true}]}`)}, nil
		case "where":
			return execx.Result{Stdout: []byte("/home/me/.local/share/mise/installs/uv/0.8.0\n")}, nil
		case "exec":
			return execx.Result{Stdout: []byte("uv 0.8.0\n")}, nil
		}
		return execx.Result{}, errors.New("unexpected command")
	}))
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
	a := newAdapter(t, runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		if request.Args[0] == "ls" {
			return execx.Result{Stdout: []byte(`{"bun":[{"version":"1.2.3","installed":true}]}`)}, nil
		}
		if request.Args[0] == "where" {
			return execx.Result{Stdout: []byte("/tmp/evil\n")}, nil
		}
		return execx.Result{}, nil
	}))
	if _, err := a.Verify(context.Background(), miseResource("bun", "latest", "bun")); err == nil || !strings.Contains(err.Error(), "trusted mise data root") {
		t.Fatalf("Verify error=%v", err)
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
