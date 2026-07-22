package execx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRunnerPreservesArgumentsWithoutShellExpansion(t *testing.T) {
	runner := NewRunner(nil, nil, func() int { return 501 })
	want := []string{"plain", "two words", "$HOME", "*.go", "; echo unsafe"}

	result, err := runner.Run(context.Background(), helperRequest("args", want...))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	var got []string
	if err := json.Unmarshal(result.Stdout, &got); err != nil {
		t.Fatalf("decode stdout: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv = %#v, want %#v", got, want)
	}
}

func TestRunnerUsesEmptyEnvironmentBaseline(t *testing.T) {
	runner := NewRunner(nil, nil, func() int { return 501 })

	result, err := runner.Run(context.Background(), helperRequest("env"))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := strings.TrimSpace(string(result.Stdout)); got != "[]" {
		t.Fatalf("environment = %s, want []", got)
	}
}

func TestRunnerAllowsOnlyConfiguredEnvironmentKeys(t *testing.T) {
	runner := NewRunner([]string{"TPOD_TEST_ALLOWED"}, nil, func() int { return 501 })
	req := helperRequest("env")
	req.Env = map[string]string{"TPOD_TEST_ALLOWED": "explicit"}

	result, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := strings.TrimSpace(string(result.Stdout)); got != `["TPOD_TEST_ALLOWED=explicit"]` {
		t.Fatalf("environment = %s", got)
	}

	req.Env["PATH"] = "/untrusted"
	if _, err := runner.Run(context.Background(), req); err == nil || !strings.Contains(err.Error(), "PATH") {
		t.Fatalf("Run() error = %v, want disallowed PATH error", err)
	}
}

func TestRunnerCapturesOutputAndPassesStdin(t *testing.T) {
	runner := NewRunner(nil, nil, func() int { return 501 })
	req := helperRequest("stdio")
	req.Stdin = strings.NewReader("input")

	result, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := string(result.Stdout); got != "stdout:input" {
		t.Fatalf("stdout = %q", got)
	}
	if got := string(result.Stderr); got != "stderr:input" {
		t.Fatalf("stderr = %q", got)
	}
}

func TestRunnerAcceptsOutputAtLimit(t *testing.T) {
	for _, stream := range []string{"stdout", "stderr"} {
		t.Run(stream, func(t *testing.T) {
			runner := NewRunner(nil, nil, func() int { return 501 })

			result, err := runner.Run(context.Background(), helperRequest("output", stream, strconv.Itoa(OutputLimit)))
			if err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			if got := resultStream(result, stream); len(got) != OutputLimit {
				t.Fatalf("captured %s bytes = %d, want %d", stream, len(got), OutputLimit)
			}
		})
	}
}

func TestRunnerCapsAndReportsOutputOverLimit(t *testing.T) {
	for _, stream := range []string{"stdout", "stderr"} {
		t.Run(stream, func(t *testing.T) {
			runner := NewRunner(nil, nil, func() int { return 501 })

			result, err := runner.Run(context.Background(), helperRequest("output", stream, strconv.Itoa(OutputLimit+1)))
			var limitErr *ErrOutputLimit
			if !errors.As(err, &limitErr) {
				t.Fatalf("Run() error = %v, want *ErrOutputLimit", err)
			}
			if !errors.Is(err, &ErrOutputLimit{}) {
				t.Fatalf("Run() error = %v, want errors.Is output limit match", err)
			}
			if want := []string{stream}; !reflect.DeepEqual(limitErr.Streams, want) {
				t.Fatalf("streams = %#v, want %#v", limitErr.Streams, want)
			}
			if got := resultStream(result, stream); len(got) != OutputLimit {
				t.Fatalf("captured %s bytes = %d, want %d", stream, len(got), OutputLimit)
			}
		})
	}
}

