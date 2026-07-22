package planner_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/planner"
	"github.com/juty9026/terrapod/internal/resource"
)

func TestBuildPlansDesiredResourcesDependencyFirst(t *testing.T) {
	registry, fixture := registryWithFixture(t)
	fixture.Observations = map[model.ResourceID]model.Observation{
		"management.homebrew": {Present: true, Provider: "brew", Package: "homebrew"},
		"core.ripgrep":        {},
		"workspace.editor":    {Present: true, Provider: "manual", Package: "editor"},
	}
	fixture.Operations = map[model.ResourceID][]model.Operation{
		"management.homebrew": {{ID: "adopt-homebrew", Kind: model.OperationAdopt}},
		"core.ripgrep":        {{ID: "install-ripgrep", Kind: model.OperationInstall}},
		"workspace.editor":    {{ID: "transfer-editor", Kind: model.OperationTransfer}},
	}
	input := baseInput(resources())
	input.Snapshot.Ownership["workspace.editor"] = model.Ownership{ResourceID: "workspace.editor", Provider: "old", Package: "editor"}

	plan, err := planner.New(registry).Build(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	assertOperationIDs(t, plan, "adopt-homebrew", "install-ripgrep", "transfer-editor")
	if len(fixture.ExecuteCalls) != 0 || len(fixture.VerifyCalls) != 0 {
		t.Fatalf("Build mutated state: execute=%v verify=%v", fixture.ExecuteCalls, fixture.VerifyCalls)
	}
}

func TestBuildIncludesUpgradeOnlyWhenRequested(t *testing.T) {
	registry, fixture := registryWithFixture(t)
	fixture.Observations = map[model.ResourceID]model.Observation{"core.ripgrep": {Present: true}}
	fixture.Operations = map[model.ResourceID][]model.Operation{
		"core.ripgrep": {{ID: "upgrade-ripgrep", Kind: model.OperationUpgrade}},
	}
	input := baseInput([]model.Resource{resourceDef("core.ripgrep", nil)})

	without, err := planner.New(registry).Build(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(without.Operations) != 0 {
		t.Fatalf("upgrade=false operations = %#v", without.Operations)
	}
	input.Upgrade = true
	with, err := planner.New(registry).Build(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	assertOperationIDs(t, with, "upgrade-ripgrep")
}

func TestBuildBlocksDependentsButContinuesIndependentBranches(t *testing.T) {
	registry, fixture := registryWithFixture(t)
	fixture.InspectErrors = map[model.ResourceID]error{"management.homebrew": errors.New("brew missing")}
	fixture.Operations = map[model.ResourceID][]model.Operation{
		"workspace.editor": {{ID: "install-editor", Kind: model.OperationInstall}},
	}
	input := baseInput([]model.Resource{
		resourceDef("management.homebrew", nil),
		resourceDef("core.ripgrep", []model.ResourceID{"management.homebrew"}),
		resourceDef("workspace.editor", nil),
	})

	plan, err := planner.New(registry).Build(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	assertOperationIDs(t, plan, "install-editor")
	if got := plan.Unavailable["management.homebrew"]; !strings.Contains(got, "brew missing") {
		t.Fatalf("root unavailable reason = %q", got)
	}
	if got := plan.Unavailable["core.ripgrep"]; !strings.Contains(got, "management.homebrew") {
		t.Fatalf("dependent unavailable reason = %q", got)
	}
}

func TestBuildPrunesRemovedOwnedResourcesDependentFirst(t *testing.T) {
	registry, fixture := registryWithFixture(t)
	fixture.Operations = map[model.ResourceID][]model.Operation{
		"management.homebrew": {{ID: "prune-homebrew", Kind: model.OperationPrune}},
		"core.ripgrep":        {{ID: "prune-ripgrep", Kind: model.OperationPrune}},
	}
	historical := catalog(resources()[:2])
	input := baseInput(nil)
	input.Historical["old"] = historical
	input.Snapshot.Ownership = ownershipFor("old", historical.Resources...)

	plan, err := planner.New(registry).Build(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	assertOperationIDs(t, plan, "prune-ripgrep", "prune-homebrew")
}

func TestBuildRejectsPruneWithoutMatchingVerifiedHistoricalCatalog(t *testing.T) {
	tests := map[string]func(*planner.Input){
		"missing digest": func(input *planner.Input) { delete(input.Historical, "old") },
		"resource ID mismatch": func(input *planner.Input) {
			input.Snapshot.Ownership["core.ripgrep"] = model.Ownership{ResourceID: "other.resource", CatalogDigest: "old", Provider: "brew", Package: "core.ripgrep", Paths: map[string]string{"bin": "/opt/bin/rg"}}
		},
		"provider mismatch":   func(input *planner.Input) { input.Historical["old"].Resources[0].Provider = "tampered" },
		"package mismatch":    func(input *planner.Input) { input.Historical["old"].Resources[0].Package = "tampered" },
		"path scope mismatch": func(input *planner.Input) { input.Historical["old"].Resources[0].Metadata["path.bin"] = "/tampered" },
		"empty path value with different key": func(input *planner.Input) {
			input.Historical["old"].Resources[0].Metadata["path.bin"] = ""
			input.Snapshot.Ownership["core.ripgrep"] = model.Ownership{ResourceID: "core.ripgrep", CatalogDigest: "old", Provider: "brew", Package: "core.ripgrep", Paths: map[string]string{"other": ""}}
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			registry, fixture := registryWithFixture(t)
			fixture.Operations = map[model.ResourceID][]model.Operation{"core.ripgrep": {{ID: "prune", Kind: model.OperationPrune}}}
			historicalResource := resourceDef("core.ripgrep", nil)
			historicalResource.Metadata = map[string]string{"path.bin": "/opt/bin/rg"}
			input := baseInput(nil)
			input.Historical["old"] = catalog([]model.Resource{historicalResource})
			input.Snapshot.Ownership[historicalResource.ID] = model.Ownership{ResourceID: historicalResource.ID, CatalogDigest: "old", Provider: historicalResource.Provider, Package: historicalResource.Package, Paths: map[string]string{"bin": "/opt/bin/rg"}}
			mutate(&input)

			plan, err := planner.New(registry).Build(context.Background(), input)
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.Operations) != 0 || plan.Unavailable[historicalResource.ID] != planner.UnbackedOwnershipReason {
				t.Fatalf("plan = %#v", plan)
			}
		})
	}
}

func TestBuildFiltersResourcesByProfile(t *testing.T) {
	registry, fixture := registryWithFixture(t)
	fixture.Operations = map[model.ResourceID][]model.Operation{"core.ripgrep": {{ID: "install", Kind: model.OperationInstall}}}
	r := resourceDef("core.ripgrep", nil)
	r.Profiles = []model.Profile{model.ProfileVPSShell}

	plan, err := planner.New(registry).Build(context.Background(), baseInput([]model.Resource{r}))
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 0 {
		t.Fatalf("profile-filtered resource was planned: %#v", plan.Operations)
	}
}

func TestBuildTreatsDisabledResourceAsRemovedAndUsesHistoricalPruneAuthority(t *testing.T) {
	registry, fixture := registryWithFixture(t)
	fixture.Operations = map[model.ResourceID][]model.Operation{"core.ripgrep": {{ID: "prune-disabled", Kind: model.OperationPrune}}}
	r := resourceDef("core.ripgrep", nil)
	r.Metadata = map[string]string{planner.EnabledByConfigMetadataKey: "tools.ripgrep"}
	input := baseInput([]model.Resource{r})
	input.Config.Terrapod = map[string]any{"tools.ripgrep": false}
	input.Historical["old"] = catalog([]model.Resource{r})
	input.Snapshot.Ownership = ownershipFor("old", r)

	plan, err := planner.New(registry).Build(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	assertOperationIDs(t, plan, "prune-disabled")
}

func TestConfigGateSupportsOnlyExactPositiveBoolean(t *testing.T) {
	values := []struct {
		name    string
		value   any
		present bool
		enabled bool
	}{{"missing", nil, false, false}, {"false", false, true, false}, {"string", "true", true, false}, {"true", true, true, true}}
	for _, tc := range values {
		t.Run(tc.name, func(t *testing.T) {
			registry, fixture := registryWithFixture(t)
			fixture.Operations = map[model.ResourceID][]model.Operation{"core.ripgrep": {{ID: "install", Kind: model.OperationInstall}}}
			r := resourceDef("core.ripgrep", nil)
			r.Metadata = map[string]string{planner.EnabledByConfigMetadataKey: "tools.ripgrep"}
			input := baseInput([]model.Resource{r})
			if tc.present {
				input.Config.Terrapod["tools.ripgrep"] = tc.value
			}
			plan, err := planner.New(registry).Build(context.Background(), input)
			if err != nil {
				t.Fatal(err)
			}
			if got := len(plan.Operations) == 1; got != tc.enabled {
				t.Fatalf("enabled = %v, want %v; plan=%#v", got, tc.enabled, plan)
			}
		})
	}
}

func TestResourceWithoutConfigGateIsEnabled(t *testing.T) {
	registry, fixture := registryWithFixture(t)
	fixture.Operations = map[model.ResourceID][]model.Operation{"core.ripgrep": {{ID: "install", Kind: model.OperationInstall}}}
	plan, err := planner.New(registry).Build(context.Background(), baseInput([]model.Resource{resourceDef("core.ripgrep", nil)}))
	if err != nil {
		t.Fatal(err)
	}
	assertOperationIDs(t, plan, "install")
}

func TestAnyConfigGateEnablesWhenAnyReferencedBooleanIsTrue(t *testing.T) {
	for _, tc := range []struct {
		name    string
		config  map[string]any
		enabled bool
	}{
		{name: "none", config: map[string]any{}, enabled: false},
		{name: "all false", config: map[string]any{"enableAiCliTools": false, "enableDevelopmentWorkspace": false}, enabled: false},
		{name: "first true", config: map[string]any{"enableAiCliTools": true, "enableDevelopmentWorkspace": false}, enabled: true},
		{name: "second true", config: map[string]any{"enableAiCliTools": false, "enableDevelopmentWorkspace": true}, enabled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registry, fixture := registryWithFixture(t)
			fixture.Operations = map[model.ResourceID][]model.Operation{"core.ripgrep": {{ID: "install", Kind: model.OperationInstall}}}
			r := resourceDef("core.ripgrep", nil)
			r.Metadata = map[string]string{
				planner.EnabledByAnyConfigMetadataPrefix + "enableAiCliTools":           "true",
				planner.EnabledByAnyConfigMetadataPrefix + "enableDevelopmentWorkspace": "true",
			}
			input := baseInput([]model.Resource{r})
			input.Config.Terrapod = tc.config

			plan, err := planner.New(registry).Build(context.Background(), input)
			if err != nil {
				t.Fatal(err)
			}
			if got := len(plan.Operations) == 1; got != tc.enabled {
				t.Fatalf("enabled = %v, want %v; plan=%#v", got, tc.enabled, plan)
			}
		})
	}
}

func TestBuildRejectsMixedConfigGateKindsDeterministically(t *testing.T) {
	registry, _ := registryWithFixture(t)
	r := resourceDef("core.ripgrep", nil)
	r.Metadata = map[string]string{
		planner.EnabledByConfigMetadataKey:                             "enableAiCliTools",
		planner.EnabledByAnyConfigMetadataPrefix + "enableEditorStack": "true",
	}

	_, err := planner.New(registry).Build(context.Background(), baseInput([]model.Resource{r}))
	if err == nil || err.Error() != `resource "core.ripgrep" mixes "enabledByConfig" and "enabledByAnyConfig.*" metadata` {
		t.Fatalf("error = %v", err)
	}
}

func TestAnyConfigGateRequiresExactTrueMetadataValue(t *testing.T) {
	registry, fixture := registryWithFixture(t)
	fixture.Operations = map[model.ResourceID][]model.Operation{"core.ripgrep": {{ID: "install", Kind: model.OperationInstall}}}
	r := resourceDef("core.ripgrep", nil)
	r.Metadata = map[string]string{planner.EnabledByAnyConfigMetadataPrefix + "enableAiCliTools": "false"}
	input := baseInput([]model.Resource{r})
	input.Config.Terrapod["enableAiCliTools"] = true

	plan, err := planner.New(registry).Build(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 0 {
		t.Fatalf("non-true metadata enabled resource: %#v", plan.Operations)
	}
}

func TestBuildRejectsDuplicateManagedFileOwnershipAndCrossCatalogScopeOverlap(t *testing.T) {
	managed := func(id model.ResourceID, scope string) model.Resource {
		return model.Resource{ID: id, Type: model.ResourceManagedFiles, Profiles: []model.Profile{model.ProfileMacOSTerminal}, VersionPolicy: model.VersionTracked, Provider: "chezmoi", Package: string(id), Metadata: map[string]string{model.ManagedFilesScopeMetadataKey: scope}}
	}
	t.Run("duplicate receipt path", func(t *testing.T) {
		one, two := managed("dotfiles.one", ".one"), managed("dotfiles.two", ".two")
		input := baseInput(nil)
		input.Historical = map[string]model.Catalog{"old": catalog([]model.Resource{one, two})}
		path := "/home/me/.config/shared"
		input.Snapshot.Ownership = map[model.ResourceID]model.Ownership{"dotfiles.one": {ResourceID: "dotfiles.one", CatalogDigest: "old", Provider: "chezmoi", Package: "dotfiles.one", Paths: map[string]string{path: "file:a"}}, "dotfiles.two": {ResourceID: "dotfiles.two", CatalogDigest: "old", Provider: "chezmoi", Package: "dotfiles.two", Paths: map[string]string{path: "file:b"}}}
		plan, err := planner.New(resource.NewRegistry()).Build(context.Background(), input)
		if err == nil || !strings.Contains(err.Error(), "duplicate managed-file ownership") {
			t.Fatalf("Build=%#v,%v", plan, err)
		}
	})
	t.Run("current historical overlap", func(t *testing.T) {
		current, old := managed("dotfiles.current", ".config"), managed("dotfiles.old", ".config/old")
		input := baseInput([]model.Resource{current})
		input.Historical = map[string]model.Catalog{"old": catalog([]model.Resource{old})}
		input.Snapshot.Ownership = map[model.ResourceID]model.Ownership{old.ID: {ResourceID: old.ID, CatalogDigest: "old", Provider: old.Provider, Package: old.Package, Paths: map[string]string{"/home/me/.config/old/a": "file:digest"}}}
		plan, err := planner.New(resource.NewRegistry()).Build(context.Background(), input)
		if err == nil || !strings.Contains(err.Error(), "overlapping managed-file authority") {
			t.Fatalf("Build=%#v,%v", plan, err)
		}
	})
}

func TestAnyConfigGateMalformedEntryDisablesEvenWhenAnotherGateIsTrue(t *testing.T) {
	for _, malformed := range map[string]string{
		"empty suffix": planner.EnabledByAnyConfigMetadataPrefix,
		"wrong value":  planner.EnabledByAnyConfigMetadataPrefix + "enableEditorStack",
	} {
		t.Run(malformed, func(t *testing.T) {
			registry, fixture := registryWithFixture(t)
			fixture.Operations = map[model.ResourceID][]model.Operation{"core.ripgrep": {{ID: "install", Kind: model.OperationInstall}}}
			r := resourceDef("core.ripgrep", nil)
			r.Metadata = map[string]string{
				planner.EnabledByAnyConfigMetadataPrefix + "enableAiCliTools": "true",
				malformed: "false",
			}
			input := baseInput([]model.Resource{r})
			input.Config.Terrapod["enableAiCliTools"] = true

			plan, err := planner.New(registry).Build(context.Background(), input)
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.Operations) != 0 {
				t.Fatalf("malformed OR metadata enabled resource: %#v", plan.Operations)
			}
		})
	}
}

func TestBuildRejectsDuplicateOperationIDs(t *testing.T) {
	registry, fixture := registryWithFixture(t)
	fixture.Operations = map[model.ResourceID][]model.Operation{
		"management.homebrew": {{ID: "duplicate", Kind: model.OperationInstall}},
		"core.ripgrep":        {{ID: "duplicate", Kind: model.OperationInstall}},
	}
	_, err := planner.New(registry).Build(context.Background(), baseInput(resources()[:2]))
	if err == nil || !strings.Contains(err.Error(), "duplicate operation ID") {
		t.Fatalf("error = %v", err)
	}
}

func TestBuildRejectsPruneFromDesiredAdapter(t *testing.T) {
	registry, fixture := registryWithFixture(t)
	fixture.Operations = map[model.ResourceID][]model.Operation{
		"core.ripgrep": {
			{ID: "install", Kind: model.OperationInstall},
			{ID: "prune", Kind: model.OperationPrune},
		},
	}

	_, err := planner.New(registry).Build(context.Background(), baseInput([]model.Resource{resourceDef("core.ripgrep", nil)}))
	if err == nil || err.Error() != `adapter planned prune operation "prune" for desired resource "core.ripgrep"` {
		t.Fatalf("error = %v", err)
	}
}

func TestBuildPropagatesContextErrors(t *testing.T) {
	t.Run("pre-canceled", func(t *testing.T) {
		registry, fixture := registryWithFixture(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := planner.New(registry).Build(ctx, baseInput([]model.Resource{resourceDef("core.ripgrep", nil)}))
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v", err)
		}
		if len(fixture.InspectCalls) != 0 {
			t.Fatalf("Inspect called with canceled context: %v", fixture.InspectCalls)
		}
	})

	t.Run("inspect canceled", func(t *testing.T) {
		registry, fixture := registryWithFixture(t)
		fixture.InspectErrors = map[model.ResourceID]error{"core.ripgrep": context.Canceled}

		_, err := planner.New(registry).Build(context.Background(), baseInput([]model.Resource{resourceDef("core.ripgrep", nil)}))
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v", err)
		}
	})

	for name, contextErr := range map[string]error{
		"plan canceled":          context.Canceled,
		"plan deadline exceeded": context.DeadlineExceeded,
	} {
		t.Run(name, func(t *testing.T) {
			registry, fixture := registryWithFixture(t)
			fixture.PlanErrors = map[model.ResourceID]error{"core.ripgrep": contextErr}

			_, err := planner.New(registry).Build(context.Background(), baseInput([]model.Resource{resourceDef("core.ripgrep", nil)}))
			if !errors.Is(err, contextErr) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestBuildIsDeterministicAcrossCatalogAndMapOrder(t *testing.T) {
	registry, fixture := registryWithFixture(t)
	fixture.Operations = map[model.ResourceID][]model.Operation{
		"management.homebrew": {{ID: "a", Kind: model.OperationInstall}},
		"core.ripgrep":        {{ID: "b", Kind: model.OperationInstall}},
		"workspace.editor":    {{ID: "c", Kind: model.OperationInstall}},
	}
	firstInput := baseInput(resources())
	secondResources := resources()
	secondResources[0], secondResources[2] = secondResources[2], secondResources[0]
	secondInput := baseInput(secondResources)

	first, err := planner.New(registry).Build(context.Background(), firstInput)
	if err != nil {
		t.Fatal(err)
	}
	second, err := planner.New(registry).Build(context.Background(), secondInput)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) || first.ID == "" {
		t.Fatalf("plans differ:\nfirst=%#v\nsecond=%#v", first, second)
	}
	firstJSON, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	secondJSON, err := json.Marshal(second)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(firstJSON, secondJSON) {
		t.Fatalf("plan JSON differs:\n%s\n%s", firstJSON, secondJSON)
	}
}

func TestRegistryUsesExactTypeProviderKeyAndRejectsDuplicates(t *testing.T) {
	registry := resource.NewRegistry()
	a := &resource.Fixture{}
	if err := registry.Register(model.ResourcePackage, "brew", a); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(model.ResourcePackage, "brew", a); err == nil {
		t.Fatal("duplicate registration succeeded")
	}
	if _, ok := registry.Lookup(model.ResourcePackage, "other"); ok {
		t.Fatal("provider-insensitive lookup succeeded")
	}
}

func TestRegistryRejectsTypedNilAdapter(t *testing.T) {
	registry := resource.NewRegistry()
	var adapter *resource.Fixture
	if err := registry.Register(model.ResourcePackage, "brew", adapter); err == nil || !strings.Contains(err.Error(), "nil adapter") {
		t.Fatalf("error = %v", err)
	}
	if _, ok := registry.Lookup(model.ResourcePackage, "brew"); ok {
		t.Fatal("typed-nil adapter was registered")
	}
}

func registryWithFixture(t *testing.T) (resource.Registry, *resource.Fixture) {
	t.Helper()
	registry := resource.NewRegistry()
	fixture := &resource.Fixture{}
	if err := registry.Register(model.ResourcePackage, "brew", fixture); err != nil {
		t.Fatal(err)
	}
	return registry, fixture
}

func baseInput(resources []model.Resource) planner.Input {
	return planner.Input{
		Catalog:       catalog(resources),
		CatalogDigest: "current",
		Historical:    map[string]model.Catalog{},
		Config:        model.Config{Version: 1, Terrapod: map[string]any{}},
		Profile:       model.ProfileMacOSTerminal,
		Snapshot:      model.Snapshot{Ownership: map[model.ResourceID]model.Ownership{}},
	}
}

func catalog(resources []model.Resource) model.Catalog {
	return model.Catalog{Version: 1, Release: "v1", Resources: resources}
}

func resources() []model.Resource {
	return []model.Resource{
		resourceDef("management.homebrew", nil),
		resourceDef("core.ripgrep", []model.ResourceID{"management.homebrew"}),
		resourceDef("workspace.editor", []model.ResourceID{"core.ripgrep"}),
	}
}

func resourceDef(id model.ResourceID, dependencies []model.ResourceID) model.Resource {
	return model.Resource{ID: id, Type: model.ResourcePackage, Profiles: []model.Profile{model.ProfileMacOSTerminal}, DependsOn: dependencies, VersionPolicy: model.VersionTracked, Provider: "brew", Package: string(id), Metadata: map[string]string{}}
}

func ownershipFor(digest string, resources ...model.Resource) map[model.ResourceID]model.Ownership {
	owned := make(map[model.ResourceID]model.Ownership, len(resources))
	for _, r := range resources {
		owned[r.ID] = model.Ownership{ResourceID: r.ID, CatalogDigest: digest, Provider: r.Provider, Package: r.Package, Paths: map[string]string{}}
	}
	return owned
}

func assertOperationIDs(t *testing.T, plan model.Plan, want ...string) {
	t.Helper()
	got := make([]string, len(plan.Operations))
	for i, operation := range plan.Operations {
		got[i] = operation.ID
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("operation IDs = %v, want %v", got, want)
	}
}
