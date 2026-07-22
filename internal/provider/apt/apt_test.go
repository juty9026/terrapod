package apt

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
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
		if request.Path == AptGetPath {
			return execx.Result{Stdout: []byte("Remv zsh [5.9-6ubuntu2]\n0 upgraded, 0 newly installed, 1 to remove and 0 not upgraded.\n")}, nil
		}
		return execx.Result{Stdout: []byte("zsh\tii \t5.9-6ubuntu2\tyes\n")}, nil
	}))
	got, err := a.Inspect(context.Background(), aptResource("zsh"))
	if err != nil {
		t.Fatal(err)
	}
	if !got.Present || got.Healthy || got.Version != "5.9-6ubuntu2" || !strings.Contains(got.Detail, "Essential") {
		t.Fatalf("observation = %#v", got)
	}
	want := aptRequest(DpkgQueryPath, []string{"--show", "--showformat=${binary:Package}\\t${db:Status-Abbrev}\\t${Version}\\t${Essential}\\n", "zsh"}, false)
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
		return execx.Result{}, exitError(t, 1)
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
		if request.Path == DpkgQueryPath {
			return execx.Result{Stdout: []byte("libssl3t64\tii \t3.0.13\tno\n")}, nil
		}
		want := []string{"-s", "install", "--", "python3-dev"}
		if !reflect.DeepEqual(request.Args, want) || !request.Privilege {
			t.Fatalf("request = %#v, want args %#v privileged", request, want)
		}
		return execx.Result{Stdout: []byte("Inst libpython3.12-dev (3.12.3 Ubuntu:24.04 [amd64])\nInst libssl3t64 [3.0.13] (3.0.14 Ubuntu:24.04 [amd64])\nInst python3-dev (3.12 Ubuntu:24.04 [amd64])\nConf python3-dev (3.12 Ubuntu:24.04 [amd64])\n1 upgraded, 2 newly installed, 0 to remove and 0 not upgraded.\n")}, nil
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
		{name: "unmanaged removal", output: "Inst curl [1] (2 repo [amd64])\nRemv wget [1]\n1 upgraded, 0 newly installed, 1 to remove and 0 not upgraded.\n", want: "unmanaged removals"},
		{name: "unknown mutation", output: "Purg curl [1]\n0 upgraded, 0 newly installed, 0 to remove and 0 not upgraded.\n", want: "unknown"},
		{name: "malformed install", output: "Inst curl\n0 upgraded, 0 newly installed, 0 to remove and 0 not upgraded.\n", want: "malformed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newAdapter(t, runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
				if request.Path == DpkgQueryPath {
					pkg := request.Args[len(request.Args)-1]
					return execx.Result{Stdout: []byte(pkg + "\tii \t1\tno\n")}, nil
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
		if request.Path == DpkgQueryPath {
			return execx.Result{Stdout: []byte("curl\tii \t1\tno\n")}, nil
		}
		return execx.Result{Stdout: []byte("Inst curl [1] (2 repo [amd64])\n1 upgraded, 0 newly installed, 0 to remove and 0 not upgraded.\n")}, nil
	}))
	if _, err := a.Simulate(context.Background(), aptOperation(model.OperationInstall, "curl")); err == nil || !strings.Contains(err.Error(), "opportunistic upgrade") {
		t.Fatalf("Simulate error = %v", err)
	}
}

