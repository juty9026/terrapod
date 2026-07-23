package setup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/juty9026/terrapod/internal/config"
	"github.com/juty9026/terrapod/internal/model"
)

func TestDetectProfile(t *testing.T) {
	tests := []struct {
		goos string
		want model.Profile
	}{
		{"darwin", model.ProfileMacOSTerminal},
		{"linux", model.ProfileVPSShell},
	}
	for _, test := range tests {
		got, err := DetectProfile(test.goos)
		if err != nil || got != test.want {
			t.Fatalf("DetectProfile(%q) = %q, %v", test.goos, got, err)
		}
	}
	if _, err := DetectProfile("windows"); err == nil {
		t.Fatal("DetectProfile accepted an unsupported OS")
	}
}

func TestExpandPresets(t *testing.T) {
	allFalse := values(false, false, false, false)
	development := values(true, true, true, false)
	workstation := values(true, true, true, true)
	tests := []struct {
		p       Preset
		profile model.Profile
		want    map[string]any
	}{
		{PresetMinimal, model.ProfileVPSShell, withProfile(allFalse, model.ProfileVPSShell)},
		{PresetDevelopment, model.ProfileVPSShell, withProfile(development, model.ProfileVPSShell)},
		{PresetMinimal, model.ProfileMacOSTerminal, withProfile(allFalse, model.ProfileMacOSTerminal)},
		{PresetDevelopment, model.ProfileMacOSTerminal, withProfile(development, model.ProfileMacOSTerminal)},
		{PresetWorkstation, model.ProfileMacOSTerminal, withProfile(workstation, model.ProfileMacOSTerminal)},
	}
	for _, test := range tests {
		got, err := Expand(test.p, test.profile)
		if err != nil || got.Version != 1 || !reflect.DeepEqual(got.Terrapod, test.want) {
			t.Fatalf("Expand(%q, %q) = %#v, %v; want %#v", test.p, test.profile, got, err, test.want)
		}
	}
	if _, err := Expand(PresetWorkstation, model.ProfileVPSShell); err == nil {
		t.Fatal("workstation accepted on VPS")
	}
	if _, err := Expand("other", model.ProfileMacOSTerminal); err == nil {
		t.Fatal("unknown Preset accepted")
	}
}

func TestRunInteractiveIsSequentialAndWorkspaceImpliesLeafStacks(t *testing.T) {
	gum := &fakeGum{choice: PresetMinimal, answers: []bool{true, false, true, false, true, false, true}}
	got, err := RunInteractive(context.Background(), nil, model.ProfileMacOSTerminal, gum)
	if err != nil {
		t.Fatal(err)
	}
	want := withProfile(values(true, true, true, false), model.ProfileMacOSTerminal)
	want["enableMacosAppGroupAutomation"] = true
	want["enableMacosAppGroupMonitoring"] = true
	if !reflect.DeepEqual(got.Terrapod, want) {
		t.Fatalf("config = %#v, want %#v", got.Terrapod, want)
	}
	wantCalls := []string{
		"choose:minimal",
		"confirm:enableDevelopmentWorkspace:false",
		"confirm:enableMacosAppGroupTerminalApps:false",
		"confirm:enableMacosAppGroupAutomation:false",
		"confirm:enableMacosAppGroupLauncher:false",
		"confirm:enableMacosAppGroupMonitoring:false",
		"confirm:enableMacosAppGroupDevelopmentApps:false",
		"confirm:write:true",
	}
	if !reflect.DeepEqual(gum.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", gum.calls, wantCalls)
	}
}

func TestRunInteractiveUsesCurrentConfigAsProposalAndHidesMacGroupsOnVPS(t *testing.T) {
	current, _ := Expand(PresetDevelopment, model.ProfileVPSShell)
	current.Terrapod["enableDevelopmentWorkspace"] = false
	gum := &fakeGum{choice: PresetDevelopment, answers: []bool{false, false, true, true}}
	_, err := RunInteractive(context.Background(), &current, model.ProfileVPSShell, gum)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"choose:development",
		"confirm:enableDevelopmentWorkspace:false",
		"confirm:enableEditorStack:true",
		"confirm:enableAiCliTools:true",
		"confirm:write:true",
	}
	if !reflect.DeepEqual(gum.calls, want) {
		t.Fatalf("calls = %#v, want %#v", gum.calls, want)
	}
}

func TestRunInteractiveCancellation(t *testing.T) {
	gum := &fakeGum{choice: PresetMinimal, answers: []bool{false, false, false, false}, cancelAt: 4}
	if _, err := RunInteractive(context.Background(), nil, model.ProfileVPSShell, gum); !errors.Is(err, ErrCancelled) {
		t.Fatalf("error = %v, want ErrCancelled", err)
	}
}

func TestCommandGumExplainsMissingDependency(t *testing.T) {
	gum := CommandGum{Binary: filepath.Join(t.TempDir(), "missing-gum")}
	_, err := gum.ChoosePreset(context.Background(), []Preset{PresetMinimal}, PresetMinimal)
	if err == nil || !strings.Contains(err.Error(), "gum is required") {
		t.Fatalf("error = %v, want gum installation guidance", err)
	}
}

