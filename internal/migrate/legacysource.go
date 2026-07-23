package migrate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const canonicalTerrapodHTTPS = "https://github.com/juty9026/terrapod.git"

type GitRunner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

type LegacySourceProof struct {
	Path   string
	Remote string
	Head   string
}

func ValidateLegacySource(ctx context.Context, sourcePath string, git GitRunner) (LegacySourceProof, error) {
	if git == nil {
		return LegacySourceProof{}, errors.New("legacy source git runner is required")
	}
	absolute, err := filepath.Abs(sourcePath)
	if err != nil {
		return LegacySourceProof{}, err
	}
	if filepath.Dir(absolute) == absolute {
		return LegacySourceProof{}, errors.New("legacy source cannot be a filesystem root")
	}
	if home, homeErr := os.UserHomeDir(); homeErr == nil && absolute == filepath.Clean(home) {
		return LegacySourceProof{}, errors.New("legacy source cannot be the user home directory")
	}
	if err := rejectSymlinkComponents(absolute); err != nil {
		return LegacySourceProof{}, err
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return LegacySourceProof{}, fmt.Errorf("inspect legacy source: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return LegacySourceProof{}, errors.New("legacy source must be a real directory")
	}
	if _, err := os.ReadDir(absolute); err != nil {
		return LegacySourceProof{}, fmt.Errorf("read legacy source: %w", err)
	}
	run := func(args ...string) (string, error) {
		output, err := git.Run(ctx, absolute, args...)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(output)), nil
	}
	remote, err := run("remote", "get-url", "origin")
	if err != nil {
		return LegacySourceProof{}, fmt.Errorf("inspect legacy source origin: %w", err)
	}
	if !canonicalLegacyRemote(remote) {
		return LegacySourceProof{}, fmt.Errorf("legacy source origin %q is not canonical Terrapod", remote)
	}
	status, err := run("status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return LegacySourceProof{}, fmt.Errorf("inspect legacy source worktree: %w", err)
	}
	if status != "" {
		return LegacySourceProof{}, errors.New("legacy source worktree is dirty")
	}
	head, err := run("rev-parse", "HEAD")
	if err != nil {
		return LegacySourceProof{}, fmt.Errorf("inspect legacy source HEAD: %w", err)
	}
	if head == "" {
		return LegacySourceProof{}, errors.New("inspect legacy source HEAD: empty result")
	}
	upstream, err := run("rev-parse", "@{upstream}")
	if err != nil {
		return LegacySourceProof{}, fmt.Errorf("inspect legacy source upstream: %w", err)
	}
	if upstream == "" {
		return LegacySourceProof{}, errors.New("inspect legacy source upstream: empty result")
	}
	if head != upstream {
		counts, countErr := run("rev-list", "--left-right", "--count", "HEAD...@{upstream}")
		if countErr != nil {
			return LegacySourceProof{}, fmt.Errorf("compare legacy source with upstream: %w", countErr)
		}
		return LegacySourceProof{}, fmt.Errorf("legacy source HEAD differs from upstream (%s)", counts)
	}
	return LegacySourceProof{Path: absolute, Remote: remote, Head: head}, nil
}

func rejectSymlinkComponents(absolute string) error {
	volume := filepath.VolumeName(absolute)
	root := volume + string(filepath.Separator)
	relative := strings.TrimPrefix(absolute, root)
	current := root
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "" {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil {
			return fmt.Errorf("inspect legacy source path component: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("legacy source path contains symlink component %q", current)
		}
	}
	return nil
}

func RemoveLegacySource(ctx context.Context, proof LegacySourceProof, git GitRunner, verifyActive func(context.Context) error) error {
	if verifyActive == nil {
		return errors.New("active source verifier is required")
	}
	if err := verifyActive(ctx); err != nil {
		return fmt.Errorf("verify active Terrapod source: %w", err)
	}
	current, err := ValidateLegacySource(ctx, proof.Path, git)
	if err != nil {
		return fmt.Errorf("revalidate legacy source before removal: %w", err)
	}
	if current != proof {
		return errors.New("legacy source changed after preflight")
	}
	if err := os.RemoveAll(proof.Path); err != nil {
		return fmt.Errorf("remove legacy source: %w", err)
	}
	return nil
}

func canonicalLegacyRemote(remote string) bool {
	switch strings.TrimSuffix(remote, "/") {
	case canonicalTerrapodHTTPS, "https://github.com/juty9026/terrapod",
		"git@github.com:juty9026/terrapod.git", "ssh://git@github.com/juty9026/terrapod.git":
		return true
	default:
		return false
	}
}
