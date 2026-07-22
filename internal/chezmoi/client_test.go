package chezmoi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/juty9026/terrapod/internal/execx"
)

type runnerFunc func(context.Context, execx.Request) (execx.Result, error)

func (f runnerFunc) Run(ctx context.Context, request execx.Request) (execx.Result, error) {
	return f(ctx, request)
}

func TestClientCommandsAlwaysUseConstrainedGlobals(t *testing.T) {
	home, source, config, binary := clientPaths(t)
	var calls []execx.Request
	var snapshotsValidated int
	runner := runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		calls = append(calls, request)
		for _, name := range []string{"--source", "--override-data-file", "--destination"} {
			if _, err := os.Stat(valueAfter(t, request.Args, name)); err != nil {
				t.Fatalf("snapshot missing during invocation: %s: %v", name, err)
			}
		}
		snapshotsValidated++
		switch commandIn(request.Args) {
		case "managed":
			return execx.Result{}, nil
		case "dump":
			return execx.Result{Stdout: []byte("{}\n")}, nil
		case "apply":
			target := request.Args[len(request.Args)-1]
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(target, []byte("desired"), 0o600); err != nil {
				t.Fatal(err)
			}
			return execx.Result{}, nil
		default:
			return execx.Result{}, nil
		}
	})
	c := Client{Runner: runner, Binary: binary, Source: source, Config: config, Destination: home}

	if _, err := c.Managed(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.TargetState(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Diff(context.Background(), []string{".zshrc"}); err != nil {
		t.Fatal(err)
	}
	if err := c.ApplyTargets(context.Background(), []string{".zshrc"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.InspectCommand(context.Background(), "status", []string{".zshrc"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.InspectCommand(context.Background(), "cat", []string{".zshrc"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.InspectCommand(context.Background(), "data", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := c.InspectCommand(context.Background(), "execute-template", []string{"literal"}); err != nil {
		t.Fatal(err)
	}

	resolvedBinary, err := filepath.EvalSymlinks(binary)
	if err != nil {
		t.Fatal(err)
	}
	for _, call := range calls {
		if call.Path != resolvedBinary {
			t.Fatalf("path = %q, want %q", call.Path, resolvedBinary)
		}
		snapshotSource := valueAfter(t, call.Args, "--source")
		snapshotConfig := valueAfter(t, call.Args, "--override-data-file")
		command := commandIn(call.Args)
		commandHome := valueAfter(t, call.Args, "--destination")
		if snapshotSource == source || snapshotConfig == config || (command == "apply" && commandHome == home) || (command != "apply" && commandHome != home) {
			t.Fatalf("invocation did not use private snapshots: %#v", call.Args)
		}
	}
	if snapshotsValidated != len(calls) {
		t.Fatalf("validated %d snapshots for %d calls", snapshotsValidated, len(calls))
	}
	for _, call := range calls {
		command := commandIn(call.Args)
		wantExclude := command == "managed" || command == "dump" || command == "diff" || command == "apply" || command == "status"
		if contains(call.Args, "--exclude") != wantExclude {
			t.Fatalf("%s exclude mismatch: %#v", command, call.Args)
		}
		if command == "diff" && (!contains(call.Args, "--no-pager") || !contains(call.Args, "--use-builtin-diff")) {
			t.Fatalf("diff is not pinned to builtin output: %#v", call.Args)
		}
	}
}

func TestTargetStateRejectsMissingFieldsAndDuplicateCleanPaths(t *testing.T) {
	for _, dump := range []string{
		`{"file":{"type":"file"}}`,
		`{"link":{"type":"symlink"}}`,
		`{"same":{"type":"file","contents":"one"},"same":{"type":"file","contents":"two"}}`,
		`{"same":{"type":"file","contents":"one"},"dir/../same":{"type":"file","contents":"two"}}`,
		`{"bad":{"type":"unknown"}}`,
	} {
		home, source, config, binary := clientPaths(t)
		c := Client{Runner: runnerFunc(func(context.Context, execx.Request) (execx.Result, error) {
			return execx.Result{Stdout: []byte(dump)}, nil
		}), Binary: binary, Source: source, Config: config, Destination: home}
		if _, err := c.TargetState(context.Background()); err == nil {
			t.Fatalf("accepted %s", dump)
		}
	}
}

func TestApplyStagesBeforeInstallingExactTarget(t *testing.T) {
	home, source, config, binary := clientPaths(t)
	target := filepath.Join(home, ".config", "app")
	var stagedRoot string
	c := Client{Runner: runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("home mutated before runner returned: %v", err)
		}
		staged := request.Args[len(request.Args)-1]
		stagedRoot = valueAfter(t, request.Args, "--destination")
		if !strings.HasPrefix(staged, stagedRoot+string(filepath.Separator)) {
			t.Fatalf("target not staged: %s", staged)
		}
		if err := os.MkdirAll(filepath.Dir(staged), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(staged, []byte("desired"), 0o640); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(filepath.Dir(staged), "undeclared"), []byte("no"), 0o600); err != nil {
			t.Fatal(err)
		}
		return execx.Result{}, nil
	}), Binary: binary, Source: source, Config: config, Destination: home}
	if err := c.ApplyTargets(context.Background(), []string{".config/app"}); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(target)
	if err != nil || string(contents) != "desired" {
		t.Fatalf("target = %q, %v", contents, err)
	}
	info, _ := os.Stat(target)
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
	if _, err := os.Lstat(filepath.Join(home, ".config", "undeclared")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("undeclared staged target installed: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(stagedRoot)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staging not cleaned: %v", err)
	}
}

func TestApplyRejectsSpecialStagedTarget(t *testing.T) {
	home, source, config, binary := clientPaths(t)
	c := Client{Runner: runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		return execx.Result{}, syscall.Mkfifo(request.Args[len(request.Args)-1], 0o600)
	}), Binary: binary, Source: source, Config: config, Destination: home}
	if err := c.ApplyTargets(context.Background(), []string{"fifo"}); err == nil {
		t.Fatal("FIFO staged target installed")
	}
	if _, err := os.Lstat(filepath.Join(home, "fifo")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("home changed: %v", err)
	}
}

func TestApplyCancellationBeforeCommitLeavesHomeUnchanged(t *testing.T) {
	home, source, config, binary := clientPaths(t)
	ctx, cancel := context.WithCancel(context.Background())
	c := Client{Runner: runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		if err := os.WriteFile(request.Args[len(request.Args)-1], []byte("desired"), 0o600); err != nil {
			t.Fatal(err)
		}
		cancel()
		return execx.Result{}, nil
	}), Binary: binary, Source: source, Config: config, Destination: home}
	if err := c.ApplyTargets(ctx, []string{"target"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(home, "target")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("home changed: %v", err)
	}
}

func TestApplyRejectsExistingSymlinkLeaf(t *testing.T) {
	home, source, config, binary := clientPaths(t)
	escape := filepath.Join(t.TempDir(), "escape")
	target := filepath.Join(home, "target")
	if err := os.Symlink(escape, target); err != nil {
		t.Fatal(err)
	}
	c := Client{Runner: runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		staged := request.Args[len(request.Args)-1]
		return execx.Result{}, os.WriteFile(staged, []byte("desired"), 0o600)
	}), Binary: binary, Source: source, Config: config, Destination: home}
	if err := c.ApplyTargets(context.Background(), []string{"target"}); err == nil {
		t.Fatal("symlink leaf replaced")
	}
	if _, err := os.Lstat(escape); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("escaped write: %v", err)
	}
	if got, err := os.Readlink(target); err != nil || got != escape {
		t.Fatalf("link = %q, %v", got, err)
	}
}

