package gitcheckout

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
	"github.com/juty9026/terrapod/internal/recovery"
	"github.com/juty9026/terrapod/internal/state"
)

const testCommit = "0123456789abcdef0123456789abcdef01234567"

type runnerFunc func(context.Context, execx.Request) (execx.Result, error)

func (f runnerFunc) Run(ctx context.Context, request execx.Request) (execx.Result, error) {
	return f(ctx, request)
}

func resourceAt(destination string) model.Resource {
	return model.Resource{
		ID: "shell.zinit", Type: model.ResourceGitCheckout, Provider: "git", Package: "zinit",
		VersionPolicy: model.VersionPinned,
		Metadata: map[string]string{
			MetadataRemote:      "https://github.com/zdharma-continuum/zinit.git",
			MetadataRef:         "refs/tags/v3.14.0",
			MetadataCommit:      testCommit,
			MetadataDestination: destination,
			MetadataVerifyFiles: "zinit.zsh",
		},
	}
}

func newFixture(t *testing.T, run runnerFunc) (*Adapter, model.Resource, *state.Store, string) {
	t.Helper()
	home := t.TempDir()
	store, err := state.Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	item := resourceAt(".local/share/zinit/zinit.git")
	adapter := &Adapter{Runner: run, Git: "/opt/homebrew/bin/git", Home: home, State: store,
		Backup: recovery.Backup{Root: filepath.Join(t.TempDir(), "recovery"), Base: home}}
	return adapter, item, store, filepath.Join(home, item.Metadata[MetadataDestination])
}

func TestPlanLifecycle(t *testing.T) {
	ctx := context.Background()
	t.Run("absent clone", func(t *testing.T) {
		a, item, _, _ := newFixture(t, func(context.Context, execx.Request) (execx.Result, error) { return execx.Result{}, nil })
		ops, err := a.Plan(ctx, item, model.Observation{}, model.Ownership{})
		if err != nil || len(ops) != 1 || ops[0].Kind != model.OperationInstall {
			t.Fatalf("ops=%#v err=%v", ops, err)
		}
	})

	t.Run("matching clean adopt and pinned update", func(t *testing.T) {
		for _, tc := range []struct {
			name, head string
			want       model.OperationKind
		}{
			{"adopt", testCommit, model.OperationAdopt}, {"update", strings.Repeat("a", 40), model.OperationUpgrade},
		} {
			t.Run(tc.name, func(t *testing.T) {
				a, item, _, destination := newFixture(t, gitInspector(t, "https://github.com/zdharma-continuum/zinit.git", tc.head, ""))
				if err := os.MkdirAll(filepath.Join(destination, ".git"), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(destination, "zinit.zsh"), []byte("zinit"), 0o600); err != nil {
					t.Fatal(err)
				}
				ops, err := a.Plan(ctx, item, model.Observation{}, model.Ownership{})
				if err != nil || len(ops) != 1 || ops[0].Kind != tc.want {
					t.Fatalf("ops=%#v err=%v", ops, err)
				}
			})
		}
	})

	t.Run("wrong remote and tracked local modification conflict", func(t *testing.T) {
		for _, tc := range []struct{ name, remote, status string }{
			{"remote", "https://example.invalid/wrong.git", ""}, {"modified", "https://github.com/zdharma-continuum/zinit.git", " M zinit.zsh\n"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				a, item, _, destination := newFixture(t, gitInspector(t, tc.remote, testCommit, tc.status))
				if err := os.MkdirAll(filepath.Join(destination, ".git"), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(destination, "zinit.zsh"), []byte("zinit"), 0o600); err != nil {
					t.Fatal(err)
				}
				if _, err := a.Plan(ctx, item, model.Observation{}, model.Ownership{}); err == nil || !strings.Contains(err.Error(), "conflict") {
					t.Fatalf("err=%v", err)
				}
			})
		}
	})
}

