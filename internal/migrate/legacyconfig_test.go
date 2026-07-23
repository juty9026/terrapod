package migrate

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/juty9026/terrapod/internal/config"
	"github.com/juty9026/terrapod/internal/model"
)

func TestConvertLegacyConfigPreservesUnrelatedBytesAndRemovesCurrentAndDeprecatedKeys(t *testing.T) {
	input, err := os.ReadFile("testdata/chezmoi-current.toml")
	if err != nil {
		t.Fatal(err)
	}
	got, err := ConvertLegacyConfig(input, legacySchema())
	if err != nil {
		t.Fatal(err)
	}
	wantConfig := model.Config{Version: 1, Terrapod: map[string]any{
		"profile":                            "macos-terminal",
		"enableEditorStack":                  true,
		"enableAiCliTools":                   false,
		"enableDevelopmentWorkspace":         true,
		"enableMacosAppGroupTerminalApps":    true,
		"enableMacosAppGroupAutomation":      false,
		"enableMacosAppGroupLauncher":        true,
		"enableMacosAppGroupMonitoring":      false,
		"enableMacosAppGroupDevelopmentApps": true,
	}}
	if !reflect.DeepEqual(got.Terrapod, wantConfig) {
		t.Fatalf("Terrapod = %#v, want %#v", got.Terrapod, wantConfig)
	}
	wantRewritten := `[sourceState]
branch = "main"

[data]
email = "minu@example.com"
# profile = "vps-shell" and a harmless """ marker in a comment
note = "enableEditorStack = false"

[edit]
command = "nvim"
`
	if string(got.RewrittenChezmoi) != wantRewritten {
		t.Fatalf("rewritten bytes differ:\n%s", got.RewrittenChezmoi)
	}
	wantRemoved := []string{
		"profile", "enableEditorStack", "enableAiCliTools", "enableDevelopmentWorkspace",
		"enableMacosAppGroupTerminalApps", "enableMacosAppGroupAutomation",
		"enableMacosAppGroupLauncher", "enableMacosAppGroupMonitoring",
		"enableMacosAppGroupDevelopmentApps", "enableMacosAppGroupAiApps",
		"enableMacosDesktopApps", "terrapodPreset",
	}
	if !reflect.DeepEqual(got.Removed, wantRemoved) {
		t.Fatalf("Removed = %#v, want %#v", got.Removed, wantRemoved)
	}
}

func TestConvertLegacyConfigAcceptsRootDottedAndPreservesCRLF(t *testing.T) {
	input := []byte("data.email = \"keep\"\r\n" +
		"data.profile = \"vps-shell\"\r\n" +
		"\"data\".'enableEditorStack' = false # managed\r\n" +
		"data.enableAiCliTools = true\r\n" +
		"data.enableDevelopmentWorkspace = false\r\n" +
		"data.enableMacosAppGroupTerminalApps = false\r\n" +
		"data.enableMacosAppGroupAutomation = false\r\n" +
		"data.enableMacosAppGroupLauncher = false\r\n" +
		"data.enableMacosAppGroupMonitoring = false\r\n" +
		"data.enableMacosAppGroupDevelopmentApps = false\r\n" +
		"\r\n[edit]\r\ncommand = \"nvim\"\r\n")
	got, err := ConvertLegacyConfig(input, legacySchema())
	if err != nil {
		t.Fatal(err)
	}
	want := []byte("data.email = \"keep\"\r\n\r\n[edit]\r\ncommand = \"nvim\"\r\n")
	if !bytes.Equal(got.RewrittenChezmoi, want) {
		t.Fatalf("CRLF rewrite = %q, want %q", got.RewrittenChezmoi, want)
	}
	if !bytes.Contains(got.RewrittenChezmoi, []byte("\r\n")) || bytes.Contains(got.RewrittenChezmoi, []byte("\n[edit]\n")) {
		t.Fatal("CRLF line endings were normalized")
	}
}

func TestConvertLegacyConfigRejectsAmbiguousOrInvalidInputs(t *testing.T) {
	valid := `[data]
profile = "vps-shell"
enableEditorStack = true
enableAiCliTools = false
enableDevelopmentWorkspace = false
enableMacosAppGroupTerminalApps = false
enableMacosAppGroupAutomation = false
enableMacosAppGroupLauncher = false
enableMacosAppGroupMonitoring = false
enableMacosAppGroupDevelopmentApps = false
`
	tests := []struct {
		name    string
		input   string
		message string
	}{
		{"duplicate", valid + "enableEditorStack = false\n", "duplicate"},
		{"invalid bool", strings.Replace(valid, "enableEditorStack = true", `enableEditorStack = "true"`, 1), "boolean"},
		{"invalid profile", strings.Replace(valid, `"vps-shell"`, `"server"`, 1), "profile"},
		{"inline data", `data = { profile = "vps-shell" }` + "\n", "inline"},
		{"multiline string", string(mustRead(t, "testdata/chezmoi-unsupported.toml")), "multiline"},
		{"multiline array", "[data]\nprofile = \"vps-shell\"\nkeep = [\n  \"x\",\n]\n", "multiline"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ConvertLegacyConfig([]byte(test.input), legacySchema()); err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("error = %v, want %q", err, test.message)
			}
		})
	}
}

