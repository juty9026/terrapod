package release

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	archivepkg "github.com/juty9026/terrapod/internal/resource/archive"
)

type Platform struct{ OS, Arch string }
type Staged struct{ Version, Path string }
type Stager struct{ ReleaseDir, ActiveRelease string }

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
	if err := release.verifySeal(); err != nil {
		return Staged{}, err
	}
	if !supportedPlatform(platform.OS, platform.Arch) {
		return Staged{}, fmt.Errorf("unsupported platform %s/%s", platform.OS, platform.Arch)
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
	binaryAsset, err := release.Manifest.BinaryAsset(platform.OS, platform.Arch)
	if err != nil {
		return Staged{}, err
	}
	sourceAsset, err := release.Manifest.SourceAsset()
	if err != nil {
		return Staged{}, err
	}
	catalogAsset, err := release.Manifest.CatalogAsset()
	if err != nil {
		return Staged{}, err
	}
	destination := filepath.Join(s.ReleaseDir, release.Manifest.Version)
	if info, statErr := os.Lstat(destination); statErr == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return Staged{}, errors.New("existing release is not a real directory")
		}
		if err := validateExistingRelease(destination, stagedRecord{Version: release.Manifest.Version, OS: platform.OS, Arch: platform.Arch, Binary: binaryAsset, Source: sourceAsset, Catalog: catalogAsset}); err != nil {
			return Staged{}, fmt.Errorf("existing release differs: %w", err)
		}
		return Staged{Version: release.Manifest.Version, Path: destination}, nil
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
	if err := copyVerifiedFile(binaryPath, filepath.Join(staging, "bin", "tpod"), binaryAsset, 0o755); err != nil {
		return Staged{}, err
	}
	if err := copyVerifiedFile(catalogPath, filepath.Join(staging, "catalog", "resources.json"), catalogAsset, 0o444); err != nil {
		return Staged{}, err
	}
	if err := copyVerifiedFile(sourcePath, filepath.Join(staging, ".artifacts", "source.tar.gz"), sourceAsset, 0o444); err != nil {
		return Staged{}, err
	}
	files, err := extractSource(sourcePath, filepath.Join(staging, "source"))
	if err != nil {
		return Staged{}, err
	}
	record := stagedRecord{Version: release.Manifest.Version, OS: platform.OS, Arch: platform.Arch, Binary: binaryAsset, Source: sourceAsset, Catalog: catalogAsset, SourceFiles: files}
	if err := writeRecord(filepath.Join(staging, ".release.json"), record); err != nil {
		return Staged{}, err
	}
	if err := makeTreeImmutable(staging); err != nil {
		return Staged{}, err
	}
	if err := syncTree(staging); err != nil {
		return Staged{}, err
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
	return Staged{Version: release.Manifest.Version, Path: destination}, nil
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
	recordPath := filepath.Join(destination, ".release.json")
	recordInfo, err := os.Lstat(recordPath)
	if err != nil || !recordInfo.Mode().IsRegular() {
		return errors.New("release target has no regular release record")
	}
	record, err := readRecord(recordPath)
	if err != nil {
		return fmt.Errorf("validate release target: %w", err)
	}
	if record.Version != version {
		return errors.New("release target version does not match requested version")
	}
	if info, err := os.Lstat(s.ActiveRelease); err == nil && info.Mode()&os.ModeSymlink == 0 {
		return errors.New("current release path is not a symlink")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
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
	target, err := filepath.Rel(parent, destination)
	if err != nil || target == ".." || strings.HasPrefix(target, ".."+string(filepath.Separator)) {
		return errors.New("release target escapes data directory")
	}
	if err := os.Symlink(target, temp); err != nil {
		return err
	}
	if err := os.Rename(temp, s.ActiveRelease); err != nil {
		return fmt.Errorf("activate release: %w", err)
	}
	return syncDirectory(parent)
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

func validateExecutable(name string, platform Platform) error {
	file, err := os.Open(name)
	if err != nil {
		return err
	}
	defer file.Close()
	header := make([]byte, 64)
	if _, err := io.ReadFull(file, header); err != nil {
		return errors.New("binary has invalid executable format")
	}
	if string(header[:4]) == "\x7fELF" {
		if platform.OS != "linux" || header[4] != 2 || header[5] != 1 || header[6] != 1 || (header[7] != 0 && header[7] != 3) {
			return errors.New("binary executable format does not match operating system")
		}
		kind := binary.LittleEndian.Uint16(header[16:18])
		if kind != 2 && kind != 3 {
			return errors.New("binary has invalid ELF executable type")
		}
		machine := binary.LittleEndian.Uint16(header[18:20])
		want := uint16(62)
		if platform.Arch == "arm64" {
			want = 183
		}
		if machine != want {
			return errors.New("binary executable architecture mismatch")
		}
		return nil
	}
	magic := binary.LittleEndian.Uint32(header[:4])
	if magic == 0xfeedfacf {
		if platform.OS != "darwin" {
			return errors.New("binary executable format does not match operating system")
		}
		cpu := binary.LittleEndian.Uint32(header[4:8])
		want := uint32(0x01000007)
		if platform.Arch == "arm64" {
			want = 0x0100000c
		}
		if cpu != want {
			return errors.New("binary executable architecture mismatch")
		}
		kind := binary.LittleEndian.Uint32(header[12:16])
		if kind != 2 {
			return errors.New("binary has invalid Mach-O executable type")
		}
		return nil
	}
	return errors.New("binary has invalid executable format")
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
func readRecord(name string) (stagedRecord, error) {
	var record stagedRecord
	data, err := os.ReadFile(name)
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

func validateExistingRelease(root string, want stagedRecord) error {
	record, err := readRecord(filepath.Join(root, ".release.json"))
	if err != nil {
		return err
	}
	if record.Version != want.Version || record.OS != want.OS || record.Arch != want.Arch || record.Binary != want.Binary || record.Source != want.Source || record.Catalog != want.Catalog {
		return errors.New("release record mismatch")
	}
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
	fixed := map[string]Asset{"bin/tpod": record.Binary, "catalog/resources.json": record.Catalog, ".artifacts/source.tar.gz": record.Source}
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
		if slash == ".release.json" {
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
	if !seen[".release.json"] {
		return errors.New("missing release record")
	}
	return nil
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
		if err == nil {
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
