package release

import (
	"bytes"
	"context"
	"crypto/sha256"
	"debug/elf"
	"debug/macho"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	archivepkg "github.com/juty9026/terrapod/internal/resource/archive"
)

type Platform struct{ OS, Arch string }
type Staged struct{ Version, Path string }
type Stager struct {
	ReleaseDir, ActiveRelease  string
	Verifier                   Verifier
	ExpectedPlatform           Platform
	afterSourceCopy            func()
	beforeCommitValidation     func(string)
	afterActivateRename        func() error
	activateSync               func(string) error
	beforeLauncherInstall      func(int) error
	launcherSync               func(string) error
	beforeLauncherBackupRemove func(int) error
	launcherCleanupSync        func(string) error
	beforeRecoveryCleanup      func(string) error
}

type stagedRecord struct {
	Version     string       `json:"version"`
	OS          string       `json:"os"`
	Arch        string       `json:"arch"`
	Binary      Asset        `json:"binary"`
	Source      Asset        `json:"source"`
	Catalog     Asset        `json:"catalog"`
	SourceFiles []stagedFile `json:"sourceFiles"`
}
type stagedFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

func (s Stager) Stage(ctx context.Context, release VerifiedRelease, platform Platform) (Staged, error) {
	manifest, err := s.verifyRelease(release)
	if err != nil {
		return Staged{}, err
	}
	if !supportedPlatform(platform.OS, platform.Arch) {
		return Staged{}, fmt.Errorf("unsupported platform %s/%s", platform.OS, platform.Arch)
	}
	if platform != s.expectedPlatform() {
		return Staged{}, fmt.Errorf("requested platform %s/%s differs from expected %s/%s", platform.OS, platform.Arch, s.expectedPlatform().OS, s.expectedPlatform().Arch)
	}
	if s.ReleaseDir == "" || s.ActiveRelease == "" {
		return Staged{}, errors.New("release paths are required")
	}
	if err := ensureRealDirectory(s.ReleaseDir, 0o755); err != nil {
		return Staged{}, fmt.Errorf("prepare releases: %w", err)
	}
	if filepath.Dir(s.ActiveRelease) != filepath.Dir(s.ReleaseDir) {
		return Staged{}, errors.New("active release and releases must share a parent directory")
	}
	binaryAsset, err := manifest.BinaryAsset(platform.OS, platform.Arch)
	if err != nil {
		return Staged{}, err
	}
	sourceAsset, err := manifest.SourceAsset()
	if err != nil {
		return Staged{}, err
	}
	catalogAsset, err := manifest.CatalogAsset()
	if err != nil {
		return Staged{}, err
	}
	destination := filepath.Join(s.ReleaseDir, manifest.Version)
	if info, statErr := os.Lstat(destination); statErr == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return Staged{}, errors.New("existing release is not a real directory")
		}
		record, err := s.validateInstalledRelease(destination, manifest.Version, release.manifestData, release.signatureData)
		if err != nil {
			return Staged{}, fmt.Errorf("existing release differs: %w", err)
		}
		if record.OS != platform.OS || record.Arch != platform.Arch {
			return Staged{}, errors.New("existing release platform differs")
		}
		return Staged{Version: manifest.Version, Path: destination}, nil
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return Staged{}, statErr
	}

	binaryPath, err := release.localAsset(ctx, binaryAsset)
	if err != nil {
		return Staged{}, err
	}
	sourcePath, err := release.localAsset(ctx, sourceAsset)
	if err != nil {
		return Staged{}, err
	}
	catalogPath, err := release.localAsset(ctx, catalogAsset)
	if err != nil {
		return Staged{}, err
	}
	if err := validateExecutable(binaryPath, platform); err != nil {
		return Staged{}, err
	}

	staging, err := os.MkdirTemp(s.ReleaseDir, ".stage-")
	if err != nil {
		return Staged{}, err
	}
	committed := false
	defer func() {
		if !committed {
			makeWritableForCleanup(staging)
			_ = os.RemoveAll(staging)
		}
	}()
	for _, directory := range []string{"bin", "catalog", ".artifacts"} {
		if err := os.Mkdir(filepath.Join(staging, directory), 0o700); err != nil {
			return Staged{}, err
		}
	}
	if err := writeReadOnlyFile(filepath.Join(staging, "release.json"), release.manifestData); err != nil {
		return Staged{}, err
	}
	if err := writeReadOnlyFile(filepath.Join(staging, "release.json.sig"), release.signatureData); err != nil {
		return Staged{}, err
	}
	if err := copyVerifiedFile(binaryPath, filepath.Join(staging, "bin", "tpod"), binaryAsset, 0o755); err != nil {
		return Staged{}, err
	}
	if err := copyVerifiedFile(catalogPath, filepath.Join(staging, "catalog", "resources.json"), catalogAsset, 0o444); err != nil {
		return Staged{}, err
	}
	if err := copyVerifiedFile(sourcePath, filepath.Join(staging, ".artifacts", "source.tar.gz"), sourceAsset, 0o444); err != nil {
		return Staged{}, err
	}
	if s.afterSourceCopy != nil {
		s.afterSourceCopy()
	}
	files, err := extractSource(filepath.Join(staging, ".artifacts", "source.tar.gz"), filepath.Join(staging, "source"))
	if err != nil {
		return Staged{}, err
	}
	record := stagedRecord{Version: manifest.Version, OS: platform.OS, Arch: platform.Arch, Binary: binaryAsset, Source: sourceAsset, Catalog: catalogAsset, SourceFiles: files}
	if err := writeRecord(filepath.Join(staging, ".stage.json"), record); err != nil {
		return Staged{}, err
	}
	if s.beforeCommitValidation != nil {
		s.beforeCommitValidation(staging)
	}
	if err := makeTreeImmutable(staging); err != nil {
		return Staged{}, err
	}
	if err := syncTree(staging); err != nil {
		return Staged{}, err
	}
	validated, err := s.validateInstalledRelease(staging, manifest.Version, release.manifestData, release.signatureData)
	if err != nil {
		return Staged{}, fmt.Errorf("validate completed release: %w", err)
	}
	if validated.OS != platform.OS || validated.Arch != platform.Arch {
		return Staged{}, errors.New("completed release platform differs")
	}
	if _, err := os.Lstat(destination); err == nil {
		return Staged{}, errors.New("release appeared while staging")
	} else if !errors.Is(err, os.ErrNotExist) {
		return Staged{}, err
	}
	if err := os.Rename(staging, destination); err != nil {
		return Staged{}, fmt.Errorf("commit release: %w", err)
	}
	committed = true
	if err := syncDirectory(s.ReleaseDir); err != nil {
		return Staged{}, err
	}
	return Staged{Version: manifest.Version, Path: destination}, nil
}