func TestExecuteCloneUsesOnlyConstrainedGitCommands(t *testing.T) {
	var requests []execx.Request
	a, item, store, destination := newFixture(t, func(_ context.Context, request execx.Request) (execx.Result, error) {
		requests = append(requests, request)
		args := strings.Join(request.Args, " ")
		switch {
		case strings.Contains(args, " init "):
			if err := os.MkdirAll(filepath.Join(request.Dir, ".git"), 0o700); err != nil {
				return execx.Result{}, err
			}
		case strings.Contains(args, " rev-parse FETCH_HEAD"):
			return execx.Result{Stdout: []byte(testCommit + "\n")}, nil
		case strings.Contains(args, " checkout "):
			if err := os.WriteFile(filepath.Join(request.Dir, "zinit.zsh"), []byte("zinit"), 0o600); err != nil {
				return execx.Result{}, err
			}
		case strings.Contains(args, " remote get-url origin"):
			return execx.Result{Stdout: []byte("https://github.com/zdharma-continuum/zinit.git\n")}, nil
		case strings.Contains(args, " rev-parse HEAD"):
			return execx.Result{Stdout: []byte(testCommit + "\n")}, nil
		case strings.Contains(args, " ls-files -z"):
			return execx.Result{Stdout: []byte("zinit.zsh\x00")}, nil
		}
		return execx.Result{}, nil
	})
	op := operation(item, model.OperationInstall)
	if _, err := store.Begin(model.Plan{ID: "git-clone", Operations: []model.Operation{op}}); err != nil {
		t.Fatal(err)
	}
	result := a.ExecuteResource(context.Background(), item, op)
	if !result.Success {
		t.Fatalf("result=%#v", result)
	}
	if _, err := os.Stat(filepath.Join(destination, "zinit.zsh")); err != nil {
		t.Fatal(err)
	}
	for _, request := range requests {
		if request.Path != "/opt/homebrew/bin/git" || request.Privilege || strings.Contains(strings.Join(request.Args, " "), "install.sh") {
			t.Fatalf("unsafe request: %#v", request)
		}
		if !containsSequence(request.Args, "-c", "core.hooksPath=/dev/null") {
			t.Fatalf("hooks not disabled: %#v", request.Args)
		}
		if request.Env["GIT_CONFIG_GLOBAL"] != "/dev/null" || request.Env["GIT_CONFIG_NOSYSTEM"] != "1" || request.Env["GIT_TERMINAL_PROMPT"] != "0" {
			t.Fatalf("Git ambient config or prompts not disabled: %#v", request.Env)
		}
	}
}

func TestPartialCheckoutIsBackedUpBeforeReinstall(t *testing.T) {
	var journalID string
	backupReady := false
	a, item, store, destination := newFixture(t, func(context.Context, execx.Request) (execx.Result, error) { return execx.Result{}, nil })
	// Replace the runner now that the fixture exposes its recovery root.
	a.Runner = runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		args := strings.Join(request.Args, " ")
		switch {
		case strings.Contains(args, " init "):
			captured := filepath.Join(a.Backup.Root, journalID, ".local", "share", "zinit", "zinit.git", "partial.txt")
			contents, err := os.ReadFile(captured)
			if err != nil || string(contents) != "partial" {
				return execx.Result{}, errors.New("recovery backup was not committed before reinstall")
			}
			backupReady = true
			if err := os.MkdirAll(filepath.Join(request.Dir, ".git"), 0o700); err != nil {
				return execx.Result{}, err
			}
		case strings.Contains(args, "rev-parse FETCH_HEAD"):
			return execx.Result{Stdout: []byte(testCommit + "\n")}, nil
		case strings.Contains(args, " checkout "):
			if err := os.WriteFile(filepath.Join(request.Dir, "zinit.zsh"), []byte("zinit"), 0o600); err != nil {
				return execx.Result{}, err
			}
		}
		return execx.Result{}, nil
	})
	if err := os.MkdirAll(filepath.Join(destination, "empty"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "partial.txt"), []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	op := operation(item, model.OperationRestore)
	journal, err := store.Begin(model.Plan{ID: "partial-restore", Operations: []model.Operation{op}})
	if err != nil {
		t.Fatal(err)
	}
	journalID = journal.ID
	result := a.ExecuteResource(context.Background(), item, op)
	if !result.Success || !backupReady {
		t.Fatalf("result=%#v backupReady=%v", result, backupReady)
	}
	if _, err := os.Stat(filepath.Join(destination, "zinit.zsh")); err != nil {
		t.Fatal(err)
	}
}

