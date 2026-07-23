package migrate

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/juty9026/terrapod/internal/catalog"
	"github.com/juty9026/terrapod/internal/model"
	legacyprovider "github.com/juty9026/terrapod/internal/provider/legacy"
	"github.com/juty9026/terrapod/internal/resource"
)

type ownershipAdapter struct {
	operations map[model.ResourceID][]model.Operation
	historical map[model.ResourceID][]model.Operation
	observed   map[model.ResourceID]model.Observation
	inspectErr map[model.ResourceID]error
	planned    []model.ResourceID
	pruned     []model.ResourceID
}

func (a *ownershipAdapter) Inspect(_ context.Context, item model.Resource) (model.Observation, error) {
	if err := a.inspectErr[item.ID]; err != nil {
		return model.Observation{}, err
	}
	if observed, ok := a.observed[item.ID]; ok {
		return observed, nil
	}
	return model.Observation{Present: true, Healthy: true, Provider: item.Provider, Package: item.Package}, nil
}
func (a *ownershipAdapter) Plan(_ context.Context, item model.Resource, _ model.Observation, _ model.Ownership) ([]model.Operation, error) {
	a.planned = append(a.planned, item.ID)
	return append([]model.Operation(nil), a.operations[item.ID]...), nil
}
func (a *ownershipAdapter) PlanHistorical(_ context.Context, item model.Resource, _ model.Observation, _ model.Ownership) ([]model.Operation, error) {
	a.pruned = append(a.pruned, item.ID)
	return append([]model.Operation(nil), a.historical[item.ID]...), nil
}
func (*ownershipAdapter) Execute(_ context.Context, operation model.Operation) model.OperationResult {
	return model.OperationResult{OperationID: operation.ID, ResourceID: operation.ResourceID, Success: true}
}
func (a *ownershipAdapter) Verify(ctx context.Context, item model.Resource) (model.Observation, error) {
	return a.Inspect(ctx, item)
}

func TestLoadLegacyBaselineValidatesCompleteCatalog(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	path := filepath.Join(filepath.Dir(file), "..", "..", "catalog", "v1", "legacy-current.json")
	verified, markers, err := LoadLegacyBaseline(path)
	if err != nil {
		t.Fatal(err)
	}
	if verified.Catalog.Release != LegacyBaselineRelease || len(verified.Catalog.Resources) < 60 {
		t.Fatalf("incomplete baseline: release=%q resources=%d", verified.Catalog.Release, len(verified.Catalog.Resources))
	}
	if !containsString(markers, "homebrew-core") || !containsString(markers, "ai-cli-tools") {
		t.Fatalf("warning markers not declared by release-bound baseline: %v", markers)
	}

	var current model.Catalog
	currentBytes, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "catalog", "v1", "resources.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(currentBytes, &current); err != nil {
		t.Fatal(err)
	}
	currentIDs := resourceIDs(current.Resources)
	baselineIDs := resourceIDs(verified.Catalog.Resources)
	if strings.Join(currentIDs, "\n") != strings.Join(baselineIDs, "\n") {
		t.Fatal("legacy baseline does not completely declare the pre-manager catalog")
	}
	comparable := verified.Catalog
	comparable.Release = current.Release
	for index := range comparable.Resources {
		if comparable.Resources[index].ID == "management.homebrew" {
			delete(comparable.Resources[index].Metadata, legacyWarningMarkersMetadataKey)
		}
	}
	if !reflect.DeepEqual(comparable, current) {
		t.Fatal("legacy baseline differs from the complete pre-manager catalog")
	}

	tampered := filepath.Join(t.TempDir(), "legacy-current.json")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	contents[len(contents)-2] ^= 1
	if err := os.WriteFile(tampered, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadLegacyBaseline(tampered); err == nil {
		t.Fatalf("tampered baseline error=%v", err)
	}
}

