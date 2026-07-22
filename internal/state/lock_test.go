package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
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

func TestLockValidateHeldRejectsNilForeignReleasedAndReplacedOwner(t *testing.T) {
	dir := t.TempDir()
	var nilLock *Lock
	if err := nilLock.ValidateHeld(dir); err == nil {
		t.Fatal("nil lock validated")
	}
	lock, err := Acquire(dir, "tpod resolve core.alpha")
	if err != nil {
		t.Fatal(err)
	}
	if err := lock.ValidateHeld(dir); err != nil {
		t.Fatalf("live exact lock rejected: %v", err)
	}
	if err := lock.ValidateHeld(t.TempDir()); err == nil {
		t.Fatal("foreign lock directory validated")
	}
	ownerPath := filepath.Join(dir, "lock", "owner.json")
	original, err := os.ReadFile(ownerPath)
	if err != nil {
		t.Fatal(err)
	}
	writeOwnerContentsForValidation(t, ownerPath, lockOwner{PID: os.Getpid(), Command: "tpod resolve core.alpha", StartedAt: time.Now().UTC(), Nonce: "abcdefabcdefabcdefabcdefabcdefab"})
	if err := lock.ValidateHeld(dir); err == nil {
		t.Fatal("replaced owner validated")
	}
	if err := os.WriteFile(ownerPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}
	if err := lock.ValidateHeld(dir); err == nil {
		t.Fatal("released lock validated")
	}
}

func writeOwnerContentsForValidation(t *testing.T, path string, owner lockOwner) {
	t.Helper()
	contents, err := json.Marshal(owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
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
	wantArchiveName := regexp.MustCompile(`^\d{8}T\d{6}\.\d{9}Z-1073741824-[0-9a-f]{32}$`)
	if !wantArchiveName.MatchString(entries[0].Name()) {
		t.Fatalf("stale archive name %q has no random suffix", entries[0].Name())
	}
	if _, err := os.Stat(filepath.Join(dir, "stale-locks", entries[0].Name(), "owner.json")); err != nil {
		t.Fatalf("archived owner is missing: %v", err)
	}
}

func TestConcurrentStaleRecoveryHasSingleWinner(t *testing.T) {
	dir := t.TempDir()
	writeTestOwner(t, dir, lockOwner{
		PID:       1 << 30,
		Command:   "tpod apply",
		StartedAt: time.Date(2026, 7, 21, 1, 2, 3, 0, time.UTC),
		Nonce:     "0123456789abcdef0123456789abcdef",
	})

	const contenders = 32
	start := make(chan struct{})
	results := make(chan *Lock, contenders)
	var workers sync.WaitGroup
	for range contenders {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			lock, _ := Acquire(dir, "tpod update")
			results <- lock
		}()
	}
	close(start)
	workers.Wait()
	close(results)

	var winner *Lock
	for lock := range results {
		if lock == nil {
			continue
		}
		if winner != nil {
			t.Fatal("more than one stale-lock contender acquired the lock")
		}
		winner = lock
	}
	if winner == nil {
		t.Fatal("no stale-lock contender acquired the lock")
	}
	t.Cleanup(func() { _ = winner.Release() })

	current, err := os.ReadFile(filepath.Join(dir, "lock", "owner.json"))
	if err != nil {
		t.Fatalf("live winner lock was moved: %v", err)
	}
	if !bytes.Contains(current, []byte(winner.owner.Nonce)) {
		t.Fatalf("current owner does not belong to winner: %s", current)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "stale-locks"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("archived stale lock count=%d, want 1", len(entries))
	}
}