func TestPruneRemovesRecordedTrackedFilesButPreservesUntrackedChildren(t *testing.T) {
	a, item, store, destination := newFixture(t, func(context.Context, execx.Request) (execx.Result, error) {
		return execx.Result{}, errors.New("git must not run during prune")
	})
	tracked := filepath.Join(destination, "lib", "tracked.zsh")
	untracked := filepath.Join(destination, "notes.txt")
	if err := os.MkdirAll(filepath.Dir(tracked), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tracked, []byte("owned"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(untracked, []byte("user"), 0o600); err != nil {
		t.Fatal(err)
	}
	receipt, err := pathReceipt(tracked)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PutOwnership(model.Ownership{ResourceID: item.ID, Provider: item.Provider, Package: item.Package, Paths: map[string]string{tracked: receipt}}); err != nil {
		t.Fatal(err)
	}
	ops, err := a.PlanHistorical(context.Background(), item, model.Observation{}, model.Ownership{ResourceID: item.ID, Provider: item.Provider, Package: item.Package, Paths: map[string]string{tracked: receipt}})
	if err != nil || len(ops) != 1 || ops[0].Kind != model.OperationPrune {
		t.Fatalf("historical ops=%#v err=%v", ops, err)
	}
	op := operation(item, model.OperationPrune)
	if _, err := store.Begin(model.Plan{ID: "git-prune", Operations: []model.Operation{op}}); err != nil {
		t.Fatal(err)
	}
	result := a.ExecuteResource(context.Background(), item, op)
	if !result.Success {
		t.Fatalf("result=%#v", result)
	}
	if _, err := os.Lstat(tracked); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("tracked remains: %v", err)
	}
	if got, err := os.ReadFile(untracked); err != nil || string(got) != "user" {
		t.Fatalf("untracked=%q err=%v", got, err)
	}
	observed, err := a.Inspect(context.Background(), item)
	if err != nil || observed.Present {
		t.Fatalf("post-prune observation=%#v err=%v", observed, err)
	}
}

func TestMetadataRejectsCommandsAndUnsafeValues(t *testing.T) {
	base := resourceAt(".local/share/zinit/zinit.git")
	for name, edit := range map[string]func(*model.Resource){
		"relative git":    func(*model.Resource) {},
		"remote":          func(r *model.Resource) { r.Metadata[MetadataRemote] = "file:///tmp/repo" },
		"ref":             func(r *model.Resource) { r.Metadata[MetadataRef] = "--upload-pack=evil" },
		"commit":          func(r *model.Resource) { r.Metadata[MetadataCommit] = "HEAD" },
		"destination":     func(r *model.Resource) { r.Metadata[MetadataDestination] = "../escape" },
		"catalog command": func(r *model.Resource) { r.Commands = []string{"sh -c evil"} },
	} {
		t.Run(name, func(t *testing.T) {
			item := base
			item.Metadata = mapsClone(base.Metadata)
			edit(&item)
			git := "/opt/homebrew/bin/git"
			if name == "relative git" {
				git = "git"
			}
			a := &Adapter{Runner: runnerFunc(func(context.Context, execx.Request) (execx.Result, error) { return execx.Result{}, nil }), Git: git, Home: t.TempDir()}
			if _, err := a.Inspect(context.Background(), item); err == nil {
				t.Fatal("unsafe declaration accepted")
			}
		})
	}
}

func gitInspector(t *testing.T, remote, head, status string) runnerFunc {
	t.Helper()
	return func(_ context.Context, request execx.Request) (execx.Result, error) {
		args := strings.Join(request.Args, " ")
		switch {
		case strings.Contains(args, "remote get-url origin"):
			return execx.Result{Stdout: []byte(remote + "\n")}, nil
		case strings.Contains(args, "rev-parse HEAD"):
			return execx.Result{Stdout: []byte(head + "\n")}, nil
		case strings.Contains(args, "status --porcelain"):
			return execx.Result{Stdout: []byte(status)}, nil
		case strings.Contains(args, "ls-files -z"):
			return execx.Result{Stdout: []byte("zinit.zsh\x00")}, nil
		default:
			return execx.Result{}, nil
		}
	}
}

func containsSequence(values []string, sequence ...string) bool {
	for i := 0; i+len(sequence) <= len(values); i++ {
		if reflect.DeepEqual(values[i:i+len(sequence)], sequence) {
			return true
		}
	}
	return false
}

func mapsClone(values map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range values {
		out[k] = v
	}
	return out
}
