package migrate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeGit struct {
	values map[string]string
	errs   map[string]error
}

func (f *fakeGit) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	key := strings.Join(args, " ")
	return []byte(f.values[key]), f.errs[key]
}

func cleanGit() *fakeGit {
	return &fakeGit{values: map[string]string{
		"remote get-url origin":                       canonicalTerrapodHTTPS,
		"status --porcelain=v1 --untracked-files=all": "",
		"rev-parse HEAD":                              "abc123",
		"rev-parse @{upstream}":                       "abc123",
	}, errs: map[string]error{}}
}

func TestValidateLegacySourceRequiresCanonicalCleanUpstreamCheckout(t *testing.T) {
	path := realTempDir(t)
	proof, err := ValidateLegacySource(context.Background(), path, cleanGit())
	if err != nil || proof.Head != "abc123" || proof.Path != path {
		t.Fatalf("proof=%#v err=%v", proof, err)
	}
	tests := []struct {
		name   string
		mutate func(*fakeGit)
		want   string
	}{
		{"wrong remote", func(g *fakeGit) { g.values["remote get-url origin"] = "https://github.com/fork/terrapod.git" }, "not canonical"},
		{"dirty", func(g *fakeGit) { g.values["status --porcelain=v1 --untracked-files=all"] = " M README.md" }, "dirty"},
		{"ahead", func(g *fakeGit) {
			g.values["rev-parse HEAD"] = "ahead"
			g.values["rev-list --left-right --count HEAD...@{upstream}"] = "1\t0"
		}, "differs"},
		{"diverged", func(g *fakeGit) {
			g.values["rev-parse HEAD"] = "local"
			g.values["rev-parse @{upstream}"] = "remote"
			g.values["rev-list --left-right --count HEAD...@{upstream}"] = "1\t2"
		}, "differs"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			git := cleanGit()
			test.mutate(git)
			if _, err := ValidateLegacySource(context.Background(), path, git); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v want=%q", err, test.want)
			}
		})
	}
}

func TestValidateLegacySourceRejectsSymlinkAndUnreadableGitState(t *testing.T) {
	root := realTempDir(t)
	link := filepath.Join(root, "source")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateLegacySource(context.Background(), link, cleanGit()); err == nil || !strings.Contains(err.Error(), "symlink component") {
		t.Fatalf("symlink error=%v", err)
	}
	git := cleanGit()
	git.errs["status --porcelain=v1 --untracked-files=all"] = errors.New("unreadable index")
	if _, err := ValidateLegacySource(context.Background(), root, git); err == nil || !strings.Contains(err.Error(), "unreadable") {
		t.Fatalf("git error=%v", err)
	}
}

func TestValidateLegacySourceRejectsBroadRemovalTargets(t *testing.T) {
	if _, err := ValidateLegacySource(context.Background(), string(filepath.Separator), cleanGit()); err == nil || !strings.Contains(err.Error(), "filesystem root") {
		t.Fatalf("root error=%v", err)
	}
	if home, err := os.UserHomeDir(); err == nil {
		if _, err := ValidateLegacySource(context.Background(), home, cleanGit()); err == nil || !strings.Contains(err.Error(), "home directory") {
			t.Fatalf("home error=%v", err)
		}
	}
}

func TestValidateLegacySourceRejectsSymlinkAncestor(t *testing.T) {
	root := realTempDir(t)
	actual := filepath.Join(root, "actual")
	source := filepath.Join(actual, "source")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(actual, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateLegacySource(context.Background(), filepath.Join(link, "source"), cleanGit()); err == nil || !strings.Contains(err.Error(), "symlink component") {
		t.Fatalf("ancestor symlink error=%v", err)
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("real source was mutated: %v", err)
	}
}

func TestRemoveLegacySourceVerifiesActiveAndExactCheckoutBeforeDeletion(t *testing.T) {
	root := realTempDir(t)
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "tracked"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	git := cleanGit()
	proof, err := ValidateLegacySource(context.Background(), source, git)
	if err != nil {
		t.Fatal(err)
	}
	verified := 0
	if err := RemoveLegacySource(context.Background(), proof, git, func(context.Context) error {
		verified++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if verified != 1 {
		t.Fatalf("active verifier calls=%d", verified)
	}
	if _, err := os.Lstat(source); !os.IsNotExist(err) {
		t.Fatalf("legacy source remains: %v", err)
	}
}

func TestRemoveLegacySourceLeavesCheckoutWhenActiveOrRevalidationFails(t *testing.T) {
	for _, test := range []struct {
		name   string
		active error
		dirty  bool
	}{
		{"active", errors.New("new apply not ready"), false},
		{"dirty", nil, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := realTempDir(t)
			git := cleanGit()
			proof, err := ValidateLegacySource(context.Background(), source, git)
			if err != nil {
				t.Fatal(err)
			}
			if test.dirty {
				git.values["status --porcelain=v1 --untracked-files=all"] = "?? unpublished"
			}
			if err := RemoveLegacySource(context.Background(), proof, git, func(context.Context) error { return test.active }); err == nil {
				t.Fatal("removal succeeded")
			}
			if _, err := os.Stat(source); err != nil {
				t.Fatalf("source was mutated: %v", err)
			}
		})
	}
}

func realTempDir(t *testing.T) string {
	t.Helper()
	path, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return path
}
