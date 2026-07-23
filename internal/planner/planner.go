package planner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	pathpkg "path"
	"path/filepath"
	"regexp"
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
	// EnabledByAnyConfigMetadataPrefix gates a resource on the logical OR of
	// bool config fields named by metadata key suffixes. Each value must be
	// exactly "true" in a validated catalog.
	EnabledByAnyConfigMetadataPrefix = "enabledByAnyConfig."
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

func (p *Planner) Registry() resource.Registry { return p.registry }

func (p *Planner) Build(ctx context.Context, input Input) (model.Plan, error) {
	if err := ctx.Err(); err != nil {
		return model.Plan{}, err
	}
	current, err := indexResources(input.Catalog.Resources)
	if err != nil {
		return model.Plan{}, err
	}
	if err := validateManagedAuthorities(input, current); err != nil {
		return model.Plan{}, err
	}
	if err := validateConfigGateKinds(current); err != nil {
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
		if err := ctx.Err(); err != nil {
			return model.Plan{}, err
		}
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
			if isContextError(inspectErr) {
				return model.Plan{}, inspectErr
			}
			if err := ctx.Err(); err != nil {
				return model.Plan{}, err
			}
			plan.Unavailable[id] = "inspect: " + inspectErr.Error()
			continue
		}
		if err := ctx.Err(); err != nil {
			return model.Plan{}, err
		}
		operations, planErr := adapter.Plan(ctx, candidate, observation, input.Snapshot.Ownership[id])
		if planErr != nil {
			if isContextError(planErr) {
				return model.Plan{}, planErr
			}
			if err := ctx.Err(); err != nil {
				return model.Plan{}, err
			}
			plan.Unavailable[id] = "plan: " + planErr.Error()
			continue
		}
		if err := ctx.Err(); err != nil {
			return model.Plan{}, err
		}
		if err := rejectDesiredPrune(candidate.ID, operations); err != nil {
			return model.Plan{}, err
		}
		operations = normalizeOperations(candidate.ID, operations, input.Upgrade)
		if err := appendOperations(&plan, operationIDs, operations); err != nil {
			return model.Plan{}, err
		}
	}

	pruneResources := make(map[model.ResourceID]model.Resource)
	ownedIDs := sortedOwnershipIDs(input.Snapshot.Ownership)
	for _, id := range ownedIDs {
		if err := ctx.Err(); err != nil {
			return model.Plan{}, err
		}
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
		if err := ctx.Err(); err != nil {
			return model.Plan{}, err
		}
		id := pruneOrder[i]
		historicalResource := pruneResources[id]
		adapter, ok := p.registry.Lookup(historicalResource.Type, historicalResource.Provider)
		if !ok {
			plan.Unavailable[id] = fmt.Sprintf("adapter unavailable for historical resource type %q and provider %q", historicalResource.Type, historicalResource.Provider)
			continue
		}
		observation, inspectErr := adapter.Inspect(ctx, historicalResource)
		if inspectErr != nil {
			if isContextError(inspectErr) {
				return model.Plan{}, inspectErr
			}
			if err := ctx.Err(); err != nil {
				return model.Plan{}, err
			}
			plan.Unavailable[id] = "inspect historical resource: " + inspectErr.Error()
			continue
		}
		if err := ctx.Err(); err != nil {
			return model.Plan{}, err
		}
		var operations []model.Operation
		var planErr error
		if historical, ok := adapter.(resource.HistoricalPlanner); ok {
			operations, planErr = historical.PlanHistorical(ctx, historicalResource, observation, input.Snapshot.Ownership[id])
		} else {
			operations, planErr = adapter.Plan(ctx, historicalResource, observation, input.Snapshot.Ownership[id])
		}
		if planErr != nil {
			if isContextError(planErr) {
				return model.Plan{}, planErr
			}
			if err := ctx.Err(); err != nil {
				return model.Plan{}, err
			}
			plan.Unavailable[id] = "plan historical prune: " + planErr.Error()
			continue
		}
		if err := ctx.Err(); err != nil {
			return model.Plan{}, err
		}
		operations = onlyPruneOperations(historicalResource.ID, operations)
		if err := appendOperations(&plan, operationIDs, operations); err != nil {
			return model.Plan{}, err
		}
	}

	if err := ctx.Err(); err != nil {
		return model.Plan{}, err
	}
	plan.ID, err = planID(plan)
	if err != nil {
		return model.Plan{}, err
	}
	return plan, nil
}

func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
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
	if gated {
		value, ok := config.Terrapod[field]
		if !ok {
			return false
		}
		enabled, ok := value.(bool)
		return ok && enabled
	}
	hasAnyGate := false
	anyEnabled := false
	for key, declared := range candidate.Metadata {
		if !strings.HasPrefix(key, EnabledByAnyConfigMetadataPrefix) {
			continue
		}
		hasAnyGate = true
		field := strings.TrimPrefix(key, EnabledByAnyConfigMetadataPrefix)
		if field == "" || declared != "true" {
			return false
		}
		if enabled, ok := config.Terrapod[field].(bool); ok && enabled {
			anyEnabled = true
		}
	}
	return !hasAnyGate || anyEnabled
}

