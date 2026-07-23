package homebrew

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/juty9026/terrapod/internal/execx"
	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/provider"
)

type Kind string

const (
	Formula Kind = "formula"
	Cask    Kind = "cask"

	artifactPathMetadata = "artifactPath"
	bundleIDMetadata     = "bundleID"
	signingIDMetadata    = "signingID"
)

var standardBrewPaths = map[string]struct{}{
	"/opt/homebrew/bin/brew":              {},
	"/usr/local/bin/brew":                 {},
	"/home/linuxbrew/.linuxbrew/bin/brew": {},
}

type Runner interface {
	Run(context.Context, execx.Request) (execx.Result, error)
}

type AppIdentity struct {
	BundleID  string
	SigningID string
}

type AppInspector interface {
	Inspect(context.Context, string) (AppIdentity, error)
}

type RunningChecker interface {
	IsRunning(context.Context, string) (bool, error)
}

type FileSystem interface {
	Lstat(string) (os.FileInfo, error)
	EvalSymlinks(string) (string, error)
	Mkdir(string, os.FileMode) error
	MkdirAll(string, os.FileMode) error
	Rename(string, string) error
}

type AppPolicy struct {
	HomeApplications string
	Inspector        AppInspector
	Running          RunningChecker
	FS               FileSystem
	Random           io.Reader
}

type Adapter struct {
	kind             Kind
	brewPath         string
	brewPrefix       string
	recoveryDir      string
	standard         bool
	runner           Runner
	homeApplications string
	inspector        AppInspector
	running          RunningChecker
	fs               FileSystem
	random           io.Reader

	mu        sync.RWMutex
	resources map[string]model.Resource
}

func New(kind Kind, brewPath, recoveryDir string, runner Runner, policy AppPolicy) (*Adapter, error) {
	if kind != Formula && kind != Cask {
		return nil, fmt.Errorf("homebrew: unsupported kind %q", kind)
	}
	if !filepath.IsAbs(brewPath) || filepath.Clean(brewPath) != brewPath {
		return nil, fmt.Errorf("homebrew: brew path must be clean and absolute: %q", brewPath)
	}
	if !filepath.IsAbs(recoveryDir) || filepath.Clean(recoveryDir) != recoveryDir {
		return nil, fmt.Errorf("homebrew: recovery directory must be clean and absolute: %q", recoveryDir)
	}
	if isNilInterface(runner) {
		return nil, errors.New("homebrew: runner is required")
	}
	if policy.FS != nil && isNilInterface(policy.FS) {
		return nil, errors.New("homebrew: filesystem must not be typed nil")
	}
	if policy.Inspector != nil && isNilInterface(policy.Inspector) {
		return nil, errors.New("homebrew: app inspector must not be typed nil")
	}
	if policy.Running != nil && isNilInterface(policy.Running) {
		return nil, errors.New("homebrew: running checker must not be typed nil")
	}
	if policy.Random != nil && isNilInterface(policy.Random) {
		return nil, errors.New("homebrew: random reader must not be typed nil")
	}
	if policy.HomeApplications != "" && (!filepath.IsAbs(policy.HomeApplications) || filepath.Clean(policy.HomeApplications) != policy.HomeApplications) {
		return nil, fmt.Errorf("homebrew: home Applications path must be clean and absolute: %q", policy.HomeApplications)
	}
	fs := policy.FS
	if fs == nil {
		fs = osFS{}
	}
	randomReader := policy.Random
	if randomReader == nil {
		randomReader = rand.Reader
	}
	_, standard := standardBrewPaths[brewPath]
	return &Adapter{
		kind:             kind,
		brewPath:         brewPath,
		brewPrefix:       filepath.Dir(filepath.Dir(brewPath)),
		recoveryDir:      recoveryDir,
		standard:         standard,
		runner:           runner,
		homeApplications: policy.HomeApplications,
		inspector:        policy.Inspector,
		running:          policy.Running,
		fs:               fs,
		random:           randomReader,
		resources:        make(map[string]model.Resource),
	}, nil
}

func (a *Adapter) Name() string {
	if a.kind == Formula {
		return "homebrew-formula"
	}
	return "homebrew-cask"
}

