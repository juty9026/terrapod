package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/juty9026/terrapod/internal/chezmoi"
	"github.com/juty9026/terrapod/internal/execx"
	"github.com/juty9026/terrapod/internal/paths"
	"github.com/juty9026/terrapod/internal/state"
)

func TestBuiltBinaryDispatchesThroughRealConstrainedChezmoiClient(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	xdgData := filepath.Join(root, "data")
	xdgConfig := filepath.Join(root, "config")
	source := filepath.Join(xdgData, "terrapod", "current")
	config := filepath.Join(xdgConfig, "terrapod", "config.json")
	logPath := filepath.Join(root, "argv.log")
	for _, dir := range []string{home, source, filepath.Dir(config)} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(source, "dot_test"), []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte(`{"version":1,"terrapod":{"profile":"macos-terminal"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := filepath.Join(root, "chezmoi")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >" + logPath + "\nprintf 'fixture-status\\n'\n"
	if err := os.WriteFile(fake, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(root, "tpod")
	build := exec.Command("go", "build", "-ldflags", "-X main.chezmoiPathOverride="+fake, "-o", binary, ".")
	build.Env = os.Environ()
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	command := exec.Command(binary, "chezmoi", "--", "status", ".zshrc")
	command.Env = append(os.Environ(), "HOME="+home, "XDG_DATA_HOME="+xdgData, "XDG_CONFIG_HOME="+xdgConfig, "XDG_STATE_HOME="+filepath.Join(root, "state"), "XDG_CACHE_HOME="+filepath.Join(root, "cache"))
	output, err := command.CombinedOutput()
	if err != nil || string(output) != "fixture-status\n" {
		t.Fatalf("tpod chezmoi: %v, output=%q", err, output)
	}
	argv, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(string(argv))
	commandIndex, excludeIndex := index(fields, "status"), index(fields, "--exclude")
	if index(fields, "--source") < 0 || index(fields, "--override-data-file") < 0 || commandIndex < 0 || excludeIndex < commandIndex || index(fields, "scripts") != excludeIndex+1 {
		t.Fatalf("unsafe argv: %q", argv)
	}
	if index(fields, "apply") >= 0 || index(fields, "update") >= 0 || index(fields, "init") >= 0 {
		t.Fatalf("mutating argv: %q", argv)
	}
}

func TestProductionPlannerComposesRealStateBoundAdapters(t *testing.T) {
	home := t.TempDir()
	layout := paths.Resolve(home, map[string]string{})
	store, err := state.Open(layout.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	client := chezmoi.Client{Runner: execx.NewRunner([]string{"HOME"}, nil, func() int { return 501 }), Binary: filepath.Join(home, "chezmoi"), Source: layout.ActiveRelease, Config: layout.ConfigFile, Destination: home}
	got, err := productionPlanner(layout, store, client)
	if err != nil || got == nil {
		t.Fatalf("productionPlanner = %#v, %v", got, err)
	}
}

func index(values []string, target string) int {
	for i, value := range values {
		if value == target {
			return i
		}
	}
	return -1
}
