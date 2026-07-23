package setup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type CommandGum struct {
	Binary string
	Stdin  *os.File
	Stderr *os.File
}

func (g CommandGum) ChoosePreset(ctx context.Context, available []Preset, initial Preset) (Preset, error) {
	binary, err := g.binary()
	if err != nil {
		return "", err
	}
	args := []string{"choose", "--header", "Choose a Preset", "--selected", string(initial)}
	for _, preset := range available {
		args = append(args, string(preset))
	}
	var stdout bytes.Buffer
	command := exec.CommandContext(ctx, binary, args...)
	command.Stdin, command.Stdout, command.Stderr = g.Stdin, &stdout, g.Stderr
	if err := command.Run(); err != nil {
		return "", gumError(err)
	}
	selected := Preset(strings.TrimSpace(stdout.String()))
	for _, candidate := range available {
		if selected == candidate {
			return selected, nil
		}
	}
	return "", fmt.Errorf("gum returned unavailable Preset %q", selected)
}

func (g CommandGum) Confirm(ctx context.Context, prompt string, initial bool) (bool, error) {
	binary, err := g.binary()
	if err != nil {
		return false, err
	}
	command := exec.CommandContext(ctx, binary, "confirm", prompt, fmt.Sprintf("--default=%t", initial))
	command.Stdin, command.Stdout, command.Stderr = g.Stdin, g.Stderr, g.Stderr
	err = command.Run()
	if err == nil {
		return true, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return false, nil
	}
	return false, gumError(err)
}

func (g CommandGum) binary() (string, error) {
	binary := g.Binary
	if binary == "" {
		binary = "gum"
	}
	resolved, err := exec.LookPath(binary)
	if err != nil {
		return "", errors.New("gum is required for 'tpod setup'; install the Terrapod Management Core and retry")
	}
	return resolved, nil
}

func gumError(err error) error {
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 130 {
		return ErrCancelled
	}
	return fmt.Errorf("gum setup interaction: %w", err)
}
