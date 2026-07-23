package migrate

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/juty9026/terrapod/internal/catalog"
	"github.com/juty9026/terrapod/internal/legacydecl"
	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/resource"
)

const (
	LegacyBaselineRelease = "legacy-current"
	LegacyBaselineKeyID   = "legacy-current-2026"

	legacyBaselinePublicKey         = "14iK8SGULGWVo50SWPWG5drZ86xbT0VApxhtCT7bz4E="
	legacyWarningMarkersMetadataKey = "migration.installWarningMarkers"
)

type LegacyArtifact struct {
	Observation    model.Observation
	LegacyPackages []string
	Modified       bool
	PriorUnknown   bool
}

type LegacyOwnershipInput struct {
	Baseline       catalog.Verified
	Current        catalog.Verified
	Registry       resource.Registry
	Desired        map[model.ResourceID]bool
	Actual         map[model.ResourceID]LegacyArtifact
	WarningMarkers map[string]string
}

type LegacyOwnershipResult struct {
	Plan           model.Plan
	Receipts       map[model.ResourceID]model.Ownership
	ArchiveMarkers []string
}

func LoadLegacyBaseline(path string) (catalog.Verified, []string, error) {
	publicKey, err := base64.StdEncoding.Strict().DecodeString(legacyBaselinePublicKey)
	if err != nil {
		return catalog.Verified{}, nil, fmt.Errorf("decode compiled legacy baseline key: %w", err)
	}
	verified, err := catalog.LoadVerified(path, catalog.SignatureSet{
		PublicKeys: map[string]ed25519.PublicKey{LegacyBaselineKeyID: publicKey},
	})
	if err != nil {
		return catalog.Verified{}, nil, err
	}
	if verified.Catalog.Release != LegacyBaselineRelease || verified.KeyID != LegacyBaselineKeyID {
		return catalog.Verified{}, nil, errors.New("legacy baseline identity mismatch")
	}
	var declaration string
	for _, item := range verified.Catalog.Resources {
		if item.ID == "management.homebrew" {
			declaration = item.Metadata[legacyWarningMarkersMetadataKey]
			break
		}
	}
	markers, err := parseWarningMarkers(declaration)
	if err != nil {
		return catalog.Verified{}, nil, err
	}
	return verified, markers, nil
}

func PlanLegacyOwnership(ctx context.Context, input LegacyOwnershipInput) (LegacyOwnershipResult, error) {
	if err := ctx.Err(); err != nil {
		return LegacyOwnershipResult{}, err
	}
	if input.Baseline.Catalog.Release != LegacyBaselineRelease || input.Baseline.Digest == "" || input.Baseline.KeyID != LegacyBaselineKeyID {
		return LegacyOwnershipResult{}, errors.New("verified legacy baseline is required")
	}
	baseline := indexLegacyResources(input.Baseline.Catalog.Resources)
	current := indexLegacyResources(input.Current.Catalog.Resources)
	declaredMarkers, err := baselineWarningMarkers(input.Baseline.Catalog.Resources)
	if err != nil {
		return LegacyOwnershipResult{}, err
	}
	result := LegacyOwnershipResult{
		Plan: model.Plan{
			Release:     input.Current.Catalog.Release,
			Unavailable: make(map[model.ResourceID]string),
		},
		Receipts: make(map[model.ResourceID]model.Ownership),
	}
	ids := make([]model.ResourceID, 0, len(input.Actual))
	for id := range input.Actual {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return LegacyOwnershipResult{}, err
		}
		item, declared := baseline[id]
		artifact := input.Actual[id]
		if !declared || !artifact.Observation.Present {
			continue
		}
		if artifact.Modified {
			result.Plan.Unavailable[id] = "legacy managed target is modified"
			continue
		}
		if artifact.PriorUnknown && item.Type != model.ResourceIntegration {
			result.Plan.Unavailable[id] = "unknown prior value is valid only for integrations"
			continue
		}
		adapter, ok := input.Registry.Lookup(item.Type, item.Provider)
		if !ok {
			result.Plan.Unavailable[id] = fmt.Sprintf("adapter unavailable for legacy resource type %q and provider %q", item.Type, item.Provider)
			continue
		}
		receipt := model.Ownership{
			ResourceID:    id,
			CatalogDigest: input.Baseline.Digest,
			Provider:      item.Provider,
			Package:       item.Package,
			Paths:         copyStringMap(artifact.Observation.Paths),
			PriorValues:   make(map[string]json.RawMessage),
			PriorUnknown:  artifact.PriorUnknown,
		}
		var operations []model.Operation
		var planErr error
		if input.Desired[id] {
			desired, exists := current[id]
			if !exists {
				result.Plan.Unavailable[id] = "desired resource is absent from current signed catalog"
				continue
			}
			observed, inspectErr := adapter.Inspect(ctx, desired)
			if inspectErr != nil {
				result.Plan.Unavailable[id] = "inspect: " + inspectErr.Error()
				continue
			}
			if requiresHealthyLegacyAdoption(desired.Type) && observed.Present && !observed.Healthy {
				result.Plan.Unavailable[id] = "legacy managed target is modified"
				continue
			}
			receipt.Paths = copyStringMap(observed.Paths)
			ownedForPlan := model.Ownership{}
			if desired.Type == model.ResourceIntegration {
				ownedForPlan = receipt
			}
			operations, planErr = adapter.Plan(ctx, desired, observed, ownedForPlan)
			if planErr == nil && len(artifact.LegacyPackages) != 0 {
				operations, planErr = markLegacyTransfer(desired, operations, artifact.LegacyPackages)
			}
		} else if historical, ok := adapter.(resource.HistoricalPlanner); ok {
			operations, planErr = historical.PlanHistorical(ctx, item, artifact.Observation, receipt)
		} else {
			operations = []model.Operation{{
				ID:                "prune-" + string(item.ID),
				ResourceID:        item.ID,
				Kind:              model.OperationPrune,
				Provider:          item.Provider,
				Package:           item.Package,
				RequiresPrivilege: item.Provider == "apt",
				Removes:           []string{item.Package},
			}}
		}
		if planErr != nil {
			result.Plan.Unavailable[id] = "plan legacy ownership: " + planErr.Error()
			continue
		}
		for _, operation := range operations {
			if operation.ResourceID != id || operation.Provider != item.Provider || operation.Package != item.Package {
				return LegacyOwnershipResult{}, fmt.Errorf("legacy operation identity mismatch for %q", id)
			}
			result.Plan.Operations = append(result.Plan.Operations, operation)
		}
		result.Receipts[id] = receipt
	}
	result.ArchiveMarkers = authorizedWarningMarkerPaths(input.WarningMarkers, declaredMarkers)
	result.Plan.ID = legacyPlanID(result.Plan)
	return result, nil
}

