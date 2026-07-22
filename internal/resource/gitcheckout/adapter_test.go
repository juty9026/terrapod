package gitcheckout

import (
	"context"
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
	store, err := state.Open(filepath.Join(t.TempDir(), "state"))
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
	item.Metadata[MetadataCommit] = second
	upgrade := operation(item, model.OperationUpgrade)
	journal, err = store.Begin(model.Plan{ID: "real-update", Operations: []model.Operation{upgrade}})
	if err != nil {
		t.Fatal(err)
	}
	if result := a.ExecuteResource(context.Background(), item, upgrade); !result.Success {
		t.Fatalf("update=%#v", result)
	}
	if err := store.Complete(journal.ID); err != nil {
		t.Fatal(err)
	}
	checkout := filepath.Join(home, prior.destination)
	marker := filepath.Join(home, "filter-ran")
	filter := filepath.Join(home, "filter.sh")
	if err := os.WriteFile(filter, []byte("#!/bin/sh\ntouch \""+marker+"\"\ncat\n"), 0o700); err != nil {
		t.Fatal(err)
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

func mapsClone(values map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range values {
		out[k] = v
	}
	return out
}
