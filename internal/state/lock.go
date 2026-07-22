package state

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"
)

const maxCommandLabelBytes = 128

type lockOwner struct {
	PID       int       `json:"pid"`
	Command   string    `json:"command"`
	StartedAt time.Time `json:"startedAt"`
	Nonce     string    `json:"nonce"`
}

type Lock struct {
	root  *os.Root
	owner lockOwner
	mu    sync.Mutex
	done  bool
}

func Acquire(dir, command string) (*Lock, error) {
	return acquireStateLock(dir, command, nil)
}

func acquireStateLock(dir, command string, staleObserved func()) (*Lock, error) {
	if err := validateCommandLabel(command); err != nil {
		return nil, err
	}
	if err := ensurePrivateDir(dir); err != nil {
		return nil, fmt.Errorf("create lock parent: %w", err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("open lock root: %w", err)
	}
	keepRoot := false
	defer func() {
		if !keepRoot {
			_ = root.Close()
		}
	}()

	owner, err := newLockOwner(command)
	if err != nil {
		return nil, err
	}
	lock, err := createLock(root, owner)
	if err == nil {
		keepRoot = true
		return lock, nil
	}
	if !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	if err := requireRootDirectory(root, "lock"); err != nil {
		return nil, fmt.Errorf("unsafe existing lock: %w", err)
	}

	existing, err := readLockOwner(root, "lock/owner.json")
	if err != nil {
		return nil, fmt.Errorf("state lock is busy: inspect existing owner: %w", err)
	}
	alive, err := processAlive(existing.PID)
	if err != nil {
		return nil, fmt.Errorf("inspect lock owner PID %d: %w", existing.PID, err)
	}
	if alive {
		return nil, fmt.Errorf("state is locked by PID %d (%s)", existing.PID, existing.Command)
	}
	if staleObserved != nil {
		staleObserved()
	}
	if err := claimAndArchiveStaleLock(root, existing); err != nil {
		return nil, err
	}
	lock, err = createLock(root, owner)
	if err != nil {
		return nil, fmt.Errorf("acquire lock after stale recovery: %w", err)
	}
	keepRoot = true
	return lock, nil
}

func (l *Lock) Release() error {
	return l.releaseWithTombstoneHook(nil)
}

func (l *Lock) releaseWithTombstoneHook(afterTombstone func()) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.done {
		return nil
	}
	if err := requireRootDirectory(l.root, "lock"); err != nil {
		return fmt.Errorf("unsafe lock during release: %w", err)
	}
	claimSuffix, err := randomHex(16)
	if err != nil {
		return fmt.Errorf("generate release claim: %w", err)
	}
	claimName := ".release-claim-" + claimSuffix + ".json"
	claimPath := "lock/" + claimName
	if _, err := l.root.Lstat(claimPath); err == nil {
		return errors.New("release claim destination already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect release claim destination: %w", err)
	}
	if err := l.root.Rename("lock/owner.json", claimPath); err != nil {
		return fmt.Errorf("claim lock for release: %w", err)
	}
	owner, err := readLockOwner(l.root, claimPath)
	if err != nil {
		return failClaim(l.root, claimPath, fmt.Errorf("verify release claim: %w", err))
	}
	if owner.Nonce != l.owner.Nonce || owner.PID != l.owner.PID {
		return failClaim(l.root, claimPath, errors.New("lock ownership changed before release"))
	}
	tombstoneSuffix, err := randomHex(16)
	if err != nil {
		return failClaim(l.root, claimPath, fmt.Errorf("generate release tombstone: %w", err))
	}
	tombstone := ".released-lock-" + time.Now().UTC().Format("20060102T150405.000000000Z") + fmt.Sprintf("-%d-%s", owner.PID, tombstoneSuffix)
	if err := l.root.Mkdir(tombstone, 0o700); err != nil {
		return failClaim(l.root, claimPath, fmt.Errorf("reserve release tombstone: %w", err))
	}
	tombstonedLock := tombstone + "/lock"
	if err := l.root.Rename("lock", tombstonedLock); err != nil {
		_ = l.root.Remove(tombstone)
		return failClaim(l.root, claimPath, fmt.Errorf("move lock to release tombstone: %w", err))
	}
	linearizeErr := syncRootDirectory(l.root, ".")
	if afterTombstone != nil {
		afterTombstone()
	}
	cleanupErr := removeLockTombstone(l.root, tombstone, tombstonedLock+"/"+claimName)
	closeErr := l.root.Close()
	l.done = true
	if err := errors.Join(linearizeErr, cleanupErr, closeErr); err != nil {
		return fmt.Errorf("finish lock release: %w", err)
	}
	return nil
}