// RepairAndActivate commits a signed release and its stable launcher pair as
// one recoverable transaction.
func (s Stager) RepairAndActivate(ctx context.Context, release VerifiedRelease, platform Platform, launcherTargets [2]string) (Staged, error) {
	manifest, err := s.verifyRelease(release)
	if err != nil {
		return Staged{}, err
	}
	if s.ReleaseDir == "" || s.ActiveRelease == "" || filepath.Dir(s.ActiveRelease) != filepath.Dir(s.ReleaseDir) {
		return Staged{}, errors.New("invalid release layout")
	}
	if err := ensureRealDirectory(s.ReleaseDir, 0o755); err != nil {
		return Staged{}, fmt.Errorf("prepare releases: %w", err)
	}
	destination := filepath.Join(s.ReleaseDir, manifest.Version)
	info, statErr := os.Lstat(destination)
	releaseWasAbsent := errors.Is(statErr, os.ErrNotExist)
	releaseBackup := ""
	if statErr != nil && !releaseWasAbsent {
		return Staged{}, statErr
	}
	if !releaseWasAbsent {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return Staged{}, errors.New("existing release is not a real directory")
		}
		if _, err := s.validateInstalledRelease(destination, manifest.Version, release.manifestData, release.signatureData); err != nil {
			releaseBackup, err = s.prepareRecoverySlot(manifest.Version)
			if err != nil {
				return Staged{}, err
			}
			if err := os.Chmod(destination, 0o700); err != nil {
				return Staged{}, err
			}
			if err := os.Rename(destination, releaseBackup); err != nil {
				return Staged{}, err
			}
			if err := errors.Join(syncDirectory(s.ReleaseDir), syncDirectory(filepath.Dir(releaseBackup))); err != nil {
				return Staged{}, errors.Join(err, restoreReplacedRelease(destination, releaseBackup))
			}
		}
	}
	staged, err := s.Stage(ctx, release, platform)
	if err != nil {
		if releaseBackup != "" {
			err = errors.Join(err, restoreReplacedRelease(destination, releaseBackup))
		}
		return Staged{}, err
	}
	if _, err := s.validateInstalledRelease(staged.Path, manifest.Version, release.manifestData, release.signatureData); err != nil {
		return Staged{}, errors.Join(err, s.rollbackRepairRelease(destination, releaseBackup, releaseWasAbsent))
	}
	if err := s.installLaunchersAndActivate(staged, launcherTargets); err != nil {
		return Staged{}, errors.Join(err, s.rollbackRepairRelease(destination, releaseBackup, releaseWasAbsent))
	}
	if releaseBackup != "" {
		var cleanupErr error
		if s.beforeRecoveryCleanup != nil {
			cleanupErr = s.beforeRecoveryCleanup(releaseBackup)
		}
		if cleanupErr == nil {
			makeWritableForCleanup(releaseBackup)
			cleanupErr = os.RemoveAll(releaseBackup)
		}
		if cleanupErr == nil {
			_ = syncDirectory(filepath.Dir(releaseBackup))
		}
	}
	return staged, nil
}

func (s Stager) prepareRecoverySlot(version string) (string, error) {
	recovery := filepath.Join(s.ReleaseDir, ".recovery")
	if err := ensureRealDirectory(recovery, 0o700); err != nil {
		return "", err
	}
	slot := filepath.Join(recovery, version)
	if info, err := os.Lstat(slot); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("release recovery slot is not a real directory")
		}
		makeWritableForCleanup(slot)
		if err := os.RemoveAll(slot); err != nil {
			return "", err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := syncDirectory(recovery); err != nil {
		return "", err
	}
	return slot, nil
}

func reserveSiblingPath(parent, pattern string) (string, error) {
	file, err := os.CreateTemp(parent, pattern)
	if err != nil {
		return "", err
	}
	name := file.Name()
	if err := file.Close(); err != nil {
		return "", err
	}
	if err := os.Remove(name); err != nil {
		return "", err
	}
	return name, nil
}

