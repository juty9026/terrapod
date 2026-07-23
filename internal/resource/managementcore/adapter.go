package managementcore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/juty9026/terrapod/internal/model"
)

// Homebrew adopts a clean installation at Terrapod's platform-owned path.
// Bootstrap and repair intentionally remain outside this adapter.
type Homebrew struct {
	binary string
	home   string
}

func NewHomebrew(binary, home string) (*Homebrew, error) {
	if binary == "" || !filepath.IsAbs(binary) {
		return nil, errors.New("management-core: absolute Homebrew path is required")
	}
	if home == "" || !filepath.IsAbs(home) {
		return nil, errors.New("management-core: absolute home path is required")
	}
	return &Homebrew{binary: filepath.Clean(binary), home: filepath.Clean(home)}, nil
}

func (a *Homebrew) Inspect(ctx context.Context, item model.Resource) (model.Observation, error) {
	if err := ctx.Err(); err != nil {
		return model.Observation{}, err
	}
	if err := validateHomebrewResource(item); err != nil {
		return model.Observation{}, err
	}
	info, err := os.Stat(a.binary)
	if err != nil {
		return model.Observation{}, unavailableHomebrew(err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return model.Observation{}, unavailableHomebrew(errors.New("brew binary is not a regular executable"))
	}
	probe := exec.CommandContext(ctx, a.binary, "--version")
	probe.Env = []string{"HOME=" + a.home, "LC_ALL=C", "PATH=/usr/bin:/bin"}
	if err := probe.Run(); err != nil {
		return model.Observation{}, unavailableHomebrew(fmt.Errorf("brew --version failed: %w", err))
	}
	return model.Observation{
		Present: true, Healthy: true, Provider: item.Provider, Package: item.Package,
		Paths: map[string]string{"brew": a.binary},
	}, nil
}

func (a *Homebrew) Plan(_ context.Context, item model.Resource, observed model.Observation, owned model.Ownership) ([]model.Operation, error) {
	if err := validateHomebrewResource(item); err != nil {
		return nil, err
	}
	if !observed.Present || !observed.Healthy || observed.Provider != item.Provider || observed.Package != item.Package || observed.Paths["brew"] != a.binary {
		return nil, unavailableHomebrew(errors.New("installation did not pass inspection"))
	}
	if owned.ResourceID == "" {
		return []model.Operation{{
			ID: "adopt-" + string(item.ID), ResourceID: item.ID, Kind: model.OperationAdopt,
			Provider: item.Provider, Package: item.Package,
			Detail: "adopt clean Homebrew installation into Terrapod ownership",
		}}, nil
	}
	if owned.ResourceID != item.ID || owned.Provider != item.Provider || owned.Package != item.Package {
		return nil, errors.New("management-core: existing ownership does not match Homebrew")
	}
	return nil, nil
}

func (a *Homebrew) Execute(_ context.Context, operation model.Operation) model.OperationResult {
	result := model.OperationResult{OperationID: operation.ID, ResourceID: operation.ResourceID}
	if operation.Kind != model.OperationAdopt || operation.Provider != "terrapod" || operation.Package != "homebrew" {
		result.Detail = "management-core: only Homebrew ownership adoption is supported"
		return result
	}
	result.Success = true
	return result
}

func (a *Homebrew) Verify(ctx context.Context, item model.Resource) (model.Observation, error) {
	return a.Inspect(ctx, item)
}

func validateHomebrewResource(item model.Resource) error {
	if item.Type != model.ResourceManagementCore || item.Provider != "terrapod" || item.Package != "homebrew" {
		return errors.New("management-core: unsupported resource")
	}
	return nil
}

func unavailableHomebrew(cause error) error {
	return fmt.Errorf("Homebrew is unavailable at Terrapod's managed path; bootstrap or repair Homebrew before retrying: %w", cause)
}
