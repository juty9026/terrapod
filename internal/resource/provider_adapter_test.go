package resource

import (
	"context"
	"errors"
	"testing"

	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/provider"
	"github.com/juty9026/terrapod/internal/provider/apt"
	"github.com/juty9026/terrapod/internal/provider/homebrew"
	"github.com/juty9026/terrapod/internal/provider/mise"
)

var _ provider.Provider = (*apt.Adapter)(nil)
var _ provider.Provider = (*homebrew.Adapter)(nil)
var _ provider.Provider = (*mise.Adapter)(nil)

type providerFixture struct{ inspected, simulated, executed, verified bool }

func (f *providerFixture) Name() string { return "fixture" }
func (f *providerFixture) Inspect(context.Context, model.Resource) (model.Observation, error) {
	f.inspected = true
	return model.Observation{Present: true}, nil
}
func (f *providerFixture) Simulate(context.Context, model.Operation) (provider.ChangeSet, error) {
	f.simulated = true
	return provider.ChangeSet{}, nil
}
func (f *providerFixture) Execute(context.Context, model.Operation) error {
	f.executed = true
	return nil
}
func (f *providerFixture) Verify(context.Context, model.Resource) (model.Observation, error) {
	f.verified = true
	return model.Observation{Present: true}, nil
}

func TestProviderAdapterConnectsProviderExecutionBoundary(t *testing.T) {
	backend := &providerFixture{}
	bridge, err := NewProviderAdapter(backend, func(context.Context, model.Resource, model.Observation, model.Ownership) ([]model.Operation, error) {
		return []model.Operation{{ID: "install"}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	item := model.Resource{ID: "core.alpha", Type: model.ResourcePackage, Provider: "fixture", Package: "alpha"}
	if _, err := bridge.Inspect(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	planned, err := bridge.Plan(context.Background(), item, model.Observation{}, model.Ownership{})
	if err != nil {
		t.Fatal(err)
	}
	if planned[0].Provider != "fixture" || planned[0].Package != "alpha" {
		t.Fatalf("planned identity=%#v", planned[0])
	}
	op := model.Operation{ID: "install", ResourceID: item.ID, Provider: item.Provider, Package: item.Package}
	if _, err := bridge.Simulate(context.Background(), item, op); err != nil {
		t.Fatal(err)
	}
	result := bridge.Execute(context.Background(), op)
	if !result.Success || result.OperationID != op.ID || result.ResourceID != item.ID {
		t.Fatalf("result=%#v", result)
	}
	if _, err := bridge.Verify(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	if !backend.inspected || !backend.simulated || !backend.executed || !backend.verified {
		t.Fatalf("backend=%#v", backend)
	}
}

func TestProviderAdapterReportsExecutionFailureAndRejectsNilInputs(t *testing.T) {
	var nilProvider *providerFixture
	if _, err := NewProviderAdapter(nilProvider, func(context.Context, model.Resource, model.Observation, model.Ownership) ([]model.Operation, error) {
		return nil, nil
	}); err == nil {
		t.Fatal("typed nil provider accepted")
	}
	backend := &failingProvider{providerFixture: providerFixture{}}
	bridge, err := NewProviderAdapter(backend, func(context.Context, model.Resource, model.Observation, model.Ownership) ([]model.Operation, error) {
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	result := bridge.Execute(context.Background(), model.Operation{ID: "x", ResourceID: "core.alpha"})
	if result.Success || result.Detail != "boom" {
		t.Fatalf("result=%#v", result)
	}
}

type failingProvider struct{ providerFixture }

func (*failingProvider) Execute(context.Context, model.Operation) error { return errors.New("boom") }
