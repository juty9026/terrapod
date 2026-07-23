package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/juty9026/terrapod/internal/catalog"
	"github.com/juty9026/terrapod/internal/chezmoi"
	"github.com/juty9026/terrapod/internal/cli"
	"github.com/juty9026/terrapod/internal/execx"
	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/paths"
	"github.com/juty9026/terrapod/internal/planner"
	"github.com/juty9026/terrapod/internal/release"
	"github.com/juty9026/terrapod/internal/resource"
	"github.com/juty9026/terrapod/internal/resource/managementcore"
	"github.com/juty9026/terrapod/internal/state"
	"github.com/juty9026/terrapod/internal/testutil"
)

func TestProductionActiveCatalogRequiresPublishedReleaseVersion(t *testing.T) {
	published := catalog.Verified{Catalog: model.Catalog{Release: "1.2.3"}}
	if err := validateActiveCatalogRelease(published, "1.2.3"); err != nil {
		t.Fatalf("published catalog rejected: %v", err)
	}
	development := catalog.Verified{Catalog: model.Catalog{Release: "development"}}
	if err := validateActiveCatalogRelease(development, "1.2.3"); err == nil {
		t.Fatal("production loader accepted development source catalog")
	}
}

func TestRepairReleaseEndpointBindsStableVersionToTag(t *testing.T) {
	oldEndpoint := releaseLatestEndpoint
	t.Cleanup(func() { releaseLatestEndpoint = oldEndpoint })
	releaseLatestEndpoint = "https://api.example.test/releases/latest"

	endpoint, err := repairReleaseEndpoint("1.2.3")
	if err != nil || endpoint != "https://api.example.test/releases/tags/v1.2.3" {
		t.Fatalf("endpoint=%q err=%v", endpoint, err)
	}
	if _, err := repairReleaseEndpoint("1.2.3-rc.1"); err == nil {
		t.Fatal("prerelease repair version accepted")
	}
	releaseLatestEndpoint = "https://api.example.test/releases/current"
	if _, err := repairReleaseEndpoint("1.2.3"); err == nil {
		t.Fatal("non-latest embedded endpoint accepted")
	}
}

func TestRepairLatestReleaseRejectsDowngradeBeforeMutation(t *testing.T) {
	digest := strings.Repeat("a", 64)
	manifest := release.Manifest{
		Version:       "1.0.0",
		CatalogSchema: 1,
		StateSchema:   1,
		Assets: []release.Asset{
			{Kind: "binary", OS: "darwin", Arch: "amd64", Name: "tpod-darwin-amd64", Size: 1, SHA256: digest},
			{Kind: "binary", OS: "darwin", Arch: "arm64", Name: "tpod-darwin-arm64", Size: 1, SHA256: digest},
			{Kind: "binary", OS: "linux", Arch: "amd64", Name: "tpod-linux-amd64", Size: 1, SHA256: digest},
			{Kind: "binary", OS: "linux", Arch: "arm64", Name: "tpod-linux-arm64", Size: 1, SHA256: digest},
			{Kind: "source", Name: "terrapod-source.tar.gz", Size: 1, SHA256: digest},
			{Kind: "catalog", Name: "resources.json", Size: 1, SHA256: digest},
		},
	}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := release.NewLocalVerifiedRelease(manifestData, nil)
	if err != nil {
		t.Fatal(err)
	}
	expectedDigest, err := verified.Manifest.Digest()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	layout := paths.Resolve(filepath.Join(root, "home"), map[string]string{"XDG_DATA_HOME": filepath.Join(root, "data"), "XDG_CACHE_HOME": filepath.Join(root, "cache")})
	if err := os.MkdirAll(filepath.Dir(layout.ActiveRelease), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("releases", "2.0.0"), layout.ActiveRelease); err != nil {
		t.Fatal(err)
	}
	called := false
	err = repairLatestRelease(context.Background(), layout, expectedDigest, func(context.Context) (release.VerifiedRelease, error) {
		called = true
		return verified, nil
	}, false)
	if err == nil || !strings.Contains(err.Error(), "downgrade") || !called {
		t.Fatalf("called=%v err=%v", called, err)
	}
	if _, err := os.Lstat(filepath.Join(layout.ReleaseDir, "1.0.0")); !os.IsNotExist(err) {
		t.Fatalf("downgrade mutated release tree: %v", err)
	}
}