func validateConfigGateKinds(resources map[model.ResourceID]model.Resource) error {
	ids := make([]model.ResourceID, 0, len(resources))
	for id := range resources {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		candidate := resources[id]
		_, hasSingle := candidate.Metadata[EnabledByConfigMetadataKey]
		hasAny := false
		for key := range candidate.Metadata {
			if strings.HasPrefix(key, EnabledByAnyConfigMetadataPrefix) {
				hasAny = true
				break
			}
		}
		if hasSingle && hasAny {
			return fmt.Errorf("resource %q mixes %q and %q metadata", id, EnabledByConfigMetadataKey, "enabledByAnyConfig.*")
		}
	}
	return nil
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

func rejectDesiredPrune(id model.ResourceID, operations []model.Operation) error {
	for _, operation := range operations {
		if operation.Kind == model.OperationPrune {
			return fmt.Errorf("adapter planned prune operation %q for desired resource %q", operation.ID, id)
		}
	}
	return nil
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
	if match.Type == model.ResourceManagedFiles {
		if _, ok := safeManagedScope(match); !ok {
			return model.Resource{}, false
		}
		for path, receipt := range ownership.Paths {
			if !filepath.IsAbs(path) || receipt == "" {
				return model.Resource{}, false
			}
		}
		return match, true
	}
	if match.Type == model.ResourceGitCheckout {
		if !validGitCheckoutOwnership(match, ownership.Paths) {
			return model.Resource{}, false
		}
		return match, true
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

var gitReceiptPattern = regexp.MustCompile(`^(file|link):[0-9a-f]{64}$`)

func validGitCheckoutOwnership(item model.Resource, paths map[string]string) bool {
	destination := item.Metadata["git.destination"]
	if destination == "" || destination != pathpkg.Clean(destination) || strings.HasPrefix(destination, "/") || destination == ".." || strings.HasPrefix(destination, "../") || strings.Contains(destination, "\\") || len(paths) == 0 {
		return false
	}
	marker := string(filepath.Separator) + filepath.FromSlash(destination) + string(filepath.Separator)
	root := ""
	for path, receipt := range paths {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path || !gitReceiptPattern.MatchString(receipt) {
			return false
		}
		index := strings.LastIndex(path, marker)
		if index < 0 {
			return false
		}
		candidateRoot := path[:index+len(marker)-1]
		relative, err := filepath.Rel(candidateRoot, path)
		if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return false
		}
		if root == "" {
			root = candidateRoot
		} else if root != candidateRoot {
			return false
		}
	}
	return true
}

func validateManagedAuthorities(input Input, current map[model.ResourceID]model.Resource) error {
	type scoped struct {
		id    model.ResourceID
		scope string
	}
	var scopes []scoped
	managedIDs := make(map[model.ResourceID]struct{})
	for id, item := range current {
		if item.Type != model.ResourceManagedFiles {
			continue
		}
		scope, ok := safeManagedScope(item)
		if !ok {
			return fmt.Errorf("managed-file resource %q has unsafe or missing declared scope", id)
		}
		scopes = append(scopes, scoped{id, scope})
		managedIDs[id] = struct{}{}
	}
	for id, ownership := range input.Snapshot.Ownership {
		catalog, ok := input.Historical[ownership.CatalogDigest]
		if !ok {
			continue
		}
		for _, item := range catalog.Resources {
			if item.ID != id || item.Type != model.ResourceManagedFiles {
				continue
			}
			scope, ok := safeManagedScope(item)
			if !ok {
				return fmt.Errorf("historical managed-file resource %q has unsafe or missing declared scope", id)
			}
			scopes = append(scopes, scoped{id, scope})
			managedIDs[id] = struct{}{}
		}
	}
	owner := make(map[string]model.ResourceID)
	for id, ownership := range input.Snapshot.Ownership {
		if _, managed := managedIDs[id]; !managed {
			continue
		}
		for path := range ownership.Paths {
			if prior, duplicate := owner[path]; duplicate && prior != id {
				return fmt.Errorf("duplicate managed-file ownership path %q for %q and %q", path, prior, id)
			}
			owner[path] = id
		}
	}
	for i := 0; i < len(scopes); i++ {
		for j := i + 1; j < len(scopes); j++ {
			if scopes[i].id != scopes[j].id && managedScopesOverlap(scopes[i].scope, scopes[j].scope) {
				return fmt.Errorf("overlapping managed-file authority for %q and %q", scopes[i].id, scopes[j].id)
			}
		}
	}
	return nil
}

func safeManagedScope(item model.Resource) (string, bool) {
	scope, ok := item.Metadata[model.ManagedFilesScopeMetadataKey]
	return scope, ok && scope != "" && scope == pathpkg.Clean(scope) && !strings.HasPrefix(scope, "/") && scope != ".." && !strings.HasPrefix(scope, "../") && !strings.Contains(scope, "\\")
}

func managedScopesOverlap(left, right string) bool {
	return left == "." || right == "." || left == right || strings.HasPrefix(left, right+"/") || strings.HasPrefix(right, left+"/")
}

func equalStrings(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		rightValue, exists := right[key]
		if !exists || rightValue != value {
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

func Digest(plan model.Plan) (string, error) { return planID(plan) }
