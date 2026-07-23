package reconcile

import (
	"context"
	"os"
	"testing"

	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/provider"
	"github.com/juty9026/terrapod/internal/provider/legacy"
	"github.com/juty9026/terrapod/internal/resource"
	"github.com/juty9026/terrapod/internal/state"
	"github.com/juty9026/terrapod/internal/testutil"
)

type transferProvider struct {
	present             bool
	removes             []string
	simulated, executed []model.Operation
	name, pkg           string
}

type transferCoordinator struct {
	preflights int
	canceled   int
	active     int
	removed    int
	onDetect   func()
	removeErr  error
}

func (c *transferCoordinator) Detect(context.Context, model.Profile, model.Resource, model.Observation) (legacy.Inventory, error) {
	if c.onDetect != nil {
		c.onDetect()
	}
	return legacy.Inventory{}, nil
}
func (c *transferCoordinator) PreflightRemovals(context.Context, legacy.Inventory) (legacy.Preflight, provider.ChangeSet, error) {
	c.preflights++
	c.active++
	return legacy.Preflight{}, provider.ChangeSet{}, nil
}
func (c *transferCoordinator) RemovePreflight(context.Context, legacy.Preflight, legacy.Inventory) error {
	c.removed++
	if c.active > 0 {
		c.active--
	}
	return c.removeErr
}
func (c *transferCoordinator) CancelPreflight(legacy.Preflight) error {
	c.canceled++
	if c.active > 0 {
		c.active--
	}
	return nil
}

func (p *transferProvider) Name() string {
	if p.name != "" {
		return p.name
	}
	return "fixture"
}
func (p *transferProvider) Inspect(context.Context, model.Resource) (model.Observation, error) {
	pkg := p.pkg
	if pkg == "" {
		pkg = "alpha"
	}
	return model.Observation{Present: p.present, Healthy: p.present, Provider: p.Name(), Package: pkg, Paths: map[string]string{}}, nil
}