func TestBuiltRepairBinaryUsesStableVersionEndpoint(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("repair intentionally rejects root")
	}
	assets := make(map[string][]byte)
	var assetsMu sync.RWMutex
	var manifestData []byte
	var server *httptest.Server
	var certificatePEM []byte
	server, certificatePEM = newRepairTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/tags/v1.2.3" {
			type metadataAsset struct {
				Name string `json:"name"`
				Size int    `json:"size"`
				URL  string `json:"browser_download_url"`
			}
			names := []string{"release.json", "tpod-darwin-amd64", "tpod-darwin-arm64", "tpod-linux-amd64", "tpod-linux-arm64", "terrapod-source.tar.gz", "resources.json", "install.sh"}
			items := make([]metadataAsset, 0, len(names))
			assetsMu.RLock()
			for _, name := range names {
				items = append(items, metadataAsset{Name: name, Size: len(assets[name]), URL: server.URL + "/assets/" + name})
			}
			assetsMu.RUnlock()
			_ = json.NewEncoder(w).Encode(struct {
				TagName    string          `json:"tag_name"`
				Draft      bool            `json:"draft"`
				Prerelease bool            `json:"prerelease"`
				Assets     []metadataAsset `json:"assets"`
			}{TagName: "v1.2.3", Assets: items})
			return
		}
		name := strings.TrimPrefix(request.URL.Path, "/assets/")
		assetsMu.RLock()
		data, ok := assets[name]
		data = append([]byte(nil), data...)
		assetsMu.RUnlock()
		if !ok || request.URL.Path == name {
			http.NotFound(w, request)
			return
		}
		_, _ = w.Write(data)
	}))
	defer server.Close()

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err == nil {
				_ = os.Chmod(path, 0o700)
			}
			return nil
		})
	})
	certificate := filepath.Join(root, "release-ca.pem")
	if err := os.WriteFile(certificate, certificatePEM, 0o600); err != nil {
		t.Fatal(err)
	}
	embeddedLdflags := "-X main.releaseLatestEndpoint=" + server.URL + "/latest"
	if runtime.GOOS == "darwin" {
		normalBinary := filepath.Join(root, "tpod-normal")
		build := exec.Command("go", "build", "-ldflags", embeddedLdflags, "-o", normalBinary, ".")
		if output, err := build.CombinedOutput(); err != nil {
			t.Fatalf("build normal repair binary: %v\n%s", err, output)
		}
		probeHome := filepath.Join(root, "probe-home")
		if err := os.MkdirAll(probeHome, 0o700); err != nil {
			t.Fatal(err)
		}
		probe := exec.Command(normalBinary, "internal-repair-stage", "--manifest-digest", strings.Repeat("0", 64), "--release-version", "1.2.3")
		probe.Env = append(os.Environ(), "HOME="+probeHome, "XDG_DATA_HOME="+filepath.Join(root, "probe-data"), "XDG_CACHE_HOME="+filepath.Join(root, "probe-cache"), "SSL_CERT_FILE="+certificate)
		output, err := probe.CombinedOutput()
		if err == nil || !strings.Contains(string(output), "certificate signed by unknown authority") {
			t.Fatalf("normal macOS build unexpectedly accepted fixture CA: err=%v output=%s", err, output)
		}
		if _, err := os.Lstat(filepath.Join(root, "probe-data", "terrapod")); !os.IsNotExist(err) {
			t.Fatalf("failed TLS probe mutated release data: %v", err)
		}
	}

	binary := filepath.Join(root, "tpod")
	build := exec.Command("go", "build", "-tags", "tpod_repair_testroot", "-ldflags",
		embeddedLdflags+" -X main.repairTestCAFile="+certificate, "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build native repair binary: %v\n%s", err, output)
	}
	binaryData, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"tpod-darwin-amd64", "tpod-darwin-arm64", "tpod-linux-amd64", "tpod-linux-arm64"} {
		assetsMu.Lock()
		assets[name] = binaryData
		assetsMu.Unlock()
	}
	assetsMu.Lock()
	assets["terrapod-source.tar.gz"] = repairSourceArchive(t)
	assets["resources.json"] = []byte(`{"version":1,"release":"1.2.3","config":[],"resources":[]}`)
	assets["install.sh"] = []byte("#!/bin/sh\n")
	assetsMu.Unlock()
	manifest := release.Manifest{
		Version:       "1.2.3",
		CatalogSchema: 1,
		StateSchema:   1,
		Assets: []release.Asset{
			repairAsset("binary", "darwin", "amd64", "tpod-darwin-amd64", assets["tpod-darwin-amd64"]),
			repairAsset("binary", "darwin", "arm64", "tpod-darwin-arm64", assets["tpod-darwin-arm64"]),
			repairAsset("binary", "linux", "amd64", "tpod-linux-amd64", assets["tpod-linux-amd64"]),
			repairAsset("binary", "linux", "arm64", "tpod-linux-arm64", assets["tpod-linux-arm64"]),
			repairAsset("source", "", "", "terrapod-source.tar.gz", assets["terrapod-source.tar.gz"]),
			repairAsset("catalog", "", "", "resources.json", assets["resources.json"]),
		},
	}
	manifestData, err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	assetsMu.Lock()
	assets["release.json"] = manifestData
	assetsMu.Unlock()
	manifestDigest := sha256.Sum256(manifestData)

	home := filepath.Join(root, "home")
	dataHome := filepath.Join(root, "data")
	cacheHome := filepath.Join(root, "cache")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, "internal-repair-stage", "--manifest-digest", hex.EncodeToString(manifestDigest[:]), "--release-version", "1.2.3")
	command.Env = append(os.Environ(), "HOME="+home, "XDG_DATA_HOME="+dataHome, "XDG_CACHE_HOME="+cacheHome)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("native repair: %v\n%s", err, output)
	}
	current := filepath.Join(dataHome, "terrapod", "current")
	if target, err := os.Readlink(current); err != nil || target != filepath.Join("releases", "1.2.3") {
		t.Fatalf("current=%q err=%v", target, err)
	}
	for _, name := range []string{"tpod", "terrapod"} {
		target := filepath.Join(home, ".local", "bin", name)
		info, err := os.Lstat(target)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o755 {
			t.Fatalf("launcher %s info=%v err=%v", name, info, err)
		}
	}
	stagedBinary := filepath.Join(dataHome, "terrapod", "releases", "1.2.3", "bin", "tpod")
	if info, err := os.Stat(stagedBinary); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("staged binary info=%v err=%v", info, err)
	}
}

