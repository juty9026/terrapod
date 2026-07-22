package jetendard

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/juty9026/terrapod/internal/model"
	archivepkg "github.com/juty9026/terrapod/internal/resource/archive"
	"github.com/juty9026/terrapod/internal/state"
)

const (
	ResourceID          model.ResourceID = "font.jetendard"
	Provider                             = "jetendard"
	MetadataURL                          = "asset.url"
	MetadataSHA256                       = "asset.sha256"
	MetadataFormat                       = "asset.format"
	MetadataTag                          = "asset.tag"
	MetadataDestination                  = "font.destination"
)

var fontPattern = regexp.MustCompile(`^Jetendard-[A-Za-z0-9]+\.ttf$`)
var shaPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
var rawSHA256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var beforeInstallFile func(string) error

type Adapter struct {
	Archive   *archivepkg.Adapter
	Home      string
	State     *state.Store
	Recovery  string
	mu        sync.Mutex
	installed map[model.ResourceID]map[string]string
}

type declaration struct {
	asset            archivepkg.Asset
	tag, destination string
}

type ResolvedAsset struct{ Tag, URL, SHA256 string }

type release struct {
	Tag        string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name   string `json:"name"`
		State  string `json:"state"`
		Digest string `json:"digest"`
		URL    string `json:"browser_download_url"`
	} `json:"assets"`
}

