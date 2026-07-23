package release

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"debug/elf"
	"debug/macho"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateExecutableAcceptsGoDarwinBinaries(t *testing.T) {
	for _, arch := range []string{"arm64", "amd64"} {
		t.Run(arch, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "tpod")
			command := exec.Command("go", "build", "-o", output, "../../cmd/tpod")
			command.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=darwin", "GOARCH="+arch)
			if result, err := command.CombinedOutput(); err != nil {
				t.Fatalf("go build: %v\n%s", err, result)
			}
			if err := validateExecutable(output, Platform{OS: "darwin", Arch: arch}); err != nil {
				logMachOSegments(t, output)
				t.Fatal(err)
			}
		})
	}
}

func logMachOSegments(t *testing.T, path string) {
	t.Helper()
	info, statErr := os.Stat(path)
	file, openErr := macho.Open(path)
	if statErr != nil || openErr != nil {
		t.Logf("Mach-O diagnostics: stat=%v open=%v", statErr, openErr)
		return
	}
	defer file.Close()
	t.Logf("Mach-O diagnostics: size=%d type=%v cpu=%v loads=%d", info.Size(), file.Type, file.Cpu, len(file.Loads))
	for index, load := range file.Loads {
		if segment, ok := load.(*macho.Segment); ok {
			t.Logf("segment[%d] name=%q offset=%d filesz=%d memsz=%d maxprot=%d prot=%d", index, segment.Name, segment.Offset, segment.Filesz, segment.Memsz, segment.Maxprot, segment.Prot)
			continue
		}
		raw := load.Raw()
		if len(raw) >= 4 && file.ByteOrder.Uint32(raw[:4]) == 0x80000028 {
			t.Logf("load[%d] LC_MAIN bytes=%d entryoff=%d", index, len(raw), file.ByteOrder.Uint64(raw[8:16]))
		}
	}
}

