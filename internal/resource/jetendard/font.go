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
	"reflect"
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
	ResourceID           model.ResourceID = "font.jetendard"
	Provider                              = "jetendard"
	MetadataURL                           = "asset.url"
	MetadataSHA256                        = "asset.sha256"
	MetadataFormat                        = "asset.format"
	MetadataTag                           = "asset.tag"
	MetadataDestination                   = "font.destination"
	MetadataFiles                         = "font.files"
	transactionDirname                    = ".terrapod-jetendard-transaction"
	transactionFilename                   = "intent.json"
	defaultFontBytes     int64            = 64 << 20
	phasePrepared                         = "prepared"
	phasePublished                        = "published"
	phaseCleanup                          = "cleanup"
	phaseRollbackCleanup                  = "rollback-cleanup"
)

var fontPattern = regexp.MustCompile(`^Jetendard-[A-Za-z0-9]+\.ttf$`)
var shaPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
var rawSHA256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var beforeInstallFile func(string) error
var afterPublishFile func(string) error
var afterCleanupArtifact func(string) error
var beforeRollbackCleanupSync func() error
var errSimulatedCrash = errors.New("jetendard: simulated crash")

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
	files            map[string]string
}

type fontSource struct{ name, source, digest string }

type transaction struct {
	Version         int                 `json:"version"`
	Phase           string              `json:"phase"`
	ResourceID      string              `json:"resourceId"`
	ManifestDigest  string              `json:"manifestDigest"`
	OwnershipDigest string              `json:"ownershipDigest"`
	JournalID       string              `json:"journalId"`
	OperationID     string              `json:"operationId"`
	OperationKind   model.OperationKind `json:"operationKind"`
	Entries         []transactionEntry  `json:"entries"`
}

type transactionEntry struct {
	Name      string `json:"path"`
	NewDigest string `json:"newDigest,omitempty"`
	NewSize   int64  `json:"newSize,omitempty"`
	OldDigest string `json:"oldDigest,omitempty"`
	OldSize   int64  `json:"oldSize,omitempty"`
	Remove    bool   `json:"remove,omitempty"`
	OldExists bool   `json:"oldExists,omitempty"`
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
	if _, err := validateFontTree(a.Home); err != nil {
		return model.Observation{}, err
	}
	owned, err := a.ownership(item)
	if err != nil {
		return model.Observation{}, err
	}
	if err := a.recoverPending(item, d, owned); err != nil {
		return model.Observation{}, err
	}
	if len(owned.Paths) == 0 {
		paths, present, healthy, detail := inspectDeclared(a.Home, d)
		return model.Observation{Present: present, Healthy: healthy, Provider: item.Provider, Package: item.Package, Version: d.tag, Paths: paths, Detail: detail}, nil
	}
	if err := validateOwnership(filepath.Join(a.Home, filepath.FromSlash(d.destination)), owned.Paths); err != nil {
		return model.Observation{}, err
	}
	paths, healthy, detail := inspectPaths(owned.Paths)
	return model.Observation{Present: len(paths) > 0, Healthy: healthy, Provider: item.Provider, Package: item.Package, Version: d.tag, Paths: paths, Detail: detail}, nil
}

