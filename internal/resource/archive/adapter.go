// Package archive downloads and extracts catalog-pinned assets without
// executing archive content or trusting archive paths.
package archive

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	defaultDownloadBytes = int64(256 << 20)
	defaultExpandedBytes = int64(512 << 20)
	defaultEntryBytes    = int64(64 << 20)
	defaultFiles         = 4096
)

var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type Asset struct {
	URL    string
	SHA256 string
	Format string
}

type Limits struct {
	DownloadBytes int64
	ExpandedBytes int64
	EntryBytes    int64
	Files         int
}

type File struct {
	Path   string
	SHA256 string
	Size   int64
}

type Manifest struct{ Files []File }

type Adapter struct {
	HTTP     *http.Client
	CacheDir string
	Limits   Limits
}

func (a Adapter) limits() Limits {
	l := a.Limits
	if l.DownloadBytes <= 0 {
		l.DownloadBytes = defaultDownloadBytes
	}
	if l.ExpandedBytes <= 0 {
		l.ExpandedBytes = defaultExpandedBytes
	}
	if l.EntryBytes <= 0 {
		l.EntryBytes = defaultEntryBytes
	}
	if l.Files <= 0 {
		l.Files = defaultFiles
	}
	return l
}

func (a Adapter) Fetch(ctx context.Context, asset Asset) (string, error) {
	if a.HTTP == nil {
		return "", errors.New("archive: HTTP client is required")
	}
	if a.CacheDir == "" {
		return "", errors.New("archive: cache directory is required")
	}
	digest := strings.TrimPrefix(strings.ToLower(asset.SHA256), "sha256:")
	if !digestPattern.MatchString(digest) {
		return "", errors.New("archive: invalid SHA-256")
	}
	if !strings.HasPrefix(asset.URL, "https://") && !strings.HasPrefix(asset.URL, "http://127.0.0.1:") && !strings.HasPrefix(asset.URL, "http://[::1]:") {
		return "", errors.New("archive: asset URL must use HTTPS")
	}
	if err := ensureAbsoluteDirectoryChain(a.CacheDir, 0o700); err != nil {
		return "", fmt.Errorf("archive: create cache: %w", err)
	}
	cachePath := filepath.Join(a.CacheDir, digest+".asset")
	if ok, err := fileMatches(cachePath, digest); err != nil {
		return "", err
	} else if ok {
		return cachePath, nil
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return "", fmt.Errorf("archive: build request: %w", err)
	}
	response, err := a.HTTP.Do(request)
	if err != nil {
		return "", fmt.Errorf("archive: download: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return "", fmt.Errorf("archive: download status %s", response.Status)
	}
	temporary, err := os.CreateTemp(a.CacheDir, ".download-*")
	if err != nil {
		return "", err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	limit := a.limits().DownloadBytes
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temporary, hash), io.LimitReader(response.Body, limit+1))
	closeErr := temporary.Close()
	if copyErr != nil {
		return "", fmt.Errorf("archive: download: %w", copyErr)
	}
	if closeErr != nil {
		return "", fmt.Errorf("archive: close download: %w", closeErr)
	}
	if written > limit {
		return "", errors.New("archive: download limit exceeded")
	}
	if actual := hex.EncodeToString(hash.Sum(nil)); actual != digest {
		return "", fmt.Errorf("archive: checksum mismatch: got %s", actual)
	}
	if err := os.Chmod(temporaryPath, 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(temporaryPath, cachePath); err != nil {
		return "", fmt.Errorf("archive: commit cache: %w", err)
	}
	return cachePath, nil
}

func fileMatches(name, digest string) (bool, error) {
	file, err := os.Open(name)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, errors.New("archive: cache entry is not a regular file")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return false, err
	}
	return hex.EncodeToString(hash.Sum(nil)) == digest, nil
}