func TestPlanLegacyOwnershipAdoptsTransfersPrunesAndDoesNotInfer(t *testing.T) {
	adopter := &ownershipAdapter{
		operations: map[model.ResourceID][]model.Operation{
			"core.git": {{ID: "adopt-core.git", ResourceID: "core.git", Kind: model.OperationAdopt, Provider: "homebrew-formula", Package: "git"}},
			"core.bat": {{ID: "install-core.bat", ResourceID: "core.bat", Kind: model.OperationInstall, Provider: "homebrew-formula", Package: "bat"}},
		},
		historical: map[model.ResourceID][]model.Operation{
			"optional.old-file": {{ID: "prune-optional.old-file", ResourceID: "optional.old-file", Kind: model.OperationPrune, Provider: "chezmoi", Package: "old-file", Removes: []string{"old-file"}}},
		},
	}
	registry := resource.NewRegistry()
	if err := registry.Register(model.ResourcePackage, "homebrew-formula", adopter); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(model.ResourceManagedFiles, "chezmoi", adopter); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(model.ResourceIntegration, "json-fields", adopter); err != nil {
		t.Fatal(err)
	}
	resources := []model.Resource{
		{ID: "core.git", Type: model.ResourcePackage, Provider: "homebrew-formula", Package: "git", VersionPolicy: model.VersionTracked},
		{ID: "core.bat", Type: model.ResourcePackage, Provider: "homebrew-formula", Package: "bat", VersionPolicy: model.VersionTracked, Metadata: map[string]string{"legacy.mise.package": "aqua:sharkdp/bat"}},
		{ID: "optional.old-file", Type: model.ResourceManagedFiles, Provider: "chezmoi", Package: "old-file", VersionPolicy: model.VersionTracked, Metadata: map[string]string{model.ManagedFilesScopeMetadataKey: ".old"}},
		{ID: "integration.old", Type: model.ResourceIntegration, Provider: "json-fields", Package: "old", VersionPolicy: model.VersionTracked},
		legacyManagementItem("homebrew-core"),
	}
	baseline := catalog.Verified{Catalog: model.Catalog{Release: LegacyBaselineRelease, Resources: resources}, Digest: "legacy-digest"}
	current := catalog.Verified{Catalog: model.Catalog{Release: "v1", Resources: resources[:2]}, Digest: "current-digest"}
	result, err := PlanLegacyOwnership(context.Background(), LegacyOwnershipInput{
		Baseline: baseline,
		Current:  current,
		Registry: registry,
		Desired:  map[model.ResourceID]bool{"core.git": true, "core.bat": true},
		Actual: map[model.ResourceID]LegacyArtifact{
			"core.git":          {Observation: model.Observation{Present: true, Healthy: true, Provider: "homebrew-formula", Package: "git"}},
			"core.bat":          {Observation: model.Observation{Present: true, Healthy: true}, LegacyPackages: []string{"aqua:sharkdp/bat"}},
			"optional.old-file": {Observation: model.Observation{Present: true, Healthy: true, Paths: map[string]string{"/home/me/.old": "file:" + strings.Repeat("a", 64)}}},
			"integration.old":   {Observation: model.Observation{Present: true, Healthy: true, Paths: map[string]string{"settings.json#/font": "sha256:" + strings.Repeat("b", 64)}}, PriorUnknown: true},
			"outside.baseline":  {Observation: model.Observation{Present: true, Healthy: true}},
		},
		WarningMarkers: map[string]string{"homebrew-core": "/state/homebrew-core", "not-declared": "/state/not-declared"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := operationKind(result.Plan.Operations, "core.git"); got != model.OperationAdopt {
		t.Fatalf("desired-provider package kind=%q", got)
	}
	transfer := operationFor(result.Plan.Operations, "core.bat")
	if transfer.Kind != model.OperationTransfer || strings.Join(transfer.Removes, ",") != "aqua:sharkdp/bat" {
		t.Fatalf("legacy transfer=%#v", transfer)
	}
	if operationKind(result.Plan.Operations, "optional.old-file") != model.OperationPrune {
		t.Fatalf("old optional file was not pruned: %#v", result.Plan.Operations)
	}
	if _, ok := result.Receipts["outside.baseline"]; ok {
		t.Fatal("artifact outside signed baseline was inferred")
	}
	if !result.Receipts["integration.old"].PriorUnknown {
		t.Fatal("unknown prior integration was not recorded")
	}
	if len(result.ArchiveMarkers) != 1 || result.ArchiveMarkers[0] != "/state/homebrew-core" {
		t.Fatalf("archive markers=%v", result.ArchiveMarkers)
	}
}

func TestPlanLegacyOwnershipRefusesModifiedManagedTarget(t *testing.T) {
	adapter := &ownershipAdapter{}
	registry := resource.NewRegistry()
	_ = registry.Register(model.ResourceManagedFiles, "chezmoi", adapter)
	item := model.Resource{ID: "optional.old-file", Type: model.ResourceManagedFiles, Provider: "chezmoi", Package: "old-file", VersionPolicy: model.VersionTracked}
	result, err := PlanLegacyOwnership(context.Background(), LegacyOwnershipInput{
		Baseline: catalog.Verified{Catalog: model.Catalog{Release: LegacyBaselineRelease, Resources: []model.Resource{item, legacyManagementItem("homebrew-core")}}, Digest: "legacy"},
		Current:  catalog.Verified{Catalog: model.Catalog{Release: "v1"}, Digest: "current"},
		Registry: registry,
		Actual:   map[model.ResourceID]LegacyArtifact{item.ID: {Observation: model.Observation{Present: true}, Modified: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Plan.Unavailable[item.ID], "modified") || len(result.Plan.Operations) != 0 || len(result.Receipts) != 0 {
		t.Fatalf("modified target was not isolated: %#v", result)
	}
}

func TestPlanLegacyOwnershipUnknownProvenanceIsUnavailableWithoutMutation(t *testing.T) {
	item := model.Resource{
		ID: "core.git", Type: model.ResourcePackage, Provider: "homebrew-formula",
		Package: "git", VersionPolicy: model.VersionTracked,
	}
	adapter := &ownershipAdapter{inspectErr: map[model.ResourceID]error{
		item.ID: &legacyprovider.ErrUnknownProvenance{
			ResourceID: item.ID,
			Command:    "git",
			Path:       "/usr/local/bin/git",
		},
	}}
	registry := resource.NewRegistry()
	if err := registry.Register(item.Type, item.Provider, adapter); err != nil {
		t.Fatal(err)
	}
	result, err := PlanLegacyOwnership(context.Background(), LegacyOwnershipInput{
		Baseline: catalog.Verified{
			Catalog: model.Catalog{Release: LegacyBaselineRelease, Resources: []model.Resource{item, legacyManagementItem("homebrew-core")}},
			Digest:  "legacy",
		},
		Current:  catalog.Verified{Catalog: model.Catalog{Release: "v1", Resources: []model.Resource{item}}, Digest: "current"},
		Registry: registry,
		Desired:  map[model.ResourceID]bool{item.ID: true},
		Actual: map[model.ResourceID]LegacyArtifact{
			item.ID: {Observation: model.Observation{Present: true}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Plan.Unavailable[item.ID], "unknown provenance") {
		t.Fatalf("Unavailable = %#v", result.Plan.Unavailable)
	}
	if len(result.Plan.Operations) != 0 || len(result.Receipts) != 0 || len(adapter.planned) != 0 || len(adapter.pruned) != 0 {
		t.Fatalf("unknown provenance mutated migration state: result=%#v adapter=%#v", result, adapter)
	}
}

func TestPlanLegacyOwnershipDerivesManagedConflictFromTypedInspection(t *testing.T) {
	item := model.Resource{
		ID: "dotfiles.home", Type: model.ResourceManagedFiles, Provider: "chezmoi",
		Package: "home", VersionPolicy: model.VersionTracked,
	}
	adapter := &ownershipAdapter{
		observed: map[model.ResourceID]model.Observation{
			item.ID: {Present: true, Healthy: false, Provider: item.Provider, Package: item.Package},
		},
		operations: map[model.ResourceID][]model.Operation{
			item.ID: {{ID: "adopt-dotfiles.home", ResourceID: item.ID, Kind: model.OperationAdopt, Provider: item.Provider, Package: item.Package}},
		},
	}
	registry := resource.NewRegistry()
	_ = registry.Register(item.Type, item.Provider, adapter)
	result, err := PlanLegacyOwnership(context.Background(), LegacyOwnershipInput{
		Baseline: catalog.Verified{
			Catalog: model.Catalog{Release: LegacyBaselineRelease, Resources: []model.Resource{item, legacyManagementItem("homebrew-core")}},
			Digest:  "legacy",
		},
		Current:  catalog.Verified{Catalog: model.Catalog{Release: "v1", Resources: []model.Resource{item}}, Digest: "current"},
		Registry: registry,
		Desired:  map[model.ResourceID]bool{item.ID: true},
		Actual: map[model.ResourceID]LegacyArtifact{
			item.ID: {Observation: model.Observation{Present: true}, Modified: false},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Plan.Unavailable[item.ID], "modified") || len(adapter.planned) != 0 {
		t.Fatalf("typed drift was not rejected: result=%#v planned=%v", result, adapter.planned)
	}
	if _, exists := result.Receipts[item.ID]; exists {
		t.Fatal("drifted target received ownership")
	}
}

func legacyManagementItem(markers string) model.Resource {
	return model.Resource{
		ID: "management.homebrew", Type: model.ResourceManagementCore, Provider: "terrapod",
		Package: "homebrew", VersionPolicy: model.VersionTracked,
		Metadata: map[string]string{legacyWarningMarkersMetadataKey: markers},
	}
}

func operationFor(operations []model.Operation, id model.ResourceID) model.Operation {
	for _, operation := range operations {
		if operation.ResourceID == id {
			return operation
		}
	}
	return model.Operation{}
}

func operationKind(operations []model.Operation, id model.ResourceID) model.OperationKind {
	return operationFor(operations, id).Kind
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
