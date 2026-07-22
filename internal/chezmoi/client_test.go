package chezmoi

import (
	"context"
	"errors"
	"os"
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
	runner := runnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		calls = append(calls, request)
		switch request.Args[8] {
		case "managed":
			return execx.Result{}, nil
		case "dump":
			return execx.Result{Stdout: []byte("{}\n")}, nil
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

	wantPrefix := []string{"--source", source, "--override-data-file", config, "--exclude", "scripts", "--destination", home}
	resolvedBinary, err := filepath.EvalSymlinks(binary)
	if err != nil {
		t.Fatal(err)
	}
	for _, call := range calls {
		if call.Path != resolvedBinary {
			t.Fatalf("path = %q, want %q", call.Path, resolvedBinary)
		}
		if !reflect.DeepEqual(call.Args[:8], wantPrefix) {
			t.Fatalf("args = %#v", call.Args)
		}
	}
	if got := calls[3].Args; !reflect.DeepEqual(got[8:], []string{"apply", "--", filepath.Join(home, ".zshrc")}) {
		t.Fatalf("apply args = %#v", got)
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
		Runner:      execx.NewRunner(nil, nil, func() int { return 501 }),
		Binary:      fixture,
		Source:      source,
		Config:      config,
		Destination: home,
	}
	result, err := c.InspectCommand(context.Background(), "status", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "--source " + source + " --override-data-file " + config + " --exclude scripts --destination " + home + " status"
	if strings.TrimSpace(string(result.Stdout)) != want {
		t.Fatalf("stdout = %q, want %q", result.Stdout, want)
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

	for _, command := range []string{"diff", "status", "managed", "cat", "data", "execute-template"} {
		if _, err := c.InspectCommand(context.Background(), command, nil); err != nil {
			t.Errorf("%s: %v", command, err)
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
	dump := `{".config/app/config":{"type":"file","contents":"hello"},".config/app/link":{"type":"symlink","target":"../config"}}`
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
	c := Client{Runner: runnerFunc(func(context.Context, execx.Request) (execx.Result, error) {
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
	for _, arg := range []string{"--source=/tmp", "-S/tmp", "--exclude", "--include=scripts", "--init", "--output=/tmp/result"} {
		if _, err := c.InspectCommand(context.Background(), "diff", []string{arg}); err == nil {
			t.Errorf("argument %q accepted", arg)
		}
	}
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
