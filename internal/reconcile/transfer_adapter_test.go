package reconcile

import (
	"context"
	"os"
	"testing"

	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/provider"
	"github.com/juty9026/terrapod/internal/provider/legacy"
	"github.com/juty9026/terrapod/internal/resource"
)

type transferProvider struct{ present bool }

func (*transferProvider) Name() string { return "fixture" }
func (p *transferProvider) Inspect(context.Context, model.Resource) (model.Observation, error) {
	return model.Observation{Present: p.present, Healthy: p.present, Provider: "fixture", Package: "alpha", Paths: map[string]string{}}, nil
}
func (*transferProvider) Simulate(context.Context, model.Operation) (provider.ChangeSet, error) {
	return provider.ChangeSet{Installs: []string{"alpha"}}, nil
}
func (p *transferProvider) Execute(context.Context, model.Operation) error {
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