func newRepairTLSServer(t *testing.T, handler http.Handler) (*httptest.Server, []byte) {
	t.Helper()
	caPrivate, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	caTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Terrapod test release CA"},
		NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caPrivate.PublicKey, caPrivate)
	if err != nil {
		t.Fatal(err)
	}
	caCertificate, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	leafPrivate, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	leafTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "127.0.0.1"},
		NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, caCertificate, &leafPrivate.PublicKey, caPrivate)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.TLS = &tls.Config{Certificates: []tls.Certificate{{
		Certificate: [][]byte{leafDER, caDER},
		PrivateKey:  leafPrivate,
	}}}
	server.StartTLS()
	return server, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
}

func repairAsset(kind, goos, goarch, name string, data []byte) release.Asset {
	digest := sha256.Sum256(data)
	return release.Asset{Kind: kind, OS: goos, Arch: goarch, Name: name, Size: int64(len(data)), SHA256: hex.EncodeToString(digest[:])}
}

func repairSourceArchive(t *testing.T) []byte {
	t.Helper()
	var compressed bytes.Buffer
	gzipWriter := gzip.NewWriter(&compressed)
	tarWriter := tar.NewWriter(gzipWriter)
	launcher := []byte("#!/bin/sh\nexit 0\n")
	if err := tarWriter.WriteHeader(&tar.Header{Name: "scripts/tpod-launcher.sh", Mode: 0o755, Size: int64(len(launcher)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(launcher); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return compressed.Bytes()
}

type privilegeRunnerFunc func(context.Context, execx.Request) (execx.Result, error)

func (f privilegeRunnerFunc) Run(ctx context.Context, request execx.Request) (execx.Result, error) {
	return f(ctx, request)
}

func TestPrivilegePreflightIsNoninteractiveAndBounded(t *testing.T) {
	called := false
	err := noninteractivePrivilegeWithRunner(context.Background(), privilegeRunnerFunc(func(_ context.Context, request execx.Request) (execx.Result, error) {
		called = true
		if request.Path != "/usr/bin/sudo" || !reflect.DeepEqual(request.Args, []string{"-n", "true"}) || request.Stdin != nil || request.Privilege {
			t.Fatalf("request=%#v", request)
		}
		return execx.Result{}, nil
	}))
	if err != nil || !called {
		t.Fatalf("called=%v err=%v", called, err)
	}
}

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
	home := testutil.WorkspaceTempDir(t)
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

func TestProductionRegistryBuildsEveryEnabledResourceForAllConfigurations(t *testing.T) {
	catalog := productionTestCatalog(t)
	fixture := &resource.Fixture{Observations: make(map[model.ResourceID]model.Observation)}
	for _, item := range catalog.Resources {
		fixture.Observations[item.ID] = model.Observation{
			Present: true, Healthy: true, Provider: item.Provider, Package: item.Package,
			Paths: map[string]string{"actual": filepath.Join(t.TempDir(), string(item.ID))},
		}
	}
	management := productionTestManagement(t, filepath.Join(t.TempDir(), "brew"), true)
	configured, err := composeProductionPlanner(productionTestAdapters(management, fixture))
	if err != nil {
		t.Fatal(err)
	}
	for _, profile := range []model.Profile{model.ProfileMacOSTerminal, model.ProfileVPSShell} {
		for _, preset := range []string{"minimal", "development", "workstation"} {
			plan, err := configured.Build(context.Background(), planner.Input{
				Catalog: catalog, CatalogDigest: "fixture-digest", Profile: profile,
				Config: productionTestConfig(profile, preset), Snapshot: model.Snapshot{Ownership: map[model.ResourceID]model.Ownership{}},
			})
			if err != nil {
				t.Fatalf("Build(%s/%s): %v", profile, preset, err)
			}
			if len(plan.Unavailable) != 0 {
				t.Fatalf("Build(%s/%s) unavailable = %#v", profile, preset, plan.Unavailable)
			}
		}
	}
}

func TestProductionRegistryReportsOnlyDeliberatelyMissingHomebrew(t *testing.T) {
	catalog := productionTestCatalog(t)
	fixture := &resource.Fixture{Observations: make(map[model.ResourceID]model.Observation)}
	for _, item := range catalog.Resources {
		fixture.Observations[item.ID] = model.Observation{Present: true, Healthy: true, Provider: item.Provider, Package: item.Package, Paths: map[string]string{}}
	}
	management := productionTestManagement(t, filepath.Join(t.TempDir(), "missing", "brew"), false)
	configured, err := composeProductionPlanner(productionTestAdapters(management, fixture))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := configured.Build(context.Background(), planner.Input{
		Catalog: catalog, CatalogDigest: "fixture-digest", Profile: model.ProfileMacOSTerminal,
		Config: productionTestConfig(model.ProfileMacOSTerminal, "minimal"), Snapshot: model.Snapshot{Ownership: map[model.ResourceID]model.Ownership{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Unavailable) != 1 || !strings.Contains(plan.Unavailable["management.homebrew"], "bootstrap or repair") {
		t.Fatalf("unavailable = %#v", plan.Unavailable)
	}
	for _, reason := range plan.Unavailable {
		if strings.Contains(reason, "adapter unavailable") {
			t.Fatalf("production registry missing adapter: %s", reason)
		}
	}
}

func productionTestCatalog(t *testing.T) model.Catalog {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "catalog", "v1", "resources.json"))
	if err != nil {
		t.Fatal(err)
	}
	var value model.Catalog
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func productionTestManagement(t *testing.T, binary string, present bool) resource.Adapter {
	t.Helper()
	if present {
		if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	adapter, err := managementcore.NewHomebrew(binary, filepath.Dir(binary))
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}

func productionTestAdapters(management resource.Adapter, fixture resource.Adapter) cli.AdapterSet {
	return cli.AdapterSet{
		ManagementCore: management, HomebrewFormula: fixture, HomebrewCask: fixture,
		APT: fixture, Mise: fixture, ManagedFiles: fixture, GitCheckout: fixture,
		Jetendard: fixture, JSONFields: fixture, PlistFields: fixture, Karabiner: fixture,
	}
}

func productionTestConfig(profile model.Profile, preset string) model.Config {
	enabled := preset != "minimal"
	groups := preset == "workstation"
	return model.Config{Version: 1, Terrapod: map[string]any{
		"profile": string(profile), "enableEditorStack": enabled, "enableAiCliTools": enabled,
		"enableDevelopmentWorkspace": enabled, "enableMacosAppGroupTerminalApps": groups,
		"enableMacosAppGroupAutomation": groups, "enableMacosAppGroupLauncher": groups,
		"enableMacosAppGroupMonitoring": groups, "enableMacosAppGroupDevelopmentApps": groups,
	}}
}

func index(values []string, target string) int {
	for i, value := range values {
		if value == target {
			return i
		}
	}
	return -1
}
