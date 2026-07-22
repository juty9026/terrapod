package recovery

import (
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

// Backup stores recovery copies under Root/<journal>/<path relative to Base>.
// Regular-file mode and symlink target metadata are preserved by the copied
// filesystem object itself.
type Backup struct {
	Root string
	Base string
}

func (b Backup) Save(journal, path string) error {
	if !journalPattern.MatchString(journal) {
		return fmt.Errorf("recovery: unsafe journal ID %q", journal)
	}
	base, root := filepath.Clean(b.Base), filepath.Clean(b.Root)
	if !filepath.IsAbs(base) || !filepath.IsAbs(root) || strings.IndexByte(path, 0) >= 0 {
		return errors.New("recovery: base, root, and source must be clean absolute paths")
	}
	source := filepath.Clean(path)
	relative, err := filepath.Rel(base, source)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("recovery: source %q escapes base", path)
	}
	destination := filepath.Join(root, journal, relative)
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return fmt.Errorf("recovery: create backup parent: %w", err)
	}
	info, err := os.Lstat(source)
	if err != nil {
		return fmt.Errorf("recovery: inspect source: %w", err)
	}
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			return fmt.Errorf("recovery: backup already exists for %q", relative)
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(source)
		if err != nil {
			return fmt.Errorf("recovery: read symlink: %w", err)
		}
		if target == "" || strings.IndexByte(target, 0) >= 0 {
			return errors.New("recovery: invalid symlink target")
		}
		if err := os.Symlink(target, destination); err != nil {
			return fmt.Errorf("recovery: copy symlink: %w", err)
		}
		return nil
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("recovery: source %q is not a regular file or symlink", relative)
	}
	fd, err := syscall.Open(source, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return fmt.Errorf("recovery: open source: %w", err)
	}
	in := os.NewFile(uintptr(fd), source)
	defer in.Close()
	opened, err := in.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return errors.New("recovery: source changed before backup")
	}
	out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return fmt.Errorf("recovery: create backup: %w", err)
	}
	if err := out.Chmod(info.Mode().Perm()); err != nil {
		_ = out.Close()
		_ = os.Remove(destination)
		return fmt.Errorf("recovery: preserve file mode: %w", err)
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		_ = os.Remove(destination)
		return fmt.Errorf("recovery: copy file: %w", err)
	}
	return nil
}
