package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/testutil"
)

func TestLoad(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.json")
		testutil.WriteFile(t, path, []byte(`{"version":1,"terrapod":{"profile":"vps-shell"}}`), 0o600)

		got, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		want := model.Config{Version: 1, Terrapod: map[string]any{"profile": "vps-shell"}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Load() = %#v, want %#v", got, want)
		}
	})

	t.Run("missing returns typed error", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "missing.json")
		_, err := Load(path)
		var missing *ErrMissing
		if !errors.As(err, &missing) {
			t.Fatalf("error = %v, want *ErrMissing", err)
		}
	})

	t.Run("rejects malformed inputs", func(t *testing.T) {
		tests := []struct {
			name string
			json string
		}{
			{"trailing JSON", `{"version":1,"terrapod":{}} {}`},
			{"unknown top-level key", `{"version":1,"terrapod":{},"other":true}`},
			{"missing terrapod", `{"version":1}`},
			{"null terrapod", `{"version":1,"terrapod":null}`},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "config.json")
				testutil.WriteFile(t, path, []byte(tt.json), 0o600)
				if _, err := Load(path); err == nil {
					t.Fatal("Load() error = nil")
				}
			})
		}
	})

	t.Run("rejects symlink and non-regular input", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "target.json")
		testutil.WriteFile(t, target, []byte(`{"version":1,"terrapod":{}}`), 0o600)
		symlink := filepath.Join(dir, "symlink.json")
		if err := os.Symlink(target, symlink); err != nil {
			t.Fatal(err)
		}
		for _, path := range []string{symlink, dir} {
			if _, err := Load(path); err == nil {
				t.Fatalf("Load(%q) error = nil", path)
			}
		}
	})
}

func TestNormalize(t *testing.T) {
	schema := model.ConfigSchema{Version: 1, Fields: []model.ConfigField{
		{ID: "profile", Kind: "string", Required: true},
		{ID: "enableEditorStack", Kind: "bool", Default: false},
	}}
	input := model.Config{Version: 0, Terrapod: map[string]any{
		"profile":  "vps-shell",
		"obsolete": true,
	}}

	got, changes, err := Normalize(input, schema)
	if err != nil {
		t.Fatal(err)
	}
	want := model.Config{Version: 1, Terrapod: map[string]any{
		"profile":           "vps-shell",
		"enableEditorStack": false,
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Normalize() = %#v, want %#v", got, want)
	}
	wantChanges := []Change{
		{Kind: ChangeNormalize, Field: "version", Before: 0, After: 1},
		{Kind: ChangeAdd, Field: "enableEditorStack", Before: nil, After: false},
		{Kind: ChangePrune, Field: "obsolete", Before: true, After: nil},
	}
	if !reflect.DeepEqual(changes, wantChanges) {
		t.Fatalf("changes = %#v, want %#v", changes, wantChanges)
	}
}

func TestNormalizePreservesValidOverride(t *testing.T) {
	schema := model.ConfigSchema{Version: 2, Fields: []model.ConfigField{
		{ID: "profile", Kind: "string", Required: true},
		{ID: "enabled", Kind: "bool", Default: false},
	}}
	input := model.Config{Version: 2, Terrapod: map[string]any{"profile": "vps-shell", "enabled": true}}

	got, changes, err := Normalize(input, schema)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, input) {
		t.Fatalf("Normalize() = %#v, want %#v", got, input)
	}
	if len(changes) != 0 {
		t.Fatalf("changes = %#v, want none", changes)
	}
}

func TestNormalizeRequiresValidRequiredFields(t *testing.T) {
	schema := model.ConfigSchema{Version: 1, Fields: []model.ConfigField{
		{ID: "profile", Kind: "string", Required: true},
	}}
	tests := []model.Config{
		{Version: 1, Terrapod: map[string]any{}},
		{Version: 1, Terrapod: map[string]any{"profile": true}},
		{Version: 1, Terrapod: map[string]any{"profile": ""}},
	}
	for _, input := range tests {
		_, _, err := Normalize(input, schema)
		var needsSetup *ErrNeedsSetup
		if !errors.As(err, &needsSetup) {
			t.Fatalf("Normalize(%#v) error = %v, want *ErrNeedsSetup", input, err)
		}
	}
}

func TestNormalizeReplacesInvalidOptionalValueWithDefault(t *testing.T) {
	schema := model.ConfigSchema{Version: 1, Fields: []model.ConfigField{
		{ID: "enabled", Kind: "bool", Default: false},
	}}
	input := model.Config{Version: 1, Terrapod: map[string]any{"enabled": "yes"}}

	got, changes, err := Normalize(input, schema)
	if err != nil {
		t.Fatal(err)
	}
	if got.Terrapod["enabled"] != false {
		t.Fatalf("enabled = %#v, want false", got.Terrapod["enabled"])
	}
	want := []Change{{Kind: ChangeNormalize, Field: "enabled", Before: "yes", After: false}}
	if !reflect.DeepEqual(changes, want) {
		t.Fatalf("changes = %#v, want %#v", changes, want)
	}
}

func TestWriteAtomicWritesCanonicalPrivateConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "terrapod", "config.json")
	cfg := model.Config{Version: 1, Terrapod: map[string]any{
		"profile": "vps-shell",
		"enabled": true,
	}}

	if err := WriteAtomic(path, cfg); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\"version\":1,\"terrapod\":{\"enabled\":true,\"profile\":\"vps-shell\"}}\n"
	if string(contents) != want {
		t.Fatalf("contents = %q, want %q", contents, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("file mode = %o, want 600", got)
	}
	parentInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if got := parentInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("parent mode = %o, want 700", got)
	}
}

func TestWriteAtomicReplacesOnlyAfterEncodingSucceeds(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	testutil.WriteFile(t, path, []byte("old\n"), 0o600)
	bad := model.Config{Version: 1, Terrapod: map[string]any{"bad": func() {}}}

	if err := WriteAtomic(path, bad); err == nil {
		t.Fatal("WriteAtomic() error = nil")
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "old\n" {
		t.Fatalf("contents = %q, want old contents", contents)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.json" {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("directory entries = %s, want only config.json", strings.Join(names, ", "))
	}

	good := model.Config{Version: 2, Terrapod: map[string]any{"profile": "vps-shell"}}
	if err := WriteAtomic(path, good); err != nil {
		t.Fatal(err)
	}
	contents, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "{\"version\":2,\"terrapod\":{\"profile\":\"vps-shell\"}}\n" {
		t.Fatalf("replacement contents = %q", contents)
	}
}
