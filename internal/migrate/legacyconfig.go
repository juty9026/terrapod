package migrate

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"

	"github.com/juty9026/terrapod/internal/config"
	"github.com/juty9026/terrapod/internal/model"
)

type ConfigConversion struct {
	Terrapod         model.Config
	RewrittenChezmoi []byte
	Removed          []string
}

var (
	dataSectionPattern = regexp.MustCompile(`^\s*\[\s*(?:data|"data"|'data')\s*\]\s*$`)
	sectionPattern     = regexp.MustCompile(`^\s*\[\[?.*\]?\]\s*$`)
	bareKeyPattern     = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
)

var currentManagedKeys = map[string]bool{
	"profile": true, "enableEditorStack": true, "enableAiCliTools": true,
	"enableDevelopmentWorkspace": true, "enableMacosAppGroupTerminalApps": true,
	"enableMacosAppGroupAutomation": true, "enableMacosAppGroupLauncher": true,
	"enableMacosAppGroupMonitoring": true, "enableMacosAppGroupDevelopmentApps": true,
}

var deprecatedManagedKeys = map[string]bool{
	"enableMacosAppGroupAiApps": true,
	"enableMacosDesktopApps":    true,
	"terrapodPreset":            true,
}

var afterLegacyRename func() error

func ConvertLegacyConfig(input []byte, schema model.ConfigSchema) (ConfigConversion, error) {
	if bytes.IndexByte(input, 0) >= 0 {
		return ConfigConversion{}, errors.New("legacy config contains NUL")
	}
	lines := splitLines(input)
	values := make(map[string]any)
	seen := make(map[string]bool)
	removed := make([]string, 0, len(currentManagedKeys)+len(deprecatedManagedKeys))
	rewritten := make([]byte, 0, len(input))
	inRoot, inData := true, false

	for number, raw := range lines {
		line := lineContents(raw)
		if hasMultilineMarker(line) {
			return ConfigConversion{}, fmt.Errorf("legacy config line %d uses an unsupported multiline string", number+1)
		}
		code, err := codeBeforeComment(line)
		if err != nil {
			return ConfigConversion{}, fmt.Errorf("legacy config line %d: %w", number+1, err)
		}
		trimmed := strings.TrimSpace(code)
		if isMultilineAssignment(trimmed) {
			return ConfigConversion{}, fmt.Errorf("legacy config line %d uses an unsupported multiline value", number+1)
		}

		if dataSectionPattern.MatchString(trimmed) {
			inRoot, inData = false, true
			rewritten = append(rewritten, raw...)
			continue
		}
		if sectionPattern.MatchString(trimmed) {
			inRoot, inData = false, false
			rewritten = append(rewritten, raw...)
			continue
		}

		key, rhs, assignment := parseAssignment(code)
		if inRoot && assignment {
			if simple, ok := parseSimpleKey(key); ok && simple == "data" && strings.HasPrefix(strings.TrimSpace(rhs), "{") {
				return ConfigConversion{}, errors.New("unsupported inline data table")
			}
		}
		if inRoot && assignment {
			if rootKey, ok := parseRootDataKey(key); ok {
				key = rootKey
			} else {
				assignment = false
			}
		} else if !inData {
			assignment = false
		} else if assignment {
			var ok bool
			key, ok = parseSimpleKey(key)
			assignment = ok
		}

		if assignment && (currentManagedKeys[key] || deprecatedManagedKeys[key]) {
			if seen[key] {
				return ConfigConversion{}, fmt.Errorf("duplicate legacy managed assignment %q", key)
			}
			seen[key] = true
			removed = append(removed, key)
			if currentManagedKeys[key] {
				value, err := parseManagedValue(key, rhs)
				if err != nil {
					return ConfigConversion{}, fmt.Errorf("legacy managed key %q: %w", key, err)
				}
				values[key] = value
			}
			continue
		}
		rewritten = append(rewritten, raw...)
	}

	profileValue, ok := values["profile"].(string)
	profile := model.Profile(profileValue)
	if !ok || !profile.Supported() {
		return ConfigConversion{}, fmt.Errorf("legacy profile %q is not supported", profileValue)
	}
	normalized, _, err := config.Normalize(model.Config{Version: schema.Version, Terrapod: values}, schema)
	if err != nil {
		return ConfigConversion{}, fmt.Errorf("normalize converted config: %w", err)
	}
	return ConfigConversion{Terrapod: normalized, RewrittenChezmoi: rewritten, Removed: removed}, nil
}

