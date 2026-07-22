package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAcquireRejectsLiveLock(t *testing.T) {
	dir := t.TempDir()
	lock, err := Acquire(dir, "tpod apply")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Release() })

	if _, err := Acquire(dir, "tpod update"); err == nil {
		t.Fatal("Acquire succeeded while a live process owned the lock")
	}
	assertMode(t, filepath.Join(dir, "lock"), 0o700)
	assertMode(t, filepath.Join(dir, "lock", "owner.json"), 0o600)
}

func TestAcquireRecoversStalePID(t *testing.T) {
	dir := t.TempDir()
	stale := lockOwner{
		PID:       1 << 30,
		Command:   "tpod apply",
		StartedAt: time.Date(2026, 7, 21, 1, 2, 3, 0, time.UTC),
		Nonce:     "0123456789abcdef0123456789abcdef",
	}
	writeTestOwner(t, dir, stale)

	lock, err := Acquire(dir, "tpod update")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Release() })
	entries, err := os.ReadDir(filepath.Join(dir, "stale-locks"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("stale lock count=%d, want 1", len(entries))
	}
	if _, err := os.Stat(filepath.Join(dir, "stale-locks", entries[0].Name(), "owner.json")); err != nil {
		t.Fatalf("archived owner is missing: %v", err)
	}
}

func TestAcquireDoesNotReclaimMalformedLock(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "lock"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "lock", "owner.json"), []byte(`{"pid":0,"command":"tpod apply"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Acquire(dir, "tpod update"); err == nil {
		t.Fatal("Acquire silently reclaimed malformed lock")
	}
	if _, err := os.Stat(filepath.Join(dir, "lock")); err != nil {
		t.Fatalf("malformed lock was moved or removed: %v", err)
	}
}

func TestAcquireDoesNotReclaimLockWithUnknownOwnerFields(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "lock"), 0o700); err != nil {
		t.Fatal(err)
	}
	owner := `{"pid":1073741824,"command":"tpod apply","startedAt":"2026-07-21T01:02:03Z","nonce":"0123456789abcdef0123456789abcdef","argv":["dangerous"]}`
	if err := os.WriteFile(filepath.Join(dir, "lock", "owner.json"), []byte(owner), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Acquire(dir, "tpod update"); err == nil {
		t.Fatal("Acquire silently reclaimed lock with unknown owner fields")
	}
	if _, err := os.Stat(filepath.Join(dir, "lock")); err != nil {
		t.Fatalf("unsafe lock was moved or removed: %v", err)
	}
}

func TestAcquireRejectsUnboundedCommandLabel(t *testing.T) {
	if _, err := Acquire(t.TempDir(), string(make([]byte, maxCommandLabelBytes+1))); err == nil {
		t.Fatal("Acquire accepted an unbounded command label")
	}
}

func TestReleaseOnlyRemovesOwnedLock(t *testing.T) {
	dir := t.TempDir()
	lock, err := Acquire(dir, "tpod apply")
	if err != nil {
		t.Fatal(err)
	}

	ownerPath := filepath.Join(dir, "lock", "owner.json")
	b, err := os.ReadFile(ownerPath)
	if err != nil {
		t.Fatal(err)
	}
	var replacement lockOwner
	if err := json.Unmarshal(b, &replacement); err != nil {
		t.Fatal(err)
	}
	replacement.Nonce = "different-owner"
	b, err = json.Marshal(replacement)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ownerPath, b, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := lock.Release(); err == nil {
		t.Fatal("Release removed a lock with different ownership")
	}
	if _, err := os.Stat(filepath.Join(dir, "lock")); err != nil {
		t.Fatalf("replacement lock was removed: %v", err)
	}
}

func writeTestOwner(t *testing.T, dir string, owner lockOwner) {
	t.Helper()
	if err := os.Mkdir(filepath.Join(dir, "lock"), 0o700); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "lock", "owner.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
}
