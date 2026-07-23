package cli

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/juty9026/terrapod/internal/catalog"
	"github.com/juty9026/terrapod/internal/config"
	"github.com/juty9026/terrapod/internal/execx"
	migratepkg "github.com/juty9026/terrapod/internal/migrate"
	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/paths"
	"github.com/juty9026/terrapod/internal/planner"
	"github.com/juty9026/terrapod/internal/reconcile"
	"github.com/juty9026/terrapod/internal/resolve"
	"github.com/juty9026/terrapod/internal/resource"
	setuppkg "github.com/juty9026/terrapod/internal/setup"
	"github.com/juty9026/terrapod/internal/state"
	updatepkg "github.com/juty9026/terrapod/internal/update"
)

const (
	exitFailure     = 1
	exitUsage       = 2
	exitUnavailable = 69
)

var beforeOpenLockOwner = func() {}

type Dependencies struct {
	Stdin           io.Reader
	Stdout          io.Writer
	Stderr          io.Writer
	Geteuid         func() int
	Paths           paths.Layout
	LoadCatalog     func() (catalog.Verified, error)
	LoadConfig      func() (model.Config, error)
	OpenState       func() (*state.Store, error)
	Planner         *planner.Planner
	PlannerForState func(*state.Store) (*planner.Planner, error)
	LoadHistorical  func() (map[string]model.Catalog, error)
	Apply           func(context.Context, reconcile.ApplyInput) (reconcile.Summary, error)
	Diff            func(context.Context) ([]byte, error)
	Resolve         func(context.Context, model.ResourceID, io.Reader, io.Writer) (resolve.Result, error)
	Chezmoi         func(context.Context, string, []string) (execx.Result, error)
	Update          func(context.Context) (updatepkg.Result, error)
	ContinueUpdate  func(context.Context, string) (updatepkg.Result, error)
	Setup           func(context.Context) (model.Config, error)
	Configure       func(context.Context, setuppkg.Preset) (model.Config, error)
	MigrateCurrent  func(context.Context, func(model.Plan) error) (migratepkg.CurrentResult, error)
}

// AdapterSet is the composition boundary for every typed provider introduced
// by the package and managed-resource plans.
type AdapterSet struct {
	ManagementCore  resource.Adapter
	HomebrewFormula resource.Adapter
	HomebrewCask    resource.Adapter
	APT             resource.Adapter
	Mise            resource.Adapter
	ManagedFiles    resource.Adapter
	GitCheckout     resource.Adapter
	Jetendard       resource.Adapter
	JSONFields      resource.Adapter
	PlistFields     resource.Adapter
	Karabiner       resource.Adapter
}

func ComposeRegistry(adapters AdapterSet) (resource.Registry, error) {
	registry := resource.NewRegistry()
	entries := []struct {
		resourceType model.ResourceType
		provider     string
		adapter      resource.Adapter
	}{
		{model.ResourceManagementCore, "terrapod", adapters.ManagementCore},
		{model.ResourcePackage, "homebrew-formula", adapters.HomebrewFormula},
		{model.ResourcePackage, "homebrew-cask", adapters.HomebrewCask},
		{model.ResourcePackage, "apt", adapters.APT},
		{model.ResourcePackage, "mise", adapters.Mise},
		{model.ResourceManagedFiles, "chezmoi", adapters.ManagedFiles},
		{model.ResourceGitCheckout, "git", adapters.GitCheckout},
		{model.ResourceArchive, "jetendard", adapters.Jetendard},
		{model.ResourceIntegration, "json-fields", adapters.JSONFields},
		{model.ResourceIntegration, "plist-fields", adapters.PlistFields},
		{model.ResourceIntegration, "karabiner", adapters.Karabiner},
	}
	for _, entry := range entries {
		if err := registry.Register(entry.resourceType, entry.provider, entry.adapter); err != nil {
			return resource.Registry{}, fmt.Errorf("compose %s/%s adapter: %w", entry.resourceType, entry.provider, err)
		}
	}
	return registry, nil
}

