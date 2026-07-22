package paths

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestResolveDefaults(t *testing.T) {
	home := filepath.Join(string(filepath.Separator), "home", "minu")
	dataDir := filepath.Join(home, ".local", "share", "terrapod")
	want := Layout{
		HomeDir:       home,
		ConfigFile:    filepath.Join(home, ".config", "terrapod", "config.json"),
		StateDir:      filepath.Join(home, ".local", "state", "terrapod"),
		DataDir:       dataDir,
		CacheDir:      filepath.Join(home, ".cache", "terrapod"),
		ReleaseDir:    filepath.Join(dataDir, "releases"),
		ActiveRelease: filepath.Join(dataDir, "current"),
	}

	if got := Resolve(home, nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("Resolve() = %#v, want %#v", got, want)
	}
}

func TestResolveOverrides(t *testing.T) {
	home := filepath.Join(string(filepath.Separator), "home", "minu")
	env := map[string]string{
		"XDG_CONFIG_HOME": filepath.Join(string(filepath.Separator), "xdg", "config"),
		"XDG_STATE_HOME":  filepath.Join(string(filepath.Separator), "xdg", "state"),
		"XDG_DATA_HOME":   filepath.Join(string(filepath.Separator), "xdg", "data"),
		"XDG_CACHE_HOME":  filepath.Join(string(filepath.Separator), "xdg", "cache"),
	}
	dataDir := filepath.Join(env["XDG_DATA_HOME"], "terrapod")
	want := Layout{
		HomeDir:       home,
		ConfigFile:    filepath.Join(env["XDG_CONFIG_HOME"], "terrapod", "config.json"),
		StateDir:      filepath.Join(env["XDG_STATE_HOME"], "terrapod"),
		DataDir:       dataDir,
		CacheDir:      filepath.Join(env["XDG_CACHE_HOME"], "terrapod"),
		ReleaseDir:    filepath.Join(dataDir, "releases"),
		ActiveRelease: filepath.Join(dataDir, "current"),
	}

	if got := Resolve(home, env); !reflect.DeepEqual(got, want) {
		t.Fatalf("Resolve() = %#v, want %#v", got, want)
	}
}
