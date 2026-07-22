package resource

import (
	"context"
	"fmt"

	"github.com/juty9026/terrapod/internal/model"
)

// Adapter is the frozen resource boundary shared by planning and execution.
type Adapter interface {
	Inspect(context.Context, model.Resource) (model.Observation, error)
	Plan(context.Context, model.Resource, model.Observation, model.Ownership) ([]model.Operation, error)
	Execute(context.Context, model.Operation) model.OperationResult
	Verify(context.Context, model.Resource) (model.Observation, error)
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
	if r.adapters == nil {
		r.adapters = make(map[registryKey]Adapter)
	}
	key := registryKey{resourceType: resourceType, provider: provider}
	if _, exists := r.adapters[key]; exists {
		return fmt.Errorf("adapter already registered for resource type %q and provider %q", resourceType, provider)
	}
	if adapter == nil {
		return fmt.Errorf("nil adapter for resource type %q and provider %q", resourceType, provider)
	}
	r.adapters[key] = adapter
	return nil
}

func (r Registry) Lookup(resourceType model.ResourceType, provider string) (Adapter, bool) {
	adapter, ok := r.adapters[registryKey{resourceType: resourceType, provider: provider}]
	return adapter, ok
}
