package main

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/juty9026/terrapod/internal/catalog"
	"github.com/juty9026/terrapod/internal/chezmoi"
	"github.com/juty9026/terrapod/internal/cli"
	"github.com/juty9026/terrapod/internal/config"
	"github.com/juty9026/terrapod/internal/execx"
	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/paths"
	"github.com/juty9026/terrapod/internal/planner"
	"github.com/juty9026/terrapod/internal/provider"
	"github.com/juty9026/terrapod/internal/provider/apt"
	"github.com/juty9026/terrapod/internal/provider/homebrew"
	"github.com/juty9026/terrapod/internal/provider/legacy"
	"github.com/juty9026/terrapod/internal/provider/mise"
	"github.com/juty9026/terrapod/internal/reconcile"
	"github.com/juty9026/terrapod/internal/recovery"
	"github.com/juty9026/terrapod/internal/release"
	"github.com/juty9026/terrapod/internal/resource"
	archivepkg "github.com/juty9026/terrapod/internal/resource/archive"
	"github.com/juty9026/terrapod/internal/resource/gitcheckout"
	"github.com/juty9026/terrapod/internal/resource/integration"
	"github.com/juty9026/terrapod/internal/resource/jetendard"
	"github.com/juty9026/terrapod/internal/resource/managedfiles"
	"github.com/juty9026/terrapod/internal/resource/managementcore"
	"github.com/juty9026/terrapod/internal/state"
	updatepkg "github.com/juty9026/terrapod/internal/update"
)

type productionPaths struct{ brew, git, mise string }

func fixedProviderPaths() productionPaths {
	if runtime.GOOS == "linux" {
		return productionPaths{"/home/linuxbrew/.linuxbrew/bin/brew", "/home/linuxbrew/.linuxbrew/bin/git", "/home/linuxbrew/.linuxbrew/bin/mise"}
	}
	if runtime.GOARCH == "arm64" {
		return productionPaths{"/opt/homebrew/bin/brew", "/opt/homebrew/bin/git", "/opt/homebrew/bin/mise"}
	}
	return productionPaths{"/usr/local/bin/brew", "/usr/local/bin/git", "/usr/local/bin/mise"}
}