type reconciliation struct {
	catalog    model.Catalog
	config     model.Config
	plan       model.Plan
	lock       string
	digest     string
	historical map[string]model.Catalog
	snapshot   model.Snapshot
	profile    model.Profile
}

func Run(ctx context.Context, args []string, deps Dependencies) int {
	stdout := deps.Stdout
	if stdout == nil {
		stdout = io.Discard
	}
	stderr := deps.Stderr
	if stderr == nil {
		stderr = io.Discard
	}

	if len(args) == 0 {
		renderHelp(stdout)
		return 0
	}
	command := args[0]
	if command == "help" || command == "--help" || command == "-h" {
		if !rejectExtraArgs(command, args[1:], stderr) {
			return exitUsage
		}
		renderHelp(stdout)
		return 0
	}
	if command == "version" || command == "--version" {
		if !rejectExtraArgs(command, args[1:], stderr) {
			return exitUsage
		}
		fmt.Fprintln(stdout, "tpod development")
		return 0
	}
	if command == "internal-continue-update" {
		if deps.Geteuid == nil || deps.Geteuid() == 0 {
			fmt.Fprintln(stderr, "Terrapod manager commands must run as a non-root user")
			return exitFailure
		}
		if len(args) != 3 || args[1] != "--journal" || args[2] == "" {
			fmt.Fprintln(stderr, "usage: tpod internal-continue-update --journal <id>")
			return exitUsage
		}
		if deps.ContinueUpdate == nil {
			fmt.Fprintln(stderr, "internal error: update continuation is not configured")
			return exitFailure
		}
		result, err := deps.ContinueUpdate(ctx, args[2])
		renderApplySummary(stdout, result.Summary)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return exitUnavailable
		}
		return 0
	}
	if command == "migrate-current" {
		if deps.Geteuid == nil || deps.Geteuid() == 0 {
			fmt.Fprintln(stderr, "Terrapod manager commands must run as a non-root user")
			return exitFailure
		}
		if len(args) != 1 {
			fmt.Fprintln(stderr, "usage: tpod migrate-current")
			return exitUsage
		}
		if deps.MigrateCurrent == nil {
			fmt.Fprintln(stderr, "internal error: legacy migration is not configured")
			return exitFailure
		}
		result, err := deps.MigrateCurrent(ctx, func(plan model.Plan) error {
			renderPlan(stdout, plan, "held by migration transaction")
			return nil
		})
		if result.AlreadyComplete {
			fmt.Fprintln(stdout, "Terrapod legacy migration is already complete.")
			return 0
		}
		renderApplySummary(stdout, result.Summary)
		if err != nil {
			fmt.Fprintln(stderr, err)
			if result.Activated {
				fmt.Fprintln(stderr, "Resolve unavailable resources and run 'tpod apply' as needed, then rerun 'install.sh --migrate' to complete legacy cleanup.")
			} else {
				fmt.Fprintln(stderr, "Resolve the reported preflight conflicts, then rerun 'install.sh --migrate'.")
			}
			return exitUnavailable
		}
		if len(result.Summary.Unavailable) != 0 {
			if result.Activated {
				fmt.Fprintln(stderr, "Resolve unavailable resources and run 'tpod apply' as needed, then rerun 'install.sh --migrate' to complete legacy cleanup.")
			} else {
				fmt.Fprintln(stderr, "Resolve the reported preflight conflicts, then rerun 'install.sh --migrate'.")
			}
			return exitUnavailable
		}
		return 0
	}

	if !isManagerCommand(command) {
		fmt.Fprintf(stderr, "unknown command %q; run 'tpod help'\n", command)
		return exitUsage
	}
	if deps.Geteuid == nil {
		fmt.Fprintln(stderr, "internal error: effective-user check is not configured")
		return exitFailure
	}
	if deps.Geteuid() == 0 {
		fmt.Fprintln(stderr, "Terrapod manager commands must run as a non-root user")
		return exitFailure
	}
	if command == "chezmoi" {
		return runChezmoi(ctx, args[1:], deps, stdout, stderr)
	}
	upgrade, ok := parseManagerArgs(command, args[1:], stderr)
	if !ok {
		return exitUsage
	}
	if command == "setup" {
		if deps.Setup == nil {
			fmt.Fprintln(stderr, "internal error: setup is not configured")
			return exitFailure
		}
		if _, err := deps.Setup(ctx); err != nil {
			fmt.Fprintln(stderr, err)
			return exitFailure
		}
		fmt.Fprintf(stdout, "Configured Terrapod in %s\n", deps.Paths.ConfigFile)
		fmt.Fprintln(stdout, "Run 'tpod apply' to reconcile these changes.")
		return 0
	}
	if command == "configure" {
		if deps.Configure == nil {
			fmt.Fprintln(stderr, "internal error: configure is not configured")
			return exitFailure
		}
		preset := setuppkg.Preset(args[1])
		if _, err := deps.Configure(ctx, preset); err != nil {
			fmt.Fprintln(stderr, err)
			return exitFailure
		}
		fmt.Fprintf(stdout, "Configured Terrapod Preset %q in %s\n", preset, deps.Paths.ConfigFile)
		fmt.Fprintln(stdout, "Run 'tpod apply' to reconcile these changes.")
		return 0
	}
	if command == "diff" {
		if deps.Diff == nil {
			fmt.Fprintln(stderr, "internal error: managed-file diff is not configured")
			return exitFailure
		}
		diff, err := deps.Diff(ctx)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return exitUnavailable
		}
		if _, err := stdout.Write(diff); err != nil {
			fmt.Fprintln(stderr, err)
			return exitFailure
		}
		return 0
	}
	if command == "resolve" {
		if deps.Resolve == nil {
			fmt.Fprintln(stderr, "internal error: conflict resolver is not configured")
			return exitFailure
		}
		input := deps.Stdin
		if input == nil {
			input = strings.NewReader("")
		}
		result, resolveErr := deps.Resolve(ctx, model.ResourceID(args[1]), input, stdout)
		if resolveErr != nil {
			fmt.Fprintln(stderr, resolveErr)
			return exitUnavailable
		}
		renderResolveResult(stdout, result)
		if len(result.Summary.Unavailable) != 0 {
			return exitUnavailable
		}
		return 0
	}
	if command == "update" {
		if deps.Update == nil {
			fmt.Fprintln(stderr, "internal error: stable update is not configured")
			return exitFailure
		}
		result, updateErr := deps.Update(ctx)
		if !result.Handoff {
			renderApplySummary(stdout, result.Summary)
		}
		if updateErr != nil {
			fmt.Fprintln(stderr, updateErr)
			return exitUnavailable
		}
		return 0
	}

	snapshot, err := buildReconciliation(ctx, deps, upgrade)
	if err != nil {
		var missing *config.ErrMissing
		if errors.As(err, &missing) {
			fmt.Fprintf(stderr, "Terrapod config is missing at %s; setup is unavailable until activation\n", missing.Path)
		} else {
			fmt.Fprintln(stderr, err)
		}
		return exitFailure
	}

	switch command {
	case "apply":
		if deps.Apply == nil {
			fmt.Fprintln(stderr, "internal error: reconciliation engine is not configured")
			return exitFailure
		}
		summary, applyErr := deps.Apply(ctx, snapshot.applyInput())
		renderApplySummary(stdout, summary)
		if applyErr != nil {
			fmt.Fprintln(stderr, applyErr)
		}
		if applyErr != nil || len(summary.Unavailable) != 0 {
			return exitUnavailable
		}
	case "plan":
		renderPlan(stdout, snapshot.plan, snapshot.lock)
	case "status":
		renderStatus(stdout, snapshot)
	case "doctor":
		unavailable := renderDoctor(stdout, snapshot)
		if unavailable {
			return exitFailure
		}
	}
	return 0
}