func TestApplyPreservesSafeSymlinkAndRejectsEscapingSymlink(t *testing.T) {
	for _, tc := range []struct {
		name, link string
		wantError  bool
	}{{"safe", "file", false}, {"escape", "../../outside", true}} {
		t.Run(tc.name, func(t *testing.T) {
			home, source, config, binary := clientPaths(t)
			c := Client{Runner: runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
				staged := request.Args[len(request.Args)-1]
				return execx.Result{}, os.Symlink(tc.link, staged)
			}), Binary: binary, Source: source, Config: config, Destination: home}
			err := c.ApplyTargets(context.Background(), []string{"link"})
			if (err != nil) != tc.wantError {
				t.Fatalf("error = %v", err)
			}
			if !tc.wantError {
				if got, err := os.Readlink(filepath.Join(home, "link")); err != nil || got != tc.link {
					t.Fatalf("link = %q, %v", got, err)
				}
			}
		})
	}
}

func TestApplyRunnerFailureLeavesHomeUnchangedAndCleansStaging(t *testing.T) {
	home, source, config, binary := clientPaths(t)
	var invocationRoot string
	c := Client{Runner: runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		invocationRoot = filepath.Dir(valueAfter(t, request.Args, "--destination"))
		return execx.Result{Stderr: []byte("failed")}, errors.New("exit")
	}), Binary: binary, Source: source, Config: config, Destination: home}
	if err := c.ApplyTargets(context.Background(), []string{"target"}); err == nil || !strings.Contains(err.Error(), "failed") {
		t.Fatalf("error = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(home, "target")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("home changed: %v", err)
	}
	if _, err := os.Stat(invocationRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staging remains: %v", err)
	}
}