func TestSimulateFreshlyRejectsEssentialWithoutPriorInspect(t *testing.T) {
	a := newAdapter(t, runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		if request.Path == AptGetPath {
			return execx.Result{Stdout: []byte("Remv zsh [5.9]\n0 upgraded, 0 newly installed, 1 to remove and 0 not upgraded.\n")}, nil
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
			return execx.Result{Stdout: []byte("Remv zlib1g-dev [1.3]\n0 upgraded, 0 newly installed, 1 to remove and 0 not upgraded.\n")}, nil
		}
		return execx.Result{}, nil
	}))
	if err := a.Execute(context.Background(), aptOperation(model.OperationPrune, "zlib1g-dev")); err != nil {
		t.Fatal(err)
	}
	want := []execx.Request{
		aptRequest(AptGetPath, []string{"-s", "remove", "--", "zlib1g-dev"}, true),
		aptRequest(DpkgQueryPath, []string{"--show", "--showformat=${binary:Package}\\t${db:Status-Abbrev}\\t${Version}\\t${Essential}\\n", "zlib1g-dev"}, false),
		aptRequest(AptGetPath, []string{"remove", "-y", "--", "zlib1g-dev"}, true),
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

func TestResolutionCapabilityBindsFreshSimulationExecutesExactConfirmedSetAndVerifies(t *testing.T) {
	installed := map[string]bool{"mise": true, "dependent-a": true, "dependent-z": true}
	var calls []execx.Request
	a := newAdapter(t, runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		calls = append(calls, request)
		if request.Path == AptGetPath {
			if len(request.Args) > 0 && request.Args[0] == "-s" {
				if reflect.DeepEqual(request.Args, []string{"-s", "remove", "--", "dependent-a", "dependent-z"}) {
					return execx.Result{Stdout: []byte("Remv dependent-a [1]\nRemv dependent-z [1]\n0 upgraded, 0 newly installed, 2 to remove and 0 not upgraded.\n")}, nil
				}
				return execx.Result{Stdout: []byte("Remv mise [1]\nRemv dependent-z [1]\nRemv dependent-a [1]\n0 upgraded, 0 newly installed, 3 to remove and 0 not upgraded.\n")}, nil
			}
			want := []string{"remove", "-y", "--", "dependent-a", "dependent-z"}
			if !reflect.DeepEqual(request.Args, want) {
				t.Fatalf("execution args = %v, want %v", request.Args, want)
			}
			installed["dependent-a"], installed["dependent-z"] = false, false
			return execx.Result{}, nil
		}
		pkg := request.Args[len(request.Args)-1]
		if !installed[pkg] {
			return execx.Result{}, exitError(t, 1)
		}
		return execx.Result{Stdout: []byte(pkg + "\tii \t1\tno\n")}, nil
	}))
	op := aptOperation(model.OperationPrune, "mise")
	capability, changes, err := a.PrepareResolution(context.Background(), op)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(changes.Removes, []string{"mise", "dependent-z", "dependent-a"}) {
		t.Fatalf("changes = %#v", changes)
	}
	confirmed := []string{"dependent-a", "dependent-z"}
	if err := a.ExecuteResolution(context.Background(), capability, confirmed); err != nil {
		t.Fatal(err)
	}
	if err := a.VerifyResolutionAbsent(context.Background(), capability); err != nil {
		t.Fatal(err)
	}
	if err := a.ExecuteResolution(context.Background(), capability, confirmed); err == nil {
		t.Fatal("consumed capability replayed")
	}
	for _, request := range calls {
		joined := strings.Join(request.Args, " ")
		if strings.Contains(joined, "autoremove") || strings.Contains(joined, "--force") {
			t.Fatalf("unsafe resolution request: %#v", request)
		}
		if len(request.Args) > 0 && request.Args[0] == "-s" && request.Privilege {
			t.Fatalf("resolution simulation requested privilege: %#v", request)
		}
	}
}