func runChezmoi(ctx context.Context, args []string, deps Dependencies, stdout, stderr io.Writer) int {
	if len(args) < 2 || args[0] != "--" || args[1] == "" {
		fmt.Fprintln(stderr, "usage: tpod chezmoi -- <read-only-command> [args...]")
		return exitUsage
	}
	if deps.Chezmoi == nil {
		fmt.Fprintln(stderr, "internal error: constrained chezmoi client is not configured")
		return exitFailure
	}
	result, err := deps.Chezmoi(ctx, args[1], args[2:])
	if len(result.Stdout) != 0 {
		if _, writeErr := stdout.Write(result.Stdout); writeErr != nil {
			fmt.Fprintln(stderr, writeErr)
			return exitFailure
		}
	}
	if len(result.Stderr) != 0 {
		if _, writeErr := stderr.Write(result.Stderr); writeErr != nil {
			return exitFailure
		}
	}
	if err != nil {
		if len(result.Stderr) == 0 {
			fmt.Fprintln(stderr, err)
		}
		return exitUnavailable
	}
	return 0
}

func parseManagerArgs(command string, args []string, stderr io.Writer) (bool, bool) {
	if command == "resolve" {
		if len(args) == 1 {
			return false, true
		}
		fmt.Fprintln(stderr, "usage: tpod resolve <resource>")
		return false, false
	}
	if len(args) == 0 {
		if command == "configure" {
			fmt.Fprintln(stderr, "usage: tpod configure <minimal|development|workstation>")
			return false, false
		}
		return false, true
	}
	if command == "configure" && len(args) == 1 {
		preset := setuppkg.Preset(args[0])
		if preset == setuppkg.PresetMinimal || preset == setuppkg.PresetDevelopment || preset == setuppkg.PresetWorkstation {
			return false, true
		}
		fmt.Fprintln(stderr, "usage: tpod configure <minimal|development|workstation>")
		return false, false
	}
	if command == "plan" && len(args) == 1 && args[0] == "--upgrade" {
		return true, true
	}
	fmt.Fprintf(stderr, "usage: tpod %s", command)
	if command == "plan" {
		fmt.Fprint(stderr, " [--upgrade]")
	}
	fmt.Fprintln(stderr)
	return false, false
}