func TestApplyConfigConversionWritesValidatedJSONBackupAndPrunedLegacy(t *testing.T) {
	root := t.TempDir()
	chezmoiPath := filepath.Join(root, "chezmoi", "chezmoi.toml")
	terrapodPath := filepath.Join(root, "terrapod", "config.json")
	backupDir := filepath.Join(root, "backup")
	if err := os.MkdirAll(filepath.Dir(chezmoiPath), 0o700); err != nil {
		t.Fatal(err)
	}
	original := mustRead(t, "testdata/chezmoi-current.toml")
	if err := os.WriteFile(chezmoiPath, original, 0o640); err != nil {
		t.Fatal(err)
	}
	conversion, err := ConvertLegacyConfig(original, legacySchema())
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyConfigConversion(chezmoiPath, terrapodPath, conversion, backupDir); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(terrapodPath)
	if err != nil || !reflect.DeepEqual(loaded, conversion.Terrapod) {
		t.Fatalf("loaded config = %#v, %v", loaded, err)
	}
	rewritten, _ := os.ReadFile(chezmoiPath)
	if !bytes.Equal(rewritten, conversion.RewrittenChezmoi) {
		t.Fatal("legacy config was not rewritten exactly")
	}
	info, _ := os.Stat(chezmoiPath)
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("legacy mode = %o, want 640", info.Mode().Perm())
	}
	backup, err := os.ReadFile(filepath.Join(backupDir, "chezmoi.toml"))
	if err != nil || !bytes.Equal(backup, original) {
		t.Fatalf("backup = %q, %v", backup, err)
	}
}

func TestApplyConfigConversionRejectsSymlinkAndRollsBackInvalidNewConfig(t *testing.T) {
	root := t.TempDir()
	realPath := filepath.Join(root, "real.toml")
	symlinkPath := filepath.Join(root, "chezmoi.toml")
	if err := os.WriteFile(realPath, []byte("[data]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realPath, symlinkPath); err != nil {
		t.Fatal(err)
	}
	terrapodPath := filepath.Join(root, "config.json")
	value := ConfigConversion{Terrapod: model.Config{Version: 1}, RewrittenChezmoi: []byte("[data]\n")}
	if err := ApplyConfigConversion(symlinkPath, terrapodPath, value, filepath.Join(root, "backup")); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlink error = %v", err)
	}
	if _, err := os.Lstat(terrapodPath); !os.IsNotExist(err) {
		t.Fatalf("invalid JSON target remains: %v", err)
	}

	legacyPath := filepath.Join(root, "legacy.toml")
	if err := os.WriteFile(legacyPath, []byte("[data]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ApplyConfigConversion(legacyPath, terrapodPath, value, filepath.Join(root, "backup2")); err == nil {
		t.Fatal("invalid independent config was accepted")
	}
	if got, _ := os.ReadFile(legacyPath); string(got) != "[data]\n" {
		t.Fatal("legacy config changed before new JSON validated")
	}
	if _, err := os.Lstat(terrapodPath); !os.IsNotExist(err) {
		t.Fatalf("invalid JSON target remains: %v", err)
	}
}

func TestApplyConfigConversionRemovesNewJSONWhenBackupFails(t *testing.T) {
	root := t.TempDir()
	legacyPath := filepath.Join(root, "chezmoi.toml")
	original := mustRead(t, "testdata/chezmoi-current.toml")
	if err := os.WriteFile(legacyPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	conversion, err := ConvertLegacyConfig(original, legacySchema())
	if err != nil {
		t.Fatal(err)
	}
	backupAsFile := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(backupAsFile, []byte("occupied"), 0o600); err != nil {
		t.Fatal(err)
	}
	terrapodPath := filepath.Join(root, "config.json")
	if err := ApplyConfigConversion(legacyPath, terrapodPath, conversion, backupAsFile); err == nil {
		t.Fatal("backup failure was accepted")
	}
	if _, err := os.Lstat(terrapodPath); !os.IsNotExist(err) {
		t.Fatalf("new JSON remains after failure: %v", err)
	}
	if got, _ := os.ReadFile(legacyPath); !bytes.Equal(got, original) {
		t.Fatal("legacy config changed after backup failure")
	}
}

func TestApplyConfigConversionRemovesNewJSONWhenLegacyRewriteFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("directory permissions do not block root")
	}
	root := t.TempDir()
	legacyDir := filepath.Join(root, "legacy")
	legacyPath := filepath.Join(legacyDir, "chezmoi.toml")
	if err := os.Mkdir(legacyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	original := mustRead(t, "testdata/chezmoi-current.toml")
	if err := os.WriteFile(legacyPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	conversion, err := ConvertLegacyConfig(original, legacySchema())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(legacyDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(legacyDir, 0o700) })
	terrapodPath := filepath.Join(root, "independent", "config.json")
	if err := ApplyConfigConversion(legacyPath, terrapodPath, conversion, filepath.Join(root, "backup")); err == nil || !strings.Contains(err.Error(), "rewrite") {
		t.Fatalf("legacy rewrite error = %v", err)
	}
	if _, err := os.Lstat(terrapodPath); !os.IsNotExist(err) {
		t.Fatalf("new JSON remains after legacy rewrite failure: %v", err)
	}
	if got, _ := os.ReadFile(legacyPath); !bytes.Equal(got, original) {
		t.Fatal("failed atomic rewrite changed legacy config")
	}
}

func legacySchema() model.ConfigSchema {
	fields := []model.ConfigField{{ID: "profile", Kind: "string", Required: true}}
	for _, id := range []string{
		"enableEditorStack", "enableAiCliTools", "enableDevelopmentWorkspace",
		"enableMacosAppGroupTerminalApps", "enableMacosAppGroupAutomation",
		"enableMacosAppGroupLauncher", "enableMacosAppGroupMonitoring",
		"enableMacosAppGroupDevelopmentApps",
	} {
		fields = append(fields, model.ConfigField{ID: id, Kind: "bool", Default: false})
	}
	return model.ConfigSchema{Version: 1, Fields: fields}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	value, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