func TestResolutionCapabilityRefusesEssentialAndSimulationTOCTOU(t *testing.T) {
	t.Run("essential", func(t *testing.T) {
		mutated := false
		a := newAdapter(t, runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
			if request.Path == AptGetPath {
				if len(request.Args) > 0 && request.Args[0] != "-s" {
					mutated = true
				}
				return execx.Result{Stdout: []byte("Remv mise [1]\nRemv init [1]\n0 upgraded, 0 newly installed, 2 to remove and 0 not upgraded.\n")}, nil
			}
			pkg := request.Args[len(request.Args)-1]
			essential := "no"
			if pkg == "init" {
				essential = "yes"
			}
			return execx.Result{Stdout: []byte(pkg + "\tii \t1\t" + essential + "\n")}, nil
		}))
		if _, _, err := a.PrepareResolution(context.Background(), aptOperation(model.OperationPrune, "mise")); err == nil || !strings.Contains(err.Error(), "Essential") {
			t.Fatalf("PrepareResolution error = %v", err)
		}
		if mutated {
			t.Fatal("essential plan mutated")
		}
	})

	t.Run("changed simulation", func(t *testing.T) {
		simulations := 0
		mutated := false
		a := newAdapter(t, runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
			if request.Path == DpkgQueryPath {
				pkg := request.Args[len(request.Args)-1]
				return execx.Result{Stdout: []byte(pkg + "\tii \t1\tno\n")}, nil
			}
			if request.Args[0] == "-s" {
				simulations++
				dependent := "dependent-a"
				if simulations == 2 {
					dependent = "dependent-b"
				}
				return execx.Result{Stdout: []byte("Remv mise [1]\nRemv " + dependent + " [1]\n0 upgraded, 0 newly installed, 2 to remove and 0 not upgraded.\n")}, nil
			}
			mutated = true
			return execx.Result{}, nil
		}))
		capability, _, err := a.PrepareResolution(context.Background(), aptOperation(model.OperationPrune, "mise"))
		if err != nil {
			t.Fatal(err)
		}
		if err := a.ExecuteResolution(context.Background(), capability, []string{"dependent-a"}); err == nil || !strings.Contains(err.Error(), "changed") {
			t.Fatalf("ExecuteResolution error = %v", err)
		}
		if mutated {
			t.Fatal("changed simulation mutated")
		}
		if err := a.ExecuteResolution(context.Background(), capability, []string{"dependent-a"}); err == nil {
			t.Fatal("failed capability replayed")
		}
	})
}

func TestResolutionCapabilityRejectsWrongConfirmationAndCancelRevokes(t *testing.T) {
	a := newAdapter(t, runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		if request.Path == DpkgQueryPath {
			pkg := request.Args[len(request.Args)-1]
			return execx.Result{Stdout: []byte(pkg + "\tii \t1\tno\n")}, nil
		}
		return execx.Result{Stdout: []byte("Remv mise [1]\nRemv dependent-a [1]\n0 upgraded, 0 newly installed, 2 to remove and 0 not upgraded.\n")}, nil
	}))
	capability, _, err := a.PrepareResolution(context.Background(), aptOperation(model.OperationPrune, "mise"))
	if err != nil {
		t.Fatal(err)
	}
	if err := a.ExecuteResolution(context.Background(), capability, []string{"different"}); err == nil {
		t.Fatal("wrong confirmation accepted")
	}
	if err := a.ExecuteResolution(context.Background(), capability, []string{"dependent-a"}); err == nil {
		t.Fatal("failed confirmation capability replayed")
	}
	second, _, err := a.PrepareResolution(context.Background(), aptOperation(model.OperationPrune, "mise"))
	if err != nil {
		t.Fatal(err)
	}
	if err := a.CancelResolution(second); err != nil {
		t.Fatal(err)
	}
	if err := a.ExecuteResolution(context.Background(), second, []string{"dependent-a"}); err == nil {
		t.Fatal("canceled capability executed")
	}
}

func TestPrepareResolutionReturnsTypedNoConflict(t *testing.T) {
	a := newAdapter(t, runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		if request.Path == DpkgQueryPath {
			return execx.Result{Stdout: []byte("mise\tii \t1\tno\n")}, nil
		}
		return execx.Result{Stdout: []byte("Remv mise [1]\n0 upgraded, 0 newly installed, 1 to remove and 0 not upgraded.\n")}, nil
	}))
	_, _, err := a.PrepareResolution(context.Background(), aptOperation(model.OperationPrune, "mise"))
	var noConflict *ErrNoResolutionConflict
	if !errors.As(err, &noConflict) || noConflict.Package != "mise" {
		t.Fatalf("PrepareResolution error = %v", err)
	}
}

