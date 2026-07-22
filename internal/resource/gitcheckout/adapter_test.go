package gitcheckout

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/juty9026/terrapod/internal/execx"
	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/reconcile"
	"github.com/juty9026/terrapod/internal/recovery"
	resourcepkg "github.com/juty9026/terrapod/internal/resource"
	"github.com/juty9026/terrapod/internal/state"
)

const testCommit = "0123456789abcdef0123456789abcdef01234567"

type runnerFunc func(context.Context, execx.Request) (execx.Result, error)

func (f runnerFunc) Run(ctx context.Context, request execx.Request) (execx.Result, error) {
	return f(ctx, request)
}

func itemRemote() string { return "https://github.com/zdharma-continuum/zinit.git" }

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
	stateDir := filepath.Join(t.TempDir(), "state")
	store, err := state.Open(stateDir)
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
	// Simulate a crash after the checkout rename but before Engine records the
	// operation or ownership. Replaying the same active journal is idempotent.
	if resumed := a.ExecuteResource(context.Background(), item, op); !resumed.Success {
		t.Fatalf("resume after rename=%#v", resumed)
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
	stagedBeforeDestructive := false
	a, item, store, destination := newFixture(t, func(context.Context, execx.Request) (execx.Result, error) { return execx.Result{}, nil })
	// Replace the runner now that the fixture exposes its recovery root.
	a.Runner = runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		args := strings.Join(request.Args, " ")
		switch {
		case strings.Contains(args, " init "):
			if err := os.MkdirAll(filepath.Join(request.Dir, ".git"), 0o700); err != nil {
				return execx.Result{}, err
			}
		case strings.Contains(args, " fetch "):
			if contents, err := os.ReadFile(filepath.Join(destination, "partial.txt")); err != nil || string(contents) != "partial" {
				return execx.Result{}, errors.New("partial checkout changed before fetch completed")
			}
			stagedBeforeDestructive = true
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
	if !result.Success || !stagedBeforeDestructive {
		t.Fatalf("result=%#v stagedBeforeDestructive=%v", result, stagedBeforeDestructive)
	}
	captured := filepath.Join(a.Backup.Root, journalID, ".local", "share", "zinit", "zinit.git", "partial.txt")
	if contents, err := os.ReadFile(captured); err != nil || string(contents) != "partial" {
		t.Fatalf("recovery=%q err=%v", contents, err)
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

func TestInstallResumeAcceptsAlreadyExactCheckout(t *testing.T) {
	a, item, store, destination := newFixture(t, gitInspector(t, "https://github.com/zdharma-continuum/zinit.git", testCommit, ""))
	if err := os.MkdirAll(filepath.Join(destination, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, ".git", "config"), []byte("config"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "zinit.zsh"), []byte("zinit"), 0o600); err != nil {
		t.Fatal(err)
	}
	op := operation(item, model.OperationInstall)
	if _, err := store.Begin(model.Plan{ID: "resume", Operations: []model.Operation{op}}); err != nil {
		t.Fatal(err)
	}
	result := a.ExecuteResource(context.Background(), item, op)
	if !result.Success {
		t.Fatalf("resume result=%#v", result)
	}
}

func TestRejectsGitfileAndSymlinkedDestinationAncestorWithoutRunningGit(t *testing.T) {
	for _, name := range []string{"gitfile", "ancestor-symlink"} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			a, item, _, destination := newFixture(t, func(context.Context, execx.Request) (execx.Result, error) { calls++; return execx.Result{}, nil })
			if name == "gitfile" {
				if err := os.MkdirAll(destination, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(destination, ".git"), []byte("gitdir: /tmp/evil"), 0o600); err != nil {
					t.Fatal(err)
				}
			} else {
				outside := t.TempDir()
				if err := os.Symlink(outside, filepath.Join(a.Home, ".local")); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := a.Inspect(context.Background(), item); err == nil {
				t.Fatal("unsafe checkout accepted")
			}
			if calls != 0 {
				t.Fatalf("Git ran %d times", calls)
			}
		})
	}
}

func TestRejectsExecutableLocalGitConfigBeforeStatus(t *testing.T) {
	statusCalled := false
	a, item, _, destination := newFixture(t, func(_ context.Context, request execx.Request) (execx.Result, error) {
		args := strings.Join(request.Args, " ")
		if strings.Contains(args, "config --local --null --list") {
			return execx.Result{Stdout: []byte("filter.evil.process\n/tmp/evil\x00")}, nil
		}
		if strings.Contains(args, "status --porcelain") {
			statusCalled = true
		}
		return execx.Result{}, nil
	})
	if err := os.MkdirAll(filepath.Join(destination, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, ".git", "config"), []byte("config"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Inspect(context.Background(), item); err == nil || !strings.Contains(err.Error(), "config") {
		t.Fatalf("err=%v", err)
	}
	if statusCalled {
		t.Fatal("status ran after executable local config")
	}
}

func TestRejectsDestinationAncestorSwapDuringGitCommand(t *testing.T) {
	for _, component := range []string{".local", filepath.Join(".local", "share"), filepath.Join(".local", "share", "zinit", "zinit.git")} {
		t.Run(strings.ReplaceAll(component, string(filepath.Separator), "-"), func(t *testing.T) {
			var a *Adapter
			swapped := false
			a, item, _, destination := newFixture(t, func(_ context.Context, request execx.Request) (execx.Result, error) {
				if !swapped && strings.Contains(strings.Join(request.Args, " "), "config --local") {
					target := filepath.Join(a.Home, component)
					if err := os.Rename(target, target+"-real"); err != nil {
						return execx.Result{}, err
					}
					if err := os.Symlink(t.TempDir(), target); err != nil {
						return execx.Result{}, err
					}
					swapped = true
				}
				return execx.Result{}, nil
			})
			if err := os.MkdirAll(filepath.Join(destination, ".git"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(destination, ".git", "config"), []byte("config"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := a.Inspect(context.Background(), item); err == nil {
				t.Fatal("ancestor swap accepted")
			}
			if !swapped {
				t.Fatal("test did not swap ancestor")
			}
		})
	}
}

func TestPrivateGitDoesNotMutateSwappedPublishedStaging(t *testing.T) {
	swapped := false
	foreignMarker := ""
	var a *Adapter
	a, item, store, destination := newFixture(t, func(_ context.Context, request execx.Request) (execx.Result, error) {
		if strings.HasPrefix(request.Dir, a.Home+string(filepath.Separator)) {
			return execx.Result{}, errors.New("Git received user-visible staging path")
		}
		args := strings.Join(request.Args, " ")
		if strings.Contains(args, " init ") {
			return execx.Result{}, os.MkdirAll(filepath.Join(request.Dir, ".git"), 0o700)
		}
		if strings.Contains(args, "rev-parse FETCH_HEAD") {
			return execx.Result{Stdout: []byte(testCommit + "\n")}, nil
		}
		if strings.Contains(args, " checkout ") {
			return execx.Result{}, os.WriteFile(filepath.Join(request.Dir, "zinit.zsh"), []byte("zinit"), 0o600)
		}
		return execx.Result{}, nil
	})
	beforeStagingQuarantine = func(relative string) {
		beforeStagingQuarantine = nil
		path := filepath.Join(a.Home, relative)
		if err := os.Rename(path, path+"-original"); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
		foreignMarker = filepath.Join(path, "git-mutated")
		swapped = true
	}
	defer func() { beforeStagingQuarantine = nil }()
	op := operation(item, model.OperationInstall)
	if _, err := store.Begin(model.Plan{ID: "staging-swap", Operations: []model.Operation{op}}); err != nil {
		t.Fatal(err)
	}
	result := a.ExecuteResource(context.Background(), item, op)
	if result.Success || !strings.Contains(result.Detail, "inode") {
		t.Fatalf("result=%#v", result)
	}
	if !swapped {
		t.Fatal("test did not swap staging")
	}
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("foreign staging committed: %v", err)
	}
	if _, err := os.Lstat(foreignMarker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("foreign staging mutated before rejection: %v", err)
	}
}

func TestPruneDoesNotDeleteSwappedQuarantine(t *testing.T) {
	a, item, store, destination := newFixture(t, func(context.Context, execx.Request) (execx.Result, error) {
		return execx.Result{}, errors.New("Git must not run")
	})
	tracked := filepath.Join(destination, "tracked.zsh")
	if err := os.MkdirAll(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tracked, []byte("owned"), 0o600); err != nil {
		t.Fatal(err)
	}
	receipt, err := pathReceipt(tracked)
	if err != nil {
		t.Fatal(err)
	}
	owned := model.Ownership{ResourceID: item.ID, Provider: item.Provider, Package: item.Package, Paths: map[string]string{tracked: receipt}}
	if err := store.PutOwnership(owned); err != nil {
		t.Fatal(err)
	}
	op := operation(item, model.OperationPrune)
	if _, err := store.Begin(model.Plan{ID: "prune-swap", Operations: []model.Operation{op}}); err != nil {
		t.Fatal(err)
	}
	var foreign, original string
	afterRemovalQuarantine = func(root, relative string) {
		afterRemovalQuarantine = nil
		foreign = filepath.Join(root, relative)
		original = foreign + "-original"
		if err := os.Rename(foreign, original); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(foreign, []byte("foreign"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	defer func() { afterRemovalQuarantine = nil }()
	result := a.ExecuteResource(context.Background(), item, op)
	if result.Success || !strings.Contains(result.Detail, "removal authority") {
		t.Fatalf("result=%#v", result)
	}
	if got, err := os.ReadFile(foreign); err != nil || string(got) != "foreign" {
		t.Fatalf("foreign=%q err=%v", got, err)
	}
	if got, err := os.ReadFile(original); err != nil || string(got) != "owned" {
		t.Fatalf("original=%q err=%v", got, err)
	}
}

func TestValidatedTreeTombstoneIsNotDeletedAfterSwap(t *testing.T) {
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "old"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "old", "metadata"), []byte("owned"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	digest, err := digestRootTree(root, "old")
	if err != nil {
		t.Fatal(err)
	}
	var foreign, original string
	afterTreeQuarantineValidated = func(relative string) {
		afterTreeQuarantineValidated = nil
		foreign = filepath.Join(base, relative)
		original = foreign + "-original"
		if err := os.Rename(foreign, original); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(foreign, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(foreign, "metadata"), []byte("foreign"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	defer func() { afterTreeQuarantineValidated = nil }()
	if err := removeTreeByDigest(root, "old", digest, "token"); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(foreign, "metadata")); err != nil || string(got) != "foreign" {
		t.Fatalf("foreign=%q err=%v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(original, "metadata")); err != nil || string(got) != "owned" {
		t.Fatalf("original=%q err=%v", got, err)
	}
}

func TestForgedAndStaleMetadataIntentCannotRecover(t *testing.T) {
	for _, name := range []string{"forged", "stale", "matching-fields"} {
		t.Run(name, func(t *testing.T) {
			a, item, store, destination := newFixture(t, func(context.Context, execx.Request) (execx.Result, error) { return execx.Result{}, nil })
			if err := os.MkdirAll(filepath.Join(destination, ".git"), 0o700); err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(destination, ".git", "marker")
			if err := os.WriteFile(marker, []byte("old"), 0o600); err != nil {
				t.Fatal(err)
			}
			op := operation(item, model.OperationUpgrade)
			journal, err := store.Begin(model.Plan{ID: "authorized-plan", Operations: []model.Operation{op}})
			if err != nil {
				t.Fatal(err)
			}
			intent := metadataIntent{Token: strings.Repeat("a", 16), OldDigest: strings.Repeat("b", 64), NewDigest: strings.Repeat("c", 64), Capability: strings.Repeat("d", 64), JournalID: "forged", PlanID: journal.Plan.ID, OperationID: op.ID, ResourceID: item.ID, Provider: item.Provider, Package: item.Package, Remote: item.Metadata[MetadataRemote], Commit: item.Metadata[MetadataCommit], Destination: item.Metadata[MetadataDestination]}
			if name == "stale" {
				intent.JournalID = journal.ID
				intent.PlanID = "prior-plan"
			} else if name == "matching-fields" {
				intent.JournalID = journal.ID
			}
			contents, _ := json.Marshal(intent)
			if err := os.WriteFile(filepath.Join(destination, ".git.tpod-transaction.json"), contents, 0o600); err != nil {
				t.Fatal(err)
			}
			result := a.ExecuteResource(context.Background(), item, op)
			if result.Success || !strings.Contains(result.Detail, "authorized") {
				t.Fatalf("result=%#v", result)
			}
			if got, err := os.ReadFile(marker); err != nil || string(got) != "old" {
				t.Fatalf("current metadata mutated=%q err=%v", got, err)
			}
		})
	}
}

func TestPartialBackupChangePreventsReplacement(t *testing.T) {
	a, item, store, destination := newFixture(t, func(_ context.Context, request execx.Request) (execx.Result, error) {
		args := strings.Join(request.Args, " ")
		switch {
		case strings.Contains(args, " init "):
			return execx.Result{}, os.MkdirAll(filepath.Join(request.Dir, ".git"), 0o700)
		case strings.Contains(args, "rev-parse FETCH_HEAD"):
			return execx.Result{Stdout: []byte(testCommit + "\n")}, nil
		case strings.Contains(args, " checkout "):
			return execx.Result{}, os.WriteFile(filepath.Join(request.Dir, "zinit.zsh"), []byte("installed"), 0o600)
		}
		return execx.Result{}, nil
	})
	partial := filepath.Join(destination, "partial.txt")
	if err := os.MkdirAll(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(partial, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	afterPartialBackup = func(quarantine string) {
		afterPartialBackup = nil
		if err := os.WriteFile(filepath.Join(quarantine, "partial.txt"), []byte("changed"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	defer func() { afterPartialBackup = nil }()
	op := operation(item, model.OperationRestore)
	if _, err := store.Begin(model.Plan{ID: "partial-change", Operations: []model.Operation{op}}); err != nil {
		t.Fatal(err)
	}
	result := a.ExecuteResource(context.Background(), item, op)
	if result.Success || !strings.Contains(result.Detail, "changed after backup") {
		t.Fatalf("result=%#v", result)
	}
	if got, err := os.ReadFile(partial); err != nil || string(got) != "changed" {
		t.Fatalf("partial=%q err=%v", got, err)
	}
	if _, err := os.Lstat(filepath.Join(destination, "zinit.zsh")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("replacement committed: %v", err)
	}
}

func TestSnapshotDetectsSameInodeContentMutation(t *testing.T) {
	a, item, _, destination := newFixture(t, gitInspector(t, itemRemote(), testCommit, ""))
	if err := os.MkdirAll(filepath.Join(destination, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	head := filepath.Join(destination, ".git", "HEAD")
	if err := os.WriteFile(head, []byte(testCommit+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "zinit.zsh"), []byte("zinit"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(head)
	if err != nil {
		t.Fatal(err)
	}
	afterSnapshotValidated = func() {
		afterSnapshotValidated = nil
		if err := os.WriteFile(head, []byte(strings.Repeat("f", 40)+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	defer func() { afterSnapshotValidated = nil }()
	if _, err := a.Inspect(context.Background(), item); err == nil || !strings.Contains(err.Error(), "changed after snapshot") {
		t.Fatalf("same-inode mutation err=%v", err)
	}
	after, err := os.Stat(head)
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("test replaced inode: before=%v after=%v err=%v", before, after, err)
	}
}

func TestUpdateQuarantinesWorktreeBeforeGitMutation(t *testing.T) {
	var destination string
	foreignStayedClean := false
	a, item, store, destination := newFixture(t, func(_ context.Context, request execx.Request) (execx.Result, error) {
		args := strings.Join(request.Args, " ")
		switch {
		case strings.Contains(args, "remote get-url origin"):
			return execx.Result{Stdout: []byte(itemRemote() + "\n")}, nil
		case strings.Contains(args, "rev-parse FETCH_HEAD"):
			return execx.Result{Stdout: []byte(testCommit + "\n")}, nil
		case strings.Contains(args, "rev-parse HEAD"):
			return execx.Result{Stdout: []byte(strings.Repeat("a", 40) + "\n")}, nil
		case strings.Contains(args, "ls-files -z"):
			return execx.Result{Stdout: []byte("zinit.zsh\x00")}, nil
		case strings.Contains(args, " checkout "):
			worktree := argumentValue(request.Args, "--work-tree=")
			if worktree == "" || worktree == destination {
				return execx.Result{}, errors.New("Git received mutable destination worktree")
			}
			if err := os.MkdirAll(destination, 0o700); err != nil {
				return execx.Result{}, err
			}
			if err := os.WriteFile(filepath.Join(worktree, "git-mutated"), []byte("git"), 0o600); err != nil {
				return execx.Result{}, err
			}
			_, markerErr := os.Lstat(filepath.Join(destination, "git-mutated"))
			foreignStayedClean = errors.Is(markerErr, os.ErrNotExist)
			if err := os.RemoveAll(destination); err != nil {
				return execx.Result{}, err
			}
		}
		return execx.Result{}, nil
	})
	if err := os.MkdirAll(filepath.Join(destination, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, ".git", "HEAD"), []byte(strings.Repeat("a", 40)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "zinit.zsh"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	op := operation(item, model.OperationUpgrade)
	if _, err := store.Begin(model.Plan{ID: "quarantined-update", Operations: []model.Operation{op}}); err != nil {
		t.Fatal(err)
	}
	if result := a.ExecuteResource(context.Background(), item, op); !result.Success {
		t.Fatalf("result=%#v", result)
	}
	if !foreignStayedClean {
		t.Fatal("foreign destination was mutated by Git")
	}
}

func TestStagingCapabilityRejectsForeignAndReplay(t *testing.T) {
	a, item, _, _ := newFixture(t, func(_ context.Context, request execx.Request) (execx.Result, error) {
		args := strings.Join(request.Args, " ")
		if strings.Contains(args, " init ") {
			return execx.Result{}, os.MkdirAll(filepath.Join(request.Dir, ".git"), 0o700)
		}
		if strings.Contains(args, "rev-parse FETCH_HEAD") {
			return execx.Result{Stdout: []byte(testCommit + "\n")}, nil
		}
		if strings.Contains(args, " checkout ") {
			return execx.Result{}, os.WriteFile(filepath.Join(request.Dir, "zinit.zsh"), []byte("zinit"), 0o600)
		}
		return execx.Result{}, nil
	})
	d, err := a.declaration(item)
	if err != nil {
		t.Fatal(err)
	}
	foreignPath := filepath.Join(filepath.FromSlash(d.destination) + ".tpod-foreign")
	if err := os.MkdirAll(filepath.Join(a.Home, foreignPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := a.commitStaging(d, stagingCapability{token: "foreign", path: foreignPath}); err == nil {
		t.Fatal("foreign staging capability accepted")
	}
	cap, err := a.stage(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.commitStaging(d, cap); err != nil {
		t.Fatal(err)
	}
	if err := a.commitStaging(d, cap); err == nil || !strings.Contains(err.Error(), "replayed") {
		t.Fatalf("replay err=%v", err)
	}
}

func TestInstallFetchesSignedCommitRatherThanMovingRef(t *testing.T) {
	var fetch []string
	a, item, store, _ := newFixture(t, func(_ context.Context, request execx.Request) (execx.Result, error) {
		args := strings.Join(request.Args, " ")
		if strings.Contains(args, " fetch ") {
			fetch = append([]string(nil), request.Args...)
		}
		if strings.Contains(args, "rev-parse FETCH_HEAD") {
			return execx.Result{Stdout: []byte(testCommit + "\n")}, nil
		}
		if strings.Contains(args, " checkout ") {
			return execx.Result{}, os.WriteFile(filepath.Join(request.Dir, "zinit.zsh"), []byte("zinit"), 0o600)
		}
		if strings.Contains(args, " init ") {
			return execx.Result{}, os.MkdirAll(filepath.Join(request.Dir, ".git"), 0o700)
		}
		return execx.Result{}, nil
	})
	op := operation(item, model.OperationInstall)
	if _, err := store.Begin(model.Plan{ID: "immutable-fetch", Operations: []model.Operation{op}}); err != nil {
		t.Fatal(err)
	}
	if result := a.ExecuteResource(context.Background(), item, op); !result.Success {
		t.Fatalf("result=%#v", result)
	}
	if !containsSequence(fetch, "origin", testCommit) || containsSequence(fetch, "origin", item.Metadata[MetadataRef]) {
		t.Fatalf("fetch args=%#v", fetch)
	}
}

func TestRealHomebrewGitInstallUpdateAndConfigIsolation(t *testing.T) {
	git := ""
	for _, candidate := range []string{"/opt/homebrew/bin/git", "/usr/local/bin/git", "/home/linuxbrew/.linuxbrew/bin/git"} {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			git = candidate
			break
		}
	}
	if git == "" {
		t.Skip("no allowed Homebrew Git")
	}
	base := t.TempDir()
	bare := filepath.Join(base, "origin.git")
	work := filepath.Join(base, "work")
	runGitTest(t, git, "init", "--bare", bare)
	runGitTest(t, git, "init", work)
	runGitTest(t, git, "-C", work, "config", "user.name", "Terrapod Test")
	runGitTest(t, git, "-C", work, "config", "user.email", "terrapod@example.invalid")
	if err := os.WriteFile(filepath.Join(work, "zinit.zsh"), []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, git, "-C", work, "add", "zinit.zsh")
	runGitTest(t, git, "-C", work, "commit", "-m", "one")
	first := strings.TrimSpace(runGitTest(t, git, "-C", work, "rev-parse", "HEAD"))
	if err := os.WriteFile(filepath.Join(work, "zinit.zsh"), []byte("two"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, git, "-C", work, "commit", "-am", "two")
	second := strings.TrimSpace(runGitTest(t, git, "-C", work, "rev-parse", "HEAD"))
	runGitTest(t, git, "-C", work, "branch", "-M", "main")
	runGitTest(t, git, "-C", work, "push", bare, "main")
	runGitTest(t, git, "-C", bare, "config", "uploadpack.allowReachableSHA1InWant", "true")
	if err := os.WriteFile(filepath.Join(bare, "git-daemon-export-ok"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	daemon := exec.Command(git, "daemon", "--reuseaddr", "--export-all", "--base-path="+base, "--listen=127.0.0.1", "--port="+strconv.Itoa(port), base)
	if err := daemon.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = daemon.Process.Kill(); _ = daemon.Wait() }()
	for attempts := 0; ; attempts++ {
		connection, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			connection.Close()
			break
		}
		if attempts == 50 {
			t.Fatalf("git daemon: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	remote := fmt.Sprintf("git://127.0.0.1:%d/origin.git", port)
	prior := fixed["shell.zinit"]
	fixed["shell.zinit"] = fixedSpec{remote: remote, destination: prior.destination, verify: prior.verify}
	defer func() { fixed["shell.zinit"] = prior }()
	home := t.TempDir()
	stateDir := filepath.Join(t.TempDir(), "state")
	store, err := state.Open(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	item := resourceAt(prior.destination)
	item.Metadata[MetadataRemote], item.Metadata[MetadataRef], item.Metadata[MetadataCommit] = remote, "refs/heads/main", first
	a := &Adapter{Runner: NewRunner(nil, func() int { return 501 }), Git: git, Home: home, State: store, Backup: recovery.Backup{Root: filepath.Join(t.TempDir(), "recovery"), Base: home}}
	op := operation(item, model.OperationInstall)
	journal, err := store.Begin(model.Plan{ID: "real-install", Operations: []model.Operation{op}})
	if err != nil {
		t.Fatal(err)
	}
	if result := a.ExecuteResource(context.Background(), item, op); !result.Success {
		t.Fatalf("install=%#v", result)
	}
	if err := store.Complete(journal.ID); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(runGitTest(t, git, "-C", filepath.Join(home, prior.destination), "rev-parse", "HEAD")); got != first {
		t.Fatalf("HEAD=%s want old signed %s", got, first)
	}
	checkout := filepath.Join(home, prior.destination)
	untracked := filepath.Join(checkout, "notes.txt")
	if err := os.WriteFile(untracked, []byte("user"), 0o600); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(home, "filter-ran")
	filter := filepath.Join(home, "filter.sh")
	if err := os.WriteFile(filter, []byte("#!/bin/sh\ntouch \""+marker+"\"\ncat\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	item.Metadata[MetadataCommit] = second
	upgrade := operation(item, model.OperationUpgrade)
	journal, err = store.Begin(model.Plan{ID: "real-update", Operations: []model.Operation{upgrade}})
	if err != nil {
		t.Fatal(err)
	}
	hookCalls := 0
	afterSnapshotValidated = func() {
		hookCalls++
		if hookCalls == 3 {
			runGitTest(t, git, "-C", checkout, "config", "filter.evil.process", filter)
			if err := os.WriteFile(filepath.Join(checkout, ".gitattributes"), []byte("* filter=evil\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	defer func() { afterSnapshotValidated = nil }()
	if result := a.ExecuteResource(context.Background(), item, upgrade); result.Success || !strings.Contains(result.Detail, "changed after snapshot") {
		t.Fatalf("concurrent config update=%#v hookCalls=%d", result, hookCalls)
	}
	if contents, err := os.ReadFile(filepath.Join(checkout, "zinit.zsh")); err != nil || string(contents) != "one" {
		t.Fatalf("worktree mutated=%q err=%v", contents, err)
	}
	if _, err := os.Lstat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("concurrent filter executed: %v", err)
	}
	afterSnapshotValidated = nil
	runGitTest(t, git, "-C", checkout, "config", "--unset", "filter.evil.process")
	if err := os.Remove(filepath.Join(checkout, ".gitattributes")); err != nil {
		t.Fatal(err)
	}
	beforeGitMetadataCommit = func() error { return errors.New("simulated crash before metadata commit") }
	if result := a.ExecuteResource(context.Background(), item, upgrade); result.Success || !strings.Contains(result.Detail, "simulated crash") {
		t.Fatalf("pre-metadata crash=%#v", result)
	}
	beforeGitMetadataCommit = nil
	defer func() { beforeGitMetadataCommit = nil }()
	if got := strings.TrimSpace(runGitTest(t, git, "-C", checkout, "rev-parse", "HEAD")); got != first {
		t.Fatalf("metadata advanced before commit: %s", got)
	}
	if contents, err := os.ReadFile(filepath.Join(checkout, "zinit.zsh")); err != nil || string(contents) != "two" {
		t.Fatalf("staged worktree=%q err=%v", contents, err)
	}
	afterMetadataOldRename = func() error { return errors.New("simulated crash after old rename") }
	if result := a.ExecuteResource(context.Background(), item, upgrade); result.Success || !strings.Contains(result.Detail, "after old rename") {
		t.Fatalf("old-rename crash=%#v", result)
	}
	afterMetadataOldRename = nil
	defer func() { afterMetadataOldRename = nil }()
	if _, err := os.Lstat(filepath.Join(checkout, ".git")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old-rename crash left current metadata: %v", err)
	}
	reopenedStore, err := state.Open(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	restarted := &Adapter{Runner: NewRunner(nil, func() int { return 501 }), Git: git, Home: home, State: reopenedStore, Backup: a.Backup}
	if result := restarted.ExecuteResource(context.Background(), item, upgrade); !result.Success {
		t.Fatalf("old-rename restart recovery=%#v", result)
	}
	a = restarted
	store = reopenedStore
	if err := store.Complete(journal.ID); err != nil {
		t.Fatal(err)
	}

	// Start the same signed transition again to exercise a hard stop after the
	// new metadata becomes current but before old metadata and intent cleanup.
	runGitTest(t, git, "-C", checkout, "reset", "--hard", first)
	journal, err = store.Begin(model.Plan{ID: "real-update-new-rename-crash", Operations: []model.Operation{upgrade}})
	if err != nil {
		t.Fatal(err)
	}
	afterMetadataNewRename = func() error { return errors.New("simulated crash after new rename") }
	if result := a.ExecuteResource(context.Background(), item, upgrade); result.Success || !strings.Contains(result.Detail, "after new rename") {
		t.Fatalf("new-rename crash=%#v", result)
	}
	afterMetadataNewRename = nil
	defer func() { afterMetadataNewRename = nil }()
	if got := strings.TrimSpace(runGitTest(t, git, "-C", checkout, "rev-parse", "HEAD")); got != second {
		t.Fatalf("new metadata was not current after crash: %s", got)
	}
	reopenedStore, err = state.Open(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	restarted = &Adapter{Runner: NewRunner(nil, func() int { return 501 }), Git: git, Home: home, State: reopenedStore, Backup: a.Backup}
	if result := restarted.ExecuteResource(context.Background(), item, upgrade); !result.Success {
		t.Fatalf("new-rename restart recovery=%#v", result)
	}
	a = restarted
	store = reopenedStore
	if err := store.Complete(journal.ID); err != nil {
		t.Fatal(err)
	}
	if contents, err := os.ReadFile(untracked); err != nil || string(contents) != "user" {
		t.Fatalf("untracked update content=%q err=%v", contents, err)
	}
	runGitTest(t, git, "-C", checkout, "config", "filter.evil.process", filter)
	if _, err := a.Inspect(context.Background(), item); err == nil || !strings.Contains(err.Error(), "config") {
		t.Fatalf("malicious config err=%v", err)
	}
	if _, err := os.Lstat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("filter executed: %v", err)
	}
	if err := os.Rename(filepath.Join(checkout, ".git"), filepath.Join(checkout, ".git-real")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(checkout, ".git"), []byte("gitdir: .git-real\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Inspect(context.Background(), item); err == nil || !strings.Contains(err.Error(), ".git") {
		t.Fatalf("gitfile err=%v", err)
	}
	if _, err := os.Lstat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("gitfile executed config: %v", err)
	}
}

func TestEngineOwnershipHistoricalPruneRoundTripPreservesUntracked(t *testing.T) {
	a, item, store, destination := newFixture(t, nil)
	a.Runner = runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		args := strings.Join(request.Args, " ")
		switch {
		case strings.Contains(args, " init "):
			if err := os.MkdirAll(filepath.Join(request.Dir, ".git"), 0o700); err != nil {
				return execx.Result{}, err
			}
			if err := os.WriteFile(filepath.Join(request.Dir, ".git", "config"), []byte("config"), 0o600); err != nil {
				return execx.Result{}, err
			}
		case strings.Contains(args, "rev-parse FETCH_HEAD"):
			return execx.Result{Stdout: []byte(testCommit + "\n")}, nil
		case strings.Contains(args, "rev-parse HEAD"):
			return execx.Result{Stdout: []byte(testCommit + "\n")}, nil
		case strings.Contains(args, "remote get-url origin"):
			return execx.Result{Stdout: []byte(item.Metadata[MetadataRemote] + "\n")}, nil
		case strings.Contains(args, "ls-files -z"):
			return execx.Result{Stdout: []byte("zinit.zsh\x00")}, nil
		case strings.Contains(args, " checkout "):
			if err := os.WriteFile(filepath.Join(request.Dir, "zinit.zsh"), []byte("zinit"), 0o600); err != nil {
				return execx.Result{}, err
			}
		}
		return execx.Result{}, nil
	})
	registry := resourcepkg.NewRegistry()
	if err := registry.Register(model.ResourceGitCheckout, "git", a); err != nil {
		t.Fatal(err)
	}
	engine := &reconcile.Engine{Registry: registry, State: store, LockDir: t.TempDir(), Resources: map[model.ResourceID]model.Resource{item.ID: item}, Enabled: []model.ResourceID{item.ID}, CatalogDigest: "signed", EffectiveUID: func() int { return 501 }}
	install := operation(item, model.OperationInstall)
	if summary, err := engine.Apply(context.Background(), model.Plan{ID: "engine-install", Operations: []model.Operation{install}}); err != nil || len(summary.Ready) != 1 {
		t.Fatalf("install summary=%#v err=%v", summary, err)
	}
	snapshot, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	owned := snapshot.Ownership[item.ID]
	if len(owned.Paths) != 1 {
		t.Fatalf("ownership=%#v", owned)
	}
	untracked := filepath.Join(destination, "notes.txt")
	if err := os.WriteFile(untracked, []byte("user"), 0o600); err != nil {
		t.Fatal(err)
	}
	operations, err := a.PlanHistorical(context.Background(), item, model.Observation{}, owned)
	if err != nil || len(operations) != 1 {
		t.Fatalf("historical=%#v err=%v", operations, err)
	}
	engine.Enabled = nil
	engine.ResourceDigests = map[model.ResourceID]string{item.ID: "signed"}
	if _, err := engine.Apply(context.Background(), model.Plan{ID: "engine-prune", Operations: operations}); err != nil {
		t.Fatal(err)
	}
	snapshot, _ = store.Snapshot()
	if _, exists := snapshot.Ownership[item.ID]; exists {
		t.Fatal("ownership remained after prune")
	}
	if got, err := os.ReadFile(untracked); err != nil || string(got) != "user" {
		t.Fatalf("untracked=%q err=%v", got, err)
	}
}

func runGitTest(t *testing.T, git string, args ...string) string {
	t.Helper()
	command := exec.Command(git, args...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
	return string(output)
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

func argumentValue(values []string, prefix string) string {
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			return strings.TrimPrefix(value, prefix)
		}
	}
	return ""
}

func mapsClone(values map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range values {
		out[k] = v
	}
	return out
}
