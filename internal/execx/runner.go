package execx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const (
	sudoPath    = "/usr/bin/sudo"
	OutputLimit = 4 * 1024 * 1024
)

type Request struct {
	Path      string
	Args      []string
	Dir       string
	Env       map[string]string
	Stdin     io.Reader
	Privilege bool
}

type Result struct {
	Stdout []byte
	Stderr []byte
}

type ErrOutputLimit struct {
	Streams []string
}

func (e *ErrOutputLimit) Error() string {
	return fmt.Sprintf("execx: output limit exceeded: %s", strings.Join(e.Streams, ", "))
}

func (e *ErrOutputLimit) Is(target error) bool {
	_, ok := target.(*ErrOutputLimit)
	return ok
}

type Runner struct {
	allowedEnv     map[string]struct{}
	preflight      func(context.Context) error
	effectiveUID   func() int
	commandContext func(context.Context, string, ...string) *exec.Cmd
}

func NewRunner(allowedEnv []string, preflight func(context.Context) error, effectiveUID func() int) *Runner {
	allowed := make(map[string]struct{}, len(allowedEnv))
	for _, key := range allowedEnv {
		allowed[key] = struct{}{}
	}
	if effectiveUID == nil {
		effectiveUID = os.Geteuid
	}
	return &Runner{
		allowedEnv:     allowed,
		preflight:      preflight,
		effectiveUID:   effectiveUID,
		commandContext: exec.CommandContext,
	}
}

func (r *Runner) Run(ctx context.Context, req Request) (Result, error) {
	if err := r.validate(req); err != nil {
		return Result{}, err
	}
	if r.effectiveUID() == 0 {
		return Result{}, errors.New("execx: refusing to execute as root")
	}

	path := req.Path
	args := append([]string(nil), req.Args...)
	if req.Privilege {
		if r.preflight == nil {
			return Result{}, errors.New("execx: privileged execution requires preflight")
		}
		if err := r.preflight(ctx); err != nil {
			return Result{}, fmt.Errorf("execx: privilege preflight: %w", err)
		}
		args = append([]string{"-n", "--", path}, args...)
		path = sudoPath
	}

	cmd := r.commandContext(ctx, path, args...)
	cmd.Dir = req.Dir
	cmd.Env = environment(req.Env)
	cmd.Stdin = req.Stdin
	var stdout, stderr captureBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	var executionErr error
	if ctxErr := ctx.Err(); ctxErr != nil {
		executionErr = fmt.Errorf("execx: execute %q: %w", path, ctxErr)
	} else if err != nil {
		executionErr = fmt.Errorf("execx: execute %q: %w", path, err)
	}

	limitErr := outputLimitError(stdout.exceeded, stderr.exceeded)
	return result, errors.Join(executionErr, limitErr)
}

func (r *Runner) validate(req Request) error {
	if !filepath.IsAbs(req.Path) {
		return fmt.Errorf("execx: executable path must be absolute: %q", req.Path)
	}
	if containsNUL(req.Path) {
		return errors.New("execx: executable path contains NUL")
	}
	if req.Dir != "" && !filepath.IsAbs(req.Dir) {
		return fmt.Errorf("execx: working directory must be absolute: %q", req.Dir)
	}
	if containsNUL(req.Dir) {
		return errors.New("execx: working directory contains NUL")
	}
	for _, arg := range req.Args {
		if containsNUL(arg) {
			return errors.New("execx: argument contains NUL")
		}
	}
	for key, value := range req.Env {
		if key == "" || strings.Contains(key, "=") || containsNUL(key) {
			return fmt.Errorf("execx: invalid environment key %q", key)
		}
		if containsNUL(value) {
			return fmt.Errorf("execx: environment value for %q contains NUL", key)
		}
		if _, ok := r.allowedEnv[key]; !ok {
			return fmt.Errorf("execx: environment key %q is not allowed", key)
		}
	}
	return nil
}

func environment(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	env := make([]string, 0, len(keys))
	for _, key := range keys {
		env = append(env, key+"="+values[key])
	}
	return env
}

func containsNUL(value string) bool {
	return strings.IndexByte(value, 0) >= 0
}

type captureBuffer struct {
	buffer   bytes.Buffer
	exceeded bool
}

func (b *captureBuffer) Write(p []byte) (int, error) {
	written := len(p)
	remaining := OutputLimit - b.buffer.Len()
	if remaining > len(p) {
		remaining = len(p)
	}
	if remaining > 0 {
		_, _ = b.buffer.Write(p[:remaining])
	}
	if remaining < len(p) {
		b.exceeded = true
	}
	return written, nil
}

func (b *captureBuffer) Bytes() []byte {
	return b.buffer.Bytes()
}

func outputLimitError(stdoutExceeded, stderrExceeded bool) error {
	streams := make([]string, 0, 2)
	if stdoutExceeded {
		streams = append(streams, "stdout")
	}
	if stderrExceeded {
		streams = append(streams, "stderr")
	}
	if len(streams) == 0 {
		return nil
	}
	return &ErrOutputLimit{Streams: streams}
}