func ApplyConfigConversion(chezmoiPath, terrapodPath string, value ConfigConversion, backupDir string) error {
	legacyInfo, err := os.Lstat(chezmoiPath)
	if err != nil {
		return fmt.Errorf("inspect legacy chezmoi config: %w", err)
	}
	if legacyInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New("legacy chezmoi config is a symlink")
	}
	if !legacyInfo.Mode().IsRegular() {
		return errors.New("legacy chezmoi config is not a regular file")
	}
	if _, err := os.Lstat(terrapodPath); err == nil {
		return errors.New("independent Terrapod config already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect independent Terrapod config: %w", err)
	}

	if err := config.WriteAtomic(terrapodPath, value.Terrapod); err != nil {
		return fmt.Errorf("write independent Terrapod config: %w", err)
	}
	keepNewConfig := false
	defer func() {
		if !keepNewConfig {
			_ = os.Remove(terrapodPath)
		}
	}()
	loaded, err := config.Load(terrapodPath)
	if err != nil || !reflect.DeepEqual(loaded, value.Terrapod) {
		if err == nil {
			err = errors.New("written config differs from conversion")
		}
		return fmt.Errorf("validate independent Terrapod config: %w", err)
	}

	original, err := os.ReadFile(chezmoiPath)
	if err != nil {
		return fmt.Errorf("read legacy chezmoi config: %w", err)
	}
	if err := writeBackup(filepath.Join(backupDir, filepath.Base(chezmoiPath)), original, legacyInfo.Mode().Perm()); err != nil {
		return fmt.Errorf("back up legacy chezmoi config: %w", err)
	}
	replaced, err := writeAtomicBytes(chezmoiPath, value.RewrittenChezmoi, legacyInfo.Mode().Perm(), afterLegacyRename)
	if err != nil {
		if replaced {
			if _, restoreErr := writeAtomicBytes(chezmoiPath, original, legacyInfo.Mode().Perm(), nil); restoreErr != nil {
				return fmt.Errorf("rewrite legacy chezmoi config: %v; restore original legacy config: %w", err, restoreErr)
			}
		}
		return fmt.Errorf("rewrite legacy chezmoi config: %w", err)
	}
	keepNewConfig = true
	return nil
}