func productionPlanner(layout paths.Layout, store *state.Store, client chezmoi.Client) (*planner.Planner, error) {
	if store == nil {
		return nil, errors.New("production composition requires state")
	}
	tools := fixedProviderPaths()
	management, err := managementcore.NewHomebrew(tools.brew, layout.HomeDir)
	if err != nil {
		return nil, err
	}
	runner := execx.NewRunner([]string{"LC_ALL"}, noninteractivePrivilege, os.Geteuid)
	formulaProvider, err := homebrew.New(homebrew.Formula, tools.brew, filepath.Join(layout.StateDir, "recovery", "homebrew"), runner, homebrew.AppPolicy{})
	if err != nil {
		return nil, err
	}
	caskProvider, err := homebrew.New(homebrew.Cask, tools.brew, filepath.Join(layout.StateDir, "recovery", "homebrew"), runner, homebrew.AppPolicy{HomeApplications: filepath.Join(layout.HomeDir, "Applications")})
	if err != nil {
		return nil, err
	}
	aptProvider, err := apt.New(apt.AptGetPath, apt.DpkgQueryPath, runner)
	if err != nil {
		return nil, err
	}
	miseData := filepath.Join(filepath.Dir(layout.DataDir), "mise")
	miseProvider, err := mise.New(tools.mise, miseData, runner)
	if err != nil {
		return nil, err
	}
	formula, err := resource.NewProviderAdapter(formulaProvider, packagePlan)
	if err != nil {
		return nil, err
	}
	cask, err := resource.NewProviderAdapter(caskProvider, packagePlan)
	if err != nil {
		return nil, err
	}
	aptAdapter, err := resource.NewProviderAdapter(aptProvider, packagePlan)
	if err != nil {
		return nil, err
	}
	miseAdapter, err := resource.NewProviderAdapter(miseProvider, packagePlan)
	if err != nil {
		return nil, err
	}
	backup := recovery.Backup{Root: filepath.Join(layout.StateDir, "recovery", "files"), Base: layout.HomeDir}
	managed := &managedfiles.Adapter{Client: client, State: store, Home: layout.HomeDir, Backup: backup}
	git := &gitcheckout.Adapter{Runner: gitcheckout.NewRunner(nil, os.Geteuid), Git: tools.git, Home: layout.HomeDir, State: store, Backup: backup}
	archive := &archivepkg.Adapter{HTTP: &http.Client{Timeout: 30 * time.Second}, CacheDir: filepath.Join(layout.CacheDir, "archives")}
	fonts := &jetendard.Adapter{Archive: archive, Home: layout.HomeDir, State: store, Recovery: filepath.Join(layout.StateDir, "recovery", "jetendard")}
	integrationRunner := execx.NewRunner(nil, nil, os.Geteuid)
	integrations := &integration.Adapter{
		Home:       layout.HomeDir,
		State:      store,
		AppRunning: productionAppRunning,
		Karabiner:  integration.KarabinerClient{Runner: integrationRunner},
	}
	legacyOptions := []legacy.Option{
		legacy.WithAPT(aptProvider, runner),
		legacy.WithAbsentHomebrew(),
		legacy.WithVendor(layout.HomeDir),
	}
	if regularPath(tools.mise) && realDirectory(miseData) {
		legacyOptions = append(legacyOptions, legacy.WithMise(tools.mise, miseData, runner))
	}
	legacyCoordinator, err := legacy.New(osPathResolver{}, legacyOptions...)
	if err != nil {
		return nil, err
	}
	profile := model.ProfileVPSShell
	if runtime.GOOS == "darwin" {
		profile = model.ProfileMacOSTerminal
	}
	formulaTransfer, err := reconcile.NewProviderTransferAdapter(formula, legacyCoordinator, profile)
	if err != nil {
		return nil, err
	}
	caskTransfer, err := reconcile.NewProviderTransferAdapter(cask, legacyCoordinator, profile)
	if err != nil {
		return nil, err
	}
	aptTransfer, err := reconcile.NewProviderTransferAdapter(aptAdapter, legacyCoordinator, profile)
	if err != nil {
		return nil, err
	}
	miseTransfer, err := reconcile.NewProviderTransferAdapter(miseAdapter, legacyCoordinator, profile)
	if err != nil {
		return nil, err
	}
	return composeProductionPlanner(cli.AdapterSet{
		ManagementCore:  management,
		HomebrewFormula: formulaTransfer,
		HomebrewCask:    caskTransfer,
		APT:             aptTransfer,
		Mise:            miseTransfer,
		ManagedFiles:    managed,
		GitCheckout:     git,
		Jetendard:       fonts,
		JSONFields:      integrations,
		PlistFields:     integrations,
		Karabiner:       integrations,
	})
}

func composeProductionPlanner(adapters cli.AdapterSet) (*planner.Planner, error) {
	registry, err := cli.ComposeRegistry(adapters)
	if err != nil {
		return nil, err
	}
	return planner.New(registry), nil
}

type osPathResolver struct{}

func (osPathResolver) ResolveCommand(command string) (string, error) { return exec.LookPath(command) }
func (osPathResolver) EvalSymlinks(path string) (string, error)      { return filepath.EvalSymlinks(path) }

