package planner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/resource"
)

const (
	// EnabledByConfigMetadataKey is the only supported config gate. A resource
	// carrying this key is enabled only when the named Config.Terrapod value is
	// exactly bool true. Resources without the key are enabled.
	EnabledByConfigMetadataKey = "enabledByConfig"
	// OwnedPathMetadataPrefix identifies historical catalog path-scope entries.
	// The complete path.* key/value set must equal the ownership receipt Paths.
	OwnedPathMetadataPrefix = "path."
	UnbackedOwnershipReason = "ownership receipt is not backed by a verified catalog"
)

type Input struct {
	Catalog       model.Catalog
	CatalogDigest string
	Historical    map[string]model.Catalog
	Config        model.Config
	Profile       model.Profile
	Snapshot      model.Snapshot
	Upgrade       bool
}

type Planner struct {
	registry resource.Registry
}

func New(registry resource.Registry) *Planner {
	return &Planner{registry: registry}
}

func (p *Planner) Build(ctx context.Context, input Input) (model.Plan, error) {
	current, err := indexResources(input.Catalog.Resources)
	if err != nil {
		return model.Plan{}, err
	}

	desired := make(map[model.ResourceID]model.Resource)
	for id, candidate := range current {
		if matchesProfile(candidate, input.Profile) && enabledByConfig(candidate, input.Config) {
			desired[id] = candidate
		}
	}

	order, err := dependencyOrder(desired)
	if err != nil {
		return model.Plan{}, err
	}
	plan := model.Plan{Release: input.Catalog.Release, Unavailable: make(map[model.ResourceID]string)}
	operationIDs := make(map[string]model.ResourceID)
	for _, id := range order {
		candidate := desired[id]
		if reason := unavailableDependency(candidate, desired, plan.Unavailable); reason != "" {
			plan.Unavailable[id] = reason
			continue
		}
		adapter, ok := p.registry.Lookup(candidate.Type, candidate.Provider)
		if !ok {
			plan.Unavailable[id] = fmt.Sprintf("adapter unavailable for resource type %q and provider %q", candidate.Type, candidate.Provider)
			continue
		}
		observation, inspectErr := adapter.Inspect(ctx, candidate)
		if inspectErr != nil {
			plan.Unavailable[id] = "inspect: " + inspectErr.Error()
			continue
		}
		operations, planErr := adapter.Plan(ctx, candidate, observation, input.Snapshot.Ownership[id])
		if planErr != nil {
			plan.Unavailable[id] = "plan: " + planErr.Error()
			continue
		}
		operations = normalizeOperations(candidate.ID, operations, input.Upgrade)
		if err := appendOperations(&plan, operationIDs, operations); err != nil {
			return model.Plan{}, err
		}
	}

	pruneResources := make(map[model.ResourceID]model.Resource)
	ownedIDs := sortedOwnershipIDs(input.Snapshot.Ownership)
	for _, id := range ownedIDs {
		if _, remainsDesired := desired[id]; remainsDesired {
			continue
		}
		ownership := input.Snapshot.Ownership[id]
		historicalResource, ok := historicalAuthority(input.Historical, id, ownership)
		if !ok {
			plan.Unavailable[id] = UnbackedOwnershipReason
			continue
		}
		pruneResources[id] = historicalResource
	}
	pruneOrder, err := dependencyOrder(pruneResources)
	if err != nil {
		return model.Plan{}, fmt.Errorf("historical prune graph: %w", err)
	}
	for i := len(pruneOrder) - 1; i >= 0; i-- {
		id := pruneOrder[i]
		historicalResource := pruneResources[id]
		adapter, ok := p.registry.Lookup(historicalResource.Type, historicalResource.Provider)
		if !ok {
			plan.Unavailable[id] = fmt.Sprintf("adapter unavailable for historical resource type %q and provider %q", historicalResource.Type, historicalResource.Provider)
			continue
		}
		observation, inspectErr := adapter.Inspect(ctx, historicalResource)
		if inspectErr != nil {
			plan.Unavailable[id] = "inspect historical resource: " + inspectErr.Error()
			continue
		}
		operations, planErr := adapter.Plan(ctx, historicalResource, observation, input.Snapshot.Ownership[id])
		if planErr != nil {
			plan.Unavailable[id] = "plan historical prune: " + planErr.Error()
			continue
		}
		operations = onlyPruneOperations(historicalResource.ID, operations)
		if err := appendOperations(&plan, operationIDs, operations); err != nil {
			return model.Plan{}, err
		}
	}

	plan.ID, err = planID(plan)
	if err != nil {
		return model.Plan{}, err
	}
	return plan, nil
}

func indexResources(resources []model.Resource) (map[model.ResourceID]model.Resource, error) {
	indexed := make(map[model.ResourceID]model.Resource, len(resources))
	for _, candidate := range resources {
		if _, exists := indexed[candidate.ID]; exists {
			return nil, fmt.Errorf("duplicate resource ID %q", candidate.ID)
		}
		indexed[candidate.ID] = candidate
	}
	return indexed, nil
}

