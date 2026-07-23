package release

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"debug/elf"
	"encoding/binary"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStagerStagesAndAtomicallyActivatesRelease(t *testing.T) {
	root := realReleaseTempDir(t)
	release := stagedFixture(t, root, Platform{OS: "linux", Arch: "amd64"}, sourceTar(t, map[string]string{"README.md": "hello"}))
	s := Stager{ReleaseDir: filepath.Join(root, "data", "releases"), ActiveRelease: filepath.Join(root, "data", "current")}
	got, err := s.Stage(context.Background(), release, Platform{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != "1.2.3" || got.Path != filepath.Join(s.ReleaseDir, "1.2.3") {
		t.Fatalf("staged=%+v", got)
	}
	if info, err := os.Stat(filepath.Join(got.Path, "bin", "tpod")); err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("binary mode=%v err=%v", info.Mode(), err)
	}
	if info, err := os.Stat(filepath.Join(got.Path, "source", "README.md")); err != nil || info.Mode().Perm()&0o222 != 0 {
		t.Fatalf("source mode=%v err=%v", info.Mode(), err)
	}
	if err := s.Activate("1.2.3"); err != nil {
		t.Fatal(err)
	}
	target, err := os.Readlink(s.ActiveRelease)
	if err != nil || target != filepath.Join("releases", "1.2.3") {
		t.Fatalf("target=%q err=%v", target, err)
	}
	if _, err := s.Stage(context.Background(), release, Platform{OS: "linux", Arch: "amd64"}); err != nil {
		t.Fatalf("idempotent stage: %v", err)
	}
	if err := s.Activate("9.9.9"); err == nil {
		t.Fatal("missing release activated")
	}
	after, _ := os.Readlink(s.ActiveRelease)
	if after != target {
		t.Fatalf("failed activation changed current to %q", after)
	}
}

func TestStagerRejectsUnsafeArchiveAndWrongBinary(t *testing.T) {
	tests := []struct {
		name    string
		archive []byte
		binary  []byte
		want    string
	}{
		{name: "traversal", archive: sourceTar(t, map[string]string{"../escape": "bad"}), binary: linuxBinary(elf.EM_X86_64), want: "unsafe"},
		{name: "symlink", archive: sourceTarWithType(t, "link", tar.TypeSymlink), binary: linuxBinary(elf.EM_X86_64), want: "unsupported"},
		{name: "duplicate", archive: sourceTarSequence(t, []tarEntry{{name: "same", body: "one"}, {name: "same", body: "two"}}), binary: linuxBinary(elf.EM_X86_64), want: "duplicate"},
		{name: "bomb", archive: sourceTarOversizedHeader(t), binary: linuxBinary(elf.EM_X86_64), want: "limit"},
		{name: "wrong format", archive: sourceTar(t, map[string]string{"ok": "ok"}), binary: []byte("script"), want: "format"},
		{name: "wrong arch", archive: sourceTar(t, map[string]string{"ok": "ok"}), binary: linuxBinary(elf.EM_AARCH64), want: "architecture"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := realReleaseTempDir(t)
			rel := stagedFixture(t, root, Platform{OS: "linux", Arch: "amd64"}, tc.archive)
			binaryAsset, _ := rel.Manifest.BinaryAsset("linux", "amd64")
			os.WriteFile(rel.Files[binaryAsset.Name], tc.binary, 0o600)
			bindFixtureFile(t, &rel, binaryAsset.Name)
			s := Stager{ReleaseDir: filepath.Join(root, "releases"), ActiveRelease: filepath.Join(root, "current")}
			if _, err := s.Stage(context.Background(), rel, Platform{OS: "linux", Arch: "amd64"}); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err=%v want=%q", err, tc.want)
			}
			if _, err := os.Stat(filepath.Join(s.ReleaseDir, "1.2.3")); !os.IsNotExist(err) {
				t.Fatalf("failed release visible: %v", err)
			}
		})
	}
}

