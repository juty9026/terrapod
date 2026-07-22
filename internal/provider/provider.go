package provider

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/juty9026/terrapod/internal/model"
)

type ChangeSet struct {
	Installs []string
	Upgrades []string
	Removes  []string
}

type Provider interface {
	Name() string
	Inspect(context.Context, model.Resource) (model.Observation, error)
	Simulate(context.Context, model.Operation) (ChangeSet, error)
	Execute(context.Context, model.Operation) error
	Verify(context.Context, model.Resource) (model.Observation, error)
}

type ErrUnmanagedRemoval struct {
	IDs []string
}

func (e *ErrUnmanagedRemoval) Error() string {
	return fmt.Sprintf("provider: unmanaged removals: %s", strings.Join(e.IDs, ", "))
}

func ValidateChangeSet(changes ChangeSet, target model.Resource, planOwnedRemovals []string) error {
	allowed := make(map[string]struct{}, len(planOwnedRemovals)+1)
	allowed[target.Package] = struct{}{}
	for _, id := range planOwnedRemovals {
		allowed[id] = struct{}{}
	}

	unmanaged := make(map[string]struct{})
	for _, id := range changes.Removes {
		if _, ok := allowed[id]; !ok {
			unmanaged[id] = struct{}{}
		}
	}
	if len(unmanaged) == 0 {
		return nil
	}

	ids := make([]string, 0, len(unmanaged))
	for id := range unmanaged {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return &ErrUnmanagedRemoval{IDs: ids}
}
