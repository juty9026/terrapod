package state

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sync"
	"time"

	"github.com/juty9026/terrapod/internal/model"
)

const (
	snapshotFilename = "snapshot.json"
	journalDirname   = "journals"
)

var journalIDPattern = regexp.MustCompile(`^\d{8}T\d{6}\.\d{9}Z-[0-9a-f]{32}$`)
var afterJournalCompleted func() error

// Store persists state for one caller. Mutating methods are serialized only for
// concurrent use of this Store; callers must hold a Lock acquired with Acquire
// to serialize mutations across Store instances or processes.
type Store struct {
	dir string
	mu  sync.Mutex
}

type persistedJournal struct {
	model.Journal
	SupersededBy string `json:"supersededBy,omitempty"`
}

func Open(dir string) (*Store, error) {
	if dir == "" {
		return nil, errors.New("state directory is empty")
	}
	if err := ensurePrivateDir(dir); err != nil {
		return nil, fmt.Errorf("create state directory: %w", err)
	}
	if err := ensurePrivateDir(filepath.Join(dir, journalDirname)); err != nil {
		return nil, fmt.Errorf("create journal directory: %w", err)
	}

	s := &Store{dir: dir}
	snapshotPath := s.snapshotPath()
	if _, err := os.Lstat(snapshotPath); errors.Is(err, os.ErrNotExist) {
		empty := model.Snapshot{
			Ownership:       make(map[model.ResourceID]model.Ownership),
			AppliedCatalogs: make([]string, 0),
		}
		if err := writeJSONAtomic(snapshotPath, empty); err != nil {
			return nil, fmt.Errorf("initialize snapshot: %w", err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("inspect snapshot: %w", err)
	} else {
		if err := requireRegularFile(snapshotPath); err != nil {
			return nil, err
		}
		if err := os.Chmod(snapshotPath, 0o600); err != nil {
			return nil, fmt.Errorf("secure snapshot: %w", err)
		}
	}
	if _, err := s.readSnapshot(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Snapshot() (model.Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	snapshot, err := s.readSnapshot()
	if err != nil {
		return model.Snapshot{}, err
	}
	if snapshot.ActiveJournal != nil {
		journal, err := s.readJournal(snapshot.ActiveJournal.ID)
		if err != nil {
			return model.Snapshot{}, err
		}
		active := journal.Journal
		snapshot.ActiveJournal = &active
	}
	return snapshot, nil
}

func (s *Store) Begin(plan model.Plan) (model.Journal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	snapshot, err := s.readSnapshot()
	if err != nil {
		return model.Journal{}, err
	}
	if snapshot.ActiveJournal != nil {
		return model.Journal{}, fmt.Errorf("journal %q is already active", snapshot.ActiveJournal.ID)
	}
	id, err := newJournalID()
	if err != nil {
		return model.Journal{}, err
	}
	journal := model.Journal{
		ID:        id,
		Plan:      plan,
		Results:   make([]model.OperationResult, 0),
		StartedAt: time.Now().UTC(),
		Status:    "active",
	}
	if err := s.writeJournal(persistedJournal{Journal: journal}); err != nil {
		return model.Journal{}, err
	}
	snapshot.ActiveJournal = &journal
	if err := s.writeSnapshot(snapshot); err != nil {
		return model.Journal{}, err
	}
	return journal, nil
}

// BeginOrResume returns the active journal when it contains the exact plan.
// Otherwise it atomically points the snapshot at a replacement journal and
// marks the prior journal as superseded.
func (s *Store) BeginOrResume(plan model.Plan) (model.Journal, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if plan.ID == "" {
		return model.Journal{}, false, errors.New("plan ID is empty")
	}

	snapshot, err := s.readSnapshot()
	if err != nil {
		return model.Journal{}, false, err
	}
	if snapshot.ActiveJournal != nil {
		active, err := s.readJournal(snapshot.ActiveJournal.ID)
		if err != nil {
			return model.Journal{}, false, err
		}
		if active.Status == "completed" {
			snapshot.ActiveJournal = nil
			if err := s.writeSnapshot(snapshot); err != nil {
				return model.Journal{}, false, err
			}
			journal, err := s.newJournal(plan)
			return journal, false, err
		}
		if active.Status != "active" {
			return model.Journal{}, false, fmt.Errorf("active journal %q has status %q", active.ID, active.Status)
		}
		if reflect.DeepEqual(active.Plan, plan) {
			return active.Journal, true, nil
		}
		return s.replaceActive(snapshot, active, plan)
	}
	journal, err := s.newJournal(plan)
	return journal, false, err
}

func (s *Store) newJournal(plan model.Plan) (model.Journal, error) {
	id, err := newJournalID()
	if err != nil {
		return model.Journal{}, err
	}
	journal := model.Journal{ID: id, Plan: plan, Results: make([]model.OperationResult, 0), StartedAt: time.Now().UTC(), Status: "active"}
	if err := s.writeJournal(persistedJournal{Journal: journal}); err != nil {
		return model.Journal{}, err
	}
	snapshot, err := s.readSnapshot()
	if err != nil {
		return model.Journal{}, err
	}
	snapshot.ActiveJournal = &journal
	if err := s.writeSnapshot(snapshot); err != nil {
		return model.Journal{}, err
	}
	return journal, nil
}

func (s *Store) replaceActive(snapshot model.Snapshot, active persistedJournal, plan model.Plan) (model.Journal, bool, error) {
	id, err := newJournalID()
	if err != nil {
		return model.Journal{}, false, err
	}
	replacement := model.Journal{ID: id, Plan: plan, Results: make([]model.OperationResult, 0), StartedAt: time.Now().UTC(), Status: "active"}
	if err := s.writeJournal(persistedJournal{Journal: replacement}); err != nil {
		return model.Journal{}, false, err
	}
	snapshot.ActiveJournal = &replacement
	if err := s.writeSnapshot(snapshot); err != nil {
		return model.Journal{}, false, err
	}
	active.Status = "superseded"
	active.SupersededBy = replacement.ID
	if err := s.writeJournal(active); err != nil {
		return model.Journal{}, false, err
	}
	return replacement, false, nil
}

func (s *Store) Record(result model.OperationResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	snapshot, err := s.readSnapshot()
	if err != nil {
		return err
	}
	if snapshot.ActiveJournal == nil {
		return errors.New("no active journal")
	}
	journal, err := s.readJournal(snapshot.ActiveJournal.ID)
	if err != nil {
		return err
	}
	if journal.Status != "active" {
		return fmt.Errorf("journal %q has status %q", journal.ID, journal.Status)
	}
	known := false
	for _, operation := range journal.Plan.Operations {
		if operation.ID == result.OperationID && operation.ResourceID == result.ResourceID {
			known = true
			break
		}
	}
	if !known {
		return fmt.Errorf("operation result %q is not in active plan", result.OperationID)
	}
	results := make([]model.OperationResult, 0, len(journal.Results)+1)
	inserted := false
	for _, existing := range journal.Results {
		if existing.OperationID == result.OperationID {
			if !inserted {
				results = append(results, result)
				inserted = true
			}
			continue
		}
		results = append(results, existing)
	}
	if !inserted {
		results = append(results, result)
	}
	journal.Results = results
	return s.writeJournal(journal)
}

func (s *Store) Complete(journalID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.finish(journalID, "completed", "")
}

func (s *Store) Supersede(journalID, replacementID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if replacementID == "" {
		return errors.New("replacement journal ID is empty")
	}
	return s.finish(journalID, "superseded", replacementID)
}

func (s *Store) PutOwnership(value model.Ownership) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if value.ResourceID == "" {
		return errors.New("ownership resource ID is empty")
	}
	snapshot, err := s.readSnapshot()
	if err != nil {
		return err
	}
	snapshot.Ownership[value.ResourceID] = value
	return s.writeSnapshot(snapshot)
}

func (s *Store) DeleteOwnership(id model.ResourceID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if id == "" {
		return errors.New("ownership resource ID is empty")
	}
	snapshot, err := s.readSnapshot()
	if err != nil {
		return err
	}
	delete(snapshot.Ownership, id)
	return s.writeSnapshot(snapshot)
}

func (s *Store) finish(journalID, status, replacementID string) error {
	snapshot, err := s.readSnapshot()
	if err != nil {
		return err
	}
	if snapshot.ActiveJournal == nil || snapshot.ActiveJournal.ID != journalID {
		return fmt.Errorf("journal %q is not active", journalID)
	}
	journal, err := s.readJournal(journalID)
	if err != nil {
		return err
	}
	if journal.Status != "active" && journal.Status != status {
		return fmt.Errorf("journal %q has status %q", journalID, journal.Status)
	}
	journal.Status = status
	journal.SupersededBy = replacementID
	if err := s.writeJournal(journal); err != nil {
		return err
	}
	if status == "completed" && afterJournalCompleted != nil {
		if err := afterJournalCompleted(); err != nil {
			return err
		}
	}
	snapshot.ActiveJournal = nil
	return s.writeSnapshot(snapshot)
}

func (s *Store) readSnapshot() (model.Snapshot, error) {
	var snapshot model.Snapshot
	if err := readJSONFile(s.snapshotPath(), &snapshot); err != nil {
		return model.Snapshot{}, fmt.Errorf("read snapshot: %w", err)
	}
	if snapshot.Ownership == nil {
		snapshot.Ownership = make(map[model.ResourceID]model.Ownership)
	}
	if snapshot.AppliedCatalogs == nil {
		snapshot.AppliedCatalogs = make([]string, 0)
	}
	return snapshot, nil
}

func (s *Store) writeSnapshot(snapshot model.Snapshot) error {
	if err := writeJSONAtomic(s.snapshotPath(), snapshot); err != nil {
		return fmt.Errorf("write snapshot: %w", err)
	}
	return nil
}

func (s *Store) readJournal(id string) (persistedJournal, error) {
	if !journalIDPattern.MatchString(id) {
		return persistedJournal{}, fmt.Errorf("unsafe journal ID %q", id)
	}
	var journal persistedJournal
	if err := readJSONFile(s.journalPath(id), &journal); err != nil {
		return persistedJournal{}, fmt.Errorf("read journal %q: %w", id, err)
	}
	if journal.ID != id {
		return persistedJournal{}, fmt.Errorf("journal file %q contains ID %q", id, journal.ID)
	}
	return journal, nil
}

func (s *Store) writeJournal(journal persistedJournal) error {
	if !journalIDPattern.MatchString(journal.ID) {
		return fmt.Errorf("unsafe journal ID %q", journal.ID)
	}
	if err := writeJSONAtomic(s.journalPath(journal.ID), journal); err != nil {
		return fmt.Errorf("write journal %q: %w", journal.ID, err)
	}
	return nil
}

func (s *Store) snapshotPath() string {
	return filepath.Join(s.dir, snapshotFilename)
}

func (s *Store) journalPath(id string) string {
	return filepath.Join(s.dir, journalDirname, id+".json")
}

func newJournalID() (string, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate journal ID: %w", err)
	}
	return time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + hex.EncodeToString(random), nil
}

func ensurePrivateDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is not a directory", path)
	}
	return os.Chmod(path, 0o700)
}

func requireRegularFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", path)
	}
	return nil
}

func readJSONFile(path string, target any) error {
	if err := requireRegularFile(path); err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return decodeJSON(f, target)
}

func writeJSONAtomic(path string, value any) error {
	dir := filepath.Dir(path)
	if err := ensurePrivateDir(dir); err != nil {
		return err
	}
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp, err := os.CreateTemp(dir, ".tmp-")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
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
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	return syncDirectory(dir)
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