func (s Stager) rollbackRepairRelease(destination, backup string, wasAbsent bool) error {
	if backup != "" {
		return restoreReplacedRelease(destination, backup)
	}
	if !wasAbsent {
		return nil
	}
	if _, err := os.Lstat(destination); err == nil {
		makeWritableForCleanup(destination)
		if err := os.RemoveAll(destination); err != nil {
			return err
		}
		return syncDirectory(filepath.Dir(destination))
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

type launcherBackup struct {
	backup string
}

type launcherState struct {
	destination string
	kind        string
	data        []byte
	mode        os.FileMode
	linkTarget  string
}

func (s Stager) installLaunchersAndActivate(staged Staged, targets [2]string) error {
	parent, err := validateLauncherTargets(targets)
	if err != nil {
		return err
	}
	if err := ensureRealDirectory(parent, 0o755); err != nil {
		return err
	}
	states := [2]launcherState{}
	for index, target := range targets {
		states[index], err = captureLauncherState(target)
		if err != nil {
			return err
		}
	}
	oldCurrentTarget, oldCurrentExists, err := captureActiveReleaseState(s.ActiveRelease)
	if err != nil {
		return err
	}
	sourceRoot, err := os.OpenRoot(staged.Path)
	if err != nil {
		return err
	}
	defer sourceRoot.Close()
	source, err := sourceRoot.Open("source/scripts/tpod-launcher.sh")
	if err != nil {
		return errors.New("signed release does not contain the stable launcher")
	}
	sourceInfo, err := source.Stat()
	if err != nil || !sourceInfo.Mode().IsRegular() || sourceInfo.Size() <= 0 || sourceInfo.Size() > MaxManifestSize {
		_ = source.Close()
		return errors.New("signed release does not contain a bounded regular stable launcher")
	}
	launcherData, readErr := io.ReadAll(io.LimitReader(source, MaxManifestSize+1))
	closeErr := source.Close()
	if readErr != nil || closeErr != nil || int64(len(launcherData)) != sourceInfo.Size() {
		return errors.New("failed to read signed stable launcher")
	}
	temporaries := [2]string{}
	for index := range targets {
		temporary, err := writeLauncherTemporary(parent, launcherData)
		if err != nil {
			cleanupLauncherTemporaries(temporaries)
			return err
		}
		temporaries[index] = temporary
	}
	defer cleanupLauncherTemporaries(temporaries)
	backups := [2]launcherBackup{}
	for index, destination := range targets {
		info, err := os.Lstat(destination)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			_ = restoreLauncherStates(states, backups)
			return err
		}
		if info.IsDir() {
			_ = restoreLauncherStates(states, backups)
			return fmt.Errorf("launcher destination is a directory: %s", destination)
		}
		backup, err := reserveSiblingPath(parent, ".launcher-backup-")
		if err != nil {
			_ = restoreLauncherStates(states, backups)
			return err
		}
		if err := os.Rename(destination, backup); err != nil {
			_ = restoreLauncherStates(states, backups)
			return err
		}
		backups[index].backup = backup
	}
	for index, destination := range targets {
		if s.beforeLauncherInstall != nil {
			if err := s.beforeLauncherInstall(index); err != nil {
				return errors.Join(err, restoreLauncherStates(states, backups))
			}
		}
		if err := os.Rename(temporaries[index], destination); err != nil {
			return errors.Join(err, restoreLauncherStates(states, backups))
		}
		temporaries[index] = ""
	}
	if err := s.syncLauncherDirectory(parent); err != nil {
		return errors.Join(err, restoreLauncherStates(states, backups))
	}
	for _, destination := range targets {
		info, err := os.Lstat(destination)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o755 {
			return errors.Join(errors.New("stable launcher postcondition failed"), restoreLauncherStates(states, backups))
		}
	}
	if err := s.Activate(staged.Version); err != nil {
		return errors.Join(err, restoreLauncherStates(states, backups))
	}
	var cleanupErr error
	for index, backup := range backups {
		if backup.backup == "" {
			continue
		}
		if s.beforeLauncherBackupRemove != nil {
			cleanupErr = s.beforeLauncherBackupRemove(index)
		}
		if cleanupErr == nil {
			cleanupErr = os.Remove(backup.backup)
		}
		if cleanupErr != nil {
			break
		}
	}
	if cleanupErr == nil {
		if s.launcherCleanupSync != nil {
			cleanupErr = s.launcherCleanupSync(parent)
		} else {
			cleanupErr = syncDirectory(parent)
		}
	}
	if cleanupErr != nil {
		return errors.Join(cleanupErr,
			s.restoreActiveReleaseDurable(oldCurrentTarget, oldCurrentExists),
			restoreLauncherStates(states, backups))
	}
	return nil
}

func validateLauncherTargets(targets [2]string) (string, error) {
	if !filepath.IsAbs(targets[0]) || !filepath.IsAbs(targets[1]) ||
		filepath.Base(targets[0]) != "tpod" || filepath.Base(targets[1]) != "terrapod" ||
		filepath.Dir(targets[0]) != filepath.Dir(targets[1]) {
		return "", errors.New("repair launcher targets are invalid")
	}
	return filepath.Dir(targets[0]), nil
}

func writeLauncherTemporary(parent string, data []byte) (string, error) {
	file, err := os.CreateTemp(parent, ".launcher-stage-")
	if err != nil {
		return "", err
	}
	name := file.Name()
	if err := file.Chmod(0o755); err != nil {
		_ = file.Close()
		_ = os.Remove(name)
		return "", err
	}
	_, writeErr := file.Write(data)
	closeErr := errors.Join(file.Sync(), file.Close())
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(name)
		return "", errors.Join(writeErr, closeErr)
	}
	return name, nil
}

func cleanupLauncherTemporaries(paths [2]string) {
	for _, path := range paths {
		if path != "" {
			_ = os.Remove(path)
		}
	}
}