func (a *Adapter) RefreshMetadata(ctx context.Context) error {
	_, err := a.run(ctx, "update")
	if err != nil {
		return fmt.Errorf("homebrew: refresh metadata: %w", err)
	}
	return nil
}

func (a *Adapter) Inspect(ctx context.Context, resource model.Resource) (model.Observation, error) {
	if err := a.validateResource(resource); err != nil {
		return model.Observation{}, err
	}
	if !a.standard {
		return model.Observation{
			Present:  true,
			Provider: a.Name(),
			Package:  resource.Package,
			Paths:    map[string]string{"brew": a.brewPath},
			Healthy:  false,
			Detail:   fmt.Sprintf("legacy Homebrew executable at nonstandard path %q", a.brewPath),
		}, nil
	}

	app, err := a.appDeclaration(resource)
	if err != nil {
		return model.Observation{}, err
	}
	infoResult, err := a.run(ctx, "info", "--json=v2", resource.Package)
	if err != nil {
		return model.Observation{}, fmt.Errorf("homebrew: inspect info for %q: %w", resource.Package, err)
	}
	installed, infoVersion, err := parseInfo(infoResult.Stdout, a.kind, resource.Package)
	if err != nil {
		return model.Observation{}, fmt.Errorf("homebrew: parse info for %q: %w", resource.Package, err)
	}
	listResult, listErr := a.run(ctx, "list", "--versions", resource.Package)
	if listErr != nil && installed {
		return model.Observation{}, fmt.Errorf("homebrew: list installed %q: %w", resource.Package, listErr)
	}
	version := ""
	if listErr == nil {
		version, err = parseListVersions(listResult.Stdout, resource.Package)
		if err != nil {
			return model.Observation{}, fmt.Errorf("homebrew: parse installed versions for %q: %w", resource.Package, err)
		}
	}
	if installed != (version != "") {
		return model.Observation{}, fmt.Errorf("homebrew: inconsistent installed state for %q", resource.Package)
	}
	if installed && infoVersion != "" && infoVersion != version {
		return model.Observation{}, fmt.Errorf("homebrew: version mismatch for %q: info=%q list=%q", resource.Package, infoVersion, version)
	}

	observation := model.Observation{
		Present:  installed,
		Provider: a.Name(),
		Package:  resource.Package,
		Version:  version,
		Paths:    make(map[string]string),
		Healthy:  installed,
	}
	if installed {
		for _, command := range resource.Commands {
			path := filepath.Join(a.brewPrefix, "bin", command)
			observation.Paths[command] = path
			info, statErr := a.fs.Lstat(path)
			if statErr != nil || info.IsDir() || info.Mode().Perm()&0o111 == 0 {
				observation.Healthy = false
				observation.Detail = fmt.Sprintf("provided command %q is not an executable at %q", command, path)
				continue
			}
			resolved, evalErr := a.fs.EvalSymlinks(path)
			if evalErr != nil {
				observation.Healthy = false
				observation.Detail = fmt.Sprintf("provided command %q has an unresolved path at %q", command, path)
				continue
			}
			if !pathAtOrWithin(resolved, a.brewPrefix) {
				observation.Healthy = false
				observation.Detail = fmt.Sprintf("provided command %q resolves outside trusted Homebrew prefix %q", command, a.brewPrefix)
				continue
			}
			resolvedInfo, statErr := a.fs.Lstat(resolved)
			if statErr != nil || !resolvedInfo.Mode().IsRegular() || resolvedInfo.Mode().Perm()&0o111 == 0 {
				observation.Healthy = false
				observation.Detail = fmt.Sprintf("provided command %q does not resolve to a regular executable within %q", command, a.brewPrefix)
			}
		}
	}
	if installed && app != nil {
		identity, inspectErr := a.inspectDeclaredApp(ctx, *app)
		if inspectErr != nil {
			return model.Observation{}, inspectErr
		}
		if identity != app.identity {
			return model.Observation{}, fmt.Errorf("homebrew: declared app identity mismatch at %q", app.path)
		}
		observation.Paths["app"] = app.path
	}

	a.mu.Lock()
	a.resources[resource.Package] = cloneResource(resource)
	a.mu.Unlock()
	return observation, nil
}