func TestApplyDetectsOriginalConfigMutationBeforeHomeCommit(t *testing.T) {
	home, source, config, binary := clientPaths(t)
	c := Client{Runner: runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		snapshot, err := os.ReadFile(valueAfter(t, request.Args, "--override-data-file"))
		if err != nil || string(snapshot) != "{}" {
			t.Fatalf("snapshot = %q, %v", snapshot, err)
		}
		if err := os.WriteFile(config, []byte("{\"changed\":true}"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(request.Args[len(request.Args)-1], []byte("desired"), 0o600); err != nil {
			t.Fatal(err)
		}
		return execx.Result{}, nil
	}), Binary: binary, Source: source, Config: config, Destination: home}
	if err := c.ApplyTargets(context.Background(), []string{"target"}); err == nil || !strings.Contains(err.Error(), "identity changed") {
		t.Fatalf("error = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(home, "target")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("home changed: %v", err)
	}
}

func TestApplyDetectsSourceFileMutationBeforeHomeCommit(t *testing.T) {
	home, source, config, binary := clientPaths(t)
	sourceFile := filepath.Join(source, "dot_target")
	if err := os.WriteFile(sourceFile, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := Client{Runner: runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		if err := os.WriteFile(sourceFile, []byte("changed"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(request.Args[len(request.Args)-1], []byte("desired"), 0o600); err != nil {
			t.Fatal(err)
		}
		return execx.Result{}, nil
	}), Binary: binary, Source: source, Config: config, Destination: home}
	if err := c.ApplyTargets(context.Background(), []string{"target"}); err == nil || !strings.Contains(err.Error(), "identity changed") {
		t.Fatalf("error = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(home, "target")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("home changed: %v", err)
	}
}

func TestClientRejectsSymlinkInsideSourceSnapshot(t *testing.T) {
	home, source, config, binary := clientPaths(t)
	if err := os.Symlink(t.TempDir(), filepath.Join(source, "escape")); err != nil {
		t.Fatal(err)
	}
	c := Client{Runner: runnerFunc(func(context.Context, execx.Request) (execx.Result, error) {
		t.Fatal("unsafe source reached runner")
		return execx.Result{}, nil
	}), Binary: binary, Source: source, Config: config, Destination: home}
	if _, err := c.Managed(context.Background()); err == nil {
		t.Fatal("source symlink accepted")
	}
}

func TestManagedParsesNULTerminatedPaths(t *testing.T) {
	home, source, config, binary := clientPaths(t)
	c := Client{Runner: runnerFunc(func(context.Context, execx.Request) (execx.Result, error) {
		return execx.Result{Stdout: []byte(".config/one\x00space name\x00")}, nil
	}), Binary: binary, Source: source, Config: config, Destination: home}

	got, err := c.Managed(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(home, ".config/one"), filepath.Join(home, "space name")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Managed() = %#v, want %#v", got, want)
	}
}

func TestClientRunsScriptFixtureWithoutAShell(t *testing.T) {
	home, source, config, _ := clientPaths(t)
	fixture, err := filepath.Abs(filepath.Join("testdata", "chezmoi"))
	if err != nil {
		t.Fatal(err)
	}
	c := Client{
		Runner:      execx.NewRunner([]string{"HOME"}, nil, func() int { return 501 }),
		Binary:      fixture,
		Source:      source,
		Config:      config,
		Destination: home,
	}
	result, err := c.InspectCommand(context.Background(), "status", nil)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(string(result.Stdout))
	if !strings.Contains(got, "--exclude scripts") || !strings.Contains(got, "--refresh-externals=never") || !strings.HasSuffix(got, "status --") {
		t.Fatalf("stdout = %q", result.Stdout)
	}
}

func TestRealChezmoiV271TargetSchemaAndCompatibleReadCommands(t *testing.T) {
	binary, err := exec.LookPath("chezmoi")
	if err != nil {
		t.Skip("chezmoi unavailable")
	}
	version, err := exec.Command(binary, "--version").Output()
	if err != nil || !strings.Contains(string(version), "v2.71") {
		t.Skipf("requires chezmoi v2.71: %s", version)
	}
	home, source, config, _ := clientPaths(t)
	if err := os.WriteFile(filepath.Join(source, "dot_file"), []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "symlink_dot_link"), []byte(".file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "dot_home.tmpl"), []byte("{{ .chezmoi.homeDir }}"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := Client{Runner: execx.NewRunner([]string{"HOME"}, nil, func() int { return 501 }), Binary: binary, Source: source, Config: config, Destination: home}
	targets, err := c.TargetState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 3 || targets[2].Kind != "symlink" || string(targets[2].Desired) != ".file" {
		t.Fatalf("targets = %#v", targets)
	}
	for _, tc := range []struct {
		command string
		args    []string
	}{{"cat", []string{".file"}}, {"data", nil}, {"execute-template", []string{"literal"}}} {
		if _, err := c.InspectCommand(context.Background(), tc.command, tc.args); err != nil {
			t.Fatalf("%s: %v", tc.command, err)
		}
	}
	if err := c.ApplyTargets(context.Background(), []string{".home"}); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(home, ".home")); err != nil || string(got) != home {
		t.Fatalf("rendered home = %q, %v", got, err)
	}
}

func TestApplyTargetsEmptyDoesNotInvokeChezmoi(t *testing.T) {
	home, source, config, binary := clientPaths(t)
	called := false
	c := Client{Runner: runnerFunc(func(context.Context, execx.Request) (execx.Result, error) {
		called = true
		return execx.Result{}, nil
	}), Binary: binary, Source: source, Config: config, Destination: home}

	if err := c.ApplyTargets(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("empty apply invoked chezmoi")
	}
}

func TestApplyTargetsCheckedRunsPreconditionAfterStagingAndBeforeInstall(t *testing.T) {
	home, source, config, binary := clientPaths(t)
	staged := false
	c := Client{Runner: runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		target := request.Args[len(request.Args)-1]
		if err := os.WriteFile(target, []byte("desired"), 0o600); err != nil {
			t.Fatal(err)
		}
		staged = true
		return execx.Result{}, nil
	}), Binary: binary, Source: source, Config: config, Destination: home}
	sum := sha256.Sum256([]byte("desired"))
	err := c.ApplyTargetsChecked(context.Background(), []ExpectedTarget{{Path: filepath.Join(home, "target"), Kind: "file", Digest: hex.EncodeToString(sum[:])}}, func(path string) error {
		if !staged || path != filepath.Join(home, "target") {
			t.Fatalf("precondition staged=%v path=%q", staged, path)
		}
		return errors.New("content changed")
	})
	if err == nil || !strings.Contains(err.Error(), "content changed") {
		t.Fatalf("ApplyTargetsChecked error = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(home, "target")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("target mutated after failed precondition: %v", err)
	}
}

func TestApplyTargetsCheckedRejectsRenderedContentDifferentFromExpected(t *testing.T) {
	home, source, config, binary := clientPaths(t)
	c := Client{Runner: runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		target := request.Args[len(request.Args)-1]
		if err := os.WriteFile(target, []byte("changed source output"), 0o600); err != nil {
			t.Fatal(err)
		}
		return execx.Result{}, nil
	}), Binary: binary, Source: source, Config: config, Destination: home}
	sum := sha256.Sum256([]byte("planned output"))
	err := c.ApplyTargetsChecked(context.Background(), []ExpectedTarget{{Path: filepath.Join(home, "target"), Kind: "file", Digest: hex.EncodeToString(sum[:])}}, func(string) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "does not match expected") {
		t.Fatalf("error=%v", err)
	}
	if _, err := os.Lstat(filepath.Join(home, "target")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("wrong content installed: %v", err)
	}
}