func captureLauncherState(destination string) (launcherState, error) {
	state := launcherState{destination: destination}
	info, err := os.Lstat(destination)
	if errors.Is(err, os.ErrNotExist) {
		state.kind = "absent"
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		state.kind = "symlink"
		state.linkTarget, err = os.Readlink(destination)
		return state, err
	}
	if !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > MaxManifestSize {
		return state, errors.New("existing launcher must be absent, a symlink, or a bounded regular file")
	}
	file, err := os.Open(destination)
	if err != nil {
		return state, err
	}
	state.data, err = io.ReadAll(io.LimitReader(file, MaxManifestSize+1))
	closeErr := file.Close()
	if err != nil || closeErr != nil || int64(len(state.data)) != info.Size() {
		return state, errors.Join(err, closeErr, errors.New("existing launcher changed while reading"))
	}
	state.kind = "regular"
	state.mode = info.Mode() & (os.ModePerm | os.ModeSetuid | os.ModeSetgid | os.ModeSticky)
	return state, nil
}

func restoreLauncherStates(states [2]launcherState, backups [2]launcherBackup) error {
	var result error
	for _, backup := range backups {
		if backup.backup != "" {
			if err := os.Remove(backup.backup); err != nil && !errors.Is(err, os.ErrNotExist) {
				result = errors.Join(result, err)
			}
		}
	}
	for _, state := range states {
		if state.destination == "" {
			continue
		}
		if err := os.Remove(state.destination); err != nil && !errors.Is(err, os.ErrNotExist) {
			result = errors.Join(result, err)
			continue
		}
		switch state.kind {
		case "absent":
		case "regular":
			temporary, err := writeLauncherTemporary(filepath.Dir(state.destination), state.data)
			if err == nil {
				err = os.Chmod(temporary, state.mode)
			}
			if err == nil {
				err = os.Rename(temporary, state.destination)
			}
			if temporary != "" {
				_ = os.Remove(temporary)
			}
			result = errors.Join(result, err)
		case "symlink":
			temporary, err := reserveSiblingPath(filepath.Dir(state.destination), ".launcher-restore-")
			if err == nil {
				err = os.Symlink(state.linkTarget, temporary)
			}
			if err == nil {
				err = os.Rename(temporary, state.destination)
			}
			if temporary != "" {
				_ = os.Remove(temporary)
			}
			result = errors.Join(result, err)
		default:
			result = errors.Join(result, errors.New("invalid launcher rollback state"))
		}
	}
	if parent := filepath.Dir(states[0].destination); parent != "." && parent != "" {
		result = errors.Join(result, syncDirectory(parent))
	}
	return result
}

func (s Stager) syncLauncherDirectory(path string) error {
	if s.launcherSync != nil {
		return s.launcherSync(path)
	}
	return syncDirectory(path)
}