func (a *Adapter) Simulate(ctx context.Context, operation model.Operation) (provider.ChangeSet, error) {
	if err := a.validateOperation(operation); err != nil {
		return provider.ChangeSet{}, err
	}
	if !a.standard {
		return provider.ChangeSet{}, fmt.Errorf("homebrew: refusing desired operation through nonstandard brew path %q", a.brewPath)
	}
	switch operation.Kind {
	case model.OperationInstall, model.OperationAdopt:
		return provider.ChangeSet{Installs: []string{operation.Package}}, nil
	case model.OperationUpgrade:
		qualifiedIdentityVerified := false
		if strings.Contains(operation.Package, "/") {
			infoResult, err := a.run(ctx, "info", "--json=v2", operation.Package)
			if err != nil {
				return provider.ChangeSet{}, fmt.Errorf("homebrew: verify canonical package %q before outdated inspection: %w", operation.Package, err)
			}
			if _, _, err := parseInfo(infoResult.Stdout, a.kind, operation.Package); err != nil {
				return provider.ChangeSet{}, fmt.Errorf("homebrew: verify canonical package %q before outdated inspection: %w", operation.Package, err)
			}
			qualifiedIdentityVerified = true
		}
		result, err := a.run(ctx, "outdated", "--json=v2", operation.Package)
		if err != nil {
			return provider.ChangeSet{}, fmt.Errorf("homebrew: inspect outdated %q: %w", operation.Package, err)
		}
		outdated, err := parseOutdated(result.Stdout, a.kind, operation.Package, qualifiedIdentityVerified)
		if err != nil {
			return provider.ChangeSet{}, fmt.Errorf("homebrew: parse outdated %q: %w", operation.Package, err)
		}
		if outdated {
			return provider.ChangeSet{Upgrades: []string{operation.Package}}, nil
		}
		return provider.ChangeSet{}, nil
	case model.OperationPrune:
		return provider.ChangeSet{Removes: []string{operation.Package}}, nil
	default:
		return provider.ChangeSet{}, fmt.Errorf("homebrew: unsupported simulation operation %q", operation.Kind)
	}
}

func (a *Adapter) Execute(ctx context.Context, operation model.Operation) error {
	if err := a.validateOperation(operation); err != nil {
		return err
	}
	if !a.standard {
		return fmt.Errorf("homebrew: refusing desired operation through nonstandard brew path %q", a.brewPath)
	}
	if a.kind == Cask && (operation.Kind == model.OperationInstall || operation.Kind == model.OperationAdopt) {
		if resource, ok := a.inspectedResource(operation.Package); ok {
			app, err := a.appDeclaration(resource)
			if err != nil {
				return err
			}
			if app != nil {
				return a.installCaskWithAdoption(ctx, operation, resource, *app)
			}
		}
	}
	args, err := a.executionArgs(operation)
	if err != nil {
		return err
	}
	_, err = a.run(ctx, args...)
	if err != nil {
		return fmt.Errorf("homebrew: execute %s for %q: %w", operation.Kind, operation.Package, err)
	}
	return nil
}

func (a *Adapter) Verify(ctx context.Context, resource model.Resource) (model.Observation, error) {
	observation, err := a.Inspect(ctx, resource)
	if err != nil {
		return model.Observation{}, err
	}
	if !observation.Present || !observation.Healthy {
		return observation, fmt.Errorf("homebrew: verification failed for %q: %s", resource.Package, observation.Detail)
	}
	return observation, nil
}

func (a *Adapter) executionArgs(operation model.Operation) ([]string, error) {
	if a.kind == Formula {
		switch operation.Kind {
		case model.OperationInstall, model.OperationAdopt:
			return []string{"install", operation.Package}, nil
		case model.OperationUpgrade:
			return []string{"upgrade", operation.Package}, nil
		case model.OperationPrune:
			return []string{"uninstall", operation.Package}, nil
		}
	} else {
		switch operation.Kind {
		case model.OperationInstall, model.OperationAdopt:
			return []string{"install", "--cask", operation.Package}, nil
		case model.OperationUpgrade:
			return []string{"upgrade", "--cask", operation.Package}, nil
		case model.OperationPrune:
			return []string{"uninstall", "--cask", operation.Package}, nil
		}
	}
	return nil, fmt.Errorf("homebrew: unsupported execution operation %q", operation.Kind)
}

