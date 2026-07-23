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
	"strings"
	"sync"
	"time"

	"github.com/juty9026/terrapod/internal/model"
)

const (
	snapshotFilename = "snapshot.json"
	journalDirname   = "journals"
	updateDirname    = "updates"
)

var journalIDPattern = regexp.MustCompile(`^\d{8}T\d{6}\.\d{9}Z-[0-9a-f]{32}$`)
var afterJournalCompleted func() error
var afterReplacementJournal, afterReplacementSnapshot, afterReplacementSuperseded func() error
var afterPrivateLstat, afterPrivateOpen, afterPrivateDecode func(string) error

// Store persists state for one caller. Mutating methods are serialized only for
// concurrent use of this Store; callers must hold a Lock acquired with Acquire
// to serialize mutations across Store instances or processes.
type Store struct {
	dir string
	mu  sync.Mutex
}

func ValidateReadOnly(dir string) error {
	if err := requireRealDirectory(dir); err != nil {
		return fmt.Errorf("validate state directory: %w", err)
	}
	store := &Store{dir: dir}
	snapshot, err := store.readSnapshot()
	if err != nil {
		return err
	}
	if snapshot.ActiveJournal != nil {
		if _, err := store.readJournal(snapshot.ActiveJournal.ID); err != nil {
			return err
		}
	}
	return nil
}

func requireRealDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("path is not a real directory")
	}
	return nil
}

var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type persistedJournal struct {
	model.Journal
	SupersededBy string `json:"supersededBy,omitempty"`
	Replaces     string `json:"replaces,omitempty"`
}

// UpdateRecord binds a pre-activation journal to the stable release facts that
// a newly activated binary must independently re-verify.
type UpdateRecord struct {
	JournalID     string `json:"journalId"`
	PlanID        string `json:"planId"`
	Version       string `json:"version"`
	CatalogDigest string `json:"catalogDigest"`
	ReleaseDigest string `json:"releaseDigest"`
	Activated     bool   `json:"activated"`
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
	if err := ensurePrivateDir(filepath.Join(dir, updateDirname)); err != nil {
		return nil, fmt.Errorf("create update directory: %w", err)
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
	}
	if err := s.repairJournals(); err != nil {
		return nil, err
	}
	if _, err := s.readSnapshot(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Journal(id string) (model.Journal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	journal, err := s.readJournal(id)
	return journal.Journal, err
}

func (s *Store) PutUpdate(record UpdateRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !validUpdateRecord(record) {
		return errors.New("invalid update record")
	}
	journal, err := s.readJournal(record.JournalID)
	if err != nil {
		return err
	}
	if journal.Status != "active" || journal.Plan.ID != record.PlanID {
		return errors.New("update record does not match active journal")
	}
	return writeJSONAtomic(filepath.Join(s.dir, updateDirname, record.JournalID+".json"), record)
}

func (s *Store) Update(id string) (UpdateRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !journalIDPattern.MatchString(id) {
		return UpdateRecord{}, fmt.Errorf("unsafe journal ID %q", id)
	}
	var record UpdateRecord
	if err := readJSONFile(filepath.Join(s.dir, updateDirname, id+".json"), &record); err != nil {
		return UpdateRecord{}, err
	}
	if record.JournalID != id || !validUpdateRecord(record) {
		return UpdateRecord{}, errors.New("invalid persisted update record")
	}
	return record, nil
}

func validUpdateRecord(record UpdateRecord) bool {
	return journalIDPattern.MatchString(record.JournalID) && record.PlanID != "" && record.Version != "" && digestPattern.MatchString(record.CatalogDigest) && digestPattern.MatchString(record.ReleaseDigest)
}

func (s *Store) MarkUpdateActivated(id string) error {
	record, err := s.Update(id)
	if err != nil {
		return err
	}
	record.Activated = true
	return s.PutUpdate(record)
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
	if err := s.writeJournal(persistedJournal{Journal: replacement, Replaces: active.ID}); err != nil {
		return model.Journal{}, false, err
	}
	if afterReplacementJournal != nil {
		if err := afterReplacementJournal(); err != nil {
			return model.Journal{}, false, err
		}
	}
	snapshot.ActiveJournal = &replacement
	if err := s.writeSnapshot(snapshot); err != nil {
		return model.Journal{}, false, err
	}
	if afterReplacementSnapshot != nil {
		if err := afterReplacementSnapshot(); err != nil {
			return model.Journal{}, false, err
		}
	}
	active.Status = "superseded"
	active.SupersededBy = replacement.ID
	if err := s.writeJournal(active); err != nil {
		return model.Journal{}, false, err
	}
	if afterReplacementSuperseded != nil {
		if err := afterReplacementSuperseded(); err != nil {
			return model.Journal{}, false, err
		}
	}
	return replacement, false, nil
}

func (s *Store) repairJournals() error {
	snapshot, err := s.readSnapshot()
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(filepath.Join(s.dir, journalDirname))
	if err != nil {
		return err
	}
	journals := make(map[string]persistedJournal)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		if !journalIDPattern.MatchString(id) {
			return fmt.Errorf("unsafe journal file %q", entry.Name())
		}
		journal, err := s.readJournal(id)
		if err != nil {
			return err
		}
		journals[id] = journal
	}
	keep := ""
	if snapshot.ActiveJournal != nil {
		pointed, ok := journals[snapshot.ActiveJournal.ID]
		if !ok {
			return fmt.Errorf("active journal %q is missing", snapshot.ActiveJournal.ID)
		}
		if pointed.Status == "active" {
			keep = pointed.ID
		} else {
			snapshot.ActiveJournal = nil
		}
	}
	for id, journal := range journals {
		if journal.Status != "active" || id == keep {
			continue
		}
		journal.Status = "superseded"
		journal.SupersededBy = keep
		if err := s.writeJournal(journal); err != nil {
			return err
		}
	}
	if keep != "" {
		active := journals[keep].Journal
		snapshot.ActiveJournal = &active
	}
	return s.writeSnapshot(snapshot)
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

func readJSONFile(path string, target any) error {
	pre, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if err := requirePrivateFile(path, pre); err != nil {
		return err
	}
	if afterPrivateLstat != nil {
		if err := afterPrivateLstat(path); err != nil {
			return err
		}
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return err
	}
	if err := requirePrivateFile(path, opened); err != nil {
		return err
	}
	if !os.SameFile(pre, opened) || pre.Mode() != opened.Mode() {
		return fmt.Errorf("%s changed while opening", path)
	}
	if afterPrivateOpen != nil {
		if err := afterPrivateOpen(path); err != nil {
			return err
		}
	}
	decodeErr := decodeJSON(f, target)
	if afterPrivateDecode != nil {
		if err := afterPrivateDecode(path); err != nil {
			return err
		}
	}
	post, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if err := requirePrivateFile(path, post); err != nil {
		return err
	}
	if !os.SameFile(opened, post) || opened.Mode() != post.Mode() {
		return fmt.Errorf("%s changed after opening", path)
	}
	return decodeErr
}

func requirePrivateFile(path string, info os.FileInfo) error {
	if !info.Mode().IsRegular() || info.Mode() != 0o600 {
		return fmt.Errorf("%s must be a regular file with mode 0600", path)
	}
	return nil
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