func removeLockTombstone(root *os.Root, tombstone, claimPath string) error {
	if err := root.Remove(claimPath); err != nil {
		return fmt.Errorf("remove tombstoned owner: %w", err)
	}
	if err := root.Remove(tombstone + "/lock"); err != nil {
		return fmt.Errorf("remove tombstoned lock: %w", err)
	}
	if err := root.Remove(tombstone); err != nil {
		return fmt.Errorf("remove release tombstone: %w", err)
	}
	return syncRootDirectory(root, ".")
}

func createLock(root *os.Root, owner lockOwner) (*Lock, error) {
	if err := root.Mkdir("lock", 0o700); err != nil {
		return nil, err
	}
	if err := writeJSONAtomicRoot(root, "lock/owner.json", owner); err != nil {
		_ = root.Remove("lock/owner.json")
		_ = root.Remove("lock")
		return nil, fmt.Errorf("write lock owner: %w", err)
	}
	if err := syncRootDirectory(root, "."); err != nil {
		_ = root.Remove("lock/owner.json")
		_ = root.Remove("lock")
		return nil, fmt.Errorf("sync lock parent: %w", err)
	}
	return &Lock{root: root, owner: owner}, nil
}

func newLockOwner(command string) (lockOwner, error) {
	nonce, err := randomHex(16)
	if err != nil {
		return lockOwner{}, fmt.Errorf("generate lock owner nonce: %w", err)
	}
	return lockOwner{
		PID:       os.Getpid(),
		Command:   command,
		StartedAt: time.Now().UTC(),
		Nonce:     nonce,
	}, nil
}

func readLockOwner(root *os.Root, name string) (lockOwner, error) {
	var owner lockOwner
	if err := readJSONRoot(root, name, &owner); err != nil {
		return lockOwner{}, err
	}
	if owner.PID <= 0 {
		return lockOwner{}, fmt.Errorf("unsafe owner PID %d", owner.PID)
	}
	if owner.StartedAt.IsZero() {
		return lockOwner{}, errors.New("owner start time is missing")
	}
	if err := validateCommandLabel(owner.Command); err != nil {
		return lockOwner{}, fmt.Errorf("unsafe owner command label: %w", err)
	}
	decoded, err := hex.DecodeString(owner.Nonce)
	if err != nil || len(decoded) != 16 {
		return lockOwner{}, errors.New("unsafe owner nonce")
	}
	return owner, nil
}

func validateCommandLabel(command string) error {
	if len(command) > maxCommandLabelBytes {
		return fmt.Errorf("command label exceeds %d bytes", maxCommandLabelBytes)
	}
	if !utf8.ValidString(command) {
		return errors.New("command label is not valid UTF-8")
	}
	if strings.IndexFunc(command, unicode.IsControl) >= 0 {
		return errors.New("command label contains control characters")
	}
	return nil
}

func processAlive(pid int) (bool, error) {
	if pid <= 0 {
		return false, fmt.Errorf("unsafe PID %d", pid)
	}
	err := syscall.Kill(pid, 0)
	switch {
	case err == nil, errors.Is(err, syscall.EPERM):
		return true, nil
	case errors.Is(err, syscall.ESRCH):
		return false, nil
	default:
		return false, err
	}
}