func (a *Adapter) Verify(_ context.Context, item model.Resource) (model.Observation, error) {
	d, err := a.declaration(item)
	if err != nil {
		return model.Observation{}, err
	}
	if exists, err := validateFontTree(a.Home); err != nil || !exists {
		if err == nil {
			err = errors.New("jetendard: Fonts directory is unavailable")
		}
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
	if err := validateOwnership(filepath.Join(a.Home, filepath.FromSlash(d.destination)), paths); err != nil {
		return model.Observation{}, err
	}
	current, healthy, detail := inspectPaths(paths)
	return model.Observation{Present: len(current) > 0, Healthy: healthy, Provider: item.Provider, Package: item.Package, Version: d.tag, Paths: current, Detail: detail}, nil
}

func (a *Adapter) Plan(ctx context.Context, item model.Resource, _ model.Observation, owned model.Ownership) ([]model.Operation, error) {
	d, err := a.declaration(item)
	if err != nil {
		return nil, err
	}
	if len(owned.Paths) == 0 {
		_, present, healthy, _ := inspectDeclared(a.Home, d)
		if !present {
			return []model.Operation{operation(item, model.OperationInstall)}, nil
		}
		if healthy {
			return []model.Operation{operation(item, model.OperationAdopt)}, nil
		}
		op := operation(item, model.OperationRestore)
		op.Detail = "take ownership of pre-existing Jetendard fonts after recovery backup"
		return []model.Operation{op}, nil
	}
	if err := validateOwnership(filepath.Join(a.Home, filepath.FromSlash(d.destination)), owned.Paths); err != nil {
		return nil, err
	}
	collisions, err := takeoverNames(a.Home, d, owned.Paths)
	if err != nil {
		return nil, err
	}
	if len(collisions) != 0 {
		op := operation(item, model.OperationRestore)
		op.Detail = "take ownership of pre-existing Jetendard fonts after recovery backup"
		return []model.Operation{op}, nil
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
	d, err := a.declaration(item)
	if err != nil {
		return nil, err
	}
	if owned.ResourceID != item.ID || owned.Provider != item.Provider || owned.Package != item.Package {
		return nil, errors.New("jetendard: exact historical ownership is required")
	}
	if len(owned.Paths) == 0 {
		return nil, errors.New("jetendard: historical ownership has no font manifest")
	}
	if err := validateOwnership(filepath.Join(a.Home, filepath.FromSlash(d.destination)), owned.Paths); err != nil {
		return nil, err
	}
	if err := validateOwnedForInstall(owned.Paths); err != nil {
		return nil, err
	}
	op := operation(item, model.OperationPrune)
	op.Removes = []string{item.Package}
	return []model.Operation{op}, nil
}

func (a *Adapter) Execute(context.Context, model.Operation) model.OperationResult {
	return model.OperationResult{Detail: "jetendard: declared resource is required", FinishedAt: time.Now().UTC()}
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
	if op.Kind == model.OperationAdopt {
		if exists, err := validateFontTree(a.Home); err != nil || !exists {
			return errors.New("jetendard: Fonts path changed before adoption")
		}
		paths, present, healthy, _ := inspectDeclared(a.Home, d)
		if !present || !healthy {
			return errors.New("jetendard: pre-existing fonts changed before adoption")
		}
		a.setInstalled(item.ID, paths)
		return nil
	}
	if op.Kind != model.OperationInstall && op.Kind != model.OperationUpgrade && op.Kind != model.OperationRestore {
		return fmt.Errorf("jetendard: unsupported operation %q", op.Kind)
	}
	if len(op.Removes) != 0 {
		return errors.New("jetendard: non-prune operation cannot remove packages")
	}
	if err := validateOwnership(filepath.Join(a.Home, filepath.FromSlash(d.destination)), owned.Paths); err != nil {
		return err
	}
	if err := validateOwnedForInstall(owned.Paths); err != nil {
		return err
	}
	collisions, err := takeoverNames(a.Home, d, owned.Paths)
	if err != nil {
		return err
	}
	if len(collisions) != 0 {
		if op.Kind != model.OperationRestore {
			return errors.New("jetendard: pre-existing declared font requires takeover replan")
		}
		if err := a.backupPreExisting(item, d, collisions); err != nil {
			return err
		}
	}
	return a.install(ctx, item, d, owned, op)
}

func (a *Adapter) install(ctx context.Context, item model.Resource, d declaration, owned model.Ownership, op model.Operation) error {
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
	if err := a.recoverPending(item, d, owned); err != nil {
		return err
	}
	extracted := filepath.Join(filepath.Dir(fonts), fmt.Sprintf(".jetendard-extract-%d", time.Now().UnixNano()))
	manifest, err := a.Archive.FetchAndExtract(ctx, d.asset, extracted)
	if err != nil {
		return err
	}
	defer os.RemoveAll(extracted)
	desired := make(map[string]fontSource)
	for _, file := range manifest.Files {
		name := filepath.Base(filepath.FromSlash(file.Path))
		if !fontPattern.MatchString(name) {
			continue
		}
		if _, duplicate := desired[name]; duplicate {
			return fmt.Errorf("jetendard: duplicate font target %q", name)
		}
		desired[name] = fontSource{name: name, source: filepath.Join(extracted, filepath.FromSlash(file.Path)), digest: "sha256:" + file.SHA256}
	}
	if len(desired) == 0 {
		return errors.New("jetendard: archive contains no Jetendard TTF files")
	}
	if len(desired) != len(d.files) {
		return errors.New("jetendard: archive font manifest differs from Resource Catalog")
	}
	for name, font := range desired {
		if d.files[name] != font.digest {
			return fmt.Errorf("jetendard: font %q differs from Resource Catalog manifest", name)
		}
	}
	txn, err := a.prepareTransaction(item, d, op, fonts, desired, owned)
	if err != nil {
		return err
	}
	if err := validateTransactionArtifacts(fonts, txn); err != nil {
		return errors.Join(err, rollbackTransaction(fonts, txn))
	}
	if err := publishTransaction(fonts, txn); err != nil {
		if errors.Is(err, errSimulatedCrash) {
			return err
		}
		return errors.Join(err, rollbackTransaction(fonts, txn))
	}
	installed := make(map[string]string, len(desired))
	for _, font := range desired {
		installed[filepath.Join(fonts, font.name)] = font.digest
	}
	if _, healthy, detail := inspectPaths(installed); !healthy {
		return errors.Join(fmt.Errorf("jetendard: installed manifest verification failed: %s", detail), rollbackTransaction(fonts, txn))
	}
	if err := finishTransaction(fonts, txn, phaseCleanup); err != nil {
		return err
	}
	a.setInstalled(item.ID, installed)
	return nil
}

func (a *Adapter) prepareTransaction(item model.Resource, d declaration, op model.Operation, fonts string, desired map[string]fontSource, owned model.Ownership) (transaction, error) {
	snapshot, err := a.State.Snapshot()
	if err != nil || snapshot.ActiveJournal == nil {
		return transaction{}, errors.New("jetendard: active journal is required for font transaction")
	}
	if err := authorizeJournalOperation(snapshot.ActiveJournal, item, op); err != nil {
		return transaction{}, err
	}
	directory := filepath.Join(fonts, transactionDirname)
	if err := os.Mkdir(directory, 0o700); err != nil {
		return transaction{}, fmt.Errorf("jetendard: create transaction: %w", err)
	}
	prepared := false
	defer func() {
		if !prepared {
			_ = cleanupTransactionFiles(directory)
		}
	}()
	names := make([]string, 0, len(desired))
	for name := range desired {
		names = append(names, name)
	}
	sort.Strings(names)
	txn := transaction{Version: 1, Phase: phasePrepared, ResourceID: string(item.ID), ManifestDigest: manifestDigest(d.files), OwnershipDigest: ownershipDigest(owned), JournalID: snapshot.ActiveJournal.ID, OperationID: op.ID, OperationKind: op.Kind}
	covered := make(map[string]bool)
	for _, name := range names {
		font := desired[name]
		_, newSize, err := digestFileSize(font.source)
		if err != nil {
			return transaction{}, err
		}
		entry := transactionEntry{Name: name, NewDigest: font.digest, NewSize: newSize}
		if err := prepareOld(fonts, directory, &entry); err != nil {
			return transaction{}, err
		}
		if err := copyFile(font.source, filepath.Join(directory, stageName(entry)), 0o600); err != nil {
			return transaction{}, err
		}
		if digest, err := digestFile(filepath.Join(directory, stageName(entry))); err != nil || digest != entry.NewDigest {
			return transaction{}, fmt.Errorf("jetendard: staged font verification failed: %s", name)
		}
		txn.Entries = append(txn.Entries, entry)
		covered[filepath.Join(fonts, name)] = true
	}
	var obsolete []string
	for ownedPath := range owned.Paths {
		if !covered[ownedPath] {
			obsolete = append(obsolete, ownedPath)
		}
	}
	sort.Strings(obsolete)
	for _, ownedPath := range obsolete {
		if err := validateOwnedPath(fonts, ownedPath); err != nil {
			return transaction{}, err
		}
		name := filepath.Base(ownedPath)
		entry := transactionEntry{Name: name, Remove: true, OldDigest: owned.Paths[ownedPath]}
		if err := prepareOld(fonts, directory, &entry); err != nil {
			return transaction{}, err
		}
		txn.Entries = append(txn.Entries, entry)
	}
	if err := writeTransaction(directory, txn); err != nil {
		return transaction{}, err
	}
	if err := syncDirectory(directory); err != nil {
		return transaction{}, err
	}
	if err := syncDirectory(fonts); err != nil {
		return transaction{}, err
	}
	prepared = true
	return txn, nil
}

func prepareOld(fonts, directory string, entry *transactionEntry) error {
	destination := filepath.Join(fonts, entry.Name)
	info, err := os.Lstat(destination)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("jetendard: target is not a regular file: %s", destination)
	}
	entry.OldExists = true
	digest, size, err := digestFileSize(destination)
	if err != nil {
		return err
	}
	if entry.OldDigest != "" && entry.OldDigest != digest {
		return fmt.Errorf("jetendard: obsolete owned font changed: %s", destination)
	}
	entry.OldDigest, entry.OldSize = digest, size
	return copyFile(destination, filepath.Join(directory, backupName(*entry)), 0o600)
}

func writeTransaction(directory string, txn transaction) error {
	temporary := filepath.Join(directory, ".intent.tmp")
	if err := os.Remove(temporary); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	encodeErr := encoder.Encode(txn)
	err = errors.Join(encodeErr, file.Sync(), file.Close())
	if err != nil {
		return err
	}
	return os.Rename(temporary, filepath.Join(directory, transactionFilename))
}

func publishTransaction(fonts string, txn transaction) error {
	directory := filepath.Join(fonts, transactionDirname)
	for _, entry := range txn.Entries {
		destination := filepath.Join(fonts, entry.Name)
		if err := validateAbsoluteDirectoryChain(directory); err != nil {
			return err
		}
		if err := validateTransactionEntry(fonts, directory, entry); err != nil {
			return err
		}
		if beforeInstallFile != nil {
			if err := beforeInstallFile(destination); err != nil {
				return err
			}
		}
		if entry.Remove {
			if err := os.Remove(destination); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		} else if _, err := os.Lstat(filepath.Join(directory, stageName(entry))); err == nil {
			if err := os.Rename(filepath.Join(directory, stageName(entry)), destination); err != nil {
				return err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		} else if digest, digestErr := digestFile(destination); digestErr != nil || digest != entry.NewDigest {
			return fmt.Errorf("jetendard: published font is incomplete: %s", entry.Name)
		}
		if afterPublishFile != nil {
			if err := afterPublishFile(destination); err != nil {
				return err
			}
		}
	}
	if err := syncDirectory(fonts); err != nil {
		return err
	}
	txn.Phase = phasePublished
	if err := writeTransaction(filepath.Join(fonts, transactionDirname), txn); err != nil {
		return err
	}
	return syncDirectory(filepath.Join(fonts, transactionDirname))
}

func rollbackTransaction(fonts string, txn transaction) error {
	directory := filepath.Join(fonts, transactionDirname)
	var result error
	for _, entry := range txn.Entries {
		destination := filepath.Join(fonts, entry.Name)
		_ = os.Remove(destination)
		if entry.OldExists {
			if digest, err := digestFile(filepath.Join(directory, backupName(entry))); err != nil || digest != entry.OldDigest {
				result = errors.Join(result, fmt.Errorf("jetendard: rollback backup invalid: %s", entry.Name))
				continue
			}
			if err := copyAtomic(filepath.Join(directory, backupName(entry)), destination); err != nil {
				result = errors.Join(result, err)
			}
		}
	}
	if result == nil {
		if err := verifyRolledBackState(fonts, txn); err != nil {
			return err
		}
		if beforeRollbackCleanupSync != nil {
			if err := beforeRollbackCleanupSync(); err != nil {
				return err
			}
		}
		if err := syncDirectory(fonts); err != nil {
			return err
		}
		result = finishTransaction(fonts, txn, phaseRollbackCleanup)
	}
	return result
}

func finishTransaction(fonts string, txn transaction, phase string) error {
	directory := filepath.Join(fonts, transactionDirname)
	if phase != phaseCleanup && phase != phaseRollbackCleanup {
		return errors.New("jetendard: invalid cleanup phase")
	}
	txn.Phase = phase
	if err := writeTransaction(directory, txn); err != nil {
		return err
	}
	if err := syncDirectory(directory); err != nil {
		return err
	}
	return cleanupTransaction(fonts, txn)
}

func cleanupTransaction(fonts string, txn transaction) error {
	directory := filepath.Join(fonts, transactionDirname)
	if err := validateCleanupArtifacts(directory, txn); err != nil {
		return err
	}
	names := make([]string, 0, len(txn.Entries)*2)
	seen := map[string]bool{}
	for _, entry := range txn.Entries {
		for _, name := range []string{stageName(entry), backupName(entry)} {
			if !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
	}
	sort.Strings(names)
	for _, name := range names {
		path := filepath.Join(directory, name)
		if err := os.Remove(path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		if err := syncDirectory(directory); err != nil {
			return err
		}
		if afterCleanupArtifact != nil {
			if err := afterCleanupArtifact(path); err != nil {
				return err
			}
		}
	}
	if err := os.Remove(filepath.Join(directory, ".intent.tmp")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := syncDirectory(directory); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(directory, transactionFilename)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := syncDirectory(directory); err != nil {
		return err
	}
	if err := os.Remove(directory); err != nil {
		return err
	}
	return syncDirectory(fonts)
}

func stageName(entry transactionEntry) string  { return "new-" + entry.Name }
func backupName(entry transactionEntry) string { return "old-" + entry.Name }

func validateCleanupArtifacts(directory string, txn transaction) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	allowed := map[string]bool{transactionFilename: true, ".intent.tmp": true}
	for _, entry := range txn.Entries {
		allowed[stageName(entry)] = true
		allowed[backupName(entry)] = true
	}
	if len(entries) > len(allowed) {
		return errors.New("jetendard: transaction cleanup is unbounded")
	}
	for _, entry := range entries {
		if !allowed[entry.Name()] || entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return errors.New("jetendard: unsafe cleanup artifact")
		}
	}
	return nil
}

func verifyPublishedState(fonts string, txn transaction) error {
	for _, entry := range txn.Entries {
		path := filepath.Join(fonts, entry.Name)
		if entry.Remove {
			if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
				continue
			} else if err != nil {
				return err
			}
			return fmt.Errorf("jetendard: obsolete font remains after publish: %s", entry.Name)
		}
		digest, size, err := digestFileSize(path)
		if err != nil || digest != entry.NewDigest || size != entry.NewSize {
			return fmt.Errorf("jetendard: published font is incomplete: %s", entry.Name)
		}
	}
	return nil
}

func verifyRolledBackState(fonts string, txn transaction) error {
	for _, entry := range txn.Entries {
		path := filepath.Join(fonts, entry.Name)
		if !entry.OldExists {
			if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
				continue
			} else if err != nil {
				return err
			}
			return fmt.Errorf("jetendard: new font remains after rollback: %s", entry.Name)
		}
		digest, size, err := digestFileSize(path)
		if err != nil || digest != entry.OldDigest || size != entry.OldSize {
			return fmt.Errorf("jetendard: rollback is incomplete: %s", entry.Name)
		}
	}
	return nil
}

func cleanupTransactionFiles(directory string) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	if len(entries) > 8193 {
		return errors.New("jetendard: transaction cleanup is unbounded")
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return errors.New("jetendard: unsafe transaction artifact")
		}
	}
	for _, entry := range entries {
		if err := os.Remove(filepath.Join(directory, entry.Name())); err != nil {
			return err
		}
	}
	return os.Remove(directory)
}