func TestApplyTargetsCheckedRejectsStagedRestoreRaceBeforeTempCopy(t *testing.T) {
	home, source, config, binary := clientPaths(t)
	planned := []byte("planned")
	c := Client{Runner: runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		return execx.Result{}, os.WriteFile(request.Args[len(request.Args)-1], planned, 0o640)
	}), Binary: binary, Source: source, Config: config, Destination: home}
	sum := sha256.Sum256(planned)
	beforeStagedCopy = func(path string) {
		beforeStagedCopy = nil
		if err := os.WriteFile(path, []byte("wrong!!"), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { beforeStagedCopy = nil })
	err := c.ApplyTargetsChecked(context.Background(), []ExpectedTarget{{Path: filepath.Join(home, "target"), Kind: "file", Digest: hex.EncodeToString(sum[:])}}, func(string) error { return nil })
	if err == nil {
		t.Fatal("staged restore race installed")
	}
	if _, err := os.Lstat(filepath.Join(home, "target")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("wrong target installed: %v", err)
	}
}

func TestApplyTargetsCheckedAllowsOnlyAuthorizedSameKindSymlinkReplacement(t *testing.T) {
	home, source, config, binary := clientPaths(t)
	path := filepath.Join(home, "link")
	if err := os.Symlink("old", path); err != nil {
		t.Fatal(err)
	}
	c := Client{Runner: runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		target := request.Args[len(request.Args)-1]
		if err := os.Symlink("new", target); err != nil {
			t.Fatal(err)
		}
		return execx.Result{}, nil
	}), Binary: binary, Source: source, Config: config, Destination: home}
	sum := sha256.Sum256([]byte("new"))
	expected := []ExpectedTarget{{Path: path, Kind: "symlink", Digest: hex.EncodeToString(sum[:])}}
	if err := c.ApplyTargetsChecked(context.Background(), expected, func(got string) error {
		if got != path {
			t.Fatalf("path=%q", got)
		}
		target, err := os.Readlink(got)
		if err != nil || target != "old" {
			t.Fatalf("old link=%q,%v", target, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got, err := os.Readlink(path); err != nil || got != "new" {
		t.Fatalf("new link=%q,%v", got, err)
	}
}

func TestApplyTargetsCheckedNeverReplacesAcrossFileAndSymlinkKinds(t *testing.T) {
	for _, existingSymlink := range []bool{true, false} {
		t.Run(map[bool]string{true: "symlink-to-file", false: "file-to-symlink"}[existingSymlink], func(t *testing.T) {
			home, source, config, binary := clientPaths(t)
			path := filepath.Join(home, "target")
			if existingSymlink {
				if err := os.Symlink("old", path); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			kind, contents := "symlink", []byte("new")
			if existingSymlink {
				kind, contents = "file", []byte("new")
			}
			c := Client{Runner: runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
				staged := request.Args[len(request.Args)-1]
				if kind == "symlink" {
					return execx.Result{}, os.Symlink(string(contents), staged)
				}
				return execx.Result{}, os.WriteFile(staged, contents, 0o600)
			}), Binary: binary, Source: source, Config: config, Destination: home}
			sum := sha256.Sum256(contents)
			err := c.ApplyTargetsChecked(context.Background(), []ExpectedTarget{{Path: path, Kind: kind, Digest: hex.EncodeToString(sum[:])}}, func(string) error { return nil })
			if err == nil {
				t.Fatal("cross-kind replacement succeeded")
			}
		})
	}
}

func TestClientPreservesStderrOnFailure(t *testing.T) {
	home, source, config, binary := clientPaths(t)
	c := Client{Runner: runnerFunc(func(context.Context, execx.Request) (execx.Result, error) {
		return execx.Result{Stderr: []byte("render failed\n")}, errors.New("exit status 1")
	}), Binary: binary, Source: source, Config: config, Destination: home}

	_, err := c.Diff(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "render failed") {
		t.Fatalf("error = %v", err)
	}
}

func TestInspectCommandAllowlist(t *testing.T) {
	home, source, config, binary := clientPaths(t)
	var calls int
	c := Client{Runner: runnerFunc(func(context.Context, execx.Request) (execx.Result, error) {
		calls++
		return execx.Result{}, nil
	}), Binary: binary, Source: source, Config: config, Destination: home}

	for _, tc := range []struct {
		command string
		args    []string
	}{{"diff", nil}, {"status", nil}, {"managed", nil}, {"cat", []string{".zshrc"}}, {"data", nil}, {"execute-template", []string{"literal"}}} {
		if _, err := c.InspectCommand(context.Background(), tc.command, tc.args); err != nil {
			t.Errorf("%s: %v", tc.command, err)
		}
	}
	for _, command := range []string{"apply", "update", "init", "add", "edit", "re-add", "forget", "destroy", "git"} {
		if _, err := c.InspectCommand(context.Background(), command, nil); err == nil {
			t.Errorf("%s allowed", command)
		}
	}
	if calls != 6 {
		t.Fatalf("calls = %d, want 6", calls)
	}
}

func TestTargetStateParsesMachineReadableDump(t *testing.T) {
	home, source, config, binary := clientPaths(t)
	dump := `{ ".config/app/config":{"type":"file","contents":"hello"},".config/app/link":{"type":"symlink","linkname":"../config"}}`
	c := Client{Runner: runnerFunc(func(context.Context, execx.Request) (execx.Result, error) {
		return execx.Result{Stdout: []byte(dump)}, nil
	}), Binary: binary, Source: source, Config: config, Destination: home}

	got, err := c.TargetState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("targets = %#v", got)
	}
	if got[0].Path != filepath.Join(home, ".config/app/config") || got[0].Kind != "file" || string(got[0].Desired) != "hello" || got[0].Digest == "" {
		t.Fatalf("target[0] = %#v", got[0])
	}
	if got[1].Kind != "symlink" || string(got[1].Desired) != "../config" {
		t.Fatalf("target[1] = %#v", got[1])
	}
}

func TestTargetPathValidationRejectsEscapeAndUnsafeParents(t *testing.T) {
	tests := []struct {
		name string
		make func(t *testing.T, home string) string
	}{
		{"parent traversal", func(_ *testing.T, _ string) string { return "../escape" }},
		{"absolute escape", func(_ *testing.T, _ string) string { return "/tmp/escape" }},
		{"symlink parent", func(t *testing.T, home string) string {
			if err := os.Symlink(t.TempDir(), filepath.Join(home, "link")); err != nil {
				t.Fatal(err)
			}
			return "link/file"
		}},
		{"non-directory parent", func(t *testing.T, home string) string {
			if err := os.Mkdir(filepath.Join(home, "fifo"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(home, "fifo", "child"), nil, 0o600); err != nil {
				t.Fatal(err)
			}
			return "fifo/child/grandchild"
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home, source, config, binary := clientPaths(t)
			path := tt.make(t, home)
			dump := `{"` + path + `":{"type":"file","contents":"x"}}`
			c := Client{Runner: runnerFunc(func(context.Context, execx.Request) (execx.Result, error) {
				return execx.Result{Stdout: []byte(dump)}, nil
			}), Binary: binary, Source: source, Config: config, Destination: home}
			if _, err := c.TargetState(context.Background()); err == nil {
				t.Fatal("unsafe target accepted")
			}
		})
	}
}

func TestClientRejectsFIFOConfigWithoutOpeningItBlocking(t *testing.T) {
	home, source, config, binary := clientPaths(t)
	if err := os.Remove(config); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(config, 0o600); err != nil {
		t.Fatal(err)
	}
	c := Client{Runner: runnerFunc(func(context.Context, execx.Request) (execx.Result, error) {
		t.Fatal("FIFO config reached runner")
		return execx.Result{}, nil
	}), Binary: binary, Source: source, Config: config, Destination: home}
	if _, err := c.Managed(context.Background()); err == nil {
		t.Fatal("FIFO config accepted")
	}
}

func TestApplyFailsIfTargetParentIdentityChangesDuringInvocation(t *testing.T) {
	home, source, config, binary := clientPaths(t)
	parent := filepath.Join(home, ".config")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	c := Client{Runner: runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		target := request.Args[len(request.Args)-1]
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte("desired"), 0o600); err != nil {
			t.Fatal(err)
		}
		moved := filepath.Join(home, ".config-old")
		if err := os.Rename(parent, moved); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(parent, 0o700); err != nil {
			t.Fatal(err)
		}
		return execx.Result{}, nil
	}), Binary: binary, Source: source, Config: config, Destination: home}

	if err := c.ApplyTargets(context.Background(), []string{".config/app"}); err == nil || !strings.Contains(err.Error(), "identity changed") {
		t.Fatalf("error = %v", err)
	}
}