func TestStagerAcceptsMachOAndRejectsExecutableOSMismatch(t *testing.T) {
	root := realReleaseTempDir(t)
	archive := sourceTar(t, map[string]string{"ok": "ok"})
	darwin := stagedFixture(t, root, Platform{OS: "darwin", Arch: "arm64"}, archive)
	s := Stager{ReleaseDir: filepath.Join(root, "releases"), ActiveRelease: filepath.Join(root, "current")}
	if _, err := s.Stage(context.Background(), darwin, Platform{OS: "darwin", Arch: "arm64"}); err != nil {
		t.Fatal(err)
	}
	otherRoot := realReleaseTempDir(t)
	linux := stagedFixture(t, otherRoot, Platform{OS: "linux", Arch: "amd64"}, archive)
	asset, _ := linux.Manifest.BinaryAsset("linux", "amd64")
	if err := os.WriteFile(linux.Files[asset.Name], machoBinary(0x01000007), 0o600); err != nil {
		t.Fatal(err)
	}
	bindFixtureFile(t, &linux, asset.Name)
	_, err := (Stager{ReleaseDir: filepath.Join(otherRoot, "releases"), ActiveRelease: filepath.Join(otherRoot, "current")}).Stage(context.Background(), linux, Platform{OS: "linux", Arch: "amd64"})
	if err == nil || !strings.Contains(err.Error(), "operating system") {
		t.Fatalf("err=%v", err)
	}
}

