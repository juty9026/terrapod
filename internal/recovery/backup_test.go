package recovery

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestSavePreservesRegularFileAndMode(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	path := filepath.Join(home, ".config", "app.conf")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("local"), 0o640); err != nil {
		t.Fatal(err)
	}
	b := Backup{Root: root, Base: home}
	if err := b.Save("journal-1", path); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "journal-1", ".config", "app.conf"))
	if err != nil || string(got) != "local" {
		t.Fatalf("backup = %q, %v", got, err)
	}
	info, err := os.Stat(filepath.Join(root, "journal-1", ".config", "app.conf"))
	if err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %v, %v", info.Mode().Perm(), err)
	}
}

func TestSaveRejectsBaseOrRecoveryRootSwap(t *testing.T) {
	for _, swapBase := range []bool{true, false} {
		t.Run(map[bool]string{true: "base", false: "root"}[swapBase], func(t *testing.T) {
			base, root := t.TempDir(), t.TempDir()
			source := filepath.Join(base, "file")
			if err := os.WriteFile(source, []byte("safe"), 0o600); err != nil {
				t.Fatal(err)
			}
			outside := t.TempDir()
			moved := filepath.Join(t.TempDir(), "moved")
			afterBackupRootsOpened = func() {
				target := root
				if swapBase {
					target = base
				}
				if err := os.Rename(target, moved); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, target); err != nil {
					t.Fatal(err)
				}
			}
			t.Cleanup(func() { afterBackupRootsOpened = nil })
			err := (Backup{Root: root, Base: base}).Save("journal", source)
			afterBackupRootsOpened = nil
			if err == nil || !strings.Contains(err.Error(), "root path changed") {
				t.Fatalf("Save error=%v", err)
			}
		})
	}
}

func TestSaveRejectsSymlinkParentAndFIFO(t *testing.T) {
	base, root := t.TempDir(), t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "file"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(base, "link")); err != nil {
		t.Fatal(err)
	}
	if err := (Backup{Root: root, Base: base}).Save("journal", filepath.Join(base, "link", "file")); err == nil || !strings.Contains(err.Error(), "unsafe source parents") {
		t.Fatalf("symlink parent error=%v", err)
	}
	fifo := filepath.Join(base, "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := (Backup{Root: root, Base: base}).Save("journal", fifo); err == nil || !strings.Contains(err.Error(), "not a regular file or symlink") {
		t.Fatalf("FIFO error=%v", err)
	}
}

func TestSavePreservesSymlinkAsSymlink(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	path := filepath.Join(home, "link")
	if err := os.Symlink("target", path); err != nil {
		t.Fatal(err)
	}
	if err := (Backup{Root: root, Base: home}).Save("journal-2", path); err != nil {
		t.Fatal(err)
	}
	got, err := os.Readlink(filepath.Join(root, "journal-2", "link"))
	if err != nil || got != "target" {
		t.Fatalf("link = %q, %v", got, err)
	}
}

func TestSaveRejectsEscapesAndUnsafeJournal(t *testing.T) {
	home := t.TempDir()
	b := Backup{Root: t.TempDir(), Base: home}
	for _, tc := range []struct{ journal, path string }{{"../bad", filepath.Join(home, "a")}, {"ok", filepath.Join(home, "..", "outside")}} {
		if err := b.Save(tc.journal, tc.path); err == nil {
			t.Fatalf("Save(%q, %q) accepted", tc.journal, tc.path)
		}
	}
}