func TestClientRejectsUntrustedConfigurationBeforeInvocation(t *testing.T) {
	home, source, config, binary := clientPaths(t)
	called := false
	runner := runnerFunc(func(context.Context, execx.Request) (execx.Result, error) { called = true; return execx.Result{}, nil })
	cases := []Client{
		{Runner: runner, Binary: "chezmoi", Source: source, Config: config, Destination: home},
		{Runner: runner, Binary: binary, Source: "relative", Config: config, Destination: home},
		{Runner: runner, Binary: binary, Source: source, Config: "relative", Destination: home},
		{Runner: runner, Binary: binary, Source: source, Config: config, Destination: "relative"},
	}
	for _, c := range cases {
		if _, err := c.Managed(context.Background()); err == nil {
			t.Errorf("accepted %#v", c)
		}
	}
	if called {
		t.Fatal("runner called for invalid client")
	}
}

func TestInspectCommandRejectsConstraintOverrides(t *testing.T) {
	home, source, config, binary := clientPaths(t)
	c := Client{Runner: runnerFunc(func(context.Context, execx.Request) (execx.Result, error) {
		t.Fatal("unsafe passthrough invoked runner")
		return execx.Result{}, nil
	}), Binary: binary, Source: source, Config: config, Destination: home}
	for _, tc := range []struct {
		command string
		args    []string
	}{
		{"diff", []string{"--source=/tmp"}}, {"diff", []string{"-S/tmp"}}, {"diff", []string{"--exclude"}},
		{"diff", []string{"--include=scripts"}}, {"diff", []string{"--init"}}, {"diff", []string{"--output=/tmp/result"}},
		{"diff", []string{"--pager=less"}}, {"diff", []string{"--working-tree=/tmp"}}, {"diff", []string{"--cache=/tmp"}},
		{"diff", []string{"--source-path"}}, {"status", []string{"--output=/tmp/result"}}, {"managed", []string{"--output=/tmp/result"}},
		{"cat", []string{"--output=/tmp/result"}}, {"data", []string{"--output=/tmp/result"}},
		{"execute-template", []string{"--output=/tmp/result"}}, {"execute-template", []string{"{{ output \"id\" }}"}},
	} {
		if _, err := c.InspectCommand(context.Background(), tc.command, tc.args); err == nil {
			t.Errorf("%s arguments %#v accepted", tc.command, tc.args)
		}
	}
}

func commandIn(args []string) string {
	for _, arg := range args {
		switch arg {
		case "managed", "dump", "diff", "apply", "status", "cat", "data", "execute-template":
			return arg
		}
	}
	return ""
}

func valueAfter(t *testing.T, args []string, name string) string {
	t.Helper()
	for i, arg := range args {
		if arg == name && i+1 < len(args) {
			return args[i+1]
		}
	}
	t.Fatalf("missing %s in %#v", name, args)
	return ""
}

func contains(args []string, value string) bool {
	for _, arg := range args {
		if arg == value {
			return true
		}
	}
	return false
}

func clientPaths(t *testing.T) (home, source, config, binary string) {
	t.Helper()
	root := t.TempDir()
	home = filepath.Join(root, "home")
	source = filepath.Join(root, "source")
	config = filepath.Join(root, "config.json")
	binary = filepath.Join(root, "bin", "chezmoi")
	for _, dir := range []string{home, source, filepath.Dir(binary)} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range []string{config, binary} {
		if err := os.WriteFile(file, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return
}