func rejectExtraArgs(command string, args []string, stderr io.Writer) bool {
	if len(args) == 0 {
		return true
	}
	fmt.Fprintf(stderr, "usage: tpod %s\n", command)
	return false
}

func buildReconciliation(ctx context.Context, deps Dependencies, upgrade bool) (reconciliation, error) {
	if deps.LoadConfig == nil {
		return reconciliation{}, errors.New("internal error: config loader is not configured")
	}
	cfg, err := deps.LoadConfig()
	if err != nil {
		return reconciliation{}, err
	}
	if deps.LoadCatalog == nil {
		return reconciliation{}, errors.New("internal error: release-bound catalog loader is not configured")
	}
	verified, err := deps.LoadCatalog()
	if err != nil {
		return reconciliation{}, fmt.Errorf("load release-bound catalog: %w", err)
	}
	if deps.OpenState == nil {
		return reconciliation{}, errors.New("internal error: read-only state loader is not configured")
	}
	store, err := deps.OpenState()
	if err != nil {
		return reconciliation{}, fmt.Errorf("open state: %w", err)
	}
	if store == nil {
		return reconciliation{}, errors.New("open state: loader returned a nil store")
	}
	persisted, err := store.Snapshot()
	if err != nil {
		return reconciliation{}, fmt.Errorf("read state snapshot: %w", err)
	}
	activePlanner := deps.Planner
	if deps.PlannerForState != nil {
		activePlanner, err = deps.PlannerForState(store)
		if err != nil {
			return reconciliation{}, fmt.Errorf("compose state-bound planner: %w", err)
		}
	}
	if activePlanner == nil {
		return reconciliation{}, errors.New("internal error: planner is not configured")
	}
	historical := map[string]model.Catalog{}
	if deps.LoadHistorical != nil {
		historical, err = deps.LoadHistorical()
		if err != nil {
			return reconciliation{}, fmt.Errorf("load historical catalogs: %w", err)
		}
		if historical == nil {
			historical = map[string]model.Catalog{}
		}
	}
	profile, err := configuredProfile(cfg)
	if err != nil {
		return reconciliation{}, err
	}
	built, err := activePlanner.Build(ctx, planner.Input{
		Catalog:       verified.Catalog,
		CatalogDigest: verified.Digest,
		Config:        cfg,
		Profile:       profile,
		Snapshot:      persisted,
		Historical:    historical,
		Upgrade:       upgrade,
	})
	if err != nil {
		return reconciliation{}, fmt.Errorf("build plan: %w", err)
	}
	lock, err := inspectLiveLock(deps.Paths.StateDir, probeProcess)
	if err != nil {
		return reconciliation{}, fmt.Errorf("inspect reconciliation lock: %w", err)
	}
	return reconciliation{catalog: verified.Catalog, config: cfg, plan: built, lock: lock, digest: verified.Digest, historical: historical, snapshot: persisted, profile: profile}, nil
}

