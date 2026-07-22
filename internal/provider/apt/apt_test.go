package apt

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/juty9026/terrapod/internal/execx"
	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/provider"
)

type runnerFunc func(context.Context, execx.Request) (execx.Result, error)

func (f runnerFunc) Run(ctx context.Context, request execx.Request) (execx.Result, error) {
	return f(ctx, request)
}

func TestConstructorRequiresFixedExecutablesAndRunner(t *testing.T) {
	var nilRunner *pointerRunner
	for _, tc := range []struct {
		name, aptGet, dpkgQuery string
		runner                  Runner
	}{
		{name: "apt-get", aptGet: "/tmp/apt-get", dpkgQuery: DpkgQueryPath, runner: runnerFunc(noCommand)},
		{name: "dpkg-query", aptGet: AptGetPath, dpkgQuery: "/tmp/dpkg-query", runner: runnerFunc(noCommand)},
		{name: "typed nil", aptGet: AptGetPath, dpkgQuery: DpkgQueryPath, runner: nilRunner},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := New(tc.aptGet, tc.dpkgQuery, tc.runner); err == nil {
				t.Fatal("New succeeded")
			}
		})
	}
}

func TestInspectUsesExactDpkgQueryAndRejectsEssentialPackage(t *testing.T) {
	var calls []execx.Request
	a := newAdapter(t, runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		calls = append(calls, request)
		return execx.Result{Stdout: []byte("zsh\tii \t5.9-6ubuntu2\tyes\n")}, nil
	}))
	got, err := a.Inspect(context.Background(), aptResource("zsh"))
	if err != nil {
		t.Fatal(err)
	}
	if !got.Present || got.Healthy || got.Version != "5.9-6ubuntu2" || !strings.Contains(got.Detail, "Essential") {
		t.Fatalf("observation = %#v", got)
	}
	want := execx.Request{Path: DpkgQueryPath, Args: []string{"--show", "--showformat=${binary:Package}\\t${db:Status-Abbrev}\\t${Version}\\t${Essential}\\n", "zsh"}}
	if !reflect.DeepEqual(calls, []execx.Request{want}) {
		t.Fatalf("calls = %#v, want %#v", calls, []execx.Request{want})
	}
	op := aptOperation(model.OperationPrune, "zsh")
	if _, err := a.Simulate(context.Background(), op); err == nil || !strings.Contains(err.Error(), "Essential") {
		t.Fatalf("Simulate error = %v", err)
	}
}

func TestInspectTreatsOnlyEmptyNotFoundAsMissingAndStrictlyParsesOneRecord(t *testing.T) {
	a := newAdapter(t, runnerFunc(func(_ context.Context, _ execx.Request) (execx.Result, error) {
		return execx.Result{}, errors.New("exit status 1")
	}))
	got, err := a.Inspect(context.Background(), aptResource("curl"))
	if err != nil || got.Present {
		t.Fatalf("observation = %#v, error = %v", got, err)
	}

	a = newAdapter(t, runnerFunc(func(_ context.Context, _ execx.Request) (execx.Result, error) {
		return execx.Result{Stdout: []byte("curl\tii \t1\tno\nlibcurl\tii \t2\tno\n")}, nil
	}))
	if _, err := a.Inspect(context.Background(), aptResource("curl")); err == nil || !strings.Contains(err.Error(), "one record") {
		t.Fatalf("Inspect error = %v", err)
	}
}

func TestSimulateParsesDependencyInstallsAndUpgrades(t *testing.T) {
	a := newAdapter(t, runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		want := []string{"-s", "install", "--", "python3-dev"}
		if !reflect.DeepEqual(request.Args, want) || !request.Privilege {
			t.Fatalf("request = %#v, want args %#v privileged", request, want)
		}
		return execx.Result{Stdout: []byte("Inst libpython3.12-dev (3.12.3 Ubuntu:24.04 [amd64])\nInst libssl3t64 [3.0.13] (3.0.14 Ubuntu:24.04 [amd64])\nInst python3-dev (3.12 Ubuntu:24.04 [amd64])\nConf python3-dev (3.12 Ubuntu:24.04 [amd64])\n")}, nil
	}))
	changes, err := a.Simulate(context.Background(), aptOperation(model.OperationInstall, "python3-dev"))
	if err != nil {
		t.Fatal(err)
	}
	want := provider.ChangeSet{Installs: []string{"libpython3.12-dev", "python3-dev"}, Upgrades: []string{"libssl3t64"}}
	if !reflect.DeepEqual(changes, want) {
		t.Fatalf("changes = %#v, want %#v", changes, want)
	}
}

