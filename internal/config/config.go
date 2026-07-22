package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"syscall"

	"github.com/juty9026/terrapod/internal/model"
)

type ChangeKind string

const (
	ChangeAdd       ChangeKind = "add"
	ChangePrune     ChangeKind = "prune"
	ChangeNormalize ChangeKind = "normalize"
)

type Change struct {
	Kind   ChangeKind
	Field  string
	Before any
	After  any
}

type ErrMissing struct {
	Path string
}

func (e *ErrMissing) Error() string {
	return fmt.Sprintf("Terrapod config %q is missing", e.Path)
}

type ErrNeedsSetup struct {
	Field string
}

func (e *ErrNeedsSetup) Error() string {
	return fmt.Sprintf("Terrapod config field %q needs setup", e.Field)
}

func Load(path string) (model.Config, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if errors.Is(err, os.ErrNotExist) {
		return model.Config{}, &ErrMissing{Path: path}
	}
	if err != nil {
		return model.Config{}, fmt.Errorf("open Terrapod config %q: %w", path, err)
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = syscall.Close(fd)
		return model.Config{}, fmt.Errorf("open Terrapod config %q: invalid file descriptor", path)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return model.Config{}, fmt.Errorf("inspect opened Terrapod config %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return model.Config{}, fmt.Errorf("Terrapod config %q is not a regular file", path)
	}

	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var cfg model.Config
	if err := decoder.Decode(&cfg); err != nil {
		return model.Config{}, fmt.Errorf("decode Terrapod config: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return model.Config{}, fmt.Errorf("decode Terrapod config: trailing JSON: %w", err)
	}
	if cfg.Terrapod == nil {
		return model.Config{}, errors.New("decode Terrapod config: missing terrapod")
	}
	return cfg, nil
}

func Normalize(cfg model.Config, schema model.ConfigSchema) (model.Config, []Change, error) {
	normalized := model.Config{
		Version:  schema.Version,
		Terrapod: make(map[string]any, len(schema.Fields)),
	}
	changes := make([]Change, 0)
	if cfg.Version != schema.Version {
		changes = append(changes, Change{Kind: ChangeNormalize, Field: "version", Before: cfg.Version, After: schema.Version})
	}

	known := make(map[string]struct{}, len(schema.Fields))
	for _, field := range schema.Fields {
		known[field.ID] = struct{}{}
		if field.Kind != "string" && field.Kind != "bool" {
			return model.Config{}, nil, fmt.Errorf("unsupported config field kind %q", field.Kind)
		}

		value, exists := cfg.Terrapod[field.ID]
		if exists && validValue(value, field.Kind) {
			normalized.Terrapod[field.ID] = value
			continue
		}
		if field.Required {
			return model.Config{}, nil, &ErrNeedsSetup{Field: field.ID}
		}
		if field.Default != nil {
			if !validValue(field.Default, field.Kind) {
				return model.Config{}, nil, fmt.Errorf("invalid default for config field %q", field.ID)
			}
			normalized.Terrapod[field.ID] = field.Default
			kind := ChangeAdd
			before := any(nil)
			if exists {
				kind = ChangeNormalize
				before = value
			}
			changes = append(changes, Change{Kind: kind, Field: field.ID, Before: before, After: field.Default})
			continue
		}
		if exists {
			changes = append(changes, Change{Kind: ChangePrune, Field: field.ID, Before: value, After: nil})
		}
	}

	obsolete := make([]string, 0)
	for field := range cfg.Terrapod {
		if _, ok := known[field]; !ok {
			obsolete = append(obsolete, field)
		}
	}
	sort.Strings(obsolete)
	for _, field := range obsolete {
		changes = append(changes, Change{Kind: ChangePrune, Field: field, Before: cfg.Terrapod[field], After: nil})
	}

	return normalized, changes, nil
}

func validValue(value any, kind string) bool {
	switch kind {
	case "string":
		value, ok := value.(string)
		return ok && value != ""
	case "bool":
		_, ok := value.(bool)
		return ok
	default:
		return false
	}
}

func WriteAtomic(path string, cfg model.Config) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create Terrapod config directory: %w", err)
	}

	temp, err := os.CreateTemp(dir, ".config.json-")
	if err != nil {
		return fmt.Errorf("create temporary Terrapod config: %w", err)
	}
	tempPath := temp.Name()
	keepTemp := true
	defer func() {
		if keepTemp {
			_ = os.Remove(tempPath)
		}
	}()

	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return fmt.Errorf("set temporary Terrapod config mode: %w", err)
	}
	if err := json.NewEncoder(temp).Encode(cfg); err != nil {
		_ = temp.Close()
		return fmt.Errorf("encode Terrapod config: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("sync temporary Terrapod config: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close temporary Terrapod config: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace Terrapod config: %w", err)
	}
	keepTemp = false

	directory, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open Terrapod config directory: %w", err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync Terrapod config directory: %w", err)
	}
	return nil
}