func (a *Adapter) validateResource(resource model.Resource) error {
	if resource.Provider != a.Name() {
		return fmt.Errorf("homebrew: resource provider %q does not match %q", resource.Provider, a.Name())
	}
	if !safeToken(resource.Package) {
		return fmt.Errorf("homebrew: unsafe package token %q", resource.Package)
	}
	for _, command := range resource.Commands {
		if !safeCommand(command) {
			return fmt.Errorf("homebrew: unsafe provided command %q", command)
		}
	}
	return nil
}

func (a *Adapter) validateOperation(operation model.Operation) error {
	if operation.RequiresPrivilege {
		return errors.New("homebrew: privileged operations are forbidden")
	}
	if operation.Provider != a.Name() {
		return fmt.Errorf("homebrew: operation provider %q does not match %q", operation.Provider, a.Name())
	}
	if !safeToken(operation.Package) {
		return fmt.Errorf("homebrew: unsafe package token %q", operation.Package)
	}
	return nil
}

func (a *Adapter) run(ctx context.Context, args ...string) (execx.Result, error) {
	return a.runner.Run(ctx, execx.Request{Path: a.brewPath, Args: append([]string(nil), args...)})
}

type appDeclaration struct {
	path     string
	identity AppIdentity
}

func (a *Adapter) appDeclaration(resource model.Resource) (*appDeclaration, error) {
	path, hasPath := resource.Metadata[artifactPathMetadata]
	bundleID, hasBundle := resource.Metadata[bundleIDMetadata]
	signingID, hasSigning := resource.Metadata[signingIDMetadata]
	if !hasPath && !hasBundle && !hasSigning {
		return nil, nil
	}
	if a.kind != Cask || !hasPath || !hasBundle || !hasSigning || bundleID == "" || signingID == "" {
		return nil, fmt.Errorf("homebrew: app adoption for %q requires artifactPath, bundleID, and signingID", resource.Package)
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Ext(path) != ".app" {
		return nil, fmt.Errorf("homebrew: unsafe app artifact path %q", path)
	}
	if !pathWithin(path, "/Applications") && (a.homeApplications == "" || !pathWithin(path, a.homeApplications)) {
		return nil, fmt.Errorf("homebrew: app artifact %q is outside trusted Applications roots", path)
	}
	return &appDeclaration{path: path, identity: AppIdentity{BundleID: bundleID, SigningID: signingID}}, nil
}

func (a *Adapter) inspectDeclaredApp(ctx context.Context, app appDeclaration) (AppIdentity, error) {
	if a.inspector == nil {
		return AppIdentity{}, errors.New("homebrew: app inspector is required for adoption")
	}
	info, err := a.fs.Lstat(app.path)
	if err != nil {
		return AppIdentity{}, fmt.Errorf("homebrew: inspect declared app %q: %w", app.path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return AppIdentity{}, fmt.Errorf("homebrew: declared app is not an unambiguous directory at %q", app.path)
	}
	resolved, err := a.fs.EvalSymlinks(app.path)
	if err != nil || resolved != app.path {
		return AppIdentity{}, fmt.Errorf("homebrew: declared app has symlink ambiguity at %q", app.path)
	}
	identity, err := a.inspector.Inspect(ctx, app.path)
	if err != nil {
		return AppIdentity{}, fmt.Errorf("homebrew: inspect app identity at %q: %w", app.path, err)
	}
	return identity, nil
}

func (a *Adapter) installCaskWithAdoption(ctx context.Context, operation model.Operation, resource model.Resource, app appDeclaration) error {
	identity, err := a.inspectDeclaredApp(ctx, app)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if _, runErr := a.run(ctx, "install", "--cask", operation.Package); runErr != nil {
				return fmt.Errorf("homebrew: install declared app cask %q for resource %q: %w", operation.Package, resource.ID, runErr)
			}
			if _, verifyErr := a.Verify(ctx, resource); verifyErr != nil {
				return fmt.Errorf("homebrew: verify declared app cask %q for resource %q: %w", operation.Package, resource.ID, verifyErr)
			}
			return nil
		}
		return err
	}
	if identity != app.identity {
		return fmt.Errorf("homebrew: refusing to replace app with different identity at %q", app.path)
	}
	if a.running == nil {
		return errors.New("homebrew: running checker is required for app adoption")
	}
	running, err := a.running.IsRunning(ctx, app.identity.BundleID)
	if err != nil {
		return fmt.Errorf("homebrew: check running app %q: %w", app.identity.BundleID, err)
	}
	if running {
		return fmt.Errorf("homebrew: refusing to adopt running app %q", app.identity.BundleID)
	}
	if _, err := a.run(ctx, "install", "--cask", "--adopt", operation.Package); err == nil {
		_, verifyErr := a.Verify(ctx, resource)
		return verifyErr
	}

	resourceChild := string(operation.ResourceID)
	if resourceChild == "" {
		resourceChild = operation.Package
	}
	if !safeRecoveryChild(resourceChild) {
		return fmt.Errorf("homebrew: unsafe recovery resource ID %q", resourceChild)
	}
	transaction, err := a.allocateReplacementTransaction(filepath.Join(a.recoveryDir, resourceChild), filepath.Base(app.path))
	if err != nil {
		return fmt.Errorf("homebrew: allocate replacement transaction: %w", err)
	}
	if err := a.fs.Rename(app.path, transaction.backup); err != nil {
		return fmt.Errorf("homebrew: stage declared app for recovery: %w", err)
	}
	restore := func(cause error) error {
		var stageErr error
		if _, statErr := a.fs.Lstat(app.path); statErr == nil {
			moveErrors := make([]error, 0, len(transaction.failed))
			staged := false
			for _, failed := range transaction.failed {
				if moveErr := a.fs.Rename(app.path, failed); moveErr != nil {
					moveErrors = append(moveErrors, moveErr)
					continue
				}
				staged = true
				break
			}
			if !staged {
				stageErr = fmt.Errorf("homebrew: stage failed replacement after %d attempts: %w", len(transaction.failed), errors.Join(moveErrors...))
			}
		} else if !errors.Is(statErr, os.ErrNotExist) {
			stageErr = fmt.Errorf("homebrew: inspect failed replacement destination: %w", statErr)
		}
		if restoreErr := a.fs.Rename(transaction.backup, app.path); restoreErr != nil {
			return errors.Join(cause, stageErr, fmt.Errorf("homebrew: restore original app: %w", restoreErr))
		}
		return errors.Join(cause, stageErr)
	}
	if _, err := a.run(ctx, "install", "--cask", operation.Package); err != nil {
		return restore(fmt.Errorf("homebrew: install replacement cask %q: %w", operation.Package, err))
	}
	if _, err := a.Verify(ctx, resource); err != nil {
		return restore(fmt.Errorf("homebrew: verify replacement cask %q: %w", operation.Package, err))
	}
	return nil
}