func (a Adapter) FetchAndExtract(ctx context.Context, asset Asset, destination string) (Manifest, error) {
	archivePath, err := a.Fetch(ctx, asset)
	if err != nil {
		return Manifest{}, err
	}
	parent := filepath.Dir(destination)
	if err := ensureAbsoluteDirectoryChain(parent, 0o700); err != nil {
		return Manifest{}, err
	}
	staging, err := os.MkdirTemp(parent, ".extract-*")
	if err != nil {
		return Manifest{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(staging)
		}
	}()
	manifest, err := a.extract(archivePath, asset.Format, staging)
	if err != nil {
		return Manifest{}, err
	}
	backup := ""
	if info, statErr := os.Lstat(destination); statErr == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return Manifest{}, errors.New("archive: destination must be a real directory")
		}
		backup, err = os.MkdirTemp(parent, ".replaced-*")
		if err != nil {
			return Manifest{}, err
		}
		if err := os.Remove(backup); err != nil {
			return Manifest{}, err
		}
		if err := os.Rename(destination, backup); err != nil {
			return Manifest{}, fmt.Errorf("archive: preserve destination: %w", err)
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return Manifest{}, statErr
	}
	if err := os.Rename(staging, destination); err != nil {
		if backup != "" {
			_ = os.Rename(backup, destination)
		}
		return Manifest{}, fmt.Errorf("archive: install staging: %w", err)
	}
	committed = true
	if backup != "" {
		_ = os.RemoveAll(backup)
	}
	return manifest, nil
}

func ensureAbsoluteDirectoryChain(path string, mode os.FileMode) error {
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) {
		return errors.New("archive: directory chain must be absolute")
	}
	volume := filepath.VolumeName(clean)
	root := volume + string(filepath.Separator)
	relative := strings.TrimPrefix(strings.TrimPrefix(clean, volume), string(filepath.Separator))
	current := root
	rootInfo, err := os.Lstat(current)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("archive: unsafe directory ancestor %q", current)
	}
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
			return fmt.Errorf("archive: unsafe directory ancestor %q", current)
		}
	}
	return nil
}

func (a Adapter) extract(source, format, staging string) (Manifest, error) {
	switch format {
	case "zip":
		return a.extractZip(source, staging)
	case "tar":
		return a.extractTar(source, staging)
	default:
		return Manifest{}, fmt.Errorf("archive: unsupported format %q", format)
	}
}

type entry struct {
	name string
	size int64
	open func() (io.ReadCloser, error)
}

func (a Adapter) extractZip(source, staging string) (Manifest, error) {
	reader, err := zip.OpenReader(source)
	if err != nil {
		return Manifest{}, fmt.Errorf("archive: open zip: %w", err)
	}
	defer reader.Close()
	if len(reader.File) > a.limits().Files {
		return Manifest{}, errors.New("archive: entry count limit exceeded")
	}
	entries := make([]entry, 0, len(reader.File))
	seen := make(map[string]bool)
	for _, file := range reader.File {
		name, err := validateName(file.Name)
		if err != nil {
			return Manifest{}, err
		}
		mode := file.Mode()
		if mode&os.ModeSymlink != 0 || (!mode.IsRegular() && !mode.IsDir()) {
			return Manifest{}, fmt.Errorf("archive: unsupported zip entry %q", file.Name)
		}
		if err := reserveTarget(seen, name, mode.IsDir()); err != nil {
			return Manifest{}, err
		}
		if mode.IsDir() {
			continue
		}
		file := file
		entries = append(entries, entry{name: name, size: int64(file.UncompressedSize64), open: func() (io.ReadCloser, error) { return file.Open() }})
	}
	return a.writeEntries(staging, entries)
}