func claimAndArchiveStaleLock(root *os.Root, observed lockOwner) error {
	claimSuffix, err := randomHex(16)
	if err != nil {
		return fmt.Errorf("generate stale lock claim: %w", err)
	}
	claimName := ".owner-claim-" + claimSuffix + ".json"
	claimPath := "lock/" + claimName
	if _, err := root.Lstat(claimPath); err == nil {
		return errors.New("stale claim destination already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect stale claim destination: %w", err)
	}
	if err := root.Rename("lock/owner.json", claimPath); err != nil {
		return fmt.Errorf("state lock is busy: claim stale owner: %w", err)
	}
	claimed, err := readLockOwner(root, claimPath)
	if err != nil {
		restoreErr := restoreClaimedOwner(root, claimPath)
		return fmt.Errorf("verify claimed stale owner: %w", errors.Join(err, restoreErr))
	}
	if claimed != observed {
		return failClaim(root, claimPath, errors.New("claimed stale owner changed before archive"))
	}
	if err := ensurePrivateRootDir(root, "stale-locks"); err != nil {
		return failClaim(root, claimPath, fmt.Errorf("create stale lock directory: %w", err))
	}
	archiveSuffix, err := randomHex(16)
	if err != nil {
		return failClaim(root, claimPath, fmt.Errorf("generate stale archive name: %w", err))
	}
	archiveName := time.Now().UTC().Format("20060102T150405.000000000Z") + fmt.Sprintf("-%d-%s", observed.PID, archiveSuffix)
	archivePath := "stale-locks/" + archiveName
	reservation := "stale-locks/.reserve-" + archiveName
	reserved, err := root.OpenFile(reservation, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return failClaim(root, claimPath, fmt.Errorf("reserve stale archive name: %w", err))
	}
	if err := reserved.Close(); err != nil {
		_ = root.Remove(reservation)
		return failClaim(root, claimPath, fmt.Errorf("close stale archive reservation: %w", err))
	}
	defer root.Remove(reservation)
	if _, err := root.Lstat(archivePath); err == nil {
		return failClaim(root, claimPath, errors.New("stale archive destination already exists"))
	} else if !errors.Is(err, os.ErrNotExist) {
		return failClaim(root, claimPath, fmt.Errorf("inspect stale archive destination: %w", err))
	}
	if err := root.Rename("lock", archivePath); err != nil {
		return failClaim(root, claimPath, fmt.Errorf("archive claimed stale lock: %w", err))
	}
	if err := normalizeArchivedOwner(root, archivePath, claimName, observed, func() error {
		return root.Rename(archivePath+"/"+claimName, archivePath+"/owner.json")
	}); err != nil {
		return err
	}
	_ = root.Remove(reservation)
	if err := syncRootDirectory(root, "stale-locks"); err != nil {
		return fmt.Errorf("sync stale lock directory: %w", err)
	}
	if err := syncRootDirectory(root, "."); err != nil {
		return fmt.Errorf("sync lock parent: %w", err)
	}
	return nil
}

func normalizeArchivedOwner(root *os.Root, archivePath, claimName string, observed lockOwner, renameClaim func() error) error {
	if err := renameClaim(); err == nil {
		if err := syncRootDirectory(root, archivePath); err == nil {
			return nil
		}
	}
	if err := writeJSONAtomicRoot(root, archivePath+"/owner.json", observed); err != nil {
		return fmt.Errorf("preserve archived owner after normalization failure: %w", err)
	}
	_ = root.Remove(archivePath + "/" + claimName)
	return syncRootDirectory(root, archivePath)
}

func failClaim(root *os.Root, claimPath string, cause error) error {
	return errors.Join(cause, restoreClaimedOwner(root, claimPath))
}

func restoreClaimedOwner(root *os.Root, claimPath string) error {
	if _, err := root.Lstat("lock/owner.json"); err == nil {
		return errors.New("cannot restore claim over an existing owner")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect owner before claim restore: %w", err)
	}
	if err := root.Rename(claimPath, "lock/owner.json"); err != nil {
		return fmt.Errorf("restore claimed owner: %w", err)
	}
	return syncRootDirectory(root, "lock")
}

func requireRootDirectory(root *os.Root, name string) error {
	info, err := root.Lstat(name)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%s is not a directory", name)
	}
	return nil
}

func ensurePrivateRootDir(root *os.Root, name string) error {
	if err := root.MkdirAll(name, 0o700); err != nil {
		return err
	}
	if err := requireRootDirectory(root, name); err != nil {
		return err
	}
	dir, err := root.Open(name)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Chmod(0o700)
}

func readJSONRoot(root *os.Root, name string, target any) error {
	info, err := root.Lstat(name)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", name)
	}
	f, err := root.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	return decodeJSON(f, target)
}

func writeJSONAtomicRoot(root *os.Root, name string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmpSuffix, err := randomHex(16)
	if err != nil {
		return err
	}
	tmpName := path.Join(path.Dir(name), ".tmp-"+tmpSuffix)
	tmp, err := root.OpenFile(tmpName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer root.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := root.Rename(tmpName, name); err != nil {
		return err
	}
	return syncRootDirectory(root, path.Dir(name))
}

func syncRootDirectory(root *os.Root, name string) error {
	dir, err := root.Open(name)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func decodeJSON(reader io.Reader, target any) error {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return errors.New("trailing JSON value")
	} else if !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func randomHex(byteCount int) (string, error) {
	random := make([]byte, byteCount)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return hex.EncodeToString(random), nil
}
