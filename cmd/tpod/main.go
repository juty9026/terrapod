package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/juty9026/terrapod/internal/catalog"
	"github.com/juty9026/terrapod/internal/chezmoi"
	"github.com/juty9026/terrapod/internal/cli"
	"github.com/juty9026/terrapod/internal/config"
	"github.com/juty9026/terrapod/internal/execx"
	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/paths"
	"github.com/juty9026/terrapod/internal/planner"
	"github.com/juty9026/terrapod/internal/state"
)

// chezmoiPathOverride is set only in the built-binary integration test. Normal
// releases always select the fixed Homebrew-owned executable for the target.
var chezmoiPathOverride string

func main() {
	home, homeErr := os.UserHomeDir()
	var layout paths.Layout
	if homeErr == nil {
		layout = paths.Resolve(home, environment())
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	deps := cli.Dependencies{
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
		Geteuid: os.Geteuid,
		Paths:   layout,
		LoadConfig: func() (model.Config, error) {
			if homeErr != nil {
				return model.Config{}, fmt.Errorf("resolve home directory: %w", homeErr)
			}
			return config.Load(layout.ConfigFile)
		},
		LoadCatalog: func() (catalog.Verified, error) {
			return catalog.Verified{}, errors.New("signed catalog is not configured in shadow build")
		},
	}
	if homeErr == nil {
		client := chezmoi.Client{
			Runner:      execx.NewRunner([]string{"HOME"}, nil, os.Geteuid),
			Binary:      productionChezmoiPath(),
			Source:      layout.ActiveRelease,
			Config:      layout.ConfigFile,
			Destination: layout.HomeDir,
		}
		deps.Chezmoi = client.InspectCommand
		deps.OpenState = func() (*state.Store, error) { return state.Open(layout.StateDir) }
		deps.PlannerForState = func(store *state.Store) (*planner.Planner, error) {
			return productionPlanner(layout, store, client)
		}
	}
	code := cli.Run(ctx, os.Args[1:], deps)
	os.Exit(code)
}

func productionChezmoiPath() string {
	if chezmoiPathOverride != "" {
		return chezmoiPathOverride
	}
	if runtime.GOOS == "linux" {
		return "/home/linuxbrew/.linuxbrew/bin/chezmoi"
	}
	if runtime.GOARCH == "arm64" {
		return "/opt/homebrew/bin/chezmoi"
	}
	return "/usr/local/bin/chezmoi"
}

func environment() map[string]string {
	env := make(map[string]string)
	for _, name := range []string{"XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME"} {
		env[name] = os.Getenv(name)
	}
	return env
}