func TestResolutionCapabilityRejectsBlockerOnlyPlanThatAlsoRemovesLegacyRoot(t *testing.T) {
	mutated := false
	a := newAdapter(t, runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		if request.Path == DpkgQueryPath {
			pkg := request.Args[len(request.Args)-1]
			return execx.Result{Stdout: []byte(pkg + "\tii \t1\tno\n")}, nil
		}
		if request.Args[0] != "-s" {
			mutated = true
			return execx.Result{}, nil
		}
		return execx.Result{Stdout: []byte("Remv mise [1]\nRemv dependent-a [1]\n0 upgraded, 0 newly installed, 2 to remove and 0 not upgraded.\n")}, nil
	}))
	capability, _, err := a.PrepareResolution(context.Background(), aptOperation(model.OperationPrune, "mise"))
	if err != nil {
		t.Fatal(err)
	}
	if err := a.ExecuteResolution(context.Background(), capability, []string{"dependent-a"}); err == nil || !strings.Contains(err.Error(), "additional") {
		t.Fatalf("ExecuteResolution error = %v", err)
	}
	if mutated {
		t.Fatal("unsafe blocker-only plan mutated")
	}
}

func TestResolutionCapabilityHonorsCancellationImmediatelyBeforeMutation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	mutated := false
	a := newAdapter(t, runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		if request.Path == DpkgQueryPath {
			pkg := request.Args[len(request.Args)-1]
			return execx.Result{Stdout: []byte(pkg + "\tii \t1\tno\n")}, nil
		}
		if reflect.DeepEqual(request.Args, []string{"-s", "remove", "--", "dependent-a"}) {
			cancel()
			return execx.Result{Stdout: []byte("Remv dependent-a [1]\n0 upgraded, 0 newly installed, 1 to remove and 0 not upgraded.\n")}, nil
		}
		if request.Args[0] != "-s" {
			mutated = true
			return execx.Result{}, nil
		}
		return execx.Result{Stdout: []byte("Remv mise [1]\nRemv dependent-a [1]\n0 upgraded, 0 newly installed, 2 to remove and 0 not upgraded.\n")}, nil
	}))
	capability, _, err := a.PrepareResolution(ctx, aptOperation(model.OperationPrune, "mise"))
	if err != nil {
		t.Fatal(err)
	}
	if err := a.ExecuteResolution(ctx, capability, []string{"dependent-a"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("ExecuteResolution error = %v", err)
	}
	if mutated {
		t.Fatal("canceled resolution mutated")
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
				if !reflect.DeepEqual(request, aptRequest(AptGetPath, []string{"update"}, true)) {
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

func TestSimulationRequiresEnglishSummaryAndMatchingCounts(t *testing.T) {
	for _, tc := range []struct{ name, output, want string }{
		{name: "missing", output: "", want: "summary"},
		{name: "localized", output: "0 aktualisiert, 0 neu installiert\n", want: "unknown"},
		{name: "mismatch", output: "Inst curl (1 repo [amd64])\n0 upgraded, 0 newly installed, 0 to remove and 0 not upgraded.\n", want: "counts"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newAdapter(t, runnerFunc(func(_ context.Context, _ execx.Request) (execx.Result, error) {
				return execx.Result{Stdout: []byte(tc.output)}, nil
			}))
			if _, err := a.Simulate(context.Background(), aptOperation(model.OperationInstall, "curl")); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestSimulationConfMustMatchOneExactInstIdentity(t *testing.T) {
	for _, tc := range []struct{ name, output string }{
		{name: "hidden conf with zero summary", output: "Conf curl (1 repo [amd64])\n0 upgraded, 0 newly installed, 0 to remove and 0 not upgraded.\n"},
		{name: "duplicate conf", output: "Inst curl (1 repo [amd64])\nConf curl (1 repo [amd64])\nConf curl (1 repo [amd64])\n0 upgraded, 1 newly installed, 0 to remove and 0 not upgraded.\n"},
		{name: "different base", output: "Inst curl (1 repo [amd64])\nConf wget (1 repo [amd64])\n0 upgraded, 1 newly installed, 0 to remove and 0 not upgraded.\n"},
		{name: "different arch", output: "Inst curl:amd64 (1 repo [amd64])\nConf curl:arm64 (1 repo [arm64])\n0 upgraded, 1 newly installed, 0 to remove and 0 not upgraded.\n"},
		{name: "remove and conf", output: "Remv curl [1]\nConf curl (1 repo [amd64])\n0 upgraded, 0 newly installed, 1 to remove and 0 not upgraded.\n"},
		{name: "malformed package", output: "Inst curl (1 repo [amd64])\nConf curl:AMD64 (1 repo [amd64])\n0 upgraded, 1 newly installed, 0 to remove and 0 not upgraded.\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parsePlan([]byte(tc.output), "curl"); err == nil || !strings.Contains(err.Error(), "Conf") {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestSimulationAcceptsConfForExactMultiarchInstAndNormalizesTarget(t *testing.T) {
	output := "Inst curl:amd64 (1 repo [amd64])\nConf curl:amd64 (1 repo [amd64])\n0 upgraded, 1 newly installed, 0 to remove and 0 not upgraded.\n"
	changes, err := parsePlan([]byte(output), "curl")
	if err != nil || !reflect.DeepEqual(changes.Installs, []string{"curl"}) {
		t.Fatalf("changes=%#v error=%v", changes, err)
	}
}

func TestSimulationRejectsEssentialDependencyAfterPlan(t *testing.T) {
	var calls []string
	a := newAdapter(t, runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		calls = append(calls, request.Path+" "+strings.Join(request.Args, " "))
		if request.Path == AptGetPath {
			return execx.Result{Stdout: []byte("Inst libssl3 [1] (2 repo [amd64])\n1 upgraded, 0 newly installed, 0 to remove and 0 not upgraded.\n")}, nil
		}
		return execx.Result{Stdout: []byte("libssl3\tii \t1\tyes\n")}, nil
	}))
	if _, err := a.Simulate(context.Background(), aptOperation(model.OperationInstall, "curl")); err == nil || !strings.Contains(err.Error(), "Essential") {
		t.Fatalf("error=%v", err)
	}
	if len(calls) != 2 || !strings.Contains(calls[1], "libssl3") {
		t.Fatalf("calls=%v", calls)
	}
}

func TestInspectPropagatesNonAbsenceFailures(t *testing.T) {
	for _, tc := range []struct {
		name string
		ctx  context.Context
		err  error
	}{
		{name: "permission", ctx: context.Background(), err: exitError(t, 2)},
		{name: "missing executable", ctx: context.Background(), err: exec.ErrNotFound},
		{name: "canceled", ctx: canceledContext(), err: exitError(t, 1)},
		{name: "output limit joined with exit one", ctx: context.Background(), err: errors.Join(exitError(t, 1), &execx.ErrOutputLimit{Streams: []string{"stderr"}})},
		{name: "joined unrelated with exit one", ctx: context.Background(), err: fmt.Errorf("outer: %w", errors.Join(exitError(t, 1), errors.New("unrelated")))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newAdapter(t, runnerFunc(func(context.Context, execx.Request) (execx.Result, error) { return execx.Result{}, tc.err }))
			if _, err := a.Inspect(tc.ctx, aptResource("curl")); err == nil {
				t.Fatal("Inspect succeeded")
			}
		})
	}
}

func TestInspectAcceptsOrdinaryWrappedExitOneAsAbsent(t *testing.T) {
	a := newAdapter(t, runnerFunc(func(context.Context, execx.Request) (execx.Result, error) {
		return execx.Result{}, fmt.Errorf("runner: %w", exitError(t, 1))
	}))
	got, err := a.Inspect(context.Background(), aptResource("curl"))
	if err != nil || got.Present {
		t.Fatalf("observation=%#v error=%v", got, err)
	}
}

func TestInspectRejectsWrongOrMalformedMultiarchBinaryName(t *testing.T) {
	for _, name := range []string{"wget:amd64", "curl:AMD64", "curl:amd64:evil"} {
		t.Run(name, func(t *testing.T) {
			a := newAdapter(t, runnerFunc(func(context.Context, execx.Request) (execx.Result, error) {
				return execx.Result{Stdout: []byte(name + "\tii \t1\tno\n")}, nil
			}))
			if _, err := a.Inspect(context.Background(), aptResource("curl")); err == nil {
				t.Fatal("Inspect succeeded")
			}
		})
	}
}

func TestMultiarchTargetInventoryAndPlanNormalization(t *testing.T) {
	a := newAdapter(t, runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		if request.Path == DpkgQueryPath {
			return execx.Result{Stdout: []byte("curl:amd64\tii \t1\tno\n")}, nil
		}
		return execx.Result{Stdout: []byte("Remv curl:amd64 [1]\n0 upgraded, 0 newly installed, 1 to remove and 0 not upgraded.\n")}, nil
	}))
	if got, err := a.Inspect(context.Background(), aptResource("curl")); err != nil || !got.Present {
		t.Fatalf("got=%#v err=%v", got, err)
	}
	changes, err := a.Simulate(context.Background(), aptOperation(model.OperationPrune, "curl"))
	if err != nil || !reflect.DeepEqual(changes.Removes, []string{"curl"}) {
		t.Fatalf("changes=%#v err=%v", changes, err)
	}
}

func TestExecutableFixturesProveArgvLocaleAndExitSemantics(t *testing.T) {
	runner := execx.NewRunner([]string{"LC_ALL"}, nil, func() int { return 501 })
	aptFixture, err := filepath.Abs(filepath.Join("testdata", "apt-get"))
	if err != nil {
		t.Fatal(err)
	}
	dpkgFixture, err := filepath.Abs(filepath.Join("testdata", "dpkg-query"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := runner.Run(context.Background(), aptRequest(aptFixture, []string{"-s", "install", "--", "curl"}, false))
	if err != nil || !strings.Contains(string(result.Stdout), "1 newly installed") {
		t.Fatalf("result=%q error=%v", result.Stdout, err)
	}
	if _, err := runner.Run(context.Background(), aptRequest(aptFixture, []string{"install", "curl"}, false)); err == nil {
		t.Fatal("fixture accepted wrong argv")
	}
	result, err = runner.Run(context.Background(), aptRequest(dpkgFixture, []string{"--show", "--showformat=" + dpkgFormat, "curl"}, false))
	if err != nil || !strings.HasPrefix(string(result.Stdout), "curl\tii ") {
		t.Fatalf("result=%q error=%v", result.Stdout, err)
	}
	if _, err := runner.Run(context.Background(), aptRequest(dpkgFixture, []string{"--show", "--showformat=" + dpkgFormat, "missing"}, false)); err == nil {
		t.Fatal("missing fixture did not exit non-zero")
	}
}

func aptRequest(path string, args []string, privilege bool) execx.Request {
	return execx.Request{Path: path, Args: args, Env: map[string]string{"LC_ALL": "C"}, Privilege: privilege}
}
func exitError(t *testing.T, code int) error {
	t.Helper()
	command, args := "/usr/bin/false", []string{}
	if code != 1 {
		command, args = "/usr/bin/grep", []string{"--definitely-invalid-option"}
	}
	err := exec.Command(command, args...).Run()
	if err == nil {
		t.Fatal("expected exit error")
	}
	return err
}
func canceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
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