type replacementTransaction struct {
	backup string
	failed []string
}

func (a *Adapter) allocateReplacementTransaction(recoveryChild, appName string) (replacementTransaction, error) {
	if err := a.fs.MkdirAll(recoveryChild, 0o700); err != nil {
		return replacementTransaction{}, fmt.Errorf("create resource recovery directory: %w", err)
	}
	var transactionDirectory string
	for attempt := 0; attempt < 8; attempt++ {
		var randomBytes [16]byte
		if _, err := io.ReadFull(a.random, randomBytes[:]); err != nil {
			return replacementTransaction{}, err
		}
		directory := filepath.Join(recoveryChild, "transaction-"+hex.EncodeToString(randomBytes[:]))
		if err := a.fs.Mkdir(directory, 0o700); err != nil {
			if errors.Is(err, os.ErrExist) {
				continue
			}
			return replacementTransaction{}, err
		}
		transactionDirectory = directory
		break
	}
	if transactionDirectory == "" {
		return replacementTransaction{}, errors.New("replacement transaction name collision limit reached")
	}
	backupDirectory := filepath.Join(transactionDirectory, "backup")
	if err := a.fs.Mkdir(backupDirectory, 0o700); err != nil {
		return replacementTransaction{}, fmt.Errorf("create transaction backup directory: %w", err)
	}
	transaction := replacementTransaction{backup: filepath.Join(backupDirectory, appName)}
	for slot := 1; slot <= 4; slot++ {
		failedDirectory := filepath.Join(transactionDirectory, fmt.Sprintf("failed-%d", slot))
		if err := a.fs.Mkdir(failedDirectory, 0o700); err != nil {
			return replacementTransaction{}, fmt.Errorf("create failed replacement slot %d: %w", slot, err)
		}
		transaction.failed = append(transaction.failed, filepath.Join(failedDirectory, appName))
	}
	return transaction, nil
}