func TestStagerStagesAndAtomicallyActivatesRelease(t *testing.T) {
	root := realReleaseTempDir(t)
	release := stagedFixture(t, root, Platform{OS: "linux", Arch: "amd64"}, sourceTar(t, map[string]string{"README.md": "hello", "scripts/tpod-launcher.sh": "#!/bin/sh\n"}))
	s := testStager(Stager{ReleaseDir: filepath.Join(root, "data", "releases"), ActiveRelease: filepath.Join(root, "data", "current")})
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
	loaded, verified, err := s.LoadActive("1.2.3")
	if err != nil || loaded.Path != got.Path || verified.Manifest.Version != "1.2.3" {
		t.Fatalf("LoadActive=%#v %#v %v", loaded, verified.Manifest, err)
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

func TestRepairAndActivateReplacesPartialSameVersionAndRestoresItOnFailure(t *testing.T) {
	root := realReleaseTempDir(t)
	stager := testStager(Stager{ReleaseDir: filepath.Join(root, "releases"), ActiveRelease: filepath.Join(root, "current")})
	release := stagedFixture(t, root, Platform{OS: "linux", Arch: "amd64"}, sourceTar(t, map[string]string{"README.md": "hello", "scripts/tpod-launcher.sh": "#!/bin/sh\n"}))
	partial := filepath.Join(stager.ReleaseDir, "1.2.3")
	if err := os.MkdirAll(filepath.Join(partial, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(partial, "bin", "tpod"), []byte("broken"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("releases", "1.2.3"), stager.ActiveRelease); err != nil {
		t.Fatal(err)
	}

	binary, _ := release.Manifest.BinaryAsset("linux", "amd64")
	original, err := os.ReadFile(release.Files[binary.Name])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(release.Files[binary.Name], []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := stager.RepairAndActivate(context.Background(), release, Platform{OS: "linux", Arch: "amd64"}); err == nil {
		t.Fatal("repair accepted a corrupt signed asset")
	}
	if got, err := os.ReadFile(filepath.Join(partial, "bin", "tpod")); err != nil || string(got) != "broken" {
		t.Fatalf("partial release was not restored: body=%q err=%v", got, err)
	}

	if err := os.WriteFile(release.Files[binary.Name], original, 0o600); err != nil {
		t.Fatal(err)
	}
	staged, err := stager.RepairAndActivate(context.Background(), release, Platform{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := stager.LoadActive(staged.Version); err != nil {
		t.Fatal(err)
	}
}

func TestRepairAndActivateDoesNotActivateReleaseWithoutLauncher(t *testing.T) {
	root := realReleaseTempDir(t)
	stager := testStager(Stager{ReleaseDir: filepath.Join(root, "releases"), ActiveRelease: filepath.Join(root, "current")})
	release := stagedFixture(t, root, Platform{OS: "linux", Arch: "amd64"}, sourceTar(t, map[string]string{"README.md": "hello"}))
	if err := os.MkdirAll(filepath.Join(stager.ReleaseDir, "0.9.0"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("releases", "0.9.0"), stager.ActiveRelease); err != nil {
		t.Fatal(err)
	}
	if _, err := stager.RepairAndActivate(context.Background(), release, Platform{OS: "linux", Arch: "amd64"}); err == nil {
		t.Fatal("release without stable launcher activated")
	}
	if target, err := os.Readlink(stager.ActiveRelease); err != nil || target != filepath.Join("releases", "0.9.0") {
		t.Fatalf("current=%q err=%v", target, err)
	}
}

func TestLoadActiveRejectsDifferentCurrentTarget(t *testing.T) {
	root := realReleaseTempDir(t)
	rel := stagedFixture(t, root, Platform{OS: "linux", Arch: "amd64"}, sourceTar(t, map[string]string{"README.md": "hello"}))
	s := testStager(Stager{ReleaseDir: filepath.Join(root, "data", "releases"), ActiveRelease: filepath.Join(root, "data", "current")})
	if _, err := s.Stage(context.Background(), rel, Platform{OS: "linux", Arch: "amd64"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("releases", "other"), s.ActiveRelease); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.LoadActive("1.2.3"); err == nil {
		t.Fatal("different active target accepted")
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
		{name: "malformed ELF", archive: sourceTar(t, map[string]string{"ok": "ok"}), binary: malformedELF(), want: "format"},
		{name: "wrong arch", archive: sourceTar(t, map[string]string{"ok": "ok"}), binary: linuxBinary(elf.EM_AARCH64), want: "architecture"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := realReleaseTempDir(t)
			rel := stagedFixture(t, root, Platform{OS: "linux", Arch: "amd64"}, tc.archive)
			binaryAsset, _ := rel.Manifest.BinaryAsset("linux", "amd64")
			os.WriteFile(rel.Files[binaryAsset.Name], tc.binary, 0o600)
			bindFixtureFile(t, &rel, binaryAsset.Name)
			s := testStager(Stager{ReleaseDir: filepath.Join(root, "releases"), ActiveRelease: filepath.Join(root, "current")})
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
	s := testStager(Stager{ReleaseDir: filepath.Join(root, "releases"), ActiveRelease: filepath.Join(root, "current"), ExpectedPlatform: Platform{OS: "darwin", Arch: "arm64"}})
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
	_, err := testStager(Stager{ReleaseDir: filepath.Join(otherRoot, "releases"), ActiveRelease: filepath.Join(otherRoot, "current")}).Stage(context.Background(), linux, Platform{OS: "linux", Arch: "amd64"})
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
	s := testStager(Stager{ReleaseDir: filepath.Join(root, "data", "releases"), ActiveRelease: filepath.Join(root, "data", "current")})
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
	s := testStager(Stager{ReleaseDir: filepath.Join(root, "releases"), ActiveRelease: filepath.Join(root, "current")})
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
	if _, err := testStager(Stager{ReleaseDir: filepath.Join(link, "releases"), ActiveRelease: filepath.Join(link, "current")}).Stage(context.Background(), rel, Platform{OS: "linux", Arch: "amd64"}); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("err=%v", err)
	}
	s := testStager(Stager{ReleaseDir: filepath.Join(root, "releases"), ActiveRelease: filepath.Join(root, "current")})
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

func TestStagerRejectsEveryInstalledReleaseTamperWithoutChangingCurrent(t *testing.T) {
	tests := []struct {
		name   string
		tamper func(*testing.T, string)
	}{
		{"binary", func(t *testing.T, root string) { flipInstalledByte(t, filepath.Join(root, "bin", "tpod"), 0o755) }},
		{"source archive", func(t *testing.T, root string) {
			flipInstalledByte(t, filepath.Join(root, ".artifacts", "source.tar.gz"), 0o444)
		}},
		{"catalog", func(t *testing.T, root string) {
			flipInstalledByte(t, filepath.Join(root, "catalog", "resources.json"), 0o444)
		}},
		{"stage record", func(t *testing.T, root string) { flipInstalledByte(t, filepath.Join(root, ".stage.json"), 0o444) }},
		{"signed manifest", func(t *testing.T, root string) { flipInstalledByte(t, filepath.Join(root, "release.json"), 0o444) }},
		{"signature", func(t *testing.T, root string) { flipInstalledByte(t, filepath.Join(root, "release.json.sig"), 0o444) }},
		{"extracted source", func(t *testing.T, root string) { flipInstalledByte(t, filepath.Join(root, "source", "ok"), 0o444) }},
		{"extra file", func(t *testing.T, root string) {
			if err := os.Chmod(root, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "extra"), []byte("extra"), 0o444); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(root, 0o555); err != nil {
				t.Fatal(err)
			}
		}},
		{"mode", func(t *testing.T, root string) {
			if err := os.Chmod(filepath.Join(root, "catalog", "resources.json"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base := realReleaseTempDir(t)
			release := stagedFixture(t, base, Platform{OS: "linux", Arch: "amd64"}, sourceTar(t, map[string]string{"ok": "ok"}))
			s := testStager(Stager{ReleaseDir: filepath.Join(base, "data", "releases"), ActiveRelease: filepath.Join(base, "data", "current")})
			staged, err := s.Stage(context.Background(), release, Platform{OS: "linux", Arch: "amd64"})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join("releases", "1.0.0"), s.ActiveRelease); err != nil {
				t.Fatal(err)
			}
			tc.tamper(t, staged.Path)
			if err := s.Activate("1.2.3"); err == nil {
				t.Fatal("tampered release activated")
			}
			target, err := os.Readlink(s.ActiveRelease)
			if err != nil || target != filepath.Join("releases", "1.0.0") {
				t.Fatalf("current=%q err=%v", target, err)
			}
			if _, err := s.Stage(context.Background(), release, Platform{OS: "linux", Arch: "amd64"}); err == nil {
				t.Fatal("tampered same-version release accepted")
			}
		})
	}
}

func TestStagerExtractsCopiedArchiveAndPrevalidatesCompletedTree(t *testing.T) {
	for _, tc := range []struct {
		name        string
		corruptTree bool
		wantFailure bool
	}{{"cache replacement is isolated", false, false}, {"tree mismatch blocks commit", true, true}} {
		t.Run(tc.name, func(t *testing.T) {
			root := realReleaseTempDir(t)
			release := stagedFixture(t, root, Platform{OS: "linux", Arch: "amd64"}, sourceTar(t, map[string]string{"ok": "A"}))
			source, _ := release.Manifest.SourceAsset()
			s := testStager(Stager{ReleaseDir: filepath.Join(root, "releases"), ActiveRelease: filepath.Join(root, "current")})
			s.afterSourceCopy = func() {
				if err := os.WriteFile(release.Files[source.Name], sourceTar(t, map[string]string{"ok": "B"}), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.corruptTree {
				s.beforeCommitValidation = func(stage string) {
					name := filepath.Join(stage, "source", "ok")
					if err := os.Chmod(name, 0o600); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(name, []byte("B"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
			}
			staged, err := s.Stage(context.Background(), release, Platform{OS: "linux", Arch: "amd64"})
			if tc.wantFailure {
				if err == nil {
					t.Fatal("mismatched completed tree committed")
				}
				if _, statErr := os.Lstat(filepath.Join(s.ReleaseDir, "1.2.3")); !os.IsNotExist(statErr) {
					t.Fatalf("release visible: %v", statErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			body, err := os.ReadFile(filepath.Join(staged.Path, "source", "ok"))
			if err != nil || string(body) != "A" {
				t.Fatalf("source=%q err=%v", body, err)
			}
		})
	}
}

func TestActivateRejectsSignedCrossPlatformSubstitution(t *testing.T) {
	root := realReleaseTempDir(t)
	release := stagedFixture(t, root, Platform{OS: "linux", Arch: "amd64"}, sourceTar(t, map[string]string{"ok": "ok"}))
	s := testStager(Stager{ReleaseDir: filepath.Join(root, "data", "releases"), ActiveRelease: filepath.Join(root, "data", "current")})
	staged, err := s.Stage(context.Background(), release, Platform{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	darwin, _ := release.Manifest.BinaryAsset("darwin", "arm64")
	body, err := os.ReadFile(release.Files[darwin.Name])
	if err != nil {
		t.Fatal(err)
	}
	binaryPath := filepath.Join(staged.Path, "bin", "tpod")
	if err := os.Chmod(binaryPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binaryPath, body, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(binaryPath, 0o755); err != nil {
		t.Fatal(err)
	}
	rewriteStageRecord(t, staged.Path, func(record *stagedRecord) { record.OS = "darwin"; record.Arch = "arm64"; record.Binary = darwin })
	if err := os.Symlink(filepath.Join("releases", "1.0.0"), s.ActiveRelease); err != nil {
		t.Fatal(err)
	}
	if err := s.Activate("1.2.3"); err == nil {
		t.Fatal("cross-platform release activated")
	}
	target, _ := os.Readlink(s.ActiveRelease)
	if target != filepath.Join("releases", "1.0.0") {
		t.Fatalf("current changed: %q", target)
	}
}

func TestStagerRejectsInvalidExecutableSegments(t *testing.T) {
	linuxNonexec := linuxBinary(elf.EM_X86_64)
	binary.LittleEndian.PutUint32(linuxNonexec[68:], uint32(elf.PF_R))
	linuxOverflow := linuxBinary(elf.EM_X86_64)
	binary.LittleEndian.PutUint64(linuxOverflow[72:], ^uint64(0)-1)
	linuxZeroEntry := linuxBinary(elf.EM_X86_64)
	binary.LittleEndian.PutUint64(linuxZeroEntry[24:], 0)
	darwinNonexec := machoBinary(0x0100000c)
	binary.LittleEndian.PutUint32(darwinNonexec[88:], 1)
	binary.LittleEndian.PutUint32(darwinNonexec[92:], 1)
	darwinOverflow := machoBinary(0x0100000c)
	binary.LittleEndian.PutUint64(darwinOverflow[72:], ^uint64(0)-1)
	darwinUnixThread := machoBinary(0x0100000c)
	binary.LittleEndian.PutUint32(darwinUnixThread[104:], 5)
	tests := []struct {
		name     string
		platform Platform
		body     []byte
	}{{"ELF nonexec", Platform{"linux", "amd64"}, linuxNonexec}, {"ELF overflow", Platform{"linux", "amd64"}, linuxOverflow}, {"ELF truncated", Platform{"linux", "amd64"}, linuxBinary(elf.EM_X86_64)[:100]}, {"ELF zero entry", Platform{"linux", "amd64"}, linuxZeroEntry}, {"Mach-O nonexec", Platform{"darwin", "arm64"}, darwinNonexec}, {"Mach-O overflow", Platform{"darwin", "arm64"}, darwinOverflow}, {"Mach-O truncated", Platform{"darwin", "arm64"}, machoBinary(0x0100000c)[:110]}, {"Mach-O UNIXTHREAD only", Platform{"darwin", "arm64"}, darwinUnixThread}, {"Mach-O duplicate LC_MAIN", Platform{"darwin", "arm64"}, duplicateMachMain(0x0100000c)}, {"Mach-O entry segment lacks max execute", Platform{"darwin", "arm64"}, machEntryInNonMaxExecutableSegment(0x0100000c)}}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := realReleaseTempDir(t)
			release := stagedFixture(t, root, tc.platform, sourceTar(t, map[string]string{"ok": "ok"}))
			asset, _ := release.Manifest.BinaryAsset(tc.platform.OS, tc.platform.Arch)
			if err := os.WriteFile(release.Files[asset.Name], tc.body, 0o600); err != nil {
				t.Fatal(err)
			}
			bindFixtureFile(t, &release, asset.Name)
			s := testStager(Stager{ReleaseDir: filepath.Join(root, "releases"), ActiveRelease: filepath.Join(root, "current"), ExpectedPlatform: tc.platform})
			if _, err := s.Stage(context.Background(), release, tc.platform); err == nil {
				t.Fatal("invalid executable accepted")
			}
		})
	}
}

func flipInstalledByte(t *testing.T, name string, mode os.FileMode) {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("empty fixture")
	}
	data[len(data)/2] ^= 0xff
	if err := os.Chmod(name, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(name, mode); err != nil {
		t.Fatal(err)
	}
}
func rewriteStageRecord(t *testing.T, root string, edit func(*stagedRecord)) {
	t.Helper()
	name := filepath.Join(root, ".stage.json")
	record, err := readRecord(name)
	if err != nil {
		t.Fatal(err)
	}
	edit(&record)
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(name, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(name, 0o444); err != nil {
		t.Fatal(err)
	}
}

func stagedFixture(t *testing.T, root string, platform Platform, archive []byte) VerifiedRelease {
	t.Helper()
	dir := filepath.Join(root, "downloads")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	m := validManifest(t)
	files := map[string]string{}
	for i := range m.Assets {
		var body []byte
		switch m.Assets[i].Kind {
		case "binary":
			if m.Assets[i].OS == "linux" {
				if m.Assets[i].Arch == "amd64" {
					body = linuxBinary(elf.EM_X86_64)
				} else {
					body = linuxBinary(elf.EM_AARCH64)
				}
			} else if m.Assets[i].OS == "darwin" {
				cpu := uint32(0x01000007)
				if m.Assets[i].Arch == "arm64" {
					cpu = 0x0100000c
				}
				body = machoBinary(cpu)
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
	resignFixture(t, &release)
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
			resignFixture(t, r)
			return
		}
	}
}
func testStager(stager Stager) Stager {
	private := ed25519.NewKeyFromSeed(testSeed)
	stager.Verifier = testVerifier(private.Public().(ed25519.PublicKey))
	if stager.ExpectedPlatform.OS == "" {
		stager.ExpectedPlatform = Platform{OS: "linux", Arch: "amd64"}
	}
	return stager
}
func resignFixture(t *testing.T, release *VerifiedRelease) {
	t.Helper()
	release.Manifest.verified = false
	private := ed25519.NewKeyFromSeed(testSeed)
	data := encodeManifest(t, release.Manifest)
	signature := signManifest(t, "root", private, data)
	verified, err := testVerifier(private.Public().(ed25519.PublicKey)).VerifyManifest(data, signature)
	if err != nil {
		t.Fatal(err)
	}
	release.Manifest = verified
	release.manifestData = data
	release.signatureData = signature
	if err := release.sealManifest(); err != nil {
		t.Fatal(err)
	}
}
func linuxBinary(machine elf.Machine) []byte {
	b := make([]byte, 120)
	copy(b, []byte{0x7f, 'E', 'L', 'F'})
	b[4] = 2
	b[5] = 1
	b[6] = 1
	binary.LittleEndian.PutUint16(b[16:], 2)
	binary.LittleEndian.PutUint16(b[18:], uint16(machine))
	binary.LittleEndian.PutUint32(b[20:], 1)
	binary.LittleEndian.PutUint64(b[24:], 16)
	binary.LittleEndian.PutUint64(b[32:], 64)
	binary.LittleEndian.PutUint16(b[52:], 64)
	binary.LittleEndian.PutUint16(b[54:], 56)
	binary.LittleEndian.PutUint16(b[56:], 1)
	binary.LittleEndian.PutUint16(b[58:], 64)
	binary.LittleEndian.PutUint32(b[64:], uint32(elf.PT_LOAD))
	binary.LittleEndian.PutUint32(b[68:], uint32(elf.PF_R|elf.PF_X))
	binary.LittleEndian.PutUint64(b[96:], uint64(len(b)))
	binary.LittleEndian.PutUint64(b[104:], uint64(len(b)))
	binary.LittleEndian.PutUint64(b[112:], 0x1000)
	return b
}
func malformedELF() []byte {
	body := linuxBinary(elf.EM_X86_64)
	body = body[:64]
	binary.LittleEndian.PutUint64(body[40:], 64)
	binary.LittleEndian.PutUint16(body[58:], 64)
	binary.LittleEndian.PutUint16(body[60:], 1)
	return body
}
func machoBinary(cpu uint32) []byte {
	body := make([]byte, 128)
	binary.LittleEndian.PutUint32(body[0:4], 0xfeedfacf)
	binary.LittleEndian.PutUint32(body[4:8], cpu)
	binary.LittleEndian.PutUint32(body[12:16], 2)
	binary.LittleEndian.PutUint32(body[16:20], 2)
	binary.LittleEndian.PutUint32(body[20:24], 96)
	binary.LittleEndian.PutUint32(body[32:36], 0x19)
	binary.LittleEndian.PutUint32(body[36:40], 72)
	copy(body[40:56], []byte("__TEXT"))
	binary.LittleEndian.PutUint64(body[64:72], uint64(len(body)))
	binary.LittleEndian.PutUint64(body[80:88], uint64(len(body)))
	binary.LittleEndian.PutUint32(body[88:92], 5)
	binary.LittleEndian.PutUint32(body[92:96], 5)
	binary.LittleEndian.PutUint32(body[104:108], 0x80000028)
	binary.LittleEndian.PutUint32(body[108:112], 24)
	return body
}
func duplicateMachMain(cpu uint32) []byte {
	body := append(machoBinary(cpu), make([]byte, 24)...)
	binary.LittleEndian.PutUint32(body[16:20], 3)
	binary.LittleEndian.PutUint32(body[20:24], 120)
	binary.LittleEndian.PutUint64(body[64:72], uint64(len(body)))
	binary.LittleEndian.PutUint64(body[80:88], uint64(len(body)))
	binary.LittleEndian.PutUint32(body[128:132], 0x80000028)
	binary.LittleEndian.PutUint32(body[132:136], 24)
	return body
}
func machEntryInNonMaxExecutableSegment(cpu uint32) []byte {
	body := make([]byte, 256)
	binary.LittleEndian.PutUint32(body[0:4], 0xfeedfacf)
	binary.LittleEndian.PutUint32(body[4:8], cpu)
	binary.LittleEndian.PutUint32(body[12:16], 2)
	binary.LittleEndian.PutUint32(body[16:20], 3)
	binary.LittleEndian.PutUint32(body[20:24], 168)
	writeSegment := func(offset int, name string, fileOffset, fileSize uint64, maxprot, prot uint32) {
		binary.LittleEndian.PutUint32(body[offset:offset+4], 0x19)
		binary.LittleEndian.PutUint32(body[offset+4:offset+8], 72)
		copy(body[offset+8:offset+24], []byte(name))
		binary.LittleEndian.PutUint64(body[offset+32:offset+40], fileSize)
		binary.LittleEndian.PutUint64(body[offset+40:offset+48], fileOffset)
		binary.LittleEndian.PutUint64(body[offset+48:offset+56], fileSize)
		binary.LittleEndian.PutUint32(body[offset+56:offset+60], maxprot)
		binary.LittleEndian.PutUint32(body[offset+60:offset+64], prot)
	}
	writeSegment(32, "__TEXT", 0, 100, 5, 5)
	writeSegment(104, "__BAD", 200, 56, 1, 5)
	binary.LittleEndian.PutUint32(body[176:180], 0x80000028)
	binary.LittleEndian.PutUint32(body[180:184], 24)
	binary.LittleEndian.PutUint64(body[184:192], 220)
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