func TestManagerWritesNormalized0600ConfigAndDoesNotApply(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config", "config.json")
	stateDir := filepath.Join(root, "state")
	schema := testSchema()
	manager := Manager{
		ConfigPath: configPath,
		StateDir:   stateDir,
		Schema:     func() (model.ConfigSchema, error) { return schema, nil },
	}
	if _, err := manager.Configure(context.Background(), PresetDevelopment, model.ProfileVPSShell); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 600", info.Mode().Perm())
	}
	got, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := Expand(PresetDevelopment, model.ProfileVPSShell)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("loaded = %#v, want %#v", got, want)
	}
	if _, err := manager.Configure(context.Background(), PresetMinimal, model.ProfileVPSShell); err != nil {
		t.Fatal(err)
	}
	got, _ = config.Load(configPath)
	want, _ = Expand(PresetMinimal, model.ProfileVPSShell)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("overwrite = %#v, want %#v", got, want)
	}
}

func TestManagerInteractiveCancellationDoesNotWrite(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.json")
	before, _ := Expand(PresetDevelopment, model.ProfileVPSShell)
	if err := config.WriteAtomic(path, before); err != nil {
		t.Fatal(err)
	}
	gum := &fakeGum{choice: PresetMinimal, answers: []bool{false, false, false, false}, cancelAt: 4}
	manager := Manager{ConfigPath: path, StateDir: filepath.Join(root, "state"), Schema: func() (model.ConfigSchema, error) { return testSchema(), nil }}
	if _, err := manager.Interactive(context.Background(), model.ProfileVPSShell, gum); !errors.Is(err, ErrCancelled) {
		t.Fatalf("error = %v, want cancellation", err)
	}
	got, _ := config.Load(path)
	if !reflect.DeepEqual(got, before) {
		t.Fatalf("cancel changed config: %#v", got)
	}
}

func TestManagerInteractiveRereadsConfigInsideFinalLock(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.json")
	before, _ := Expand(PresetMinimal, model.ProfileVPSShell)
	if err := config.WriteAtomic(path, before); err != nil {
		t.Fatal(err)
	}
	changed, _ := Expand(PresetDevelopment, model.ProfileVPSShell)
	gum := &fakeGum{
		choice:  PresetMinimal,
		answers: []bool{false, false, false, true},
		onCall: func(call int) {
			if call == 4 {
				if err := config.WriteAtomic(path, changed); err != nil {
					t.Fatal(err)
				}
			}
		},
	}
	manager := Manager{ConfigPath: path, StateDir: filepath.Join(root, "state"), Schema: func() (model.ConfigSchema, error) { return testSchema(), nil }}
	if _, err := manager.Interactive(context.Background(), model.ProfileVPSShell, gum); err == nil || !strings.Contains(err.Error(), "changed while setup was running") {
		t.Fatalf("error = %v, want concurrent-change rejection", err)
	}
	got, _ := config.Load(path)
	if !reflect.DeepEqual(got, changed) {
		t.Fatalf("setup overwrote concurrent config: %#v", got)
	}
}

type fakeGum struct {
	choice   Preset
	answers  []bool
	cancelAt int
	calls    []string
	onCall   func(int)
}

func (f *fakeGum) ChoosePreset(_ context.Context, available []Preset, initial Preset) (Preset, error) {
	f.calls = append(f.calls, "choose:"+string(initial))
	for _, candidate := range available {
		if candidate == f.choice {
			return f.choice, nil
		}
	}
	return "", errors.New("unavailable choice")
}

func (f *fakeGum) Confirm(_ context.Context, prompt string, initial bool) (bool, error) {
	f.calls = append(f.calls, "confirm:"+prompt+":"+boolString(initial))
	if f.onCall != nil {
		f.onCall(len(f.calls) - 1)
	}
	if f.cancelAt == len(f.calls)-1 {
		return false, ErrCancelled
	}
	answer := f.answers[0]
	f.answers = f.answers[1:]
	return answer, nil
}

func values(editor, ai, workspace, apps bool) map[string]any {
	return map[string]any{
		"enableEditorStack":                  editor,
		"enableAiCliTools":                   ai,
		"enableDevelopmentWorkspace":         workspace,
		"enableMacosAppGroupTerminalApps":    apps,
		"enableMacosAppGroupAutomation":      apps,
		"enableMacosAppGroupLauncher":        apps,
		"enableMacosAppGroupMonitoring":      apps,
		"enableMacosAppGroupDevelopmentApps": apps,
	}
}

func withProfile(input map[string]any, profile model.Profile) map[string]any {
	result := make(map[string]any, len(input)+1)
	for key, value := range input {
		result[key] = value
	}
	result["profile"] = string(profile)
	return result
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func testSchema() model.ConfigSchema {
	return model.ConfigSchema{Version: 1, Fields: []model.ConfigField{
		{ID: "profile", Kind: "string", Required: true},
		{ID: "enableEditorStack", Kind: "bool", Default: false},
		{ID: "enableAiCliTools", Kind: "bool", Default: false},
		{ID: "enableDevelopmentWorkspace", Kind: "bool", Default: false},
		{ID: "enableMacosAppGroupTerminalApps", Kind: "bool", Default: false},
		{ID: "enableMacosAppGroupAutomation", Kind: "bool", Default: false},
		{ID: "enableMacosAppGroupLauncher", Kind: "bool", Default: false},
		{ID: "enableMacosAppGroupMonitoring", Kind: "bool", Default: false},
		{ID: "enableMacosAppGroupDevelopmentApps", Kind: "bool", Default: false},
	}}
}
