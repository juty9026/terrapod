package managementcore

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/juty9026/terrapod/internal/model"
)

func TestHomebrewPlansOwnershipWithoutMutatingInstall(t *testing.T) {
	home := t.TempDir()
	binary := filepath.Join(home, "brew")
	scriptContents := "#!/bin/sh\n[ \"$HOME\" = \"" + home + "\" ]\n"
	if err := os.WriteFile(binary, []byte(scriptContents), 0o700); err != nil {
		t.Fatal(err)
	}
	adapter, err := NewHomebrew(binary, home)
	if err != nil {
		t.Fatal(err)
	}
	item := homebrewResource()
	observed, err := adapter.Inspect(context.Background(), item)
	if err != nil || !observed.Present || !observed.Healthy || observed.Paths["brew"] != binary {
		t.Fatalf("Inspect = %#v, %v", observed, err)
	}
	operations, err := adapter.Plan(context.Background(), item, observed, model.Ownership{})
	if err != nil || len(operations) != 1 || operations[0].Kind != model.OperationAdopt {
		t.Fatalf("Plan(unowned) = %#v, %v", operations, err)
	}
	result := adapter.Execute(context.Background(), operations[0])
	if !result.Success {
		t.Fatalf("Execute(adopt) = %#v", result)
	}
	observedContents, err := os.ReadFile(binary)
	if err != nil || string(observedContents) != scriptContents {
		t.Fatalf("brew binary was mutated: %q, %v", observedContents, err)
	}
	owned := model.Ownership{ResourceID: item.ID, Provider: item.Provider, Package: item.Package}
	operations, err = adapter.Plan(context.Background(), item, observed, owned)
	if err != nil || len(operations) != 0 {
		t.Fatalf("Plan(owned) = %#v, %v", operations, err)
	}
}

func TestHomebrewRejectsMissingOrBrokenInstallWithRecoveryGuidance(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "missing", "brew")
	broken := filepath.Join(root, "brew")
	failing := filepath.Join(root, "failing-brew")
	if err := os.WriteFile(broken, []byte("not executable"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(failing, []byte("#!/bin/sh\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{missing, broken, failing} {
		adapter, err := NewHomebrew(path, root)
		if err != nil {
			t.Fatal(err)
		}
		_, err = adapter.Inspect(context.Background(), homebrewResource())
		if err == nil || !strings.Contains(err.Error(), "Homebrew") || !strings.Contains(err.Error(), "bootstrap or repair") {
			t.Fatalf("Inspect(%q) error = %v", path, err)
		}
	}
}

func homebrewResource() model.Resource {
	return model.Resource{ID: "management.homebrew", Type: model.ResourceManagementCore, Provider: "terrapod", Package: "homebrew", Commands: []string{"brew"}}
}
