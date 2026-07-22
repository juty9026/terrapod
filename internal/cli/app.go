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
	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/paths"
	"github.com/juty9026/terrapod/internal/planner"
	"github.com/juty9026/terrapod/internal/state"
)

const (
	exitFailure     = 1
	exitUsage       = 2
	exitUnavailable = 69
)

type Dependencies struct {
	Stdout      io.Writer
	Stderr      io.Writer
	Geteuid     func() int
	Paths       paths.Layout
	LoadCatalog func() (catalog.Verified, error)
	LoadConfig  func() (model.Config, error)
	OpenState   func() (*state.Store, error)
	Planner     *planner.Planner
}

type reconciliation struct {
	catalog model.Catalog
	config  model.Config
	plan    model.Plan
	lock    string
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

	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		renderHelp(stdout)
		return 0
	}
	if args[0] == "version" || args[0] == "--version" {
		fmt.Fprintln(stdout, "tpod development")
		return 0
	}

	command := args[0]
	if isMutationCommand(command) {
		fmt.Fprintf(stderr, "%s is unavailable until activation\n", command)
		return exitUnavailable
	}
	if command != "plan" && command != "status" && command != "doctor" && command != "diff" {
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
	if command == "diff" {
		fmt.Fprintln(stderr, "shadow mode: managed-file adapter is not active")
		return exitUnavailable
	}

	upgrade, ok := parseReadOnlyArgs(command, args[1:], stderr)
	if !ok {
		return exitUsage
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

func parseReadOnlyArgs(command string, args []string, stderr io.Writer) (bool, bool) {
	if len(args) == 0 {
		return false, true
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

func buildReconciliation(ctx context.Context, deps Dependencies, upgrade bool) (reconciliation, error) {
	if deps.LoadConfig == nil {
		return reconciliation{}, errors.New("internal error: config loader is not configured")
	}
	cfg, err := deps.LoadConfig()
	if err != nil {
		return reconciliation{}, err
	}
	if deps.LoadCatalog == nil {
		return reconciliation{}, errors.New("internal error: signed catalog loader is not configured")
	}
	verified, err := deps.LoadCatalog()
	if err != nil {
		return reconciliation{}, fmt.Errorf("load signed catalog: %w", err)
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
	if deps.Planner == nil {
		return reconciliation{}, errors.New("internal error: planner is not configured")
	}
	profile, err := configuredProfile(cfg)
	if err != nil {
		return reconciliation{}, err
	}
	built, err := deps.Planner.Build(ctx, planner.Input{
		Catalog:       verified.Catalog,
		CatalogDigest: verified.Digest,
		Config:        cfg,
		Profile:       profile,
		Snapshot:      persisted,
		Upgrade:       upgrade,
	})
	if err != nil {
		return reconciliation{}, fmt.Errorf("build plan: %w", err)
	}
	lock, err := inspectLiveLock(deps.Paths.StateDir)
	if err != nil {
		return reconciliation{}, fmt.Errorf("inspect reconciliation lock: %w", err)
	}
	return reconciliation{catalog: verified.Catalog, config: cfg, plan: built, lock: lock}, nil
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
		resources = append(resources, candidate)
	}
	sort.Slice(resources, func(i, j int) bool { return resources[i].ID < resources[j].ID })
	return resources
}

func inspectLiveLock(stateDir string) (string, error) {
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
	info, err := root.Lstat("lock")
	if errors.Is(err, os.ErrNotExist) {
		return "none", nil
	}
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("unsafe lock path is not a directory")
	}
	ownerFile, err := root.OpenFile("lock/owner.json", os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", err
	}
	defer ownerFile.Close()
	ownerInfo, err := ownerFile.Stat()
	if err != nil {
		return "", err
	}
	if !ownerInfo.Mode().IsRegular() {
		return "", errors.New("unsafe lock owner is not a regular file")
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
	process, err := os.FindProcess(owner.PID)
	if err != nil {
		return "", err
	}
	err = process.Signal(syscall.Signal(0))
	if err == nil || errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.EPERM) {
		return fmt.Sprintf("active (PID %d, command %s)", owner.PID, owner.Command), nil
	}
	if errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH) {
		return "none (stale lock present)", nil
	}
	return "", err
}

func isMutationCommand(command string) bool {
	switch command {
	case "apply", "update", "resolve", "setup", "configure", "chezmoi":
		return true
	default:
		return false
	}
}