func matchesProfile(candidate model.Resource, profile model.Profile) bool {
	if len(candidate.Profiles) == 0 {
		return true
	}
	for _, allowed := range candidate.Profiles {
		if allowed == profile {
			return true
		}
	}
	return false
}

func enabledByConfig(candidate model.Resource, config model.Config) bool {
	field, gated := candidate.Metadata[EnabledByConfigMetadataKey]
	if !gated {
		return true
	}
	value, ok := config.Terrapod[field]
	if !ok {
		return false
	}
	enabled, ok := value.(bool)
	return ok && enabled
}

func dependencyOrder(resources map[model.ResourceID]model.Resource) ([]model.ResourceID, error) {
	ids := make([]model.ResourceID, 0, len(resources))
	for id := range resources {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	state := make(map[model.ResourceID]uint8, len(resources))
	order := make([]model.ResourceID, 0, len(resources))
	var visit func(model.ResourceID) error
	visit = func(id model.ResourceID) error {
		switch state[id] {
		case 1:
			return fmt.Errorf("dependency cycle involving resource %q", id)
		case 2:
			return nil
		}
		state[id] = 1
		dependencies := append([]model.ResourceID(nil), resources[id].DependsOn...)
		sort.Slice(dependencies, func(i, j int) bool { return dependencies[i] < dependencies[j] })
		for _, dependency := range dependencies {
			if _, exists := resources[dependency]; exists {
				if err := visit(dependency); err != nil {
					return err
				}
			}
		}
		state[id] = 2
		order = append(order, id)
		return nil
	}
	for _, id := range ids {
		if err := visit(id); err != nil {
			return nil, err
		}
	}
	return order, nil
}

func unavailableDependency(candidate model.Resource, desired map[model.ResourceID]model.Resource, unavailable map[model.ResourceID]string) string {
	dependencies := append([]model.ResourceID(nil), candidate.DependsOn...)
	sort.Slice(dependencies, func(i, j int) bool { return dependencies[i] < dependencies[j] })
	for _, dependency := range dependencies {
		if _, ok := desired[dependency]; !ok {
			return fmt.Sprintf("dependency %q is not available in desired state", dependency)
		}
		if _, blocked := unavailable[dependency]; blocked {
			return fmt.Sprintf("dependency %q is unavailable", dependency)
		}
	}
	return ""
}

func normalizeOperations(id model.ResourceID, operations []model.Operation, upgrade bool) []model.Operation {
	normalized := make([]model.Operation, 0, len(operations))
	for _, operation := range operations {
		if operation.Kind == model.OperationUpgrade && !upgrade {
			continue
		}
		operation.ResourceID = id
		operation.Removes = append([]string(nil), operation.Removes...)
		normalized = append(normalized, operation)
	}
	return normalized
}

func onlyPruneOperations(id model.ResourceID, operations []model.Operation) []model.Operation {
	prunes := make([]model.Operation, 0, len(operations))
	for _, operation := range operations {
		if operation.Kind != model.OperationPrune {
			continue
		}
		operation.ResourceID = id
		operation.Removes = append([]string(nil), operation.Removes...)
		prunes = append(prunes, operation)
	}
	return prunes
}

func appendOperations(plan *model.Plan, seen map[string]model.ResourceID, operations []model.Operation) error {
	for _, operation := range operations {
		if previous, exists := seen[operation.ID]; exists {
			return fmt.Errorf("duplicate operation ID %q for resources %q and %q", operation.ID, previous, operation.ResourceID)
		}
		seen[operation.ID] = operation.ResourceID
		plan.Operations = append(plan.Operations, operation)
	}
	return nil
}

func sortedOwnershipIDs(ownership map[model.ResourceID]model.Ownership) []model.ResourceID {
	ids := make([]model.ResourceID, 0, len(ownership))
	for id := range ownership {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func historicalAuthority(historical map[string]model.Catalog, id model.ResourceID, ownership model.Ownership) (model.Resource, bool) {
	if ownership.ResourceID != id {
		return model.Resource{}, false
	}
	catalog, ok := historical[ownership.CatalogDigest]
	if !ok {
		return model.Resource{}, false
	}
	var match model.Resource
	found := false
	for _, candidate := range catalog.Resources {
		if candidate.ID != ownership.ResourceID {
			continue
		}
		if found {
			return model.Resource{}, false
		}
		match, found = candidate, true
	}
	if !found || match.Provider != ownership.Provider || match.Package != ownership.Package {
		return model.Resource{}, false
	}
	paths := make(map[string]string)
	for key, value := range match.Metadata {
		if strings.HasPrefix(key, OwnedPathMetadataPrefix) {
			paths[strings.TrimPrefix(key, OwnedPathMetadataPrefix)] = value
		}
	}
	if !equalStrings(paths, ownership.Paths) {
		return model.Resource{}, false
	}
	return match, true
}

func equalStrings(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func planID(plan model.Plan) (string, error) {
	plan.ID = ""
	contents, err := json.Marshal(plan)
	if err != nil {
		return "", fmt.Errorf("marshal canonical plan: %w", err)
	}
	digest := sha256.Sum256(contents)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}