func (a *Adapter) inspectedResource(packageName string) (model.Resource, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	resource, ok := a.resources[packageName]
	return cloneResource(resource), ok
}

func parseInfo(contents []byte, kind Kind, packageName string) (bool, string, error) {
	formulae, casks, err := decodeHomebrewEnvelope(contents)
	if err != nil {
		return false, "", err
	}
	if kind == Formula {
		if len(formulae) != 1 || len(casks) != 0 {
			return false, "", errors.New("expected exactly one formula and no casks")
		}
		if err := rejectUnknownRecordFields(formulae[0], formulaInfoFields); err != nil {
			return false, "", fmt.Errorf("formula record: %w", err)
		}
		var item struct {
			Name      string `json:"name"`
			FullName  string `json:"full_name"`
			Installed []struct {
				Version string `json:"version"`
			} `json:"installed"`
		}
		if err := json.Unmarshal(formulae[0], &item); err != nil {
			return false, "", err
		}
		if !matchesPackage(item.Name, item.FullName, packageName) {
			return false, "", fmt.Errorf("formula package mismatch: got %q/%q", item.Name, item.FullName)
		}
		if len(item.Installed) > 1 {
			return false, "", errors.New("multiple installed formula versions are ambiguous")
		}
		if len(item.Installed) == 0 {
			return false, "", nil
		}
		if item.Installed[0].Version == "" {
			return false, "", errors.New("installed formula version is empty")
		}
		return true, item.Installed[0].Version, nil
	}
	if len(casks) != 1 || len(formulae) != 0 {
		return false, "", errors.New("expected exactly one cask and no formulae")
	}
	if err := rejectUnknownRecordFields(casks[0], caskInfoFields); err != nil {
		return false, "", fmt.Errorf("cask record: %w", err)
	}
	var item struct {
		Token     string          `json:"token"`
		FullToken string          `json:"full_token"`
		Installed json.RawMessage `json:"installed"`
	}
	if err := json.Unmarshal(casks[0], &item); err != nil {
		return false, "", err
	}
	if !matchesPackage(item.Token, item.FullToken, packageName) {
		return false, "", fmt.Errorf("cask package mismatch: got %q/%q", item.Token, item.FullToken)
	}
	if len(item.Installed) == 0 || bytes.Equal(item.Installed, []byte("null")) || bytes.Equal(item.Installed, []byte("false")) {
		return false, "", nil
	}
	var version string
	if err := json.Unmarshal(item.Installed, &version); err != nil || version == "" {
		return false, "", errors.New("installed cask version is malformed")
	}
	return true, version, nil
}

func parseListVersions(contents []byte, packageName string) (string, error) {
	line := strings.TrimSpace(string(contents))
	if line == "" {
		return "", nil
	}
	if strings.Contains(line, "\n") {
		return "", errors.New("multiple package lines")
	}
	fields := strings.Fields(line)
	if len(fields) != 2 || !matchesListPackage(fields[0], packageName) || fields[1] == "" {
		return "", fmt.Errorf("unexpected list output %q", line)
	}
	return fields[1], nil
}