func regularPath(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func realDirectory(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0
}

func packagePlan(_ context.Context, item model.Resource, observed model.Observation, owned model.Ownership) ([]model.Operation, error) {
	kind := model.OperationUpgrade
	if !observed.Present {
		kind = model.OperationInstall
	} else if owned.ResourceID == "" {
		kind = model.OperationAdopt
	}
	return []model.Operation{{
		ID:                string(kind) + "-" + string(item.ID),
		ResourceID:        item.ID,
		Kind:              kind,
		Provider:          item.Provider,
		Package:           item.Package,
		RequiresPrivilege: item.Provider == "apt",
	}}, nil
}

func productionAppRunning(name string) bool {
	if name != "Orca" {
		return true
	}
	return exec.Command("/usr/bin/pgrep", "-x", name).Run() == nil
}

type sudoPrivilege struct{}

func (sudoPrivilege) Acquire(ctx context.Context) error {
	return noninteractivePrivilege(ctx)
}

type privilegeRunner interface {
	Run(context.Context, execx.Request) (execx.Result, error)
}

func noninteractivePrivilege(ctx context.Context) error {
	return noninteractivePrivilegeWithRunner(ctx, execx.NewRunner(nil, nil, os.Geteuid))
}

func noninteractivePrivilegeWithRunner(ctx context.Context, runner privilegeRunner) error {
	_, err := runner.Run(ctx, execx.Request{Path: "/usr/bin/sudo", Args: []string{"-n", "true"}})
	if err != nil {
		return fmt.Errorf("sudo noninteractive privilege preflight: %w", err)
	}
	return nil
}

func configureSignedUpdate(deps *cli.Dependencies, layout paths.Layout, chezmoiClient chezmoi.Client, compiled map[string]ed25519.PublicKey) {
	build := func(output bool) (updatepkg.Dependencies, error) {
		store, err := state.Open(layout.StateDir)
		if err != nil {
			return updatepkg.Dependencies{}, err
		}
		proofs, err := store.TrustProofs()
		if err != nil {
			return updatepkg.Dependencies{}, err
		}
		if _, err := release.VerifyProofChain(compiled, proofs); err != nil {
			return updatepkg.Dependencies{}, err
		}
		verifier := release.Verifier{CompiledKeys: compiled, PersistedProofs: proofs}
		stager := release.Stager{ReleaseDir: layout.ReleaseDir, ActiveRelease: layout.ActiveRelease, Verifier: verifier, ExpectedPlatform: release.Platform{OS: runtime.GOOS, Arch: runtime.GOARCH}}
		configuredPlanner, err := productionPlanner(layout, store, chezmoiClient)
		if err != nil {
			return updatepkg.Dependencies{}, err
		}
		registry := configuredPlanner.Registry()
		refreshers := make([]provider.MetadataRefresher, 0, 4)
		for _, name := range []string{"homebrew-formula", "homebrew-cask", "apt", "mise"} {
			adapter, ok := registry.Lookup(model.ResourcePackage, name)
			if !ok {
				continue
			}
			if refresher, ok := adapter.(provider.MetadataRefresher); ok {
				refreshers = append(refreshers, refresher)
			}
		}
		load := func(_ context.Context, staged release.Staged) (updatepkg.Inputs, error) {
			_, verified, err := stager.Load(staged.Version)
			if err != nil {
				return updatepkg.Inputs{}, err
			}
			asset, err := verified.Manifest.CatalogAsset()
			if err != nil {
				return updatepkg.Inputs{}, err
			}
			bound, err := catalog.LoadReleaseBound(filepath.Join(staged.Path, "catalog", "resources.json"), asset.SHA256)
			if err != nil {
				return updatepkg.Inputs{}, err
			}
			cfg, err := config.Load(layout.ConfigFile)
			if err != nil {
				return updatepkg.Inputs{}, err
			}
			normalized, _, err := config.Normalize(cfg, bound.Catalog.Config)
			if err != nil {
				return updatepkg.Inputs{}, err
			}
			profileValue, ok := normalized.Terrapod["profile"].(string)
			profile := model.Profile(profileValue)
			if !ok || !profile.Supported() {
				return updatepkg.Inputs{}, fmt.Errorf("unsupported configured profile %q", profileValue)
			}
			historical, err := productionHistoricalCatalogs(store, stager, layout.ReleaseDir)
			if err != nil {
				return updatepkg.Inputs{}, err
			}
			return updatepkg.Inputs{Catalog: bound, Config: normalized, Historical: historical, Profile: profile}, nil
		}
		result := updatepkg.Dependencies{
			Releases: release.Client{HTTP: &http.Client{Timeout: 30 * time.Second}, CacheDir: layout.ReleaseCacheDir, Verifier: verifier}, Stager: stager, Platform: release.Platform{OS: runtime.GOOS, Arch: runtime.GOARCH}, Refreshers: refreshers,
			Planner: configuredPlanner, Engine: &reconcile.Engine{Registry: registry, State: store, LockDir: layout.StateDir, Privilege: sudoPrivilege{}, EffectiveUID: os.Geteuid}, State: store, LockDir: layout.StateDir,
			LoadStaged: load, CurrentVersion: stager.CurrentVersion,
			VerifyActive: func(ctx context.Context, version string) (release.Staged, release.VerifiedRelease, updatepkg.Inputs, error) {
				staged, verified, err := stager.LoadActive(version)
				if err != nil {
					return release.Staged{}, release.VerifiedRelease{}, updatepkg.Inputs{}, err
				}
				inputs, err := load(ctx, staged)
				return staged, verified, inputs, err
			},
			SelfCheck: func(ctx context.Context, binary, releaseDir, digest string) error {
				command := exec.CommandContext(ctx, binary, "internal-self-check", "--release", releaseDir, "--manifest-digest", digest)
				command.Env = []string{"HOME=" + layout.HomeDir}
				for _, name := range []string{"XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME"} {
					if value := os.Getenv(name); value != "" {
						command.Env = append(command.Env, name+"="+value)
					}
				}
				return command.Run()
			},
			PrintPlan: func(plan model.Plan) error {
				if output {
					cli.RenderUpdatePlan(deps.Stdout, plan)
				}
				return nil
			}, WriteConfig: func(cfg model.Config) error { return config.WriteAtomic(layout.ConfigFile, cfg) },
			BuildTrusted: func(value release.VerifiedRelease) (release.PersistedTrust, error) {
				current, err := store.TrustProofs()
				if err != nil {
					return release.PersistedTrust{}, err
				}
				return release.BuildPersistedTrust(compiled, current, value)
			}, ReleaseDigest: func(value release.VerifiedRelease) (string, error) { return value.Manifest.Digest() }, PersistTrusted: func(trust release.PersistedTrust) error {
				return store.PutTrustProofs(trust.Proofs)
			}, LoadTrusted: func() (release.PersistedTrust, error) {
				current, err := store.TrustProofs()
				if err != nil {
					return release.PersistedTrust{}, err
				}
				return release.VerifyProofChain(compiled, current)
			},
			Exec: func(path string, args, environment []string) error {
				return syscall.Exec(path, append([]string{path}, args...), environment)
			}, Environment: os.Environ(), HandoffToken: func() string { return os.Getenv("TPOD_UPDATE_LOCK_NONCE") },
		}
		return result, nil
	}
	deps.Update = func(ctx context.Context) (updatepkg.Result, error) {
		configured, err := build(true)
		if err != nil {
			return updatepkg.Result{}, err
		}
		return updatepkg.Run(ctx, configured)
	}
	deps.ContinueUpdate = func(ctx context.Context, journal string) (updatepkg.Result, error) {
		configured, err := build(false)
		if err != nil {
			return updatepkg.Result{}, err
		}
		return updatepkg.Continue(ctx, journal, configured)
	}
}

func productionHistoricalCatalogs(store *state.Store, stager release.Stager, releaseDir string) (map[string]model.Catalog, error) {
	snapshot, err := store.Snapshot()
	if err != nil {
		return nil, err
	}
	wanted := make(map[string]struct{}, len(snapshot.Ownership))
	for _, owned := range snapshot.Ownership {
		wanted[owned.CatalogDigest] = struct{}{}
	}
	result := make(map[string]model.Catalog)
	entries, err := os.ReadDir(releaseDir)
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		staged, verified, err := stager.Load(entry.Name())
		if err != nil {
			continue
		}
		asset, err := verified.Manifest.CatalogAsset()
		if err != nil {
			continue
		}
		bound, err := catalog.LoadReleaseBound(filepath.Join(staged.Path, "catalog", "resources.json"), asset.SHA256)
		if err != nil {
			continue
		}
		if _, ok := wanted[bound.Digest]; ok {
			result[bound.Digest] = bound.Catalog
		}
	}
	return result, nil
}

