package setup

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/juty9026/terrapod/internal/config"
	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/state"
)

type Preset string

const (
	PresetMinimal     Preset = "minimal"
	PresetDevelopment Preset = "development"
	PresetWorkstation Preset = "workstation"
)

var ErrCancelled = errors.New("setup cancelled")

var boolFields = []string{
	"enableEditorStack",
	"enableAiCliTools",
	"enableDevelopmentWorkspace",
	"enableMacosAppGroupTerminalApps",
	"enableMacosAppGroupAutomation",
	"enableMacosAppGroupLauncher",
	"enableMacosAppGroupMonitoring",
	"enableMacosAppGroupDevelopmentApps",
}

var macAppFields = boolFields[3:]

type Gum interface {
	ChoosePreset(context.Context, []Preset, Preset) (Preset, error)
	Confirm(context.Context, string, bool) (bool, error)
}

type Manager struct {
	ConfigPath string
	StateDir   string
	Schema     func() (model.ConfigSchema, error)
}

func DetectProfile(goos string) (model.Profile, error) {
	switch goos {
	case "darwin":
		return model.ProfileMacOSTerminal, nil
	case "linux":
		return model.ProfileVPSShell, nil
	default:
		return "", fmt.Errorf("unsupported operating system %q", goos)
	}
}

func Expand(p Preset, profile model.Profile) (model.Config, error) {
	if !profile.Supported() {
		return model.Config{}, fmt.Errorf("unsupported profile %q", profile)
	}
	if p != PresetMinimal && p != PresetDevelopment && p != PresetWorkstation {
		return model.Config{}, fmt.Errorf("unknown Preset %q", p)
	}
	if p == PresetWorkstation && profile != model.ProfileMacOSTerminal {
		return model.Config{}, errors.New("workstation Preset is only available for the macOS Terminal Profile")
	}

	enabledDevelopment := p == PresetDevelopment || p == PresetWorkstation
	enabledApps := p == PresetWorkstation
	values := map[string]any{
		"profile":                            string(profile),
		"enableEditorStack":                  enabledDevelopment,
		"enableAiCliTools":                   enabledDevelopment,
		"enableDevelopmentWorkspace":         enabledDevelopment,
		"enableMacosAppGroupTerminalApps":    enabledApps,
		"enableMacosAppGroupAutomation":      enabledApps,
		"enableMacosAppGroupLauncher":        enabledApps,
		"enableMacosAppGroupMonitoring":      enabledApps,
		"enableMacosAppGroupDevelopmentApps": enabledApps,
	}
	return model.Config{Version: 1, Terrapod: values}, nil
}

func RunInteractive(ctx context.Context, current *model.Config, profile model.Profile, gum Gum) (model.Config, error) {
	if gum == nil {
		return model.Config{}, errors.New("gum is required for 'tpod setup'; install the Terrapod Management Core and retry")
	}
	if !profile.Supported() {
		return model.Config{}, fmt.Errorf("unsupported profile %q", profile)
	}

	available := []Preset{PresetMinimal, PresetDevelopment}
	if profile == model.ProfileMacOSTerminal {
		available = append(available, PresetWorkstation)
	}
	initial := inferPreset(current, profile)
	selected, err := gum.ChoosePreset(ctx, available, initial)
	if err != nil {
		return model.Config{}, err
	}
	proposal, err := Expand(selected, profile)
	if err != nil {
		return model.Config{}, err
	}
	if current != nil && currentProfile(*current) == profile {
		for _, field := range boolFields {
			if value, ok := current.Terrapod[field].(bool); ok {
				proposal.Terrapod[field] = value
			}
		}
	}

	workspace, err := confirm(ctx, gum, "enableDevelopmentWorkspace", proposal)
	if err != nil {
		return model.Config{}, err
	}
	if workspace {
		proposal.Terrapod["enableEditorStack"] = true
		proposal.Terrapod["enableAiCliTools"] = true
	} else {
		if _, err := confirm(ctx, gum, "enableEditorStack", proposal); err != nil {
			return model.Config{}, err
		}
		if _, err := confirm(ctx, gum, "enableAiCliTools", proposal); err != nil {
			return model.Config{}, err
		}
	}
	if profile == model.ProfileMacOSTerminal {
		for _, field := range macAppFields {
			if _, err := confirm(ctx, gum, field, proposal); err != nil {
				return model.Config{}, err
			}
		}
	} else {
		for _, field := range macAppFields {
			proposal.Terrapod[field] = false
		}
	}
	accepted, err := gum.Confirm(ctx, "write", true)
	if err != nil {
		return model.Config{}, err
	}
	if !accepted {
		return model.Config{}, ErrCancelled
	}
	return proposal, nil
}

