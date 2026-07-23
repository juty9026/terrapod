package resource

import (
	"context"
	"fmt"
	"reflect"

	"github.com/juty9026/terrapod/internal/model"
)

// Adapter is the frozen resource boundary shared by planning and execution.
type Adapter interface {
	Inspect(context.Context, model.Resource) (model.Observation, error)
	Plan(context.Context, model.Resource, model.Observation, model.Ownership) ([]model.Operation, error)
	Execute(context.Context, model.Operation) model.OperationResult
	Verify(context.Context, model.Resource) (model.Observation, error)
}

// BoundExecutor receives the exact declared resource selected by reconciliation.
// It is used by adapters whose mutation authority includes resource metadata.
type BoundExecutor interface {
	ExecuteResource(context.Context, model.Resource, model.Operation) model.OperationResult
}

// HistoricalPlanner plans removal using a release-bound historical resource and its
// ownership receipt. Adapters implement it when desired and historical state
// cannot be distinguished from the Resource value alone.
type HistoricalPlanner interface {
	PlanHistorical(context.Context, model.Resource, model.Observation, model.Ownership) ([]model.Operation, error)
}

type registryKey struct {
	resourceType model.ResourceType
	provider     string
}

// Registry resolves adapters by the exact resource type and provider pair.
type Registry struct {
	adapters map[registryKey]Adapter
}

func NewRegistry() Registry {
	return Registry{adapters: make(map[registryKey]Adapter)}
}

func (r *Registry) Register(resourceType model.ResourceType, provider string, adapter Adapter) error {
	if isNilAdapter(adapter) {
		return fmt.Errorf("nil adapter for resource type %q and provider %q", resourceType, provider)
	}
	if r.adapters == nil {
		r.adapters = make(map[registryKey]Adapter)
	}
	key := registryKey{resourceType: resourceType, provider: provider}
	if _, exists := r.adapters[key]; exists {
		return fmt.Errorf("adapter already registered for resource type %q and provider %q", resourceType, provider)
	}
	r.adapters[key] = adapter
	return nil
}

func (r Registry) Lookup(resourceType model.ResourceType, provider string) (Adapter, bool) {
	adapter, ok := r.adapters[registryKey{resourceType: resourceType, provider: provider}]
	return adapter, ok
}

func isNilAdapter(adapter Adapter) bool {
	if adapter == nil {
		return true
	}
	value := reflect.ValueOf(adapter)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