func (a Adapter) extractTar(source, staging string) (Manifest, error) {
	file, err := os.Open(source)
	if err != nil {
		return Manifest{}, err
	}
	defer file.Close()
	reader := tar.NewReader(bufio.NewReader(file))
	entries := make([]entry, 0)
	seen := make(map[string]bool)
	limits := a.limits()
	var declaredExpanded int64
	// tar.Reader is streaming, so spool validated regular files privately before install.
	spool, err := os.MkdirTemp(filepath.Dir(staging), ".tar-spool-*")
	if err != nil {
		return Manifest{}, err
	}
	defer os.RemoveAll(spool)
	for index := 0; ; index++ {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Manifest{}, fmt.Errorf("archive: read tar: %w", err)
		}
		if index >= limits.Files {
			return Manifest{}, errors.New("archive: entry count limit exceeded")
		}
		name, err := validateName(header.Name)
		if err != nil {
			return Manifest{}, err
		}
		isDir := header.Typeflag == tar.TypeDir
		if header.Typeflag != tar.TypeReg && !isDir {
			return Manifest{}, fmt.Errorf("archive: unsupported tar entry %q", header.Name)
		}
		if err := reserveTarget(seen, name, isDir); err != nil {
			return Manifest{}, err
		}
		if isDir {
			continue
		}
		if header.Size < 0 || header.Size > limits.EntryBytes {
			return Manifest{}, fmt.Errorf("archive: entry %q exceeds limit", name)
		}
		if declaredExpanded > limits.ExpandedBytes-header.Size {
			return Manifest{}, fmt.Errorf("archive: expanded size limit exceeded at %q", name)
		}
		declaredExpanded += header.Size
		spooled := filepath.Join(spool, fmt.Sprintf("%08d", index))
		output, err := os.OpenFile(spooled, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return Manifest{}, err
		}
		written, copyErr := io.Copy(output, io.LimitReader(reader, header.Size+1))
		closeErr := output.Close()
		if copyErr != nil || closeErr != nil {
			return Manifest{}, errors.Join(copyErr, closeErr)
		}
		if written != header.Size {
			return Manifest{}, fmt.Errorf("archive: entry %q exceeds limit or is truncated", name)
		}
		pathCopy := spooled
		entries = append(entries, entry{name: name, size: written, open: func() (io.ReadCloser, error) { return os.Open(pathCopy) }})
	}
	return a.writeEntries(staging, entries)
}

func validateName(name string) (string, error) {
	if name == "" || strings.ContainsRune(name, 0) || strings.Contains(name, `\`) || strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("archive: unsafe absolute or malformed path %q", name)
	}
	trimmed := strings.TrimSuffix(name, "/")
	cleaned := path.Clean(trimmed)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || cleaned != trimmed {
		return "", fmt.Errorf("archive: unsafe traversal path %q", name)
	}
	return cleaned, nil
}

func reserveTarget(seen map[string]bool, name string, directory bool) error {
	if _, exists := seen[name]; exists {
		return fmt.Errorf("archive: duplicate target %q", name)
	}
	for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
		if dir, exists := seen[parent]; exists && !dir {
			return fmt.Errorf("archive: target parent %q is a file", parent)
		}
	}
	if !directory {
		prefix := name + "/"
		for existing := range seen {
			if strings.HasPrefix(existing, prefix) {
				return fmt.Errorf("archive: target %q replaces a directory", name)
			}
		}
	}
	seen[name] = directory
	return nil
}

func (a Adapter) writeEntries(staging string, entries []entry) (Manifest, error) {
	l := a.limits()
	if len(entries) > l.Files {
		return Manifest{}, errors.New("archive: file count limit exceeded")
	}
	var expanded int64
	manifest := Manifest{Files: make([]File, 0, len(entries))}
	for _, item := range entries {
		if item.size < 0 || item.size > l.EntryBytes || expanded > l.ExpandedBytes-item.size {
			return Manifest{}, fmt.Errorf("archive: expanded size limit exceeded at %q", item.name)
		}
		expanded += item.size
		destination := filepath.Join(staging, filepath.FromSlash(item.name))
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			return Manifest{}, err
		}
		input, err := item.open()
		if err != nil {
			return Manifest{}, err
		}
		output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			input.Close()
			return Manifest{}, err
		}
		hash := sha256.New()
		written, copyErr := io.Copy(io.MultiWriter(output, hash), io.LimitReader(input, item.size+1))
		closeErr := errors.Join(input.Close(), output.Close())
		if copyErr != nil || closeErr != nil {
			return Manifest{}, errors.Join(copyErr, closeErr)
		}
		if written != item.size {
			return Manifest{}, fmt.Errorf("archive: entry %q size mismatch", item.name)
		}
		manifest.Files = append(manifest.Files, File{Path: item.name, SHA256: hex.EncodeToString(hash.Sum(nil)), Size: written})
	}
	sort.Slice(manifest.Files, func(i, j int) bool { return manifest.Files[i].Path < manifest.Files[j].Path })
	return manifest, nil
}
