package testutil

import (
	"os"
	"path/filepath"
	"testing"
)

func WorkspaceTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp(".", ".terrapod-test-")
	if err != nil {
		t.Fatal(err)
	}
	createdDir := dir
	t.Cleanup(func() {
		if err := os.RemoveAll(createdDir); err != nil {
			t.Errorf("remove workspace temp dir: %v", err)
		}
	})
	dir, err = filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func WriteFile(t *testing.T, path string, contents []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, contents, mode); err != nil {
		t.Fatal(err)
	}
}
