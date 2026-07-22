package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/juty9026/terrapod/internal/catalog"
	"github.com/juty9026/terrapod/internal/cli"
	"github.com/juty9026/terrapod/internal/config"
	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/paths"
)

func main() {
	home, homeErr := os.UserHomeDir()
	var layout paths.Layout
	if homeErr == nil {
		layout = paths.Resolve(home, environment())
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	code := cli.Run(ctx, os.Args[1:], cli.Dependencies{
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
	})
	os.Exit(code)
}

func environment() map[string]string {
	env := make(map[string]string)
	for _, name := range []string{"XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME"} {
		env[name] = os.Getenv(name)
	}
	return env
}