func restoreReplacedRelease(destination, backup string) error {
	if _, err := os.Lstat(destination); err == nil {
		makeWritableForCleanup(destination)
		if err := os.RemoveAll(destination); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(backup, destination); err != nil {
		return err
	}
	destinationParent := filepath.Dir(destination)
	backupParent := filepath.Dir(backup)
	if destinationParent == backupParent {
		return syncDirectory(destinationParent)
	}
	return errors.Join(syncDirectory(destinationParent), syncDirectory(backupParent))
}

func (s Stager) expectedPlatform() Platform {
	if s.ExpectedPlatform.OS == "" && s.ExpectedPlatform.Arch == "" {
		return Platform{OS: runtime.GOOS, Arch: runtime.GOARCH}
	}
	return s.ExpectedPlatform
}

func (s Stager) Activate(version string) error {
	if !stableSemVerPattern.MatchString(version) {
		return fmt.Errorf("invalid release version %q", version)
	}
	if s.ReleaseDir == "" || s.ActiveRelease == "" || filepath.Dir(s.ReleaseDir) != filepath.Dir(s.ActiveRelease) {
		return errors.New("invalid release layout")
	}
	if err := validateNoSymlinkComponents(filepath.Dir(s.ReleaseDir)); err != nil {
		return err
	}
	destination := filepath.Join(s.ReleaseDir, version)
	info, err := os.Lstat(destination)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("release target must be a real directory")
	}
	if _, err := s.validateInstalledRelease(destination, version, nil, nil); err != nil {
		return fmt.Errorf("validate release target: %w", err)
	}
	oldTarget := ""
	oldExists := false
	if info, err := os.Lstat(s.ActiveRelease); err == nil {
		if info.Mode()&os.ModeSymlink == 0 {
			return errors.New("current release path is not a symlink")
		}
		oldTarget, err = os.Readlink(s.ActiveRelease)
		if err != nil {
			return err
		}
		oldExists = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	parent := filepath.Dir(s.ActiveRelease)
	if err := ensureRealDirectory(parent, 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(parent, ".current-")
	if err != nil {
		return err
	}
	temp := temporary.Name()
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Remove(temp); err != nil {
		return err
	}
	defer os.Remove(temp)
	intendedTarget, err := filepath.Rel(parent, destination)
	if err != nil || intendedTarget == ".." || strings.HasPrefix(intendedTarget, ".."+string(filepath.Separator)) {
		return errors.New("release target escapes data directory")
	}
	if err := os.Symlink(intendedTarget, temp); err != nil {
		return err
	}
	if err := os.Rename(temp, s.ActiveRelease); err != nil {
		return fmt.Errorf("activate release: %w", err)
	}
	var commitErr error
	if s.afterActivateRename != nil {
		commitErr = s.afterActivateRename()
	}
	if commitErr == nil {
		target, err := os.Readlink(s.ActiveRelease)
		if err != nil || target != intendedTarget {
			commitErr = errors.New("active release postcondition failed")
		}
	}
	if commitErr == nil {
		commitErr = s.syncActiveDirectory(parent)
	}
	if commitErr != nil {
		return errors.Join(commitErr, s.restoreActiveRelease(parent, oldTarget, oldExists))
	}
	return nil
}

func (s Stager) syncActiveDirectory(path string) error {
	if s.activateSync != nil {
		return s.activateSync(path)
	}
	return syncDirectory(path)
}

func captureActiveReleaseState(path string) (string, bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return "", false, errors.New("current release path is not a symlink")
	}
	target, err := os.Readlink(path)
	return target, true, err
}

func (s Stager) restoreActiveReleaseDurable(oldTarget string, oldExists bool) error {
	parent := filepath.Dir(s.ActiveRelease)
	if !oldExists {
		if err := os.Remove(s.ActiveRelease); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return syncDirectory(parent)
	}
	temporary, err := reserveSiblingPath(parent, ".current-cleanup-restore-")
	if err != nil {
		return err
	}
	defer os.Remove(temporary)
	if err := os.Symlink(oldTarget, temporary); err != nil {
		return err
	}
	if err := os.Rename(temporary, s.ActiveRelease); err != nil {
		return err
	}
	return syncDirectory(parent)
}

func (s Stager) restoreActiveRelease(parent, oldTarget string, oldExists bool) error {
	if !oldExists {
		if err := os.Remove(s.ActiveRelease); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return s.syncActiveDirectory(parent)
	}
	temporary, err := os.CreateTemp(parent, ".current-restore-")
	if err != nil {
		return err
	}
	name := temporary.Name()
	if err := temporary.Close(); err != nil {
		return err
	}
	defer os.Remove(name)
	if err := os.Remove(name); err != nil {
		return err
	}
	if err := os.Symlink(oldTarget, name); err != nil {
		return err
	}
	if err := os.Rename(name, s.ActiveRelease); err != nil {
		return err
	}
	return s.syncActiveDirectory(parent)
}

// LoadActive re-verifies the signed manifest and every staged artifact before
// an update continuation trusts the active release.
func (s Stager) LoadActive(version string) (Staged, VerifiedRelease, error) {
	if !stableSemVerPattern.MatchString(version) {
		return Staged{}, VerifiedRelease{}, fmt.Errorf("invalid release version %q", version)
	}
	target, err := os.Readlink(s.ActiveRelease)
	if err != nil {
		return Staged{}, VerifiedRelease{}, err
	}
	if filepath.IsAbs(target) {
		return Staged{}, VerifiedRelease{}, errors.New("active release link must be relative")
	}
	activePath := filepath.Clean(filepath.Join(filepath.Dir(s.ActiveRelease), target))
	expected := filepath.Join(s.ReleaseDir, version)
	if activePath != expected {
		return Staged{}, VerifiedRelease{}, errors.New("active release differs from expected version")
	}
	return s.Load(version)
}

func (s Stager) Load(version string) (Staged, VerifiedRelease, error) {
	if !stableSemVerPattern.MatchString(version) {
		return Staged{}, VerifiedRelease{}, fmt.Errorf("invalid release version %q", version)
	}
	expected := filepath.Join(s.ReleaseDir, version)
	manifestData, err := readImmutableFile(filepath.Join(expected, "release.json"), MaxManifestSize)
	if err != nil {
		return Staged{}, VerifiedRelease{}, err
	}
	signatureData, err := readImmutableFile(filepath.Join(expected, "release.json.sig"), MaxManifestSize)
	if err != nil {
		return Staged{}, VerifiedRelease{}, err
	}
	record, err := s.validateInstalledRelease(expected, version, manifestData, signatureData)
	if err != nil {
		return Staged{}, VerifiedRelease{}, err
	}
	manifest, err := s.Verifier.VerifyManifest(manifestData, signatureData)
	if err != nil {
		return Staged{}, VerifiedRelease{}, err
	}
	files := map[string]string{record.Binary.Name: filepath.Join(expected, "bin", "tpod"), record.Catalog.Name: filepath.Join(expected, "catalog", "resources.json"), record.Source.Name: filepath.Join(expected, ".artifacts", "source.tar.gz")}
	verified := VerifiedRelease{Manifest: manifest, Files: files, manifestData: manifestData, signatureData: signatureData}
	if err := verified.sealManifest(); err != nil {
		return Staged{}, VerifiedRelease{}, err
	}
	return Staged{Version: version, Path: expected}, verified, nil
}

func (s Stager) CurrentVersion() (string, error) {
	target, err := os.Readlink(s.ActiveRelease)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(target) {
		return "", errors.New("active release link must be relative")
	}
	clean := filepath.Clean(filepath.Join(filepath.Dir(s.ActiveRelease), target))
	relative, err := filepath.Rel(s.ReleaseDir, clean)
	if err != nil || strings.Contains(relative, string(filepath.Separator)) || !stableSemVerPattern.MatchString(relative) {
		return "", errors.New("active release link has unsafe target")
	}
	return relative, nil
}

func extractSource(source, destination string) ([]stagedFile, error) {
	manifest, err := (archivepkg.Adapter{}).ExtractFile(source, "tar.gz", destination)
	if err != nil {
		return nil, err
	}
	files := make([]stagedFile, len(manifest.Files))
	for index, file := range manifest.Files {
		files[index] = stagedFile{Path: file.Path, SHA256: file.SHA256, Size: file.Size}
	}
	return files, nil
}

func (s Stager) verifyRelease(release VerifiedRelease) (Manifest, error) {
	if err := release.verifySeal(); err != nil {
		return Manifest{}, err
	}
	manifest, err := s.Verifier.VerifyManifest(release.manifestData, release.signatureData)
	if err != nil {
		return Manifest{}, fmt.Errorf("re-verify release manifest: %w", err)
	}
	want, _ := json.Marshal(release.Manifest)
	got, _ := json.Marshal(manifest)
	if !bytes.Equal(got, want) {
		return Manifest{}, errors.New("verified release differs from signed manifest")
	}
	return manifest, nil
}

func validateExecutable(name string, platform Platform) error {
	info, err := os.Stat(name)
	if err != nil {
		return err
	}
	size := uint64(info.Size())
	input, err := os.Open(name)
	if err != nil {
		return err
	}
	magic := make([]byte, 4)
	_, readErr := io.ReadFull(input, magic)
	closeErr := input.Close()
	if readErr != nil || closeErr != nil {
		return errors.New("binary has invalid executable format")
	}
	isELF := bytes.Equal(magic, []byte{0x7f, 'E', 'L', 'F'})
	isMachO := bytes.Equal(magic, []byte{0xcf, 0xfa, 0xed, 0xfe})
	if (platform.OS == "linux" && isMachO) || (platform.OS == "darwin" && isELF) {
		return errors.New("binary executable format does not match operating system")
	}
	if platform.OS == "linux" {
		file, err := elf.Open(name)
		if err != nil {
			return fmt.Errorf("binary has invalid ELF executable format: %w", err)
		}
		defer file.Close()
		if file.Class != elf.ELFCLASS64 || file.Data != elf.ELFDATA2LSB || file.Version != elf.EV_CURRENT || (file.OSABI != elf.ELFOSABI_NONE && file.OSABI != elf.ELFOSABI_LINUX) || (file.Type != elf.ET_EXEC && file.Type != elf.ET_DYN) {
			return errors.New("binary has invalid ELF executable format")
		}
		want := elf.EM_X86_64
		if platform.Arch == "arm64" {
			want = elf.EM_AARCH64
		}
		if file.Machine != want {
			return errors.New("binary executable architecture mismatch")
		}
		if file.Entry == 0 {
			return errors.New("binary ELF entry point must be non-zero")
		}
		loadable := false
		entryValid := false
		for _, program := range file.Progs {
			if program.Type != elf.PT_LOAD {
				continue
			}
			if program.Off > size || program.Filesz > size-program.Off || program.Memsz < program.Filesz {
				return errors.New("binary ELF load segment is out of file bounds")
			}
			if program.Flags&elf.PF_X != 0 && program.Filesz > 0 {
				loadable = true
				if file.Entry >= program.Vaddr && file.Entry-program.Vaddr < program.Filesz {
					entryValid = true
				}
			}
		}
		if !loadable {
			return errors.New("binary has no executable ELF load segment")
		}
		if !entryValid {
			return errors.New("binary ELF entry point is outside executable segments")
		}
		return nil
	}
	if platform.OS == "darwin" {
		file, err := macho.Open(name)
		if err != nil {
			return fmt.Errorf("binary has invalid Mach-O executable format: %w", err)
		}
		defer file.Close()
		if file.Type != macho.TypeExec {
			return errors.New("binary has invalid Mach-O executable format")
		}
		want := macho.CpuAmd64
		if platform.Arch == "arm64" {
			want = macho.CpuArm64
		}
		if file.Cpu != want {
			return errors.New("binary executable architecture mismatch")
		}
		loadable := false
		entryFound := false
		entryOffset := uint64(0)
		entryIsVirtualAddress := false
		for _, load := range file.Loads {
			raw := load.Raw()
			if len(raw) < 8 {
				return errors.New("binary has malformed Mach-O load command")
			}
			command := file.ByteOrder.Uint32(raw[:4])
			if command == 0x80000028 {
				if len(raw) < 24 {
					return errors.New("binary has truncated Mach-O entry command")
				}
				if entryFound {
					return errors.New("binary has duplicate Mach-O entry commands")
				}
				entryFound = true
				entryOffset = file.ByteOrder.Uint64(raw[8:16])
			}
			if command == 0x5 {
				if file.Cpu != macho.CpuAmd64 || len(raw) != 184 ||
					file.ByteOrder.Uint32(raw[4:8]) != 184 ||
					file.ByteOrder.Uint32(raw[8:12]) != 4 ||
					file.ByteOrder.Uint32(raw[12:16]) != 42 {
					return errors.New("binary has malformed Mach-O UNIXTHREAD entry command")
				}
				if entryFound {
					return errors.New("binary has duplicate Mach-O entry commands")
				}
				entryFound = true
				entryIsVirtualAddress = true
				entryOffset = file.ByteOrder.Uint64(raw[144:152])
			}
			if segment, ok := load.(*macho.Segment); ok {
				if segment.Offset > size || segment.Filesz > size-segment.Offset ||
					((segment.Maxprot != 0 || segment.Prot != 0) && segment.Memsz < segment.Filesz) {
					return errors.New("binary Mach-O segment is out of file bounds")
				}
				if segment.Maxprot&4 != 0 && segment.Prot&4 != 0 && segment.Filesz > 0 {
					loadable = true
				}
			}
		}
		if !loadable {
			return errors.New("binary has no Mach-O load segment")
		}
		if !entryFound {
			return errors.New("binary has no Mach-O entry command")
		}
		valid := false
		for _, load := range file.Loads {
			if segment, ok := load.(*macho.Segment); ok && segment.Maxprot&4 != 0 && segment.Prot&4 != 0 && segment.Filesz > 0 {
				start := segment.Offset
				if entryIsVirtualAddress {
					start = segment.Addr
				}
				if entryOffset >= start && entryOffset-start < segment.Filesz {
					valid = true
					break
				}
			}
		}
		if !valid {
			return errors.New("binary Mach-O entry point is outside executable segments")
		}
		return nil
	}
	return fmt.Errorf("unsupported executable operating system %q", platform.OS)
}

func copyVerifiedFile(source, destination string, asset Asset, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("asset %q is not a regular file", asset.Name)
	}
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(out, hash), io.LimitReader(input, asset.Size+1))
	closeErr := errors.Join(out.Sync(), out.Close())
	if copyErr != nil || closeErr != nil {
		return errors.Join(copyErr, closeErr)
	}
	if written != asset.Size {
		return fmt.Errorf("asset %q size mismatch", asset.Name)
	}
	if hex.EncodeToString(hash.Sum(nil)) != asset.SHA256 {
		return fmt.Errorf("asset %q checksum mismatch", asset.Name)
	}
	return os.Chmod(destination, mode)
}
func writeRecord(name string, record stagedRecord) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o400)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(append(data, '\n'))
	return errors.Join(writeErr, file.Sync(), file.Close())
}
func writeReadOnlyFile(name string, data []byte) error {
	if len(data) == 0 || len(data) > MaxManifestSize {
		return errors.New("signed release metadata size is invalid")
	}
	file, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o400)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	return errors.Join(writeErr, file.Sync(), file.Close())
}
func readRecord(name string) (stagedRecord, error) {
	var record stagedRecord
	data, err := readImmutableFile(name, MaxManifestSize)
	if err != nil {
		return record, err
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil {
		return record, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return record, errors.New("invalid release record")
	}
	return record, nil
}

func readImmutableFile(name string, limit int64) ([]byte, error) {
	info, err := os.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o444 || info.Size() <= 0 || info.Size() > limit {
		return nil, fmt.Errorf("%q is not bounded read-only release metadata", name)
	}
	data, err := os.ReadFile(name)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != info.Size() {
		return nil, errors.New("release metadata changed while reading")
	}
	return data, nil
}

func (s Stager) validateInstalledRelease(root, version string, expectedManifest, expectedSignature []byte) (stagedRecord, error) {
	manifestData, err := readImmutableFile(filepath.Join(root, "release.json"), MaxManifestSize)
	if err != nil {
		return stagedRecord{}, err
	}
	signatureData, err := readImmutableFile(filepath.Join(root, "release.json.sig"), MaxManifestSize)
	if err != nil {
		return stagedRecord{}, err
	}
	if expectedManifest != nil && !bytes.Equal(manifestData, expectedManifest) {
		return stagedRecord{}, errors.New("stored signed manifest differs")
	}
	if expectedSignature != nil && !bytes.Equal(signatureData, expectedSignature) {
		return stagedRecord{}, errors.New("stored manifest signature differs")
	}
	manifest, err := s.Verifier.VerifyManifest(manifestData, signatureData)
	if err != nil {
		return stagedRecord{}, err
	}
	if manifest.Version != version {
		return stagedRecord{}, errors.New("signed manifest version differs from release path")
	}
	record, err := readRecord(filepath.Join(root, ".stage.json"))
	if err != nil {
		return stagedRecord{}, err
	}
	expected := s.expectedPlatform()
	if record.OS != expected.OS || record.Arch != expected.Arch {
		return stagedRecord{}, errors.New("release record platform differs from expected platform")
	}
	binaryAsset, err := manifest.BinaryAsset(expected.OS, expected.Arch)
	if err != nil {
		return stagedRecord{}, err
	}
	sourceAsset, err := manifest.SourceAsset()
	if err != nil {
		return stagedRecord{}, err
	}
	catalogAsset, err := manifest.CatalogAsset()
	if err != nil {
		return stagedRecord{}, err
	}
	if record.Version != version || record.Binary != binaryAsset || record.Source != sourceAsset || record.Catalog != catalogAsset {
		return stagedRecord{}, errors.New("release record differs from signed manifest")
	}
	if err := validateExistingRelease(root, record, manifestData, signatureData); err != nil {
		return stagedRecord{}, err
	}
	if err := validateExecutable(filepath.Join(root, "bin", "tpod"), Platform{OS: record.OS, Arch: record.Arch}); err != nil {
		return stagedRecord{}, err
	}
	return record, nil
}

func validateExistingRelease(root string, record stagedRecord, manifestData, signatureData []byte) error {
	for _, file := range record.SourceFiles {
		if file.Size < 0 || !digestPattern.MatchString(file.SHA256) {
			return errors.New("invalid source file record")
		}
	}
	temporary, err := os.MkdirTemp(filepath.Dir(root), ".verify-source-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temporary)
	extracted, err := extractSource(filepath.Join(root, ".artifacts", "source.tar.gz"), temporary)
	if err != nil {
		return err
	}
	if !equalStagedFiles(extracted, record.SourceFiles) {
		return errors.New("extracted source differs from signed archive")
	}
	expected := map[string]stagedFile{}
	expectedDirs := map[string]bool{"bin": true, "source": true, "catalog": true, ".artifacts": true}
	for _, file := range record.SourceFiles {
		if _, duplicate := expected[file.Path]; duplicate {
			return errors.New("duplicate source file record")
		}
		expected[filepath.Join("source", filepath.FromSlash(file.Path))] = file
		for parent := filepath.Dir(filepath.Join("source", filepath.FromSlash(file.Path))); parent != "."; parent = filepath.Dir(parent) {
			expectedDirs[parent] = true
		}
	}
	fixed := map[string]Asset{"bin/tpod": record.Binary, "catalog/resources.json": record.Catalog, ".artifacts/source.tar.gz": record.Source, "release.json": bytesAsset("release.json", manifestData), "release.json.sig": bytesAsset("release.json.sig", signatureData)}
	seen := map[string]bool{}
	err = filepath.WalkDir(root, func(name string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, _ := filepath.Rel(root, name)
		if relative == "." {
			return requireImmutableEntry(name, entry, true)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("release contains a symlink")
		}
		if entry.IsDir() {
			if relative != "." && !expectedDirs[relative] {
				return fmt.Errorf("unexpected release directory %q", relative)
			}
			return requireImmutableEntry(name, entry, true)
		}
		if !entry.Type().IsRegular() {
			return errors.New("release contains a special file")
		}
		if err := requireImmutableEntry(name, entry, false); err != nil {
			return err
		}
		slash := filepath.ToSlash(relative)
		if slash == ".stage.json" {
			info, _ := entry.Info()
			if info.Mode().Perm() != 0o444 {
				return errors.New("release record mode mismatch")
			}
			seen[slash] = true
			return nil
		}
		if asset, ok := fixed[slash]; ok {
			match, err := matchingRegularFile(name, asset)
			if err != nil || !match {
				return fmt.Errorf("asset %q mismatch", slash)
			}
			info, _ := entry.Info()
			wantMode := os.FileMode(0o444)
			if slash == "bin/tpod" {
				wantMode = 0o755
			}
			if info.Mode().Perm() != wantMode {
				return fmt.Errorf("asset %q mode mismatch", slash)
			}
			seen[slash] = true
			return nil
		}
		file, ok := expected[relative]
		if !ok {
			return fmt.Errorf("unexpected release file %q", relative)
		}
		asset := Asset{Name: file.Path, Size: file.Size, SHA256: file.SHA256}
		match, err := matchingRegularFile(name, asset)
		if err != nil || !match {
			return fmt.Errorf("source file %q mismatch", file.Path)
		}
		info, _ := entry.Info()
		if info.Mode().Perm() != 0o444 {
			return fmt.Errorf("source file %q mode mismatch", file.Path)
		}
		seen[relative] = true
		return nil
	})
	if err != nil {
		return err
	}
	for name := range fixed {
		if !seen[name] {
			return fmt.Errorf("missing release file %q", name)
		}
	}
	for name := range expected {
		if !seen[name] {
			return fmt.Errorf("missing source file %q", name)
		}
	}
	if !seen[".stage.json"] {
		return errors.New("missing release record")
	}
	return nil
}
func bytesAsset(name string, data []byte) Asset {
	digest := sha256.Sum256(data)
	return Asset{Name: name, Size: int64(len(data)), SHA256: hex.EncodeToString(digest[:])}
}
func requireImmutableEntry(name string, entry os.DirEntry, directory bool) error {
	info, err := entry.Info()
	if err != nil {
		return err
	}
	allowedWrite := os.FileMode(0)
	if !directory && filepath.Base(name) == "tpod" && filepath.Base(filepath.Dir(name)) == "bin" {
		allowedWrite = 0o200
	}
	if info.Mode().Perm()&0o222 != allowedWrite {
		return fmt.Errorf("release entry %q is writable", name)
	}
	if directory && !info.IsDir() {
		return fmt.Errorf("release entry %q is not directory", name)
	}
	if directory && info.Mode().Perm() != 0o555 {
		return fmt.Errorf("release directory %q mode mismatch", name)
	}
	return nil
}
func makeTreeImmutable(root string) error {
	var dirs []string
	err := filepath.WalkDir(root, func(name string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("staged release contains symlink")
		}
		if entry.IsDir() {
			dirs = append(dirs, name)
			return nil
		}
		if !entry.Type().IsRegular() {
			return errors.New("staged release contains special file")
		}
		if filepath.ToSlash(strings.TrimPrefix(name, root+string(filepath.Separator))) == "bin/tpod" {
			return os.Chmod(name, 0o755)
		}
		return os.Chmod(name, 0o444)
	})
	if err != nil {
		return err
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		if err := os.Chmod(dirs[i], 0o555); err != nil {
			return err
		}
	}
	return nil
}
func syncTree(root string) error {
	var directories []string
	err := filepath.WalkDir(root, func(name string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			directories = append(directories, name)
			return nil
		}
		file, err := os.Open(name)
		if err != nil {
			return err
		}
		return errors.Join(file.Sync(), file.Close())
	})
	if err != nil {
		return err
	}
	for index := len(directories) - 1; index >= 0; index-- {
		if err := syncDirectory(directories[index]); err != nil {
			return err
		}
	}
	return nil
}