func parseOutdated(contents []byte, kind Kind, packageName string, qualifiedIdentityVerified bool) (bool, error) {
	formulae, casks, err := decodeHomebrewEnvelope(contents)
	if err != nil {
		return false, err
	}
	if kind == Formula {
		if len(casks) != 0 || len(formulae) > 1 {
			return false, errors.New("outdated response contains unexpected packages")
		}
		if len(formulae) == 0 {
			return false, nil
		}
		if err := rejectUnknownRecordFields(formulae[0], outdatedFields); err != nil {
			return false, fmt.Errorf("outdated formula record: %w", err)
		}
		var item struct {
			Name     string `json:"name"`
			FullName string `json:"full_name"`
		}
		if err := json.Unmarshal(formulae[0], &item); err != nil {
			return false, err
		}
		if !matchesOutdatedPackage(item.Name, item.FullName, packageName, qualifiedIdentityVerified) {
			return false, errors.New("outdated formula package mismatch")
		}
		return true, nil
	}
	if len(formulae) != 0 || len(casks) > 1 {
		return false, errors.New("outdated response contains unexpected packages")
	}
	if len(casks) == 0 {
		return false, nil
	}
	if err := rejectUnknownRecordFields(casks[0], outdatedFields); err != nil {
		return false, fmt.Errorf("outdated cask record: %w", err)
	}
	var item struct {
		Name      string `json:"name"`
		Token     string `json:"token"`
		FullToken string `json:"full_token"`
	}
	if err := json.Unmarshal(casks[0], &item); err != nil {
		return false, err
	}
	short := item.Token
	if short == "" {
		short = item.Name
	}
	if !matchesOutdatedPackage(short, item.FullToken, packageName, qualifiedIdentityVerified) {
		return false, errors.New("outdated cask package mismatch")
	}
	return true, nil
}

