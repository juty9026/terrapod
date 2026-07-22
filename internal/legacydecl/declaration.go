// Package legacydecl owns the closed grammar for known historical package sources.
package legacydecl

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/juty9026/terrapod/internal/model"
)

type Kind string

const (
	APT      Kind = "apt"
	Mise     Kind = "mise"
	Homebrew Kind = "homebrew"
	Vendor   Kind = "vendor"
)

type Declaration struct {
	Kind          Kind
	Package       string
	ReceiptKind   string
	UninstallKind string
	Profile       model.Profile
}

var (
	dpkgPackage  = regexp.MustCompile(`^[a-z0-9][a-z0-9+.~-]*(?::[a-z0-9][a-z0-9-]*)?$`)
	brewToken    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9@+_.-]*(?:/[A-Za-z0-9][A-Za-z0-9@+_.-]*){0,2}$`)
	misePackages = map[model.ResourceID]string{
		"core.bat": "aqua:sharkdp/bat", "core.btop": "aqua:aristocratos/btop",
		"core.dust": "aqua:bootandy/dust", "core.duf": "aqua:muesli/duf",
		"core.fastfetch": "aqua:fastfetch-cli/fastfetch", "core.fd": "aqua:sharkdp/fd",
		"core.fzf": "aqua:junegunn/fzf", "core.gh": "aqua:cli/cli",
		"core.git-delta": "aqua:dandavison/delta", "core.lazygit": "aqua:jesseduffield/lazygit",
		"core.lsd": "aqua:lsd-rs/lsd", "core.neovim": "aqua:neovim/neovim",
		"core.ripgrep": "aqua:BurntSushi/ripgrep", "core.starship": "aqua:starship/starship",
		"core.zellij": "aqua:zellij-org/zellij", "core.zoxide": "aqua:ajeetdsouza/zoxide",
	}
	miseCommands = map[string][]string{
		"aqua:sharkdp/bat": {"bat"}, "aqua:aristocratos/btop": {"btop"},
		"aqua:bootandy/dust": {"dust"}, "aqua:muesli/duf": {"duf"},
		"aqua:fastfetch-cli/fastfetch": {"fastfetch"}, "aqua:sharkdp/fd": {"fd"},
		"aqua:junegunn/fzf": {"fzf"}, "aqua:cli/cli": {"gh"},
		"aqua:dandavison/delta": {"delta"}, "aqua:jesseduffield/lazygit": {"lazygit"},
		"aqua:lsd-rs/lsd": {"lsd"}, "aqua:neovim/neovim": {"nvim"},
		"aqua:BurntSushi/ripgrep": {"rg"}, "aqua:starship/starship": {"starship"},
		"aqua:zellij-org/zellij": {"zellij"}, "aqua:ajeetdsouza/zoxide": {"zoxide"},
	}
	aptPackages = map[model.ResourceID]string{"core.gum": "gum", "core.mise": "mise"}
	vendors     = map[model.ResourceID]struct{ Package, Kind string }{
		"optional-ai.antigravity-cli": {"antigravity-cli", "antigravity-native"},
		"optional-ai.claude-code":     {"claude-code", "claude-native"},
		"optional-ai.codex":           {"codex", "codex-standalone"},
	}
)

func Parse(resource model.Resource) ([]Declaration, error) {
	known := map[string]struct{}{
		"legacy.apt.package": {}, "legacy.mise.package": {}, "legacy.mise.profile": {},
		"legacy.homebrew.package": {}, "legacy.vendor.receipt": {}, "legacy.vendor.uninstall": {},
	}
	for key := range resource.Metadata {
		if strings.HasPrefix(key, "legacy.") {
			if _, ok := known[key]; !ok {
				return nil, fmt.Errorf("resource %q has unknown legacy metadata %q", resource.ID, key)
			}
		}
	}
	if resource.Type != model.ResourcePackage {
		for key := range resource.Metadata {
			if strings.HasPrefix(key, "legacy.") {
				return nil, fmt.Errorf("resource %q legacy metadata requires package type", resource.ID)
			}
		}
		return nil, nil
	}
	var declarations []Declaration
	if pkg, ok := resource.Metadata["legacy.apt.package"]; ok {
		if resource.Provider != "homebrew-formula" || !dpkgPackage.MatchString(pkg) || aptPackages[resource.ID] != pkg {
			return nil, fmt.Errorf("resource %q has unsupported legacy APT transition %q", resource.ID, pkg)
		}
		declarations = append(declarations, Declaration{Kind: APT, Package: pkg})
	}
	if pkg, ok := resource.Metadata["legacy.mise.package"]; ok {
		if resource.Provider != "homebrew-formula" || misePackages[resource.ID] != pkg {
			return nil, fmt.Errorf("resource %q has unsupported legacy mise transition %q", resource.ID, pkg)
		}
		declaration := Declaration{Kind: Mise, Package: pkg}
		if scope, scoped := resource.Metadata["legacy.mise.profile"]; scoped {
			if resource.ID != "core.btop" || scope != string(model.ProfileVPSShell) {
				return nil, fmt.Errorf("resource %q has unsupported legacy mise profile %q", resource.ID, scope)
			}
			declaration.Profile = model.ProfileVPSShell
		} else if resource.ID == "core.btop" {
			return nil, fmt.Errorf("resource %q requires legacy mise profile %q", resource.ID, model.ProfileVPSShell)
		}
		declarations = append(declarations, declaration)
	} else if _, scoped := resource.Metadata["legacy.mise.profile"]; scoped {
		return nil, fmt.Errorf("resource %q has legacy mise profile without package", resource.ID)
	}
	if pkg, ok := resource.Metadata["legacy.homebrew.package"]; ok {
		if (resource.Provider != "homebrew-formula" && resource.Provider != "homebrew-cask") || !brewToken.MatchString(pkg) || pkg != resource.Package {
			return nil, fmt.Errorf("resource %q has unsupported legacy Homebrew transition %q", resource.ID, pkg)
		}
		declarations = append(declarations, Declaration{Kind: Homebrew, Package: pkg})
	}
	receipt, hasReceipt := resource.Metadata["legacy.vendor.receipt"]
	uninstall, hasUninstall := resource.Metadata["legacy.vendor.uninstall"]
	if hasReceipt != hasUninstall {
		return nil, fmt.Errorf("resource %q must pair legacy vendor receipt and uninstall kinds", resource.ID)
	}
	if hasReceipt {
		knownVendor, ok := vendors[resource.ID]
		if !ok || resource.Provider != "homebrew-cask" || resource.Package != knownVendor.Package || receipt != knownVendor.Kind || uninstall != knownVendor.Kind {
			return nil, fmt.Errorf("resource %q has unsupported legacy vendor transition %q/%q", resource.ID, receipt, uninstall)
		}
		declarations = append(declarations, Declaration{Kind: Vendor, Package: resource.Package, ReceiptKind: receipt, UninstallKind: uninstall})
	}
	return declarations, nil
}

func Commands(packageID string) ([]string, bool) {
	commands, ok := miseCommands[packageID]
	return append([]string(nil), commands...), ok
}
