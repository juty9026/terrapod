package state

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	dir   string
	owner lockOwner
	mu    sync.Mutex
	done  bool
}

func Acquire(dir, command string) (*Lock, error) {
	if err := validateCommandLabel(command); err != nil {
		return nil, err
	}
	if err := ensurePrivateDir(dir); err != nil {
		return nil, fmt.Errorf("create lock parent: %w", err)
	}
	owner, err := newLockOwner(command)
	if err != nil {
		return nil, err
	}
	lock, err := createLock(dir, owner)
	if err == nil {
		return lock, nil
	}
	if !errors.Is(err, os.ErrExist) {
		return nil, err
	}

	existing, err := readLockOwner(filepath.Join(dir, "lock", "owner.json"))
	if err != nil {
		return nil, fmt.Errorf("inspect existing lock: %w", err)
	}
	alive, err := processAlive(existing.PID)
	if err != nil {
		return nil, fmt.Errorf("inspect lock owner PID %d: %w", existing.PID, err)
	}
	if alive {
		return nil, fmt.Errorf("state is locked by PID %d (%s)", existing.PID, existing.Command)
	}
	if err := archiveStaleLock(dir, existing); err != nil {
		return nil, err
	}
	lock, err = createLock(dir, owner)
	if err != nil {
		return nil, fmt.Errorf("acquire lock after stale recovery: %w", err)
	}
	return lock, nil
}

func (l *Lock) Release() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.done {
		return nil
	}
	ownerPath := filepath.Join(l.dir, "lock", "owner.json")
	owner, err := readLockOwner(ownerPath)
	if err != nil {
		return fmt.Errorf("verify lock ownership: %w", err)
	}
	if owner.Nonce != l.owner.Nonce || owner.PID != l.owner.PID {
		return errors.New("lock ownership changed before release")
	}
	if err := os.Remove(ownerPath); err != nil {
		return fmt.Errorf("remove lock owner: %w", err)
	}
	if err := os.Remove(filepath.Join(l.dir, "lock")); err != nil {
		return fmt.Errorf("remove lock directory: %w", err)
	}
	if err := syncDirectory(l.dir); err != nil {
		return fmt.Errorf("sync lock parent: %w", err)
	}
	l.done = true
	return nil
}

func createLock(dir string, owner lockOwner) (*Lock, error) {
	lockDir := filepath.Join(dir, "lock")
	if err := os.Mkdir(lockDir, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(lockDir, 0o700); err != nil {
		_ = os.Remove(lockDir)
		return nil, err
	}
	if err := writeJSONAtomic(filepath.Join(lockDir, "owner.json"), owner); err != nil {
		_ = os.Remove(filepath.Join(lockDir, "owner.json"))
		_ = os.Remove(lockDir)
		return nil, fmt.Errorf("write lock owner: %w", err)
	}
	if err := syncDirectory(dir); err != nil {
		_ = os.Remove(filepath.Join(lockDir, "owner.json"))
		_ = os.Remove(lockDir)
		return nil, fmt.Errorf("sync lock parent: %w", err)
	}
	return &Lock{dir: dir, owner: owner}, nil
}

func newLockOwner(command string) (lockOwner, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return lockOwner{}, fmt.Errorf("generate lock owner nonce: %w", err)
	}
	return lockOwner{
		PID:       os.Getpid(),
		Command:   command,
		StartedAt: time.Now().UTC(),
		Nonce:     hex.EncodeToString(random),
	}, nil
}

func readLockOwner(path string) (lockOwner, error) {
	var owner lockOwner
	if err := readJSONFile(path, &owner); err != nil {
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

func archiveStaleLock(dir string, owner lockOwner) error {
	staleDir := filepath.Join(dir, "stale-locks")
	if err := ensurePrivateDir(staleDir); err != nil {
		return fmt.Errorf("create stale lock directory: %w", err)
	}
	name := time.Now().UTC().Format("20060102T150405.000000000Z") + fmt.Sprintf("-%d", owner.PID)
	destination := filepath.Join(staleDir, name)
	if err := os.Rename(filepath.Join(dir, "lock"), destination); err != nil {
		return fmt.Errorf("archive stale lock: %w", err)
	}
	if err := syncDirectory(staleDir); err != nil {
		return fmt.Errorf("sync stale lock directory: %w", err)
	}
	if err := syncDirectory(dir); err != nil {
		return fmt.Errorf("sync lock parent: %w", err)
	}
	return nil
}