func (m Manager) Configure(ctx context.Context, preset Preset, profile model.Profile) (model.Config, error) {
	proposal, err := Expand(preset, profile)
	if err != nil {
		return model.Config{}, err
	}
	return m.write(ctx, proposal)
}

func (m Manager) Interactive(ctx context.Context, profile model.Profile, gum Gum) (model.Config, error) {
	var current *model.Config
	loaded, err := config.Load(m.ConfigPath)
	if err == nil {
		current = &loaded
	} else {
		var missing *config.ErrMissing
		if !errors.As(err, &missing) {
			return model.Config{}, err
		}
	}
	proposal, err := RunInteractive(ctx, current, profile, gum)
	if err != nil {
		return model.Config{}, err
	}

	lock, err := state.Acquire(m.StateDir, "tpod setup")
	if err != nil {
		return model.Config{}, err
	}
	defer lock.Release()
	reloaded, reloadErr := config.Load(m.ConfigPath)
	if current == nil {
		var missing *config.ErrMissing
		if !errors.As(reloadErr, &missing) {
			if reloadErr == nil {
				return model.Config{}, errors.New("Terrapod config changed while setup was running; rerun 'tpod setup'")
			}
			return model.Config{}, reloadErr
		}
	} else if reloadErr != nil || !reflect.DeepEqual(reloaded, *current) {
		return model.Config{}, errors.New("Terrapod config changed while setup was running; rerun 'tpod setup'")
	}
	return m.writeHeld(proposal)
}

func (m Manager) write(_ context.Context, proposal model.Config) (model.Config, error) {
	lock, err := state.Acquire(m.StateDir, "tpod configure")
	if err != nil {
		return model.Config{}, err
	}
	defer lock.Release()
	// Deliberately reread under the mutation lock. Configure is an explicit
	// non-interactive overwrite, so the current value does not alter its result.
	if _, err := config.Load(m.ConfigPath); err != nil {
		var missing *config.ErrMissing
		if !errors.As(err, &missing) {
			return model.Config{}, err
		}
	}
	return m.writeHeld(proposal)
}

func (m Manager) writeHeld(proposal model.Config) (model.Config, error) {
	if m.Schema == nil {
		return model.Config{}, errors.New("setup schema loader is not configured")
	}
	schema, err := m.Schema()
	if err != nil {
		return model.Config{}, err
	}
	normalized, _, err := config.Normalize(proposal, schema)
	if err != nil {
		return model.Config{}, err
	}
	if err := config.WriteAtomic(m.ConfigPath, normalized); err != nil {
		return model.Config{}, err
	}
	return normalized, nil
}

func confirm(ctx context.Context, gum Gum, field string, cfg model.Config) (bool, error) {
	initial, _ := cfg.Terrapod[field].(bool)
	value, err := gum.Confirm(ctx, field, initial)
	if err != nil {
		return false, err
	}
	cfg.Terrapod[field] = value
	return value, nil
}

func inferPreset(current *model.Config, profile model.Profile) Preset {
	if current == nil || currentProfile(*current) != profile {
		return PresetMinimal
	}
	candidates := []Preset{PresetMinimal, PresetDevelopment}
	if profile == model.ProfileMacOSTerminal {
		candidates = append(candidates, PresetWorkstation)
	}
	best, bestDistance := PresetMinimal, len(boolFields)+1
	for _, candidate := range candidates {
		expanded, _ := Expand(candidate, profile)
		distance := 0
		for _, field := range boolFields {
			currentValue, currentOK := current.Terrapod[field].(bool)
			candidateValue, _ := expanded.Terrapod[field].(bool)
			if !currentOK || currentValue != candidateValue {
				distance++
			}
		}
		if distance < bestDistance {
			best, bestDistance = candidate, distance
		}
	}
	return best
}

func currentProfile(cfg model.Config) model.Profile {
	value, _ := cfg.Terrapod["profile"].(string)
	return model.Profile(value)
}
