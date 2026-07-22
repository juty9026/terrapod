package resource

import (
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/provider"
)

type PlanFunc func(context.Context, model.Resource, model.Observation, model.Ownership) ([]model.Operation, error)

// ProviderAdapter is the executable composition boundary between a typed
// package provider and the frozen planning/execution resource interface.
type ProviderAdapter struct {
	provider provider.Provider
	plan     PlanFunc
}

func NewProviderAdapter(backend provider.Provider, plan PlanFunc) (*ProviderAdapter, error) {
	if nilInterface(backend) {
		return nil, errors.New("resource: provider is required")
	}
	if plan == nil {
		return nil, errors.New("resource: provider planning policy is required")
	}
	return &ProviderAdapter{provider: backend, plan: plan}, nil
}

func (a *ProviderAdapter) Inspect(ctx context.Context, item model.Resource) (model.Observation, error) {
	return a.provider.Inspect(ctx, item)
}
func (a *ProviderAdapter) Plan(ctx context.Context, item model.Resource, observed model.Observation, owned model.Ownership) ([]model.Operation, error) {
	operations, err := a.plan(ctx, item, observed, owned)
	if err != nil {
		return nil, err
	}
	for index := range operations {
		if operations[index].Provider == "" {
			operations[index].Provider = item.Provider
		}
		if operations[index].Package == "" {
			operations[index].Package = item.Package
		}
		if operations[index].Provider != item.Provider || operations[index].Package != item.Package {
			return nil, errors.New("resource: planned operation identity mismatch")
		}
	}
	return operations, nil
}
func (a *ProviderAdapter) Execute(ctx context.Context, operation model.Operation) model.OperationResult {
	err := a.provider.Execute(ctx, operation)
	result := model.OperationResult{OperationID: operation.ID, ResourceID: operation.ResourceID, Success: err == nil, FinishedAt: time.Now().UTC()}
	if err != nil {
		result.Detail = err.Error()
	}
	return result
}
func (a *ProviderAdapter) Verify(ctx context.Context, item model.Resource) (model.Observation, error) {
	return a.provider.Verify(ctx, item)
}
func (a *ProviderAdapter) Simulate(ctx context.Context, item model.Resource, operation model.Operation) (provider.ChangeSet, error) {
	if item.ID != operation.ResourceID || item.Provider != operation.Provider || item.Package != operation.Package {
		return provider.ChangeSet{}, errors.New("resource: simulation identity mismatch")
	}
	return a.provider.Simulate(ctx, operation)
}

func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return v.IsNil()
	}
	return false
}