func TestDelayedStaleContenderDoesNotClaimLiveWinner(t *testing.T) {
	dir := t.TempDir()
	writeTestOwner(t, dir, lockOwner{
		PID:       1 << 30,
		Command:   "tpod apply",
		StartedAt: time.Date(2026, 7, 21, 1, 2, 3, 0, time.UTC),
		Nonce:     "0123456789abcdef0123456789abcdef",
	})

	firstObserved := make(chan struct{})
	secondObserved := make(chan struct{})
	firstAcquired := make(chan struct{})
	type acquireResult struct {
		lock *Lock
		err  error
	}
	firstResult := make(chan acquireResult, 1)
	secondResult := make(chan acquireResult, 1)
	go func() {
		lock, err := acquireStateLock(dir, "tpod update", func() {
			close(firstObserved)
			<-secondObserved
		})
		firstResult <- acquireResult{lock: lock, err: err}
		close(firstAcquired)
	}()
	go func() {
		lock, err := acquireStateLock(dir, "tpod update", func() {
			close(secondObserved)
			<-firstObserved
			<-firstAcquired
		})
		secondResult <- acquireResult{lock: lock, err: err}
	}()

	first := <-firstResult
	if first.err != nil {
		t.Fatalf("first contender did not acquire stale lock: %v", first.err)
	}
	t.Cleanup(func() { _ = first.lock.Release() })
	second := <-secondResult
	if second.err == nil || second.lock != nil {
		t.Fatal("delayed stale contender acquired a live winner lock")
	}
	current, err := os.ReadFile(filepath.Join(dir, "lock", "owner.json"))
	if err != nil {
		t.Fatalf("delayed contender removed live winner owner: %v", err)
	}
	if !bytes.Contains(current, []byte(first.lock.owner.Nonce)) {
		t.Fatalf("current owner does not belong to live winner: %s", current)
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

func TestReleaseRejectsSwappedExternalLockSymlink(t *testing.T) {
	dir := t.TempDir()
	lock, err := Acquire(dir, "tpod apply")
	if err != nil {
		t.Fatal(err)
	}
	originalLock := filepath.Join(dir, "original-lock")
	if err := os.Rename(filepath.Join(dir, "lock"), originalLock); err != nil {
		t.Fatal(err)
	}

	external := t.TempDir()
	b, err := json.Marshal(lock.owner)
	if err != nil {
		t.Fatal(err)
	}
	externalOwner := filepath.Join(external, "owner.json")
	if err := os.WriteFile(externalOwner, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(dir, "lock")); err != nil {
		t.Fatal(err)
	}

	if err := lock.Release(); err == nil {
		t.Fatal("Release followed a swapped external lock symlink")
	}
	got, err := os.ReadFile(externalOwner)
	if err != nil {
		t.Fatalf("Release deleted external owner: %v", err)
	}
	if !bytes.Equal(got, b) {
		t.Fatalf("Release mutated external owner: got %s want %s", got, b)
	}

	if err := os.Remove(filepath.Join(dir, "lock")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(originalLock, filepath.Join(dir, "lock")); err != nil {
		t.Fatal(err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("release restored lock: %v", err)
	}
}

func TestReleaseTombstoneDoesNotRemoveReplacementWinner(t *testing.T) {
	dir := t.TempDir()
	lock, err := Acquire(dir, "tpod apply")
	if err != nil {
		t.Fatal(err)
	}

	var replacement *Lock
	var replacementErr error
	err = lock.releaseWithTombstoneHook(func() {
		replacement, replacementErr = Acquire(dir, "tpod update")
	})
	if err != nil {
		t.Fatal(err)
	}
	if replacementErr != nil {
		t.Fatalf("replacement did not acquire after release linearized: %v", replacementErr)
	}
	t.Cleanup(func() { _ = replacement.Release() })

	current, err := os.ReadFile(filepath.Join(dir, "lock", "owner.json"))
	if err != nil {
		t.Fatalf("Release removed replacement winner: %v", err)
	}
	if !bytes.Contains(current, []byte(replacement.owner.Nonce)) {
		t.Fatalf("current owner does not belong to replacement winner: %s", current)
	}
}

func TestReleaseCleanupFailureDoesNotBlockReplacement(t *testing.T) {
	dir := t.TempDir()
	lock, err := Acquire(dir, "tpod apply")
	if err != nil {
		t.Fatal(err)
	}

	var replacement *Lock
	var replacementErr error
	var blocker string
	err = lock.releaseWithTombstoneHook(func() {
		entries, readErr := os.ReadDir(dir)
		if readErr != nil {
			replacementErr = readErr
			return
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".released-lock-") {
				blocker = filepath.Join(dir, entry.Name(), "lock", "cleanup-blocker")
				break
			}
		}
		if blocker == "" {
			replacementErr = errors.New("release tombstone not found")
			return
		}
		if writeErr := os.WriteFile(blocker, []byte("block cleanup"), 0o600); writeErr != nil {
			replacementErr = writeErr
			return
		}
		replacement, replacementErr = Acquire(dir, "tpod update")
	})
	if err == nil {
		t.Fatal("Release did not report tombstone cleanup failure")
	}
	if replacementErr != nil {
		t.Fatalf("replacement did not acquire despite isolated cleanup failure: %v", replacementErr)
	}
	if replacement == nil {
		t.Fatal("replacement lock is nil")
	}
	if err := replacement.Release(); err != nil {
		t.Fatalf("release replacement: %v", err)
	}
	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	tombstonedLock := filepath.Dir(blocker)
	if err := os.Remove(tombstonedLock); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Dir(tombstonedLock)); err != nil {
		t.Fatal(err)
	}
}

func TestNormalizeArchivedOwnerFallsBackToAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "stale-locks", "archive"), 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	owner := lockOwner{
		PID:       1 << 30,
		Command:   "tpod apply",
		StartedAt: time.Date(2026, 7, 21, 1, 2, 3, 0, time.UTC),
		Nonce:     "0123456789abcdef0123456789abcdef",
	}
	if err := writeJSONAtomicRoot(root, "stale-locks/archive/claim.json", owner); err != nil {
		t.Fatal(err)
	}

	err = normalizeArchivedOwner(root, "stale-locks/archive", "claim.json", owner, func() error {
		return errors.New("injected rename failure")
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := readLockOwner(root, "stale-locks/archive/owner.json")
	if err != nil {
		t.Fatalf("fallback did not preserve strict owner.json: %v", err)
	}
	if got != owner {
		t.Fatalf("archived owner=%#v, want %#v", got, owner)
	}
	if _, err := root.Lstat("stale-locks/archive/claim.json"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("claim was not cleaned after fallback: %v", err)
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