func requiresHealthyLegacyAdoption(resourceType model.ResourceType) bool {
	switch resourceType {
	case model.ResourceManagedFiles, model.ResourceGitCheckout, model.ResourceArchive, model.ResourceIntegration:
		return true
	default:
		return false
	}
}

func markLegacyTransfer(item model.Resource, operations []model.Operation, packages []string) ([]model.Operation, error) {
	authorized := make(map[string]struct{})
	declarations, err := legacydecl.Parse(item)
	if err != nil {
		return nil, err
	}
	for _, declaration := range declarations {
		authorized[declaration.Package] = struct{}{}
	}
	removes := append([]string(nil), packages...)
	sort.Strings(removes)
	for index, packageID := range removes {
		if packageID == "" || (index > 0 && packageID == removes[index-1]) {
			return nil, errors.New("legacy packages must be non-empty and unique")
		}
		if _, ok := authorized[packageID]; !ok {
			return nil, fmt.Errorf("legacy package %q is outside the signed baseline declaration", packageID)
		}
	}
	if len(operations) != 1 {
		return nil, errors.New("legacy transfer requires exactly one desired-provider operation")
	}
	operations = append([]model.Operation(nil), operations...)
	operations[0].ID = "transfer-" + string(item.ID)
	operations[0].Kind = model.OperationTransfer
	operations[0].Removes = removes
	return operations, nil
}

func parseWarningMarkers(declaration string) ([]string, error) {
	if declaration == "" {
		return nil, errors.New("legacy baseline warning marker declaration is missing")
	}
	seen := make(map[string]struct{})
	var markers []string
	for _, marker := range strings.Split(declaration, ",") {
		if marker == "" || strings.TrimSpace(marker) != marker || strings.Contains(marker, "/") {
			return nil, fmt.Errorf("invalid legacy warning marker %q", marker)
		}
		if _, duplicate := seen[marker]; duplicate {
			return nil, fmt.Errorf("duplicate legacy warning marker %q", marker)
		}
		seen[marker] = struct{}{}
		markers = append(markers, marker)
	}
	sort.Strings(markers)
	return markers, nil
}

func baselineWarningMarkers(resources []model.Resource) ([]string, error) {
	for _, item := range resources {
		if item.ID == "management.homebrew" {
			return parseWarningMarkers(item.Metadata[legacyWarningMarkersMetadataKey])
		}
	}
	return nil, errors.New("legacy baseline management core declaration is missing")
}

func authorizedWarningMarkerPaths(actual map[string]string, declared []string) []string {
	allowed := make(map[string]struct{}, len(declared))
	for _, marker := range declared {
		allowed[marker] = struct{}{}
	}
	var paths []string
	for marker, path := range actual {
		if _, ok := allowed[marker]; !ok || !filepath.IsAbs(path) || filepath.Clean(path) != path {
			continue
		}
		base := filepath.Base(path)
		if base == marker || (marker == "optional-ai-cli-tools" && base == "ai-cli-tools") {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	return paths
}

func indexLegacyResources(resources []model.Resource) map[model.ResourceID]model.Resource {
	indexed := make(map[model.ResourceID]model.Resource, len(resources))
	for _, item := range resources {
		indexed[item.ID] = item
	}
	return indexed
}

func resourceIDs(resources []model.Resource) []string {
	ids := make([]string, 0, len(resources))
	for _, item := range resources {
		ids = append(ids, string(item.ID))
	}
	sort.Strings(ids)
	return ids
}

func copyStringMap(input map[string]string) map[string]string {
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func legacyPlanID(plan model.Plan) string {
	copy := plan
	copy.ID = ""
	encoded, _ := json.Marshal(copy)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}