func TestRunnerReportsExceededStreamsInDeterministicOrder(t *testing.T) {
	runner := NewRunner(nil, nil, func() int { return 501 })

	result, err := runner.Run(context.Background(), helperRequest("output", "both", strconv.Itoa(OutputLimit+1)))
	var limitErr *ErrOutputLimit
	if !errors.As(err, &limitErr) {
		t.Fatalf("Run() error = %v, want *ErrOutputLimit", err)
	}
	if want := []string{"stdout", "stderr"}; !reflect.DeepEqual(limitErr.Streams, want) {
		t.Fatalf("streams = %#v, want %#v", limitErr.Streams, want)
	}
	if len(result.Stdout) != OutputLimit || len(result.Stderr) != OutputLimit {
		t.Fatalf("captured lengths = (%d, %d), want (%d, %d)", len(result.Stdout), len(result.Stderr), OutputLimit, OutputLimit)
	}
}

func TestRunnerPreservesContextErrorWithOutputLimit(t *testing.T) {
	runner := NewRunner(nil, nil, func() int { return 501 })
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	result, err := runner.Run(ctx, helperRequest("output-sleep", "stdout", strconv.Itoa(OutputLimit+1)))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run() error = %v, want context deadline exceeded", err)
	}
	var limitErr *ErrOutputLimit
	if !errors.As(err, &limitErr) {
		t.Fatalf("Run() error = %v, want joined *ErrOutputLimit", err)
	}
	if got := len(result.Stdout); got != OutputLimit {
		t.Fatalf("captured stdout bytes = %d, want %d", got, OutputLimit)
	}
}

func TestRunnerUsesAbsoluteWorkingDirectory(t *testing.T) {
	runner := NewRunner(nil, nil, func() int { return 501 })
	req := helperRequest("cwd")
	req.Dir = t.TempDir()

	result, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	got, err := filepath.EvalSymlinks(strings.TrimSpace(string(result.Stdout)))
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(req.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("cwd = %q, want %q", got, want)
	}

	req.Dir = "relative"
	if _, err := runner.Run(context.Background(), req); err == nil {
		t.Fatal("Run() accepted a relative working directory")
	}
}

func TestRunnerReturnsContextCancellation(t *testing.T) {
	runner := NewRunner(nil, nil, func() int { return 501 })
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := runner.Run(ctx, helperRequest("sleep"))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run() error = %v, want context deadline exceeded", err)
	}
}

func TestRunnerRejectsRootBeforeExecution(t *testing.T) {
	runner := NewRunner(nil, nil, func() int { return 0 })
	marker := filepath.Join(t.TempDir(), "executed")

	_, err := runner.Run(context.Background(), helperRequest("marker", marker))
	if err == nil || !strings.Contains(err.Error(), "root") {
		t.Fatalf("Run() error = %v, want root rejection", err)
	}
	if _, statErr := os.Stat(marker); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("child executed: stat error = %v", statErr)
	}
}

func TestRunnerRejectsInvalidRequestStrings(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Request)
	}{
		{name: "relative path", edit: func(req *Request) { req.Path = "tool" }},
		{name: "path NUL", edit: func(req *Request) { req.Path += "\x00" }},
		{name: "argument NUL", edit: func(req *Request) { req.Args = append(req.Args, "bad\x00arg") }},
		{name: "directory NUL", edit: func(req *Request) { req.Dir = "/tmp/bad\x00dir" }},
		{name: "environment key NUL", edit: func(req *Request) { req.Env = map[string]string{"BAD\x00KEY": "value"} }},
		{name: "environment value NUL", edit: func(req *Request) { req.Env = map[string]string{"ALLOWED": "bad\x00value"} }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := NewRunner([]string{"ALLOWED", "BAD\x00KEY"}, nil, func() int { return 501 })
			req := helperRequest("args")
			tt.edit(&req)
			if _, err := runner.Run(context.Background(), req); err == nil {
				t.Fatal("Run() accepted invalid request")
			}
		})
	}
}

