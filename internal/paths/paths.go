package paths

import "path/filepath"

type Layout struct {
	HomeDir         string
	ConfigFile      string
	StateDir        string
	DataDir         string
	CacheDir        string
	ReleaseCacheDir string
	ReleaseDir      string
	ActiveRelease   string
}

func Resolve(home string, env map[string]string) Layout {
	configHome := envOrDefault(env, "XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	stateHome := envOrDefault(env, "XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	dataHome := envOrDefault(env, "XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	cacheHome := envOrDefault(env, "XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	dataDir := filepath.Join(dataHome, "terrapod")

	cacheDir := filepath.Join(cacheHome, "terrapod")
	return Layout{
		HomeDir:         home,
		ConfigFile:      filepath.Join(configHome, "terrapod", "config.json"),
		StateDir:        filepath.Join(stateHome, "terrapod"),
		DataDir:         dataDir,
		CacheDir:        cacheDir,
		ReleaseCacheDir: filepath.Join(cacheDir, "releases"),
		ReleaseDir:      filepath.Join(dataDir, "releases"),
		ActiveRelease:   filepath.Join(dataDir, "current"),
	}
}

func envOrDefault(env map[string]string, name, fallback string) string {
	if value := env[name]; value != "" {
		return value
	}
	return fallback
}
