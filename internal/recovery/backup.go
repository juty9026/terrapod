package recovery

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

var journalPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
var afterBackupRootsOpened func()

// Backup stores recovery copies under Root/<journal>/<path relative to Base>.
type Backup struct{ Root, Base string }

func (b Backup) Save(journal, path string) (retErr error) {
	if !journalPattern.MatchString(journal) {
		return fmt.Errorf("recovery: unsafe journal ID %q", journal)
	}
	basePath, rootPath := filepath.Clean(b.Base), filepath.Clean(b.Root)
	if !filepath.IsAbs(basePath) || !filepath.IsAbs(rootPath) || !filepath.IsAbs(path) || strings.IndexByte(path, 0) >= 0 {
		return errors.New("recovery: base, root, and source must be clean absolute paths")
	}
	relative, err := filepath.Rel(basePath, filepath.Clean(path))
	if err != nil || !safeRelative(relative) {
		return fmt.Errorf("recovery: source %q escapes base", path)
	}
	if err := os.MkdirAll(rootPath, 0o700); err != nil {
		return fmt.Errorf("recovery: create root: %w", err)
	}
	base, baseInfo, err := openAnchoredRoot(basePath)
	if err != nil {
		return fmt.Errorf("recovery: open base: %w", err)
	}
	defer base.Close()
	root, rootInfo, err := openAnchoredRoot(rootPath)
	if err != nil {
		return fmt.Errorf("recovery: open root: %w", err)
	}
	defer root.Close()
	if afterBackupRootsOpened != nil {
		afterBackupRootsOpened()
	}
	if err := verifyRealParents(base, relative); err != nil {
		return fmt.Errorf("recovery: unsafe source parents: %w", err)
	}
	destination := filepath.Join(journal, relative)
	if err := ensureRealParents(root, filepath.Dir(destination)); err != nil {
		return fmt.Errorf("recovery: create destination parents: %w", err)
	}
	sourceInfo, err := base.Lstat(relative)
	if err != nil {
		return fmt.Errorf("recovery: inspect source: %w", err)
	}
	if _, err := root.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			same, compareErr := sameCapturedObject(base, relative, sourceInfo, root, destination)
			if compareErr != nil {
				return compareErr
			}
			if same {
				return nil
			}
			return fmt.Errorf("recovery: existing backup for %q differs from current object", relative)
		}
		return err
	}
	token := make([]byte, 12)
	if _, err := rand.Read(token); err != nil {
		return err
	}
	temporary := filepath.Join(filepath.Dir(destination), ".tpod-backup-"+hex.EncodeToString(token))
	defer func() {
		if retErr != nil {
			_ = root.Remove(temporary)
		}
	}()
	if sourceInfo.Mode()&os.ModeSymlink != 0 {
		target, err := base.Readlink(relative)
		if err != nil || target == "" || strings.IndexByte(target, 0) >= 0 {
			return errors.New("recovery: invalid symlink target")
		}
		if err := root.Symlink(target, temporary); err != nil {
			return fmt.Errorf("recovery: stage symlink: %w", err)
		}
		if current, err := base.Readlink(relative); err != nil || current != target {
			return errors.New("recovery: symlink changed during backup")
		}
	} else {
		if !sourceInfo.Mode().IsRegular() {
			return fmt.Errorf("recovery: source %q is not a regular file or symlink", relative)
		}
		fd, err := base.OpenFile(relative, os.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
		if err != nil {
			return fmt.Errorf("recovery: open source: %w", err)
		}
		opened, err := fd.Stat()
		if err != nil || !opened.Mode().IsRegular() || !os.SameFile(sourceInfo, opened) {
			fd.Close()
			return errors.New("recovery: source changed before backup")
		}
		out, err := root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, sourceInfo.Mode().Perm())
		if err != nil {
			fd.Close()
			return fmt.Errorf("recovery: create staged backup: %w", err)
		}
		hash := sha256.New()
		_, copyErr := io.Copy(io.MultiWriter(out, hash), fd)
		chmodErr := out.Chmod(sourceInfo.Mode().Perm())
		syncErr := out.Sync()
		closeErr := errors.Join(fd.Close(), out.Close())
		if err := errors.Join(copyErr, syncErr, chmodErr, closeErr); err != nil {
			return fmt.Errorf("recovery: copy file: %w", err)
		}
		currentDigest, currentInfo, err := digestRegular(base, relative)
		if err != nil || !os.SameFile(sourceInfo, currentInfo) || !equalDigest(hash.Sum(nil), currentDigest) {
			return errors.New("recovery: source changed during backup")
		}
	}
	if err := verifyAnchoredPath(basePath, baseInfo, base); err != nil {
		return err
	}
	if err := verifyAnchoredPath(rootPath, rootInfo, root); err != nil {
		return err
	}
	if _, err := root.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		return errors.New("recovery: destination appeared before commit")
	}
	if err := root.Rename(temporary, destination); err != nil {
		return fmt.Errorf("recovery: commit backup: %w", err)
	}
	return syncDirectory(root, filepath.Dir(destination))
}