func equalStagedFiles(left, right []stagedFile) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func makeWritableForCleanup(root string) {
	_ = filepath.WalkDir(root, func(name string, entry os.DirEntry, err error) error {
		if err == nil && entry.Type().IsDir() {
			_ = os.Chmod(name, 0o700)
		}
		return nil
	})
}

func ensureRealDirectory(name string, mode os.FileMode) error {
	if !filepath.IsAbs(name) {
		return errors.New("directory path must be absolute")
	}
	clean := filepath.Clean(name)
	volume := filepath.VolumeName(clean)
	current := volume + string(filepath.Separator)
	relative := strings.TrimPrefix(strings.TrimPrefix(clean, volume), string(filepath.Separator))
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "" {
			continue
		}
		current = filepath.Join(current, component)
		if err := os.Mkdir(current, mode); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
		info, err := os.Lstat(current)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("directory ancestor %q is a symlink or not a directory", current)
		}
	}
	return nil
}
func validateNoSymlinkComponents(name string) error {
	if !filepath.IsAbs(name) {
		return errors.New("path must be absolute")
	}
	clean := filepath.Clean(name)
	volume := filepath.VolumeName(clean)
	current := volume + string(filepath.Separator)
	relative := strings.TrimPrefix(strings.TrimPrefix(clean, volume), string(filepath.Separator))
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "" {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("path ancestor %q is a symlink or not a directory", current)
		}
	}
	return nil
}
func syncDirectory(name string) error {
	directory, err := os.Open(name)
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}