func decodeHomebrewEnvelope(contents []byte) ([]json.RawMessage, []json.RawMessage, error) {
	var envelope map[string]json.RawMessage
	if err := decodeOne(contents, &envelope); err != nil {
		return nil, nil, err
	}
	if len(envelope) != 2 {
		keys := make([]string, 0, len(envelope))
		for key := range envelope {
			if key != "formulae" && key != "casks" {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		if len(keys) != 0 {
			return nil, nil, fmt.Errorf("unknown top-level fields: %s", strings.Join(keys, ", "))
		}
		return nil, nil, errors.New("Homebrew JSON must contain exactly formulae and casks")
	}
	formulaRaw, formulaOK := envelope["formulae"]
	caskRaw, caskOK := envelope["casks"]
	if !formulaOK || !caskOK {
		return nil, nil, errors.New("Homebrew JSON must contain formulae and casks")
	}
	if !rawJSONArray(formulaRaw) {
		return nil, nil, errors.New("formulae must be a JSON array")
	}
	if !rawJSONArray(caskRaw) {
		return nil, nil, errors.New("casks must be a JSON array")
	}
	var formulae, casks []json.RawMessage
	if err := json.Unmarshal(formulaRaw, &formulae); err != nil {
		return nil, nil, fmt.Errorf("formulae must be an array: %w", err)
	}
	if err := json.Unmarshal(caskRaw, &casks); err != nil {
		return nil, nil, fmt.Errorf("casks must be an array: %w", err)
	}
	return formulae, casks, nil
}

func rawJSONArray(contents json.RawMessage) bool {
	trimmed := bytes.TrimSpace(contents)
	return len(trimmed) != 0 && trimmed[0] == '['
}

func rejectUnknownRecordFields(contents json.RawMessage, allowed map[string]struct{}) error {
	var record map[string]json.RawMessage
	if err := json.Unmarshal(contents, &record); err != nil {
		return err
	}
	unknown := make([]string, 0)
	for key := range record {
		if _, ok := allowed[key]; !ok {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	return fmt.Errorf("unknown fields: %s", strings.Join(unknown, ", "))
}

var formulaInfoFields = stringSet(
	"aliases", "autobump", "bottle", "build_dependencies", "caveats", "compatibility_version",
	"conflicts_with", "conflicts_with_reasons", "dependencies", "deprecate_args", "deprecated",
	"deprecation_date", "deprecation_reason", "deprecation_replacement_cask", "deprecation_replacement_formula",
	"desc", "disable_args", "disable_date", "disable_reason", "disable_replacement_cask",
	"disable_replacement_formula", "disabled", "full_name", "homepage", "installed", "keg_only",
	"keg_only_reason", "license", "link_overwrite", "linked_keg", "name", "no_autobump_message",
	"oldnames", "optional_dependencies", "options", "outdated", "patches", "pinned", "post_install_defined",
	"post_install_steps", "pour_bottle_only_if", "recommended_dependencies", "requirements", "revision",
	"ruby_source_checksum", "ruby_source_path", "service", "skip_livecheck", "tap", "tap_git_head",
	"test_dependencies", "urls", "uses_from_macos", "uses_from_macos_bounds", "version_scheme",
	"versioned_formulae", "versions",
)

var caskInfoFields = stringSet(
	"artifacts", "auto_updates", "autobump", "bundle_short_version", "bundle_version", "caveats",
	"caveats_rosetta", "conflicts_with", "container", "depends_on", "deprecate_args", "deprecated",
	"deprecation_date", "deprecation_reason", "deprecation_replacement_cask", "deprecation_replacement_formula",
	"desc", "disable_args", "disable_date", "disable_reason", "disable_replacement_cask",
	"disable_replacement_formula", "disabled", "full_token", "homepage", "installed", "installed_time",
	"languages", "name", "no_autobump_message", "old_tokens", "outdated", "pinned", "pinned_version",
	"rename", "ruby_source_checksum", "ruby_source_path", "sha256", "skip_livecheck", "tap", "tap_git_head",
	"token", "url", "url_specs", "version",
)

var outdatedFields = stringSet(
	"current_version", "full_name", "full_token", "installed_versions", "name", "pinned", "pinned_version", "token",
)

func stringSet(values ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func decodeOne(contents []byte, target any) error {
	if len(contents) > execx.OutputLimit {
		return errors.New("JSON exceeds bounded runner output limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return fmt.Errorf("trailing JSON: %w", err)
	}
	return nil
}

func matchesPackage(short, full, packageName string) bool {
	if strings.Contains(packageName, "/") {
		return full == packageName
	}
	return short == packageName && (full == "" || full == packageName || strings.HasSuffix(full, "/"+packageName))
}

func matchesListPackage(short, packageName string) bool {
	if slash := strings.LastIndexByte(packageName, '/'); slash >= 0 {
		return short == packageName[slash+1:]
	}
	return short == packageName
}

func matchesOutdatedPackage(short, full, packageName string, qualifiedIdentityVerified bool) bool {
	if !strings.Contains(packageName, "/") {
		return matchesPackage(short, full, packageName)
	}
	if full != "" {
		return full == packageName
	}
	return qualifiedIdentityVerified && matchesListPackage(short, packageName)
}

func safeToken(value string) bool {
	if value == "" || strings.HasPrefix(value, "-") || strings.HasPrefix(value, "/") {
		return false
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._+@/-", r)) {
			return false
		}
	}
	return true
}

func safeCommand(value string) bool {
	return safeToken(value) && !strings.Contains(value, "/")
}

func safeRecoveryChild(value string) bool {
	return value != "" && filepath.Base(value) == value && value != "." && value != ".."
}

func pathWithin(path, root string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func pathAtOrWithin(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func isNilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func cloneResource(resource model.Resource) model.Resource {
	resource.Commands = append([]string(nil), resource.Commands...)
	metadata := make(map[string]string, len(resource.Metadata))
	for key, value := range resource.Metadata {
		metadata[key] = value
	}
	resource.Metadata = metadata
	return resource
}

type osFS struct{}

func (osFS) Lstat(path string) (os.FileInfo, error)       { return os.Lstat(path) }
func (osFS) EvalSymlinks(path string) (string, error)     { return filepath.EvalSymlinks(path) }
func (osFS) Mkdir(path string, mode os.FileMode) error    { return os.Mkdir(path, mode) }
func (osFS) MkdirAll(path string, mode os.FileMode) error { return os.MkdirAll(path, mode) }
func (osFS) Rename(oldPath, newPath string) error         { return os.Rename(oldPath, newPath) }

var _ provider.Provider = (*Adapter)(nil)