func sameCapturedObject(base *os.Root, source string, sourceInfo os.FileInfo, backup *os.Root, destination string) (bool, error) {
	backupInfo, err := backup.Lstat(destination)
	if err != nil {
		return false, err
	}
	if sourceInfo.Mode()&os.ModeSymlink != 0 {
		return sameSymlink(base, source, sourceInfo, backup, destination, backupInfo)
	}
	if !sourceInfo.Mode().IsRegular() || !backupInfo.Mode().IsRegular() {
		return false, nil
	}
	sourceDigest, currentSource, err := digestRegular(base, source)
	if err != nil {
		return false, err
	}
	backupDigest, currentBackup, err := digestRegular(backup, destination)
	if err != nil {
		return false, err
	}
	return os.SameFile(sourceInfo, currentSource) && os.SameFile(backupInfo, currentBackup) && sourceInfo.Mode().Perm() == backupInfo.Mode().Perm() && equalDigest(sourceDigest, backupDigest), nil
}
func sameSymlink(base *os.Root, source string, sourceInfo os.FileInfo, backup *os.Root, destination string, backupInfo os.FileInfo) (bool, error) {
	if backupInfo.Mode()&os.ModeSymlink == 0 {
		return false, nil
	}
	left, err := base.Readlink(source)
	if err != nil {
		return false, err
	}
	right, err := backup.Readlink(destination)
	if err != nil {
		return false, err
	}
	currentSource, err := base.Lstat(source)
	if err != nil {
		return false, err
	}
	currentBackup, err := backup.Lstat(destination)
	if err != nil {
		return false, err
	}
	return os.SameFile(sourceInfo, currentSource) && os.SameFile(backupInfo, currentBackup) && left == right, nil
}

func openAnchoredRoot(path string) (*os.Root, os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, nil, errors.New("path is not a real directory")
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, nil, err
	}
	anchored, err := root.Stat(".")
	if err != nil || !os.SameFile(info, anchored) {
		root.Close()
		return nil, nil, errors.New("root changed while opening")
	}
	return root, info, nil
}
func verifyAnchoredPath(path string, expected os.FileInfo, root *os.Root) error {
	current, err := os.Lstat(path)
	if err != nil || current.Mode()&os.ModeSymlink != 0 || !os.SameFile(expected, current) {
		return errors.New("recovery: root path changed during backup")
	}
	anchored, err := root.Stat(".")
	if err != nil || !os.SameFile(expected, anchored) {
		return errors.New("recovery: anchored root identity changed")
	}
	return nil
}
func safeRelative(path string) bool {
	return path != "." && !filepath.IsAbs(path) && path != ".." && !strings.HasPrefix(path, ".."+string(filepath.Separator)) && filepath.Clean(path) == path
}
func verifyRealParents(root *os.Root, path string) error {
	parent := filepath.Dir(path)
	if parent == "." {
		return nil
	}
	current := ""
	for _, part := range strings.Split(parent, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := root.Lstat(current)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("parent %q is not a real directory", current)
		}
	}
	return nil
}
func ensureRealParents(root *os.Root, path string) error {
	if path == "." {
		return nil
	}
	current := ""
	for _, part := range strings.Split(path, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := root.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			if err := root.Mkdir(current, 0o700); err != nil {
				return err
			}
			if err := syncDirectory(root, filepath.Dir(current)); err != nil {
				return err
			}
			info, err = root.Lstat(current)
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("parent %q is not a real directory", current)
		}
	}
	return nil
}
func digestRegular(root *os.Root, path string) ([]byte, os.FileInfo, error) {
	file, err := root.OpenFile(path, os.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, nil, errors.New("not a regular file")
	}
	hash := sha256.New()
	_, err = io.Copy(hash, file)
	return hash.Sum(nil), info, err
}
func equalDigest(left, right []byte) bool { return string(left) == string(right) }
func syncDirectory(root *os.Root, path string) error {
	directory, err := root.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