func readTransaction(directory string) (transaction, error) {
	intentPath := filepath.Join(directory, transactionFilename)
	info, err := os.Lstat(intentPath)
	if err != nil {
		return transaction{}, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Size() <= 0 || info.Size() > 1<<20 {
		return transaction{}, errors.New("jetendard: unsafe transaction intent")
	}
	file, err := os.Open(intentPath)
	if err != nil {
		return transaction{}, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 1<<20))
	decoder.DisallowUnknownFields()
	var txn transaction
	if err := decoder.Decode(&txn); err != nil {
		return transaction{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return transaction{}, errors.New("jetendard: trailing transaction data")
	}
	validPhase := txn.Phase == phasePrepared || txn.Phase == phasePublished || txn.Phase == phaseCleanup || txn.Phase == phaseRollbackCleanup
	if txn.Version != 1 || !validPhase || !rawSHA256Pattern.MatchString(txn.ManifestDigest) || !rawSHA256Pattern.MatchString(txn.OwnershipDigest) || txn.ResourceID == "" || txn.JournalID == "" || txn.OperationID == "" || len(txn.Entries) == 0 || len(txn.Entries) > 4096 {
		return transaction{}, errors.New("jetendard: invalid transaction intent")
	}
	seen := map[string]bool{}
	for _, entry := range txn.Entries {
		invalidNew := entry.Remove && (entry.NewDigest != "" || entry.NewSize != 0) || !entry.Remove && (!shaPattern.MatchString(entry.NewDigest) || entry.NewSize <= 0 || entry.NewSize > defaultFontBytes)
		invalidOld := entry.OldExists && (!shaPattern.MatchString(entry.OldDigest) || entry.OldSize < 0 || entry.OldSize > defaultFontBytes) || !entry.OldExists && ((entry.Remove && !shaPattern.MatchString(entry.OldDigest)) || (!entry.Remove && entry.OldDigest != "") || entry.OldSize != 0)
		if !fontPattern.MatchString(entry.Name) || seen[entry.Name] || invalidNew || invalidOld {
			return transaction{}, errors.New("jetendard: invalid transaction entry")
		}
		seen[entry.Name] = true
	}
	return txn, nil
}

func validateTransactionAuthority(txn transaction, item model.Resource, d declaration, owned model.Ownership, journal *model.Journal, fonts string) error {
	if txn.ResourceID != string(item.ID) || txn.ManifestDigest != manifestDigest(d.files) || txn.OwnershipDigest != ownershipDigest(owned) || journal == nil || txn.JournalID != journal.ID {
		return errors.New("jetendard: stale or foreign font transaction authority")
	}
	op := model.Operation{ID: txn.OperationID, ResourceID: item.ID, Kind: txn.OperationKind, Provider: item.Provider, Package: item.Package}
	if err := authorizeJournalOperation(journal, item, op); err != nil {
		return err
	}
	installNames := map[string]bool{}
	for _, entry := range txn.Entries {
		if entry.Remove {
			if _, stillDesired := d.files[entry.Name]; stillDesired {
				return errors.New("jetendard: transaction removes a declared font")
			}
			if owned.Paths[filepath.Join(fonts, entry.Name)] != entry.OldDigest {
				return errors.New("jetendard: transaction removes a font outside exact ownership")
			}
			continue
		}
		if d.files[entry.Name] != entry.NewDigest {
			return errors.New("jetendard: transaction install differs from declared font manifest")
		}
		installNames[entry.Name] = true
	}
	if len(installNames) != len(d.files) {
		return errors.New("jetendard: transaction omits declared fonts")
	}
	return nil
}

func validateTransactionArtifacts(fonts string, txn transaction) error {
	directory := filepath.Join(fonts, transactionDirname)
	if err := validateAbsoluteDirectoryChain(directory); err != nil {
		return err
	}
	info, err := os.Lstat(directory)
	if err != nil || info.Mode().Perm() != 0o700 {
		return errors.New("jetendard: unsafe transaction directory mode")
	}
	persisted, err := readTransaction(directory)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(persisted, txn) {
		return errors.New("jetendard: transaction intent changed before publish")
	}
	for _, entry := range txn.Entries {
		if err := validateTransactionEntry(fonts, directory, entry); err != nil {
			return err
		}
	}
	return nil
}

func validateRollbackBackups(fonts string, txn transaction) error {
	directory := filepath.Join(fonts, transactionDirname)
	if err := validateAbsoluteDirectoryChain(directory); err != nil {
		return err
	}
	for _, entry := range txn.Entries {
		backup := filepath.Join(directory, backupName(entry))
		if entry.OldExists {
			if err := validateArtifact(backup, entry.OldDigest, entry.OldSize); err != nil {
				return err
			}
		} else if _, err := os.Lstat(backup); err == nil || !errors.Is(err, os.ErrNotExist) {
			return errors.New("jetendard: unexpected rollback artifact")
		}
		stage := filepath.Join(directory, stageName(entry))
		if entry.Remove {
			if _, err := os.Lstat(stage); err == nil || !errors.Is(err, os.ErrNotExist) {
				return errors.New("jetendard: remove transaction has a stage artifact")
			}
			continue
		}
		if _, err := os.Lstat(stage); err == nil {
			if err := validateArtifact(stage, entry.NewDigest, entry.NewSize); err != nil {
				return err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func validateTransactionEntry(fonts, directory string, entry transactionEntry) error {
	if entry.OldExists {
		if err := validateArtifact(filepath.Join(directory, backupName(entry)), entry.OldDigest, entry.OldSize); err != nil {
			return err
		}
	} else if _, err := os.Lstat(filepath.Join(directory, backupName(entry))); err == nil || !errors.Is(err, os.ErrNotExist) {
		return errors.New("jetendard: unexpected rollback artifact")
	}
	target := filepath.Join(fonts, entry.Name)
	if entry.Remove {
		if _, err := os.Lstat(filepath.Join(directory, stageName(entry))); err == nil || !errors.Is(err, os.ErrNotExist) {
			return errors.New("jetendard: remove transaction has a stage artifact")
		}
		if _, err := os.Lstat(target); errors.Is(err, os.ErrNotExist) {
			return nil
		} else if err != nil {
			return err
		}
		if !entry.OldExists {
			return fmt.Errorf("jetendard: remove target lacks rollback backup: %s", entry.Name)
		}
		digest, size, err := digestFileSize(target)
		if err != nil || digest != entry.OldDigest || (entry.OldExists && size != entry.OldSize) {
			return fmt.Errorf("jetendard: remove target changed: %s", entry.Name)
		}
		return nil
	}
	stage := filepath.Join(directory, stageName(entry))
	if _, err := os.Lstat(stage); err == nil {
		if err := validateArtifact(stage, entry.NewDigest, entry.NewSize); err != nil {
			return err
		}
		if entry.OldExists {
			digest, size, err := digestFileSize(target)
			if err != nil || digest != entry.OldDigest || size != entry.OldSize {
				return fmt.Errorf("jetendard: install target changed: %s", entry.Name)
			}
		} else if _, err := os.Lstat(target); err == nil || !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("jetendard: unexpected install target: %s", entry.Name)
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	digest, size, err := digestFileSize(target)
	if err != nil || digest != entry.NewDigest || size != entry.NewSize {
		return fmt.Errorf("jetendard: published target invalid: %s", entry.Name)
	}
	return nil
}

func validateArtifact(path, expectedDigest string, expectedSize int64) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Size() != expectedSize {
		return fmt.Errorf("jetendard: unsafe transaction artifact: %s", path)
	}
	digest, size, err := digestFileSize(path)
	if err != nil || digest != expectedDigest || size != expectedSize {
		return fmt.Errorf("jetendard: transaction artifact digest mismatch: %s", path)
	}
	return nil
}

func (a *Adapter) recoverPending(item model.Resource, d declaration, owned model.Ownership) error {
	fonts := filepath.Join(a.Home, filepath.FromSlash(d.destination))
	exists, err := validateFontTree(a.Home)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	directory := filepath.Join(fonts, transactionDirname)
	info, err := os.Lstat(directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("jetendard: transaction path is unsafe")
	}
	if err := ensureRealDirectory(fonts); err != nil {
		return err
	}
	txn, err := readTransaction(directory)
	if errors.Is(err, os.ErrNotExist) {
		if err := cleanupTransactionFiles(directory); err != nil {
			return err
		}
		return syncDirectory(fonts)
	}
	if err != nil {
		return err
	}
	snapshot, err := a.State.Snapshot()
	if err != nil || snapshot.ActiveJournal == nil {
		return errors.New("jetendard: active journal is required to recover font transaction")
	}
	if err := validateTransactionAuthority(txn, item, d, owned, snapshot.ActiveJournal, fonts); err != nil {
		return err
	}
	if txn.Phase == phaseCleanup {
		if err := verifyPublishedState(fonts, txn); err != nil {
			return err
		}
		return cleanupTransaction(fonts, txn)
	}
	if txn.Phase == phaseRollbackCleanup {
		if err := verifyRolledBackState(fonts, txn); err != nil {
			return err
		}
		return cleanupTransaction(fonts, txn)
	}
	if err := verifyRolledBackState(fonts, txn); err == nil {
		if err := validateRollbackBackups(fonts, txn); err != nil {
			return err
		}
		if err := syncDirectory(fonts); err != nil {
			return err
		}
		return finishTransaction(fonts, txn, phaseRollbackCleanup)
	}
	if err := validateTransactionArtifacts(fonts, txn); err != nil {
		return err
	}
	if err := publishTransaction(fonts, txn); err != nil {
		return err
	}
	expected := make(map[string]string)
	for _, entry := range txn.Entries {
		if !entry.Remove {
			expected[filepath.Join(fonts, entry.Name)] = entry.NewDigest
		}
	}
	if _, healthy, detail := inspectPaths(expected); !healthy {
		return fmt.Errorf("jetendard: recovered transaction failed verification: %s", detail)
	}
	return finishTransaction(fonts, txn, phaseCleanup)
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
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
	if err := a.recoverPending(item, d, owned); err != nil {
		return err
	}
	exists, err := validateFontTree(a.Home)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
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
		// Revalidate the complete Home/Library/Fonts chain immediately before
		// every destructive syscall; a persistent symlink state must fail closed.
		exists, err := validateFontTree(a.Home)
		if err != nil || !exists {
			if err == nil {
				err = errors.New("jetendard: Fonts directory disappeared before prune")
			}
			return err
		}
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
		return declaration{}, errors.New("jetendard: unsupported declared resource")
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
	if len(item.Metadata[MetadataFiles]) == 0 || len(item.Metadata[MetadataFiles]) > 64<<10 {
		return declaration{}, errors.New("jetendard: declared font manifest is missing or too large")
	}
	decoder := json.NewDecoder(strings.NewReader(item.Metadata[MetadataFiles]))
	if err := decoder.Decode(&d.files); err != nil || len(d.files) == 0 || len(d.files) > 64 {
		return declaration{}, errors.New("jetendard: invalid declared font manifest")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return declaration{}, errors.New("jetendard: trailing declared font manifest data")
	}
	for name, digest := range d.files {
		if !fontPattern.MatchString(name) || !rawSHA256Pattern.MatchString(digest) {
			return declaration{}, errors.New("jetendard: invalid declared font manifest entry")
		}
		d.files[name] = "sha256:" + digest
	}
	return d, nil
}

func inspectDeclared(home string, d declaration) (map[string]string, bool, bool, string) {
	paths := make(map[string]string, len(d.files))
	present := false
	healthy := true
	var details []string
	fonts := filepath.Join(home, filepath.FromSlash(d.destination))
	for name, expected := range d.files {
		path := filepath.Join(fonts, name)
		actual, err := digestFile(path)
		if errors.Is(err, os.ErrNotExist) {
			healthy = false
			details = append(details, "missing: "+path)
			continue
		}
		present = true
		if err != nil {
			healthy = false
			details = append(details, fmt.Sprintf("%s: %v", path, err))
			continue
		}
		paths[path] = actual
		if actual != expected {
			healthy = false
			details = append(details, "digest mismatch: "+path)
		}
	}
	sort.Strings(details)
	return paths, present, healthy, strings.Join(details, "; ")
}

func takeoverNames(home string, d declaration, owned map[string]string) ([]string, error) {
	fonts := filepath.Join(home, filepath.FromSlash(d.destination))
	var names []string
	for name := range d.files {
		path := filepath.Join(fonts, name)
		if _, ok := owned[path]; ok {
			continue
		}
		if _, err := os.Lstat(path); err == nil {
			names = append(names, name)
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	sort.Strings(names)
	return names, nil
}

func (a *Adapter) backupPreExisting(item model.Resource, d declaration, names []string) error {
	exists, err := validateFontTree(a.Home)
	if err != nil || !exists {
		if err == nil {
			err = errors.New("jetendard: Fonts path disappeared before takeover backup")
		}
		return err
	}
	if err := ensurePrivateDirectory(a.Recovery); err != nil {
		return err
	}
	snapshot, err := a.State.Snapshot()
	if err != nil || snapshot.ActiveJournal == nil {
		return errors.New("jetendard: active journal is required for takeover backup")
	}
	root := filepath.Join(a.Recovery, snapshot.ActiveJournal.ID, "preexisting")
	if err := ensurePrivateDirectory(root); err != nil {
		return fmt.Errorf("jetendard: create takeover backup: %w", err)
	}
	fonts := filepath.Join(a.Home, filepath.FromSlash(d.destination))
	for _, name := range names {
		source := filepath.Join(fonts, name)
		info, err := os.Lstat(source)
		if errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("jetendard: pre-existing font is not a regular file: %s", source)
		}
		destination := filepath.Join(root, name)
		if existing, err := digestFile(destination); err == nil {
			current, currentErr := digestFile(source)
			if currentErr != nil || current != existing {
				return fmt.Errorf("jetendard: takeover backup differs: %s", name)
			}
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := copyFile(source, destination, 0o600); err != nil {
			return fmt.Errorf("jetendard: backup pre-existing font: %w", err)
		}
	}
	return syncDirectory(root)
}

func (a *Adapter) setInstalled(id model.ResourceID, paths map[string]string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.installed == nil {
		a.installed = make(map[model.ResourceID]map[string]string)
	}
	a.installed[id] = clonePaths(paths)
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

func manifestDigest(files map[string]string) string { return mapDigest(files) }
func ownershipDigest(owned model.Ownership) string {
	values := map[string]string{"@resource": string(owned.ResourceID), "@catalog": owned.CatalogDigest, "@provider": owned.Provider, "@package": owned.Package}
	for path, digest := range owned.Paths {
		values["path:"+path] = digest
	}
	for key, value := range owned.PriorValues {
		values["prior:"+key] = string(value)
	}
	return mapDigest(values)
}
func mapDigest(values map[string]string) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	hash := sha256.New()
	for _, key := range keys {
		_, _ = io.WriteString(hash, key)
		_, _ = hash.Write([]byte{0})
		_, _ = io.WriteString(hash, values[key])
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}
func digestString(contents []byte) string {
	sum := sha256.Sum256(contents)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func authorizeJournalOperation(journal *model.Journal, item model.Resource, op model.Operation) error {
	if journal == nil || journal.Status != "active" {
		return errors.New("jetendard: active journal authority is required")
	}
	matches := 0
	for _, planned := range journal.Plan.Operations {
		if planned.ID == op.ID && planned.ResourceID == item.ID && planned.Kind == op.Kind && planned.Provider == item.Provider && planned.Package == item.Package {
			matches++
		}
	}
	if matches != 1 {
		return errors.New("jetendard: font transaction operation is not authorized by active journal")
	}
	return nil
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
	digest, _, err := digestFileSize(path)
	return digest, err
}
func digestFileSize(path string) (string, int64, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", 0, err
	}
	if !info.Mode().IsRegular() {
		return "", 0, errors.New("not a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", 0, err
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), info.Size(), nil
}
func ensureRealDirectory(path string) error {
	home := filepath.Dir(filepath.Dir(path))
	if err := validateAbsoluteDirectoryChain(home); err != nil {
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

func validateFontTree(home string) (bool, error) {
	if err := validateAbsoluteDirectoryChain(home); err != nil {
		return false, fmt.Errorf("jetendard: unsafe home: %w", err)
	}
	current := home
	for _, component := range []string{"Library", "Fonts"} {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return false, fmt.Errorf("jetendard: path must be a real directory: %s", current)
		}
	}
	return true, nil
}

func validateAbsoluteDirectoryChain(path string) error {
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) {
		return errors.New("jetendard: directory chain must be absolute")
	}
	volume := filepath.VolumeName(clean)
	root := volume + string(filepath.Separator)
	relative := strings.TrimPrefix(strings.TrimPrefix(clean, volume), string(filepath.Separator))
	current := root
	if err := requireRealDirectory(current); err != nil {
		return fmt.Errorf("jetendard: unsafe directory ancestor %q: %w", current, err)
	}
	if relative == "" {
		return nil
	}
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "" {
			continue
		}
		current = filepath.Join(current, component)
		if err := requireRealDirectory(current); err != nil {
			return fmt.Errorf("jetendard: unsafe directory ancestor %q: %w", current, err)
		}
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
	if err := ensureAbsoluteDirectoryChain(path, 0o700); err != nil {
		return err
	}
	if err := requireRealDirectory(path); err != nil {
		return fmt.Errorf("jetendard: unsafe recovery directory: %w", err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return err
	}
	return syncDirectory(path)
}

func ensureAbsoluteDirectoryChain(path string, mode os.FileMode) error {
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) {
		return errors.New("jetendard: directory chain must be absolute")
	}
	volume := filepath.VolumeName(clean)
	root := volume + string(filepath.Separator)
	relative := strings.TrimPrefix(strings.TrimPrefix(clean, volume), string(filepath.Separator))
	current := root
	if err := requireRealDirectory(current); err != nil {
		return fmt.Errorf("jetendard: unsafe directory ancestor %q: %w", current, err)
	}
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "" {
			continue
		}
		current = filepath.Join(current, component)
		created := false
		if err := os.Mkdir(current, mode); err == nil {
			created = true
		} else if !errors.Is(err, os.ErrExist) {
			return err
		}
		if err := requireRealDirectory(current); err != nil {
			return fmt.Errorf("jetendard: unsafe directory ancestor %q: %w", current, err)
		}
		if created {
			if err := syncDirectory(filepath.Dir(current)); err != nil {
				return err
			}
		}
	}
	return nil
}
func validateOwnedPath(fonts, path string) error {
	relative, err := filepath.Rel(fonts, path)
	if err != nil || filepath.Dir(relative) != "." || !fontPattern.MatchString(relative) {
		return fmt.Errorf("jetendard: unsafe owned font path %q", path)
	}
	return nil
}

func validateOwnership(fonts string, paths map[string]string) error {
	for path, digest := range paths {
		if err := validateOwnedPath(fonts, path); err != nil {
			return err
		}
		if !shaPattern.MatchString(digest) {
			return fmt.Errorf("jetendard: invalid owned font digest for %q", path)
		}
	}
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