func (r reconciliation) applyInput() reconcile.ApplyInput {
	enabled := enabledResources(r.catalog, r.config)
	ids := make([]model.ResourceID, len(enabled))
	enabledSet := make(map[model.ResourceID]struct{}, len(enabled))
	for index, item := range enabled {
		ids[index] = item.ID
		enabledSet[item.ID] = struct{}{}
	}
	historical := make(map[model.ResourceID]reconcile.HistoricalResource)
	for id, owned := range r.snapshot.Ownership {
		if _, current := enabledSet[id]; current {
			continue
		}
		catalog, ok := r.historical[owned.CatalogDigest]
		if !ok {
			continue
		}
		for _, item := range catalog.Resources {
			if item.ID == id {
				historical[id] = reconcile.HistoricalResource{Resource: item, CatalogDigest: owned.CatalogDigest}
				break
			}
		}
	}
	return reconcile.ApplyInput{Plan: r.plan, CurrentResources: append([]model.Resource(nil), r.catalog.Resources...), EnabledIDs: ids, HistoricalResources: historical, CatalogDigest: r.digest, Profile: r.profile}
}

func configuredProfile(cfg model.Config) (model.Profile, error) {
	value, ok := cfg.Terrapod["profile"].(string)
	profile := model.Profile(value)
	if !ok || !profile.Supported() {
		return "", fmt.Errorf("config profile %q is not supported", value)
	}
	return profile, nil
}

func enabledResources(catalog model.Catalog, cfg model.Config) []model.Resource {
	profile, _ := configuredProfile(cfg)
	resources := make([]model.Resource, 0, len(catalog.Resources))
	for _, candidate := range catalog.Resources {
		matchesProfile := len(candidate.Profiles) == 0
		for _, allowed := range candidate.Profiles {
			if allowed == profile {
				matchesProfile = true
			}
		}
		if !matchesProfile {
			continue
		}
		if field, gated := candidate.Metadata[planner.EnabledByConfigMetadataKey]; gated {
			enabled, _ := cfg.Terrapod[field].(bool)
			if !enabled {
				continue
			}
		}
		hasAnyGate, anyEnabled := false, false
		for key := range candidate.Metadata {
			if !strings.HasPrefix(key, planner.EnabledByAnyConfigMetadataPrefix) {
				continue
			}
			hasAnyGate = true
			field := strings.TrimPrefix(key, planner.EnabledByAnyConfigMetadataPrefix)
			enabled, _ := cfg.Terrapod[field].(bool)
			anyEnabled = anyEnabled || enabled
		}
		if hasAnyGate && !anyEnabled {
			continue
		}
		resources = append(resources, candidate)
	}
	sort.Slice(resources, func(i, j int) bool { return resources[i].ID < resources[j].ID })
	return resources
}