func TestEngineRunsRealTransferPreflightWithoutPrivilege(t *testing.T) {
	home := testutil.WorkspaceTempDir(t)
	backend := &transferProvider{name: "homebrew-cask", pkg: "claude-code"}
	desired, _ := resource.NewProviderAdapter(backend, func(context.Context, model.Resource, model.Observation, model.Ownership) ([]model.Operation, error) {
		return nil, nil
	})
	coordinator, err := legacy.New(transferPaths{}, legacy.WithVendor(home))
	if err != nil {
		t.Fatal(err)
	}
	defer coordinator.Close()
	adapter, err := NewProviderTransferAdapter(desired, coordinator, model.ProfileMacOSTerminal)
	if err != nil {
		t.Fatal(err)
	}
	registry := resource.NewRegistry()
	if err := registry.Register(model.ResourcePackage, "homebrew-cask", adapter); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	store, err := state.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	item := model.Resource{ID: "optional-ai.claude-code", Type: model.ResourcePackage, Provider: "homebrew-cask", Package: "claude-code", VersionPolicy: model.VersionTracked, Metadata: map[string]string{"legacy.vendor.receipt": "claude-native", "legacy.vendor.uninstall": "claude-native"}}
	operation := model.Operation{ID: "transfer", ResourceID: item.ID, Kind: model.OperationTransfer, Provider: item.Provider, Package: item.Package, Removes: []string{"claude-code"}}
	engine := Engine{Registry: registry, State: store, LockDir: dir, EffectiveUID: func() int { return 501 }}
	input := ApplyInput{Plan: model.Plan{ID: "p", Operations: []model.Operation{operation}}, CurrentResources: []model.Resource{item}, EnabledIDs: []model.ResourceID{item.ID}, HistoricalResources: map[model.ResourceID]HistoricalResource{}, CatalogDigest: "signed", Profile: model.ProfileMacOSTerminal}
	if _, err := engine.ApplyInput(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if len(backend.simulated) != 1 {
		t.Fatalf("simulate calls=%d", len(backend.simulated))
	}
}

func TestTransferDesiredPhaseUsesDesiredProviderPrivilege(t *testing.T) {
	backend := &transferProvider{}
	desired, _ := resource.NewProviderAdapter(backend, func(context.Context, model.Resource, model.Observation, model.Ownership) ([]model.Operation, error) {
		return nil, nil
	})
	coordinator, _ := legacy.New(transferPaths{})
	defer coordinator.Close()
	adapter, _ := NewProviderTransferAdapter(desired, coordinator, model.ProfileVPSShell)
	item := model.Resource{ID: "core.alpha", Type: model.ResourcePackage, Provider: "fixture", Package: "alpha"}
	op := model.Operation{ID: "transfer", ResourceID: item.ID, Kind: model.OperationTransfer, Provider: item.Provider, Package: item.Package, RequiresPrivilege: true}
	if _, err := adapter.Simulate(context.Background(), item, op); err != nil {
		t.Fatal(err)
	}
	if backend.simulated[0].RequiresPrivilege {
		t.Fatal("legacy aggregate privilege leaked into desired simulation")
	}
	if result := adapter.InstallDesired(context.Background(), item, op); !result.Success {
		t.Fatal(result.Detail)
	}
	if backend.executed[0].RequiresPrivilege {
		t.Fatal("legacy aggregate privilege leaked into desired install")
	}
}
func (p *transferProvider) Simulate(_ context.Context, operation model.Operation) (provider.ChangeSet, error) {
	p.simulated = append(p.simulated, operation)
	return provider.ChangeSet{Installs: []string{"alpha"}, Removes: append([]string(nil), p.removes...)}, nil
}

func TestTransferSimulationRejectsDesiredProviderRemoval(t *testing.T) {
	backend := &transferProvider{removes: []string{"legacy-looking"}}
	desired, _ := resource.NewProviderAdapter(backend, func(context.Context, model.Resource, model.Observation, model.Ownership) ([]model.Operation, error) {
		return nil, nil
	})
	coordinator, _ := legacy.New(transferPaths{})
	defer coordinator.Close()
	adapter, _ := NewProviderTransferAdapter(desired, coordinator, model.ProfileVPSShell)
	item := model.Resource{ID: "core.alpha", Type: model.ResourcePackage, Provider: "fixture", Package: "alpha"}
	op := model.Operation{ID: "transfer", ResourceID: item.ID, Kind: model.OperationTransfer, Provider: item.Provider, Package: item.Package, Removes: []string{"legacy-looking"}}
	if _, err := adapter.Simulate(context.Background(), item, op); err == nil {
		t.Fatal("desired removal accepted")
	}
}
func (p *transferProvider) Execute(_ context.Context, operation model.Operation) error {
	p.executed = append(p.executed, operation)
	p.present = true
	return nil
}
func (p *transferProvider) Verify(ctx context.Context, item model.Resource) (model.Observation, error) {
	return p.Inspect(ctx, item)
}

type transferPaths struct{}

func (transferPaths) ResolveCommand(string) (string, error)    { return "", os.ErrNotExist }
func (transferPaths) EvalSymlinks(path string) (string, error) { return path, nil }

func TestProviderTransferAdapterComposesRealOpaqueCoordinator(t *testing.T) {
	backend := &transferProvider{}
	desired, err := resource.NewProviderAdapter(backend, func(context.Context, model.Resource, model.Observation, model.Ownership) ([]model.Operation, error) {
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := legacy.New(transferPaths{})
	if err != nil {
		t.Fatal(err)
	}
	defer coordinator.Close()
	adapter, err := NewProviderTransferAdapter(desired, coordinator, model.ProfileVPSShell)
	if err != nil {
		t.Fatal(err)
	}
	item := model.Resource{ID: "core.alpha", Type: model.ResourcePackage, Provider: "fixture", Package: "alpha", VersionPolicy: model.VersionTracked}
	op := model.Operation{ID: "transfer", ResourceID: item.ID, Kind: model.OperationTransfer, Provider: item.Provider, Package: item.Package}
	if _, err := adapter.Simulate(context.Background(), item, op); err != nil {
		t.Fatal(err)
	}
	if result := adapter.InstallDesired(context.Background(), item, op); !result.Success {
		t.Fatalf("install=%#v", result)
	}
	if result := adapter.RemoveLegacy(context.Background(), item, op); !result.Success {
		t.Fatalf("remove=%#v", result)
	}
	if err := adapter.VerifyLegacyAbsent(context.Background(), item, op); err != nil {
		t.Fatal(err)
	}
}

func TestProviderTransferAdapterRevokesSupersededAndCanceledSimulation(t *testing.T) {
	backend := &transferProvider{}
	desired, err := resource.NewProviderAdapter(backend, func(context.Context, model.Resource, model.Observation, model.Ownership) ([]model.Operation, error) {
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	coordinator := &transferCoordinator{}
	adapter, err := NewProviderTransferAdapter(desired, coordinator, model.ProfileVPSShell)
	if err != nil {
		t.Fatal(err)
	}
	item := model.Resource{ID: "core.alpha", Type: model.ResourcePackage, Provider: "fixture", Package: "alpha", VersionPolicy: model.VersionTracked}
	op := model.Operation{ID: "transfer", ResourceID: item.ID, Kind: model.OperationTransfer, Provider: item.Provider, Package: item.Package}
	if _, err := adapter.Simulate(context.Background(), item, op); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Simulate(context.Background(), item, op); err != nil {
		t.Fatal(err)
	}
	if coordinator.preflights != 2 || coordinator.canceled != 1 {
		t.Fatalf("preflights=%d canceled=%d", coordinator.preflights, coordinator.canceled)
	}
	mismatched := op
	mismatched.Package = "different"
	if err := adapter.CancelSimulation(mismatched); err != nil {
		t.Fatal(err)
	}
	if coordinator.canceled != 1 {
		t.Fatal("mismatched operation canceled the current capability")
	}
	if err := adapter.CancelSimulation(op); err != nil {
		t.Fatal(err)
	}
	if err := adapter.CancelSimulation(op); err != nil {
		t.Fatal(err)
	}
	if coordinator.canceled != 2 {
		t.Fatalf("idempotent cancel count=%d", coordinator.canceled)
	}
	if coordinator.active != 0 {
		t.Fatalf("active capabilities=%d", coordinator.active)
	}
}

func TestProviderTransferAdapterKeepsCapabilityCancelableWhenDetectCancelsContext(t *testing.T) {
	backend := &transferProvider{present: true}
	desired, err := resource.NewProviderAdapter(backend, func(context.Context, model.Resource, model.Observation, model.Ownership) ([]model.Operation, error) {
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	coordinator := &transferCoordinator{onDetect: cancel}
	adapter, err := NewProviderTransferAdapter(desired, coordinator, model.ProfileVPSShell)
	if err != nil {
		t.Fatal(err)
	}
	item := model.Resource{ID: "core.alpha", Type: model.ResourcePackage, Provider: "fixture", Package: "alpha", VersionPolicy: model.VersionTracked}
	op := model.Operation{ID: "transfer", ResourceID: item.ID, Kind: model.OperationTransfer, Provider: item.Provider, Package: item.Package}
	coordinator.onDetect = nil
	if _, err := adapter.Simulate(context.Background(), item, op); err != nil {
		t.Fatal(err)
	}
	coordinator.onDetect = cancel
	if result := adapter.RemoveLegacy(ctx, item, op); result.Success || result.Detail != context.Canceled.Error() {
		t.Fatalf("result=%#v", result)
	}
	if coordinator.removed != 0 || coordinator.active != 1 {
		t.Fatalf("removed=%d active=%d", coordinator.removed, coordinator.active)
	}
	if err := adapter.CancelSimulation(op); err != nil {
		t.Fatal(err)
	}
	if coordinator.active != 0 {
		t.Fatalf("orphaned capabilities=%d", coordinator.active)
	}
}

func TestProviderTransferAdapterOperationMismatchDoesNotOrphanCapability(t *testing.T) {
	backend := &transferProvider{present: true}
	desired, err := resource.NewProviderAdapter(backend, func(context.Context, model.Resource, model.Observation, model.Ownership) ([]model.Operation, error) {
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	coordinator := &transferCoordinator{}
	adapter, err := NewProviderTransferAdapter(desired, coordinator, model.ProfileVPSShell)
	if err != nil {
		t.Fatal(err)
	}
	item := model.Resource{ID: "core.alpha", Type: model.ResourcePackage, Provider: "fixture", Package: "alpha", VersionPolicy: model.VersionTracked}
	op := model.Operation{ID: "transfer", ResourceID: item.ID, Kind: model.OperationTransfer, Provider: item.Provider, Package: item.Package}
	if _, err := adapter.Simulate(context.Background(), item, op); err != nil {
		t.Fatal(err)
	}
	mismatched := op
	mismatched.Package = "different"
	if result := adapter.RemoveLegacy(context.Background(), item, mismatched); result.Success {
		t.Fatalf("mismatched removal=%#v", result)
	}
	if coordinator.removed != 0 || coordinator.active != 1 {
		t.Fatalf("removed=%d active=%d", coordinator.removed, coordinator.active)
	}
	if err := adapter.CancelSimulation(op); err != nil {
		t.Fatal(err)
	}
	if coordinator.active != 0 {
		t.Fatalf("orphaned capabilities=%d", coordinator.active)
	}
	if result := adapter.RemoveLegacy(context.Background(), item, op); result.Success {
		t.Fatal("revoked capability replayed")
	}
}

func TestProviderTransferAdapterRevokesCapabilityWhenRemovalFails(t *testing.T) {
	backend := &transferProvider{present: true}
	desired, err := resource.NewProviderAdapter(backend, func(context.Context, model.Resource, model.Observation, model.Ownership) ([]model.Operation, error) {
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	coordinator := &transferCoordinator{removeErr: context.Canceled}
	adapter, err := NewProviderTransferAdapter(desired, coordinator, model.ProfileVPSShell)
	if err != nil {
		t.Fatal(err)
	}
	item := model.Resource{ID: "core.alpha", Type: model.ResourcePackage, Provider: "fixture", Package: "alpha", VersionPolicy: model.VersionTracked}
	op := model.Operation{ID: "transfer", ResourceID: item.ID, Kind: model.OperationTransfer, Provider: item.Provider, Package: item.Package}
	if _, err := adapter.Simulate(context.Background(), item, op); err != nil {
		t.Fatal(err)
	}
	if result := adapter.RemoveLegacy(context.Background(), item, op); result.Success || result.Detail != context.Canceled.Error() {
		t.Fatalf("result=%#v", result)
	}
	if coordinator.removed != 1 || coordinator.canceled != 1 || coordinator.active != 0 {
		t.Fatalf("removed=%d canceled=%d active=%d", coordinator.removed, coordinator.canceled, coordinator.active)
	}
}

func TestAuthorizedLegacySubsetAllowsResumeAndRejectsExtras(t *testing.T) {
	authorized := []string{"old-a", "old-b"}
	for _, test := range []struct {
		name     string
		observed []legacy.Observation
		wantErr  bool
	}{{"all", []legacy.Observation{{Package: "old-a"}, {Package: "old-b"}}, false}, {"partial", []legacy.Observation{{Package: "old-b"}}, false}, {"all removed", nil, false}, {"extra", []legacy.Observation{{Package: "unknown"}}, true}} {
		t.Run(test.name, func(t *testing.T) {
			err := authorizedLegacySubset(test.observed, authorized)
			if (err != nil) != test.wantErr {
				t.Fatalf("err=%v", err)
			}
		})
	}
}
