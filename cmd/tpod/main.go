package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
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
	"github.com/juty9026/terrapod/internal/release"
	"github.com/juty9026/terrapod/internal/state"
	updatepkg "github.com/juty9026/terrapod/internal/update"
)

var releaseRootKeyID string
var releaseRootPublicKey string
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
		roots, err := compiledReleaseRoots()
		if err == nil && homeErr == nil && os.Geteuid() != 0 {
			err = productionSelfCheck(layout, roots, os.Args[3], os.Args[5])
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
		if roots, err := compiledReleaseRoots(); err != nil {
			deps.Update = func(context.Context) (updatepkg.Result, error) { return updatepkg.Result{}, err }
			deps.ContinueUpdate = func(context.Context, string) (updatepkg.Result, error) { return updatepkg.Result{}, err }
		} else {
			configureSignedUpdate(&deps, layout, client, roots)
		}
	}
	if len(os.Args) > 1 && os.Args[1] == "internal-release-root-check" {
		if len(os.Args) != 2 {
			fmt.Fprintln(os.Stderr, "usage: tpod internal-release-root-check")
			os.Exit(2)
		}
		_, err := compiledReleaseRoots()
		if err == nil && (deps.Update == nil || deps.ContinueUpdate == nil) {
			err = errors.New("signed update dependencies are not configured")
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
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
	if len(args) != 2 || args[0] != "--manifest-digest" {
		return errors.New("usage: tpod internal-repair-stage --manifest-digest <sha256>")
	}
	roots, err := compiledReleaseRoots()
	if err != nil {
		return err
	}
	verifier := release.Verifier{CompiledKeys: roots}
	client := release.Client{
		HTTP:     repairHTTPClient(),
		Endpoint: releaseLatestEndpoint,
		CacheDir: layout.ReleaseCacheDir,
		Verifier: verifier,
	}
	return repairLatestRelease(context.Background(), layout, args[1], verifier, client.LatestStable)
}

func repairLatestRelease(ctx context.Context, layout paths.Layout, expectedDigest string, verifier release.Verifier, latest func(context.Context) (release.VerifiedRelease, error)) error {
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
		Verifier:         verifier,
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
	_, err = stager.RepairAndActivate(ctx, verified, release.Platform{OS: runtime.GOOS, Arch: runtime.GOARCH}, launchers)
	return err
}

func compiledReleaseRoots() (map[string]ed25519.PublicKey, error) {
	if releaseRootKeyID == "" && releaseRootPublicKey == "" {
		return nil, errors.New("signed update is unavailable: release trust root was not embedded in this build")
	}
	if releaseRootKeyID == "" || releaseRootPublicKey == "" {
		return nil, errors.New("signed update is unavailable: incomplete embedded release trust root")
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(releaseRootPublicKey)
	if err != nil || len(decoded) != ed25519.PublicKeySize || base64.StdEncoding.EncodeToString(decoded) != releaseRootPublicKey {
		return nil, errors.New("signed update is unavailable: invalid embedded release public key")
	}
	return map[string]ed25519.PublicKey{releaseRootKeyID: ed25519.PublicKey(decoded)}, nil
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