func TestStagerRejectsDigestDriftAndPreservesCurrent(t *testing.T) {
	root := realReleaseTempDir(t)
	rel := stagedFixture(t, root, Platform{OS: "linux", Arch: "amd64"}, sourceTar(t, map[string]string{"ok": "ok"}))
	catalog, _ := rel.Manifest.CatalogAsset()
	if err := os.WriteFile(rel.Files[catalog.Name], []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := Stager{ReleaseDir: filepath.Join(root, "data", "releases"), ActiveRelease: filepath.Join(root, "data", "current")}
	if err := os.MkdirAll(filepath.Join(s.ReleaseDir, "1.0.0"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("releases", "1.0.0"), s.ActiveRelease); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Stage(context.Background(), rel, Platform{OS: "linux", Arch: "amd64"}); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("err=%v", err)
	}
	target, _ := os.Readlink(s.ActiveRelease)
	if target != filepath.Join("releases", "1.0.0") {
		t.Fatalf("current changed: %q", target)
	}
}

func TestStagerRejectsManifestMutationAfterVerification(t *testing.T) {
	root := realReleaseTempDir(t)
	rel := stagedFixture(t, root, Platform{OS: "linux", Arch: "amd64"}, sourceTar(t, map[string]string{"ok": "ok"}))
	rel.Manifest.Version = "9.9.9"
	s := Stager{ReleaseDir: filepath.Join(root, "releases"), ActiveRelease: filepath.Join(root, "current")}
	if _, err := s.Stage(context.Background(), rel, Platform{OS: "linux", Arch: "amd64"}); err == nil || !strings.Contains(err.Error(), "modified") {
		t.Fatalf("err=%v", err)
	}
}

func TestStagerRejectsAncestorSymlinkAndMismatchedExistingRelease(t *testing.T) {
	root := realReleaseTempDir(t)
	rel := stagedFixture(t, root, Platform{OS: "linux", Arch: "amd64"}, sourceTar(t, map[string]string{"ok": "ok"}))
	real := filepath.Join(root, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, err := (Stager{ReleaseDir: filepath.Join(link, "releases"), ActiveRelease: filepath.Join(link, "current")}).Stage(context.Background(), rel, Platform{OS: "linux", Arch: "amd64"}); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("err=%v", err)
	}
	s := Stager{ReleaseDir: filepath.Join(root, "releases"), ActiveRelease: filepath.Join(root, "current")}
	if _, err := s.Stage(context.Background(), rel, Platform{OS: "linux", Arch: "amd64"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(s.ReleaseDir, "1.2.3", "catalog", "resources.json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Stage(context.Background(), rel, Platform{OS: "linux", Arch: "amd64"}); err == nil || !strings.Contains(err.Error(), "existing release") {
		t.Fatalf("err=%v", err)
	}
}

func stagedFixture(t *testing.T, root string, platform Platform, archive []byte) VerifiedRelease {
	t.Helper()
	dir := filepath.Join(root, "downloads")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	m := validManifest(t)
	m.verified = true
	files := map[string]string{}
	for i := range m.Assets {
		var body []byte
		switch m.Assets[i].Kind {
		case "binary":
			if m.Assets[i].OS == platform.OS && m.Assets[i].Arch == platform.Arch {
				if platform.OS == "linux" {
					if platform.Arch == "amd64" {
						body = linuxBinary(elf.EM_X86_64)
					} else {
						body = linuxBinary(elf.EM_AARCH64)
					}
				} else if platform.OS == "darwin" {
					cpu := uint32(0x01000007)
					if platform.Arch == "arm64" {
						cpu = 0x0100000c
					}
					body = machoBinary(cpu)
				}
			} else {
				body = []byte("unused")
			}
		case "source":
			body = archive
		case "catalog":
			body = []byte(`{"schema":1}`)
		}
		path := filepath.Join(dir, m.Assets[i].Name)
		if err := os.WriteFile(path, body, 0o600); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(body)
		m.Assets[i].Size = int64(len(body))
		m.Assets[i].SHA256 = hex.EncodeToString(sum[:])
		files[m.Assets[i].Name] = path
	}
	release := VerifiedRelease{Manifest: m, Files: files}
	if err := release.sealManifest(); err != nil {
		t.Fatal(err)
	}
	return release
}
func bindFixtureFile(t *testing.T, r *VerifiedRelease, name string) {
	t.Helper()
	for i := range r.Manifest.Assets {
		if r.Manifest.Assets[i].Name == name {
			b, err := os.ReadFile(r.Files[name])
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(b)
			r.Manifest.Assets[i].Size = int64(len(b))
			r.Manifest.Assets[i].SHA256 = hex.EncodeToString(sum[:])
			if err := r.sealManifest(); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
}
func linuxBinary(machine elf.Machine) []byte {
	b := make([]byte, 64)
	copy(b, []byte{0x7f, 'E', 'L', 'F'})
	b[4] = 2
	b[5] = 1
	b[6] = 1
	binary.LittleEndian.PutUint16(b[16:], 2)
	binary.LittleEndian.PutUint16(b[18:], uint16(machine))
	return b
}
func machoBinary(cpu uint32) []byte {
	body := make([]byte, 64)
	binary.LittleEndian.PutUint32(body[0:4], 0xfeedfacf)
	binary.LittleEndian.PutUint32(body[4:8], cpu)
	binary.LittleEndian.PutUint32(body[12:16], 2)
	return body
}

type tarEntry struct {
	name, body string
	kind       byte
}

func sourceTarSequence(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	tw := tar.NewWriter(gz)
	for _, entry := range entries {
		kind := entry.kind
		if kind == 0 {
			kind = tar.TypeReg
		}
		if err := tw.WriteHeader(&tar.Header{Name: entry.name, Mode: 0o644, Size: int64(len(entry.body)), Typeflag: kind}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(entry.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
func sourceTarOversizedHeader(t *testing.T) []byte {
	t.Helper()
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "huge", Mode: 0o644, Size: int64(64<<20) + 1, Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	_ = tw.Close()
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
func sourceTar(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
func sourceTarWithType(t *testing.T, name string, kind byte) []byte {
	t.Helper()
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o777, Typeflag: kind, Linkname: "target"}); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gz.Close()
	return out.Bytes()
}