func TestSimulateUpgradeIsTargetedAndRejectsUnknownOrUnmanagedRemoval(t *testing.T) {
	for _, tc := range []struct {
		name, output, want string
	}{
		{name: "unmanaged removal", output: "Inst curl [1] (2 repo [amd64])\nRemv wget [1]\n", want: "unmanaged removals"},
		{name: "unknown mutation", output: "Purg curl [1]\n", want: "unknown plan mutation"},
		{name: "malformed install", output: "Inst curl\n", want: "malformed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newAdapter(t, runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
				if request.Path == DpkgQueryPath {
					return execx.Result{Stdout: []byte("curl\tii \t1\tno\n")}, nil
				}
				want := []string{"-s", "install", "--only-upgrade", "--", "curl"}
				if !reflect.DeepEqual(request.Args, want) {
					t.Fatalf("args = %#v, want %#v", request.Args, want)
				}
				return execx.Result{Stdout: []byte(tc.output)}, nil
			}))
			if _, err := a.Simulate(context.Background(), aptOperation(model.OperationUpgrade, "curl")); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Simulate error = %v", err)
			}
		})
	}
}

func TestNormalInstallRejectsTargetUpgrade(t *testing.T) {
	a := newAdapter(t, runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		return execx.Result{Stdout: []byte("Inst curl [1] (2 repo [amd64])\n")}, nil
	}))
	if _, err := a.Simulate(context.Background(), aptOperation(model.OperationInstall, "curl")); err == nil || !strings.Contains(err.Error(), "opportunistic upgrade") {
		t.Fatalf("Simulate error = %v", err)
	}
}

func TestSimulateFreshlyRejectsEssentialWithoutPriorInspect(t *testing.T) {
	a := newAdapter(t, runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		if request.Path != DpkgQueryPath {
			t.Fatal("apt-get simulation ran for Essential package")
		}
		return execx.Result{Stdout: []byte("zsh\tii \t5.9\tyes\n")}, nil
	}))
	if _, err := a.Simulate(context.Background(), aptOperation(model.OperationPrune, "zsh")); err == nil || !strings.Contains(err.Error(), "Essential") {
		t.Fatalf("Simulate error = %v", err)
	}
}

func TestExecuteSimulatesImmediatelyBeforeTargetedMutation(t *testing.T) {
	var calls []execx.Request
	a := newAdapter(t, runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		calls = append(calls, request)
		if request.Path == DpkgQueryPath {
			return execx.Result{Stdout: []byte("zlib1g-dev\tii \t1.3\tno\n")}, nil
		}
		if len(request.Args) > 0 && request.Args[0] == "-s" {
			return execx.Result{Stdout: []byte("Remv zlib1g-dev [1.3]\n")}, nil
		}
		return execx.Result{}, nil
	}))
	if err := a.Execute(context.Background(), aptOperation(model.OperationPrune, "zlib1g-dev")); err != nil {
		t.Fatal(err)
	}
	want := []execx.Request{
		{Path: DpkgQueryPath, Args: []string{"--show", "--showformat=${binary:Package}\\t${db:Status-Abbrev}\\t${Version}\\t${Essential}\\n", "zlib1g-dev"}},
		{Path: AptGetPath, Args: []string{"-s", "remove", "--", "zlib1g-dev"}, Privilege: true},
		{Path: AptGetPath, Args: []string{"remove", "-y", "--", "zlib1g-dev"}, Privilege: true},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
	for _, call := range calls {
		if strings.Contains(strings.Join(call.Args, " "), "autoremove") {
			t.Fatal("autoremove invoked")
		}
	}
}

func TestRefreshMetadataCachesConcurrentSuccessAndError(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "error"}[fail], func(t *testing.T) {
			var mu sync.Mutex
			calls := 0
			a := newAdapter(t, runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
				mu.Lock()
				calls++
				mu.Unlock()
				if !reflect.DeepEqual(request, execx.Request{Path: AptGetPath, Args: []string{"update"}, Privilege: true}) {
					t.Fatalf("request = %#v", request)
				}
				if fail {
					return execx.Result{}, errors.New("offline")
				}
				return execx.Result{}, nil
			}))
			var wg sync.WaitGroup
			for range 8 {
				wg.Add(1)
				go func() { defer wg.Done(); _ = a.RefreshMetadata(context.Background()) }()
			}
			wg.Wait()
			if calls != 1 {
				t.Fatalf("calls = %d, want 1", calls)
			}
		})
	}
}

func aptResource(pkg string) model.Resource {
	return model.Resource{ID: model.ResourceID("bootstrap-apt." + pkg), Type: model.ResourcePackage, VersionPolicy: model.VersionTracked, Provider: "apt", Package: pkg, Metadata: map[string]string{"bootstrapOnly": "true"}}
}

func aptOperation(kind model.OperationKind, pkg string) model.Operation {
	return model.Operation{Kind: kind, Provider: "apt", Package: pkg, RequiresPrivilege: true}
}

func newAdapter(t *testing.T, runner Runner) *Adapter {
	t.Helper()
	a, err := New(AptGetPath, DpkgQueryPath, runner)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func noCommand(context.Context, execx.Request) (execx.Result, error) {
	return execx.Result{}, errors.New("unexpected command")
}

type pointerRunner struct{}

func (*pointerRunner) Run(context.Context, execx.Request) (execx.Result, error) {
	return execx.Result{}, nil
}