func productionActiveCatalog(layout paths.Layout, compiled map[string]ed25519.PublicKey) (catalog.Verified, error) {
	proofs, err := state.ReadTrustProofs(layout.StateDir)
	if err != nil {
		return catalog.Verified{}, err
	}
	if _, err := release.VerifyProofChain(compiled, proofs); err != nil {
		return catalog.Verified{}, err
	}
	stager := release.Stager{
		ReleaseDir:       layout.ReleaseDir,
		ActiveRelease:    layout.ActiveRelease,
		Verifier:         release.Verifier{CompiledKeys: compiled, PersistedProofs: proofs},
		ExpectedPlatform: release.Platform{OS: runtime.GOOS, Arch: runtime.GOARCH},
	}
	version, err := stager.CurrentVersion()
	if err != nil {
		return catalog.Verified{}, err
	}
	if version == "" {
		return catalog.Verified{}, errors.New("active Terrapod release is missing")
	}
	staged, verified, err := stager.LoadActive(version)
	if err != nil {
		return catalog.Verified{}, err
	}
	asset, err := verified.Manifest.CatalogAsset()
	if err != nil {
		return catalog.Verified{}, err
	}
	bound, err := catalog.LoadReleaseBound(filepath.Join(staged.Path, "catalog", "resources.json"), asset.SHA256)
	if err != nil {
		return catalog.Verified{}, err
	}
	if bound.Catalog.Release != version {
		return catalog.Verified{}, errors.New("active catalog release differs from active Terrapod release")
	}
	return bound, nil
}

