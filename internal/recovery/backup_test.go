package recovery

import (
	"os"
	"path/filepath"
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