func inspectLiveLock(stateDir string, probe func(int) error) (string, error) {
	if stateDir == "" {
		return "none", nil
	}
	root, err := os.OpenRoot(stateDir)
	if errors.Is(err, os.ErrNotExist) {
		return "none", nil
	}
	if err != nil {
		return "", err
	}
	defer root.Close()
	lockInfo, err := root.Lstat("lock")
	if errors.Is(err, os.ErrNotExist) {
		return "none", nil
	}
	if err != nil {
		return "", err
	}
	if !lockInfo.IsDir() || lockInfo.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("unsafe lock path is not a directory")
	}
	lockRoot, err := root.OpenRoot("lock")
	if err != nil {
		return "", err
	}
	defer lockRoot.Close()
	openedLockInfo, err := lockRoot.Stat(".")
	if err != nil {
		return "", err
	}
	if err := requireSameFile(lockInfo, openedLockInfo, "lock directory"); err != nil {
		return "", err
	}
	currentLockInfo, err := root.Lstat("lock")
	if err != nil {
		return "", err
	}
	if !currentLockInfo.IsDir() || currentLockInfo.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("unsafe lock path changed during inspection")
	}
	if err := requireSameFile(lockInfo, currentLockInfo, "lock directory"); err != nil {
		return "", err
	}

	ownerInfo, err := lockRoot.Lstat("owner.json")
	if err != nil {
		return "", err
	}
	if !ownerInfo.Mode().IsRegular() || ownerInfo.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("unsafe lock owner is not a regular file")
	}
	beforeOpenLockOwner()
	ownerFile, err := lockRoot.OpenFile("owner.json", os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", err
	}
	defer ownerFile.Close()
	openedOwnerInfo, err := ownerFile.Stat()
	if err != nil {
		return "", err
	}
	if err := requireSameFile(ownerInfo, openedOwnerInfo, "lock owner"); err != nil {
		return "", err
	}
	currentOwnerInfo, err := lockRoot.Lstat("owner.json")
	if err != nil {
		return "", err
	}
	if !currentOwnerInfo.Mode().IsRegular() || currentOwnerInfo.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("unsafe lock owner changed during inspection")
	}
	if err := requireSameFile(ownerInfo, currentOwnerInfo, "lock owner"); err != nil {
		return "", err
	}
	contents, err := io.ReadAll(io.LimitReader(ownerFile, 64*1024+1))
	if err != nil {
		return "", err
	}
	if len(contents) > 64*1024 {
		return "", errors.New("lock owner metadata exceeds 64 KiB")
	}
	var owner struct {
		PID       int       `json:"pid"`
		Command   string    `json:"command"`
		StartedAt time.Time `json:"startedAt"`
		Nonce     string    `json:"nonce"`
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&owner); err != nil {
		return "", err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("trailing JSON value")
		}
		return "", err
	}
	nonce, nonceErr := hex.DecodeString(owner.Nonce)
	if owner.PID <= 0 || owner.StartedAt.IsZero() || nonceErr != nil || len(nonce) != 16 || len(owner.Command) == 0 || len(owner.Command) > 128 || !utf8.ValidString(owner.Command) || strings.IndexFunc(owner.Command, unicode.IsControl) >= 0 {
		return "", errors.New("unsafe lock owner metadata")
	}
	if probe == nil {
		return "", errors.New("process probe is not configured")
	}
	err = probe(owner.PID)
	if err == nil || errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.EPERM) {
		return fmt.Sprintf("active (PID %d, command %s)", owner.PID, owner.Command), nil
	}
	if errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH) {
		return "none (stale lock present)", nil
	}
	return "", err
}

func requireSameFile(expected, actual os.FileInfo, label string) error {
	if !os.SameFile(expected, actual) {
		return fmt.Errorf("unsafe %s identity changed during inspection", label)
	}
	return nil
}

func probeProcess(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Signal(syscall.Signal(0))
}

func isMutationCommand(command string) bool {
	switch command {
	case "apply", "update", "resolve", "setup", "configure":
		return true
	default:
		return false
	}
}

func isManagerCommand(command string) bool {
	return command == "plan" || command == "status" || command == "doctor" || command == "diff" || command == "chezmoi" || isMutationCommand(command)
}