func productionSelfCheck(layout paths.Layout, compiled map[string]ed25519.PublicKey, releaseDir, expectedDigest string) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return err
	}
	derived := filepath.Dir(filepath.Dir(executable))
	releaseDir, err = filepath.Abs(releaseDir)
	if err != nil || filepath.Clean(releaseDir) != derived {
		return errors.New("self-check release directory differs from executable release root")
	}
	proofs, err := state.ReadTrustProofs(layout.StateDir)
	if err != nil {
		return fmt.Errorf("self-check trust proofs: %w", err)
	}
	if _, err := release.VerifyProofChain(compiled, proofs); err != nil {
		return fmt.Errorf("self-check trust proof chain: %w", err)
	}
	verifier := release.Verifier{CompiledKeys: compiled, PersistedProofs: proofs}
	stager := release.Stager{ReleaseDir: filepath.Dir(releaseDir), ActiveRelease: layout.ActiveRelease, Verifier: verifier, ExpectedPlatform: release.Platform{OS: runtime.GOOS, Arch: runtime.GOARCH}}
	staged, verified, err := stager.Load(filepath.Base(releaseDir))
	if err != nil {
		return fmt.Errorf("self-check release: %w", err)
	}
	digest, err := verified.Manifest.Digest()
	if err != nil || digest != expectedDigest {
		return errors.New("self-check manifest digest mismatch")
	}
	asset, err := verified.Manifest.CatalogAsset()
	if err != nil {
		return err
	}
	bound, err := catalog.LoadReleaseBound(filepath.Join(staged.Path, "catalog", "resources.json"), asset.SHA256)
	if err != nil {
		return fmt.Errorf("self-check catalog: %w", err)
	}
	if bound.Catalog.Release != staged.Version {
		return errors.New("self-check catalog release mismatch")
	}
	if err := state.ValidateReadOnly(layout.StateDir); err != nil {
		return fmt.Errorf("self-check state schema: %w", err)
	}
	return nil
}