// ResolveLatest is update-preflight functionality. Reconciliation never calls it.
func ResolveLatest(ctx context.Context, client *http.Client, endpoint string) (ResolvedAsset, error) {
	if client == nil {
		return ResolvedAsset{}, errors.New("jetendard: HTTP client is required")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ResolvedAsset{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	response, err := client.Do(request)
	if err != nil {
		return ResolvedAsset{}, fmt.Errorf("jetendard: resolve latest: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return ResolvedAsset{}, fmt.Errorf("jetendard: release status %s", response.Status)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, (1<<20)+1))
	var value release
	if err := decoder.Decode(&value); err != nil {
		return ResolvedAsset{}, fmt.Errorf("jetendard: decode release: %w", err)
	}
	if value.Draft || value.Prerelease || value.Tag == "" {
		return ResolvedAsset{}, errors.New("jetendard: latest release is not stable")
	}
	var matched *ResolvedAsset
	for _, asset := range value.Assets {
		if asset.Name != "Jetendard-TTF.zip" || asset.State != "uploaded" {
			continue
		}
		digest := strings.TrimPrefix(strings.ToLower(asset.Digest), "sha256:")
		if !rawSHA256Pattern.MatchString(digest) || asset.URL == "" {
			return ResolvedAsset{}, errors.New("jetendard: release asset lacks a valid SHA-256 or URL")
		}
		if matched != nil {
			return ResolvedAsset{}, errors.New("jetendard: release has duplicate font assets")
		}
		candidate := ResolvedAsset{Tag: value.Tag, URL: asset.URL, SHA256: digest}
		matched = &candidate
	}
	if matched == nil {
		return ResolvedAsset{}, errors.New("jetendard: stable release lacks Jetendard-TTF.zip")
	}
	return *matched, nil
}

func (a *Adapter) Inspect(_ context.Context, item model.Resource) (model.Observation, error) {
	d, err := a.declaration(item)
	if err != nil {
		return model.Observation{}, err
	}
	owned, err := a.ownership(item)
	if err != nil {
		return model.Observation{}, err
	}
	if len(owned.Paths) == 0 {
		return model.Observation{Provider: item.Provider, Package: item.Package, Version: d.tag, Paths: map[string]string{}}, nil
	}
	paths, healthy, detail := inspectPaths(owned.Paths)
	return model.Observation{Present: len(paths) > 0, Healthy: healthy, Provider: item.Provider, Package: item.Package, Version: d.tag, Paths: paths, Detail: detail}, nil
}

func (a *Adapter) Verify(_ context.Context, item model.Resource) (model.Observation, error) {
	d, err := a.declaration(item)
	if err != nil {
		return model.Observation{}, err
	}
	a.mu.Lock()
	paths := clonePaths(a.installed[item.ID])
	a.mu.Unlock()
	if len(paths) == 0 {
		owned, ownErr := a.ownership(item)
		if ownErr != nil {
			return model.Observation{}, ownErr
		}
		paths = clonePaths(owned.Paths)
	}
	current, healthy, detail := inspectPaths(paths)
	return model.Observation{Present: len(current) > 0, Healthy: healthy, Provider: item.Provider, Package: item.Package, Version: d.tag, Paths: current, Detail: detail}, nil
}

func (a *Adapter) Plan(ctx context.Context, item model.Resource, _ model.Observation, owned model.Ownership) ([]model.Operation, error) {
	if _, err := a.declaration(item); err != nil {
		return nil, err
	}
	if len(owned.Paths) == 0 {
		return []model.Operation{operation(item, model.OperationInstall)}, nil
	}
	_, healthy, _ := inspectPaths(owned.Paths)
	if !healthy {
		return []model.Operation{operation(item, model.OperationRestore)}, nil
	}
	// The planner removes Upgrade during ordinary apply. During `tpod update`,
	// this consumes only the asset metadata already resolved into the catalog.
	return []model.Operation{operation(item, model.OperationUpgrade)}, nil
}

func (a *Adapter) PlanHistorical(_ context.Context, item model.Resource, _ model.Observation, owned model.Ownership) ([]model.Operation, error) {
	if _, err := a.declaration(item); err != nil {
		return nil, err
	}
	if owned.ResourceID != item.ID || owned.Provider != item.Provider || owned.Package != item.Package {
		return nil, errors.New("jetendard: exact historical ownership is required")
	}
	if len(owned.Paths) == 0 {
		return nil, errors.New("jetendard: historical ownership has no font manifest")
	}
	if err := validateOwnedForInstall(owned.Paths); err != nil {
		return nil, err
	}
	op := operation(item, model.OperationPrune)
	op.Removes = []string{item.Package}
	return []model.Operation{op}, nil
}

func (a *Adapter) Execute(context.Context, model.Operation) model.OperationResult {
	return model.OperationResult{Detail: "jetendard: signed resource is required", FinishedAt: time.Now().UTC()}
}

func (a *Adapter) ExecuteResource(ctx context.Context, item model.Resource, op model.Operation) model.OperationResult {
	result := model.OperationResult{OperationID: op.ID, ResourceID: op.ResourceID, FinishedAt: time.Now().UTC()}
	if err := a.execute(ctx, item, op); err != nil {
		result.Detail = err.Error()
		return result
	}
	result.Success = true
	return result
}

func (a *Adapter) execute(ctx context.Context, item model.Resource, op model.Operation) error {
	d, err := a.declaration(item)
	if err != nil {
		return err
	}
	if op.ResourceID != item.ID || op.Provider != item.Provider || op.Package != item.Package {
		return errors.New("jetendard: operation identity mismatch")
	}
	owned, err := a.ownership(item)
	if err != nil {
		return err
	}
	if op.Kind == model.OperationPrune {
		return a.Prune(ctx, item, op, owned)
	}
	if op.Kind != model.OperationInstall && op.Kind != model.OperationUpgrade && op.Kind != model.OperationRestore {
		return fmt.Errorf("jetendard: unsupported operation %q", op.Kind)
	}
	if len(op.Removes) != 0 {
		return errors.New("jetendard: non-prune operation cannot remove packages")
	}
	if err := validateOwnedForInstall(owned.Paths); err != nil {
		return err
	}
	return a.install(ctx, item, d, owned.Paths)
}

func (a *Adapter) install(ctx context.Context, item model.Resource, d declaration, owned map[string]string) error {
	if a.Archive == nil {
		return errors.New("jetendard: archive adapter is required")
	}
	if a.Recovery == "" {
		return errors.New("jetendard: recovery path is required")
	}
	fonts := filepath.Join(a.Home, filepath.FromSlash(d.destination))
	if err := ensureRealDirectory(fonts); err != nil {
		return err
	}
	extracted := filepath.Join(filepath.Dir(fonts), fmt.Sprintf(".jetendard-extract-%d", time.Now().UnixNano()))
	manifest, err := a.Archive.FetchAndExtract(ctx, d.asset, extracted)
	if err != nil {
		return err
	}
	defer os.RemoveAll(extracted)
	type selected struct{ name, source, digest string }
	desired := make(map[string]selected)
	for _, file := range manifest.Files {
		name := filepath.Base(filepath.FromSlash(file.Path))
		if !fontPattern.MatchString(name) {
			continue
		}
		if _, duplicate := desired[name]; duplicate {
			return fmt.Errorf("jetendard: duplicate font target %q", name)
		}
		desired[name] = selected{name: name, source: filepath.Join(extracted, filepath.FromSlash(file.Path)), digest: "sha256:" + file.SHA256}
	}
	if len(desired) == 0 {
		return errors.New("jetendard: archive contains no Jetendard TTF files")
	}
	if err := ensurePrivateDirectory(a.Recovery); err != nil {
		return err
	}
	transaction, err := os.MkdirTemp(a.Recovery, "font-rollback-*")
	if err != nil {
		return err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(transaction)
		}
	}()
	if err := os.Chmod(transaction, 0o700); err != nil {
		return err
	}
	touched := make(map[string]bool)
	backups := make(map[string]string)
	stages := make(map[string]string)
	defer func() {
		for _, stage := range stages {
			_ = os.Remove(stage)
		}
	}()
	for name, font := range desired {
		destination := filepath.Join(fonts, name)
		if err := backupExisting(destination, transaction, backups); err != nil {
			return err
		}
		stage, err := stageFile(font.source, fonts, name)
		if err != nil {
			return err
		}
		stages[destination] = stage
		touched[destination] = true
	}
	for path := range owned {
		if _, still := touched[path]; still {
			continue
		}
		if err := validateOwnedPath(fonts, path); err != nil {
			return err
		}
		if err := backupExisting(path, transaction, backups); err != nil {
			return err
		}
		touched[path] = true
	}
	rollback := func(cause error) error {
		var rollbackErr error
		for destination := range touched {
			_ = os.Remove(destination)
			if backup := backups[destination]; backup != "" {
				if err := copyAtomic(backup, destination); err != nil {
					rollbackErr = errors.Join(rollbackErr, fmt.Errorf("restore %s: %w", destination, err))
				}
			}
		}
		for _, stage := range stages {
			_ = os.Remove(stage)
		}
		cleanup = rollbackErr == nil
		return errors.Join(cause, rollbackErr)
	}
	for destination, stage := range stages {
		if beforeInstallFile != nil {
			if err := beforeInstallFile(destination); err != nil {
				return rollback(err)
			}
		}
		if err := os.Rename(stage, destination); err != nil {
			return rollback(err)
		}
	}
	for path := range owned {
		if _, still := stages[path]; still {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return rollback(err)
		}
	}
	installed := make(map[string]string, len(desired))
	for _, font := range desired {
		installed[filepath.Join(fonts, font.name)] = font.digest
	}
	if _, healthy, detail := inspectPaths(installed); !healthy {
		return rollback(fmt.Errorf("jetendard: installed manifest verification failed: %s", detail))
	}
	a.mu.Lock()
	if a.installed == nil {
		a.installed = make(map[model.ResourceID]map[string]string)
	}
	a.installed[item.ID] = clonePaths(installed)
	a.mu.Unlock()
	cleanup = true
	return nil
}

func (a *Adapter) Prune(_ context.Context, item model.Resource, op model.Operation, owned model.Ownership) error {
	d, err := a.declaration(item)
	if err != nil {
		return err
	}
	if op.ResourceID != item.ID || op.Kind != model.OperationPrune || len(op.Removes) != 1 || op.Removes[0] != item.Package {
		return errors.New("jetendard: prune authority mismatch")
	}
	fonts := filepath.Join(a.Home, filepath.FromSlash(d.destination))
	for path, expected := range owned.Paths {
		if err := validateOwnedPath(fonts, path); err != nil {
			return err
		}
		actual, err := digestFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || actual != expected {
			return fmt.Errorf("jetendard conflict: owned font differs before prune: %s", path)
		}
	}
	for path := range owned.Paths {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	a.mu.Lock()
	delete(a.installed, item.ID)
	a.mu.Unlock()
	return nil
}

func (a *Adapter) declaration(item model.Resource) (declaration, error) {
	if item.ID != ResourceID || item.Type != model.ResourceArchive || item.Provider != Provider || item.Package != "jetendard" {
		return declaration{}, errors.New("jetendard: unsupported signed resource")
	}
	if item.VersionPolicy != model.VersionPinned {
		return declaration{}, errors.New("jetendard: apply requires resolved pinned metadata")
	}
	d := declaration{asset: archivepkg.Asset{URL: item.Metadata[MetadataURL], SHA256: item.Metadata[MetadataSHA256], Format: item.Metadata[MetadataFormat]}, tag: item.Metadata[MetadataTag], destination: item.Metadata[MetadataDestination]}
	digest := strings.TrimPrefix(d.asset.SHA256, "sha256:")
	if d.tag == "" || d.asset.URL == "" || d.asset.Format != "zip" || !rawSHA256Pattern.MatchString(digest) {
		return declaration{}, errors.New("jetendard: incomplete resolved asset metadata")
	}
	if d.destination != "Library/Fonts" {
		return declaration{}, errors.New("jetendard: destination must be Library/Fonts")
	}
	return d, nil
}

func (a *Adapter) ownership(item model.Resource) (model.Ownership, error) {
	if a.State == nil {
		return model.Ownership{}, errors.New("jetendard: state store is required")
	}
	snapshot, err := a.State.Snapshot()
	if err != nil {
		return model.Ownership{}, err
	}
	owned := snapshot.Ownership[item.ID]
	if owned.ResourceID != "" && (owned.ResourceID != item.ID || owned.Provider != item.Provider || owned.Package != item.Package) {
		return model.Ownership{}, errors.New("jetendard: ownership identity mismatch")
	}
	return owned, nil
}

func inspectPaths(expected map[string]string) (map[string]string, bool, string) {
	current := make(map[string]string, len(expected))
	healthy := true
	var details []string
	for path, want := range expected {
		if !filepath.IsAbs(path) || !shaPattern.MatchString(want) {
			healthy = false
			details = append(details, "invalid receipt: "+path)
			continue
		}
		got, err := digestFile(path)
		if err != nil {
			healthy = false
			details = append(details, fmt.Sprintf("%s: %v", path, err))
			continue
		}
		current[path] = got
		if got != want {
			healthy = false
			details = append(details, "digest mismatch: "+path)
		}
	}
	sort.Strings(details)
	return current, healthy, strings.Join(details, "; ")
}

func validateOwnedForInstall(expected map[string]string) error {
	for path, want := range expected {
		got, err := digestFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || got != want {
			return fmt.Errorf("jetendard conflict: owned font differs: %s", path)
		}
	}
	return nil
}
func digestFile(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("not a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}
func ensureRealDirectory(path string) error {
	home := filepath.Dir(filepath.Dir(path))
	if err := requireRealDirectory(home); err != nil {
		return fmt.Errorf("jetendard: unsafe home: %w", err)
	}
	current := home
	for _, component := range []string{"Library", "Fonts"} {
		current = filepath.Join(current, component)
		if err := os.Mkdir(current, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
		if err := requireRealDirectory(current); err != nil {
			return err
		}
	}
	if filepath.Clean(current) != filepath.Clean(path) {
		return errors.New("jetendard: unexpected Fonts path")
	}
	return nil
}

func requireRealDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("jetendard: path must be a real directory")
	}
	return nil
}

func ensurePrivateDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	if err := requireRealDirectory(path); err != nil {
		return fmt.Errorf("jetendard: unsafe recovery directory: %w", err)
	}
	return os.Chmod(path, 0o700)
}
func validateOwnedPath(fonts, path string) error {
	relative, err := filepath.Rel(fonts, path)
	if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) || !fontPattern.MatchString(filepath.Base(path)) {
		return fmt.Errorf("jetendard: unsafe owned font path %q", path)
	}
	return nil
}
func backupExisting(path, transaction string, backups map[string]string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		backups[path] = ""
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("jetendard: target is not a regular file: %s", path)
	}
	backup := filepath.Join(transaction, fmt.Sprintf("%x", sha256.Sum256([]byte(path))))
	if err := copyFile(path, backup, 0o600); err != nil {
		return err
	}
	backups[path] = backup
	return nil
}
func stageFile(source, fonts, name string) (string, error) {
	file, err := os.CreateTemp(fonts, "."+name+".stage-*")
	if err != nil {
		return "", err
	}
	stage := file.Name()
	input, err := os.Open(source)
	if err != nil {
		file.Close()
		os.Remove(stage)
		return "", err
	}
	_, copyErr := io.Copy(file, input)
	err = errors.Join(copyErr, input.Close(), file.Sync(), file.Close())
	if err != nil {
		os.Remove(stage)
		return "", err
	}
	if err := os.Chmod(stage, 0o644); err != nil {
		os.Remove(stage)
		return "", err
	}
	return stage, nil
}
func copyFile(source, destination string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	return errors.Join(copyErr, output.Sync(), output.Close())
}
func copyAtomic(source, destination string) error {
	stage, err := stageFile(source, filepath.Dir(destination), filepath.Base(destination))
	if err != nil {
		return err
	}
	if err := os.Rename(stage, destination); err != nil {
		os.Remove(stage)
		return err
	}
	return nil
}
func clonePaths(input map[string]string) map[string]string {
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}
func operation(item model.Resource, kind model.OperationKind) model.Operation {
	return model.Operation{ID: fmt.Sprintf("%s:%s", kind, item.ID), ResourceID: item.ID, Kind: kind, Provider: item.Provider, Package: item.Package}
}
