package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/juty9026/terrapod/internal/chezmoi"
	"github.com/juty9026/terrapod/internal/cli"
	"github.com/juty9026/terrapod/internal/execx"
	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/paths"
	"github.com/juty9026/terrapod/internal/planner"
	"github.com/juty9026/terrapod/internal/provider/apt"
	"github.com/juty9026/terrapod/internal/provider/homebrew"
	"github.com/juty9026/terrapod/internal/provider/legacy"
	"github.com/juty9026/terrapod/internal/provider/mise"
	"github.com/juty9026/terrapod/internal/reconcile"
	"github.com/juty9026/terrapod/internal/recovery"
	"github.com/juty9026/terrapod/internal/resource"
	archivepkg "github.com/juty9026/terrapod/internal/resource/archive"
	"github.com/juty9026/terrapod/internal/resource/gitcheckout"
	"github.com/juty9026/terrapod/internal/resource/integration"
	"github.com/juty9026/terrapod/internal/resource/jetendard"
	"github.com/juty9026/terrapod/internal/resource/managedfiles"
	"github.com/juty9026/terrapod/internal/state"
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
	runner := execx.NewRunner([]string{"LC_ALL"}, nil, os.Geteuid)
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
	registry, err := cli.ComposeRegistry(cli.AdapterSet{
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
