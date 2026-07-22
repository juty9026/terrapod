package resource

import (
	"context"
	"sync"

	"github.com/juty9026/terrapod/internal/model"
)

// Fixture is a deterministic Adapter used by planner and downstream adapter tests.
type Fixture struct {
	Observations  map[model.ResourceID]model.Observation
	InspectErrors map[model.ResourceID]error
	Operations    map[model.ResourceID][]model.Operation
	PlanErrors    map[model.ResourceID]error

	mu           sync.Mutex
	InspectCalls []model.ResourceID
	PlanCalls    []model.ResourceID
	ExecuteCalls []model.Operation
	VerifyCalls  []model.ResourceID
}

func (f *Fixture) Inspect(_ context.Context, desired model.Resource) (model.Observation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.InspectCalls = append(f.InspectCalls, desired.ID)
	return f.Observations[desired.ID], f.InspectErrors[desired.ID]
}

func (f *Fixture) Plan(_ context.Context, desired model.Resource, _ model.Observation, _ model.Ownership) ([]model.Operation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.PlanCalls = append(f.PlanCalls, desired.ID)
	operations := append([]model.Operation(nil), f.Operations[desired.ID]...)
	return operations, f.PlanErrors[desired.ID]
}

func (f *Fixture) Execute(_ context.Context, operation model.Operation) model.OperationResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ExecuteCalls = append(f.ExecuteCalls, operation)
	return model.OperationResult{OperationID: operation.ID, ResourceID: operation.ResourceID, Success: true}
}

func (f *Fixture) Verify(_ context.Context, desired model.Resource) (model.Observation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.VerifyCalls = append(f.VerifyCalls, desired.ID)
	return f.Observations[desired.ID], f.InspectErrors[desired.ID]
}
