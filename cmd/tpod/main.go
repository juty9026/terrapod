package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/juty9026/terrapod/internal/catalog"
	"github.com/juty9026/terrapod/internal/chezmoi"
	"github.com/juty9026/terrapod/internal/cli"
	"github.com/juty9026/terrapod/internal/config"
	"github.com/juty9026/terrapod/internal/execx"
	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/paths"
	"github.com/juty9026/terrapod/internal/planner"
	"github.com/juty9026/terrapod/internal/release"
	setuppkg "github.com/juty9026/terrapod/internal/setup"
	"github.com/juty9026/terrapod/internal/state"
)

var releaseLatestEndpoint = release.DefaultLatestReleaseEndpoint

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
	if len(os.Args) > 1 && os.Args[1] == "internal-self-check" {
		if len(os.Args) != 6 || os.Args[2] != "--release" || os.Args[4] != "--manifest-digest" {
			fmt.Fprintln(os.Stderr, "usage: tpod internal-self-check --release <dir> --manifest-digest <sha256>")
			os.Exit(2)
		}
		var err error
		if homeErr == nil && os.Geteuid() != 0 {
			err = productionSelfCheck(layout, os.Args[3], os.Args[5])
		}
		if err == nil && homeErr != nil {
			err = homeErr
		}
		if err == nil && os.Geteuid() == 0 {
			err = errors.New("self-check must run as a non-root user")
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "internal-repair-stage" {
		err := repairManagementCore(layout, homeErr, os.Args[2:])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
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
			return catalog.Verified{}, errors.New("release-bound catalog is not configured in shadow build")
		},
	}
	if homeErr == nil {
		profile, profileErr := setuppkg.DetectProfile(runtime.GOOS)
		setupManager := setuppkg.Manager{
			ConfigPath: layout.ConfigFile,
			StateDir:   layout.StateDir,
			Schema: func() (model.ConfigSchema, error) {
				verified, err := deps.LoadCatalog()
				if err != nil {
					return model.ConfigSchema{}, fmt.Errorf("load setup catalog: %w", err)
				}
				return verified.Catalog.Config, nil
			},
		}
		deps.Setup = func(ctx context.Context) (model.Config, error) {
			if profileErr != nil {
				return model.Config{}, profileErr
			}
			return setupManager.Interactive(ctx, profile, setuppkg.CommandGum{Stdin: os.Stdin, Stderr: os.Stderr})
		}
		deps.Configure = func(ctx context.Context, preset setuppkg.Preset) (model.Config, error) {
			if profileErr != nil {
				return model.Config{}, profileErr
			}
			return setupManager.Configure(ctx, preset, profile)
		}
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
		deps.LoadCatalog = func() (catalog.Verified, error) {
			return productionActiveCatalog(layout)
		}
		configureStableUpdate(&deps, layout, client)
		configureCurrentMigration(&deps, layout, client)
	}
	if len(os.Args) > 1 && os.Args[1] == "internal-release-contract-check" {
		if len(os.Args) != 2 {
			fmt.Fprintln(os.Stderr, "usage: tpod internal-release-contract-check")
			os.Exit(2)
		}
		if deps.Update == nil || deps.ContinueUpdate == nil || deps.LoadCatalog == nil || deps.MigrateCurrent == nil || releaseLatestEndpoint != release.DefaultLatestReleaseEndpoint || chezmoiPathOverride != "" {
			fmt.Fprintln(os.Stderr, "stable release dependencies are not configured")
			os.Exit(1)
		}
		return
	}
	code := cli.Run(ctx, os.Args[1:], deps)
	os.Exit(code)
}

func repairManagementCore(layout paths.Layout, homeErr error, args []string) error {
	if homeErr != nil {
		return homeErr
	}
	if os.Geteuid() == 0 {
		return errors.New("repair staging must run as a non-root user")
	}
	stageOnly := len(args) == 5 && args[4] == "--stage-only"
	if (len(args) != 4 && !stageOnly) || args[0] != "--manifest-digest" || args[2] != "--release-version" {
		return errors.New("usage: tpod internal-repair-stage --manifest-digest <sha256> --release-version <version> [--stage-only]")
	}
	endpoint, err := repairReleaseEndpoint(args[3])
	if err != nil {
		return err
	}
	client := release.Client{
		HTTP:     repairHTTPClient(),
		Endpoint: endpoint,
		CacheDir: layout.ReleaseCacheDir,
	}
	return repairLatestRelease(context.Background(), layout, args[1], client.LatestStable, stageOnly)
}

func repairReleaseEndpoint(version string) (string, error) {
	if _, err := release.CompareStableVersions(version, version); err != nil {
		return "", fmt.Errorf("repair release version: %w", err)
	}
	const latestSuffix = "/latest"
	if !strings.HasSuffix(releaseLatestEndpoint, latestSuffix) {
		return "", errors.New("embedded release endpoint is not a latest-release endpoint")
	}
	return strings.TrimSuffix(releaseLatestEndpoint, latestSuffix) + "/tags/v" + version, nil
}

func repairLatestRelease(ctx context.Context, layout paths.Layout, expectedDigest string, latest func(context.Context) (release.VerifiedRelease, error), stageOnly bool) error {
	verified, err := latest(ctx)
	if err != nil {
		return err
	}
	digest, err := verified.Manifest.Digest()
	if err != nil {
		return err
	}
	if digest != expectedDigest {
		return errors.New("repair manifest digest differs from the shell-verified manifest")
	}
	stager := release.Stager{
		ReleaseDir:       layout.ReleaseDir,
		ActiveRelease:    layout.ActiveRelease,
		ExpectedPlatform: release.Platform{OS: runtime.GOOS, Arch: runtime.GOARCH},
	}
	current, err := stager.CurrentVersion()
	if err != nil {
		return err
	}
	if current != "" {
		order, err := release.CompareStableVersions(verified.Manifest.Version, current)
		if err != nil {
			return err
		}
		if order < 0 {
			return fmt.Errorf("repair release %s would downgrade current release %s", verified.Manifest.Version, current)
		}
	}
	launchers := [2]string{filepath.Join(layout.HomeDir, ".local", "bin", "tpod"), filepath.Join(layout.HomeDir, ".local", "bin", "terrapod")}
	if stageOnly {
		_, err = stager.Stage(ctx, verified, release.Platform{OS: runtime.GOOS, Arch: runtime.GOARCH})
		return err
	}
	_, err = stager.RepairAndActivate(ctx, verified, release.Platform{OS: runtime.GOOS, Arch: runtime.GOARCH}, launchers)
	return err
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