func TestPrivilegedRunnerRequiresSuccessfulPreflight(t *testing.T) {
	wantErr := errors.New("authorization denied")
	runner := NewRunner(nil, func(context.Context) error { return wantErr }, func() int { return 501 })
	req := helperRequest("marker", filepath.Join(t.TempDir(), "executed"))
	req.Privilege = true

	_, err := runner.Run(context.Background(), req)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want preflight error", err)
	}
	if _, statErr := os.Stat(req.Args[len(req.Args)-1]); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("child executed: stat error = %v", statErr)
	}

	runner = NewRunner(nil, nil, func() int { return 501 })
	if _, err := runner.Run(context.Background(), req); err == nil || !strings.Contains(err.Error(), "preflight") {
		t.Fatalf("Run() error = %v, want missing preflight error", err)
	}
}

func TestPrivilegedRunnerUsesNonInteractiveSudoArgv(t *testing.T) {
	runner := NewRunner(nil, func(context.Context) error { return nil }, func() int { return 501 })
	var gotPath string
	var gotArgs []string
	runner.commandContext = func(ctx context.Context, path string, args ...string) *exec.Cmd {
		gotPath = path
		gotArgs = append([]string(nil), args...)
		req := helperRequest("args")
		return exec.CommandContext(ctx, req.Path, req.Args...)
	}
	req := Request{Path: "/opt/provider/bin/tool", Args: []string{"remove", "pkg with spaces"}, Privilege: true}

	if _, err := runner.Run(context.Background(), req); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if gotPath != "/usr/bin/sudo" {
		t.Fatalf("path = %q, want /usr/bin/sudo", gotPath)
	}
	wantArgs := []string{"-n", "--", req.Path, "remove", "pkg with spaces"}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("args = %#v, want %#v", gotArgs, wantArgs)
	}
}

func helperRequest(mode string, args ...string) Request {
	path, err := os.Executable()
	if err != nil {
		panic(err)
	}
	requestArgs := []string{"-test.run=^TestExecxHelper$", "--", mode}
	requestArgs = append(requestArgs, args...)
	return Request{Path: path, Args: requestArgs}
}

func TestExecxHelper(t *testing.T) {
	separator := -1
	for i, arg := range os.Args {
		if arg == "--" {
			separator = i
			break
		}
	}
	if separator < 0 {
		return
	}
	args := os.Args[separator+1:]
	if len(args) == 0 {
		os.Exit(2)
	}

	switch args[0] {
	case "args":
		_ = json.NewEncoder(os.Stdout).Encode(args[1:])
	case "env":
		_ = json.NewEncoder(os.Stdout).Encode(os.Environ())
	case "stdio":
		input, _ := io.ReadAll(os.Stdin)
		_, _ = fmt.Fprintf(os.Stdout, "stdout:%s", input)
		_, _ = fmt.Fprintf(os.Stderr, "stderr:%s", input)
	case "cwd":
		cwd, _ := os.Getwd()
		_, _ = fmt.Fprint(os.Stdout, cwd)
	case "sleep":
		time.Sleep(10 * time.Second)
	case "output", "output-sleep":
		if len(args) != 3 {
			os.Exit(2)
		}
		count, err := strconv.Atoi(args[2])
		if err != nil {
			os.Exit(2)
		}
		var output io.Writer
		switch args[1] {
		case "stdout":
			output = os.Stdout
		case "stderr":
			output = os.Stderr
		case "both":
			chunk := bytes.Repeat([]byte("x"), count)
			if _, err := os.Stdout.Write(chunk); err != nil {
				os.Exit(2)
			}
			output = os.Stderr
		default:
			os.Exit(2)
		}
		chunk := bytes.Repeat([]byte("x"), count)
		if _, err := output.Write(chunk); err != nil {
			os.Exit(2)
		}
		if args[0] == "output-sleep" {
			time.Sleep(10 * time.Second)
		}
	case "marker":
		if len(args) != 2 {
			os.Exit(2)
		}
		if err := os.WriteFile(args[1], []byte("executed"), 0o600); err != nil {
			os.Exit(2)
		}
	default:
		os.Exit(2)
	}
	os.Exit(0)
}

func resultStream(result Result, stream string) []byte {
	if stream == "stdout" {
		return result.Stdout
	}
	return result.Stderr
}