func hasMultilineMarker(line string) bool {
	var quote byte
	escaped := false
	for i := 0; i < len(line); i++ {
		ch := line[i]
		if quote == '"' {
			if escaped {
				escaped = false
			} else if ch == '\\' {
				escaped = true
			} else if ch == quote {
				quote = 0
			}
			continue
		}
		if quote == '\'' {
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '#' {
			return false
		}
		if i+2 < len(line) && ((ch == '"' && line[i:i+3] == `"""`) || (ch == '\'' && line[i:i+3] == `'''`)) {
			return true
		}
		if ch == '"' || ch == '\'' {
			quote = ch
		}
	}
	return false
}

func splitLines(input []byte) [][]byte {
	lines := make([][]byte, 0, bytes.Count(input, []byte{'\n'})+1)
	for len(input) > 0 {
		end := bytes.IndexByte(input, '\n')
		if end < 0 {
			lines = append(lines, input)
			break
		}
		end++
		lines = append(lines, input[:end])
		input = input[end:]
	}
	return lines
}

func lineContents(raw []byte) string {
	value := strings.TrimSuffix(string(raw), "\n")
	return strings.TrimSuffix(value, "\r")
}

func codeBeforeComment(line string) (string, error) {
	var quote byte
	escaped := false
	for i := 0; i < len(line); i++ {
		ch := line[i]
		if quote == '"' {
			if escaped {
				escaped = false
			} else if ch == '\\' {
				escaped = true
			} else if ch == quote {
				quote = 0
			}
			continue
		}
		if quote == '\'' {
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' {
			quote = ch
		} else if ch == '#' {
			return line[:i], nil
		}
	}
	if quote != 0 {
		return "", errors.New("unterminated string")
	}
	return line, nil
}

func parseAssignment(line string) (string, string, bool) {
	var quote byte
	escaped := false
	for i := 0; i < len(line); i++ {
		ch := line[i]
		if quote == '"' {
			if escaped {
				escaped = false
			} else if ch == '\\' {
				escaped = true
			} else if ch == quote {
				quote = 0
			}
			continue
		}
		if quote == '\'' {
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' {
			quote = ch
		} else if ch == '=' {
			return strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+1:]), true
		}
	}
	return "", "", false
}

func parseSimpleKey(input string) (string, bool) {
	input = strings.TrimSpace(input)
	if bareKeyPattern.MatchString(input) {
		return input, true
	}
	if len(input) >= 2 && ((input[0] == '"' && input[len(input)-1] == '"') || (input[0] == '\'' && input[len(input)-1] == '\'')) {
		value := input[1 : len(input)-1]
		return value, bareKeyPattern.MatchString(value)
	}
	return "", false
}

func parseRootDataKey(input string) (string, bool) {
	input = strings.TrimSpace(input)
	for _, prefix := range []string{"data", `"data"`, "'data'"} {
		if !strings.HasPrefix(input, prefix) {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(input, prefix))
		if !strings.HasPrefix(rest, ".") {
			return "", false
		}
		return parseSimpleKey(strings.TrimSpace(strings.TrimPrefix(rest, ".")))
	}
	return "", false
}

func parseManagedValue(key, rhs string) (any, error) {
	rhs = strings.TrimSpace(rhs)
	if key == "profile" {
		if len(rhs) < 2 || !((rhs[0] == '"' && rhs[len(rhs)-1] == '"') || (rhs[0] == '\'' && rhs[len(rhs)-1] == '\'')) {
			return nil, errors.New("profile must be a quoted string")
		}
		value := rhs[1 : len(rhs)-1]
		if !model.Profile(value).Supported() {
			return nil, fmt.Errorf("profile %q is not supported", value)
		}
		return value, nil
	}
	switch rhs {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return nil, errors.New("value must be a boolean")
	}
}

func isMultilineAssignment(line string) bool {
	_, rhs, ok := parseAssignment(line)
	if !ok {
		return false
	}
	rhs = strings.TrimSpace(rhs)
	if !strings.HasPrefix(rhs, "[") {
		return false
	}
	return !strings.HasSuffix(rhs, "]")
}

func writeBackup(path string, contents []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := file.Write(contents); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func writeAtomicBytes(path string, contents []byte, mode os.FileMode, afterRename func() error) (bool, error) {
	dir := filepath.Dir(path)
	temp, err := os.CreateTemp(dir, ".terrapod-migrate-")
	if err != nil {
		return false, err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(mode); err != nil {
		_ = temp.Close()
		return false, err
	}
	if _, err := temp.Write(contents); err != nil {
		_ = temp.Close()
		return false, err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return false, err
	}
	if err := temp.Close(); err != nil {
		return false, err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return false, err
	}
	if afterRename != nil {
		if err := afterRename(); err != nil {
			return true, err
		}
	}
	directory, err := os.Open(dir)
	if err != nil {
		return true, err
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return true, err
	}
	return true, nil
}
