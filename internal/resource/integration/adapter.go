package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/juty9026/terrapod/internal/execx"
	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/state"
)

const (
	ProviderJSONFields  = "json-fields"
	ProviderPlistFields = "plist-fields"
	ProviderKarabiner   = "karabiner"

	MetadataHandler  = "integration.handler"
	MetadataPath     = "integration.path"
	MetadataPathGlob = "integration.pathGlob"
	MetadataFields   = "integration.fields"
	MetadataFormat   = "integration.format"

	HandlerFields          = "fields"
	HandlerJetendardZed    = "jetendard-zed"
	HandlerJetendardOrca   = "jetendard-orca"
	HandlerKarabinerOpener = "karabiner-opener"

	unknownPriorBackupSuffix = ".terrapod-legacy-backup"
)

type Karabiner interface {
	Guidance(context.Context) ([]byte, error)
	Open(context.Context) error
}

type CommandRunner interface {
	Run(context.Context, execx.Request) (execx.Result, error)
}

type KarabinerClient struct {
	Runner            CommandRunner
	CLIPath, OpenPath string
}

func (c KarabinerClient) Guidance(ctx context.Context) ([]byte, error) {
	if c.Runner == nil {
		return nil, errors.New("integration: command runner is required")
	}
	cli := c.CLIPath
	if cli == "" {
		cli = "/Library/Application Support/org.pqrs/Karabiner-Elements/bin/karabiner_cli"
	}
	result, err := c.Runner.Run(ctx, execx.Request{Path: cli, Args: []string{"--show-settings-window-guidance"}})
	if err != nil {
		return nil, fmt.Errorf("integration: inspect Karabiner guidance: %w", err)
	}
	return result.Stdout, nil
}
func (c KarabinerClient) Open(ctx context.Context) error {
	if c.Runner == nil {
		return errors.New("integration: command runner is required")
	}
	open := c.OpenPath
	if open == "" {
		open = "/usr/bin/open"
	}
	_, err := c.Runner.Run(ctx, execx.Request{Path: open, Args: []string{"-a", "Karabiner-Elements"}})
	if err != nil {
		return fmt.Errorf("integration: open Karabiner-Elements: %w", err)
	}
	return nil
}

type Adapter struct {
	Home       string
	State      *state.Store
	AppRunning func(string) bool
	Karabiner  Karabiner

	mu     sync.Mutex
	pruned map[model.ResourceID]bool
}

type declaration struct {
	handler, format, path, pathGlob string
	fields                          map[string]any
}
type target struct{ relative, absolute string }
type priorValue struct {
	Exists bool            `json:"exists"`
	Type   string          `json:"type,omitempty"`
	Value  json.RawMessage `json:"value,omitempty"`
}

func (a *Adapter) Inspect(ctx context.Context, item model.Resource) (model.Observation, error) {
	d, err := a.declaration(item)
	if err != nil {
		return model.Observation{}, err
	}
	if d.handler == HandlerKarabinerOpener {
		return a.inspectKarabiner(ctx, item)
	}
	a.mu.Lock()
	wasPruned := a.pruned != nil && a.pruned[item.ID]
	a.mu.Unlock()
	if wasPruned {
		return model.Observation{Provider: item.Provider, Package: item.Package, Healthy: true}, nil
	}
	targets, err := a.targets(d)
	if err != nil {
		return model.Observation{}, err
	}
	owned, err := a.ownership(item)
	if err != nil {
		return model.Observation{}, err
	}
	pruneReplay := a.activePrune(item.ID)
	present := len(targets) > 0 || d.pathGlob != ""
	healthy := true
	paths := make(map[string]string)
	for _, target := range targets {
		doc, exists, err := a.readDocument(d, target)
		if err != nil {
			return model.Observation{}, err
		}
		if !exists {
			present = false
			healthy = false
			continue
		}
		for pointer, desired := range d.fields {
			current, err := doc.get(pointer)
			if err != nil {
				return model.Observation{}, fmt.Errorf("integration: %s: %w", target.relative, err)
			}
			matchesDesired, canonicalErr := sameValue(current.Value, desired)
			if canonicalErr != nil {
				return model.Observation{}, fmt.Errorf("integration: canonicalize %s%s: %w", target.relative, pointer, canonicalErr)
			}
			if !current.Exists || !matchesDesired {
				matchesPrior := false
				if pruneReplay {
					prior, ok, priorErr := decodePrior(owned.PriorValues[fieldKey(target.relative, pointer)])
					if priorErr != nil {
						return model.Observation{}, priorErr
					}
					if ok {
						matchesPrior, canonicalErr = sameField(current, prior)
						if canonicalErr != nil {
							return model.Observation{}, canonicalErr
						}
					}
				}
				if !matchesPrior {
					healthy = false
				}
			}
			digest, canonicalErr := digestValue(current)
			if canonicalErr != nil {
				return model.Observation{}, canonicalErr
			}
			paths[fieldKey(target.relative, pointer)] = digest
		}
	}
	for key, digest := range owned.Paths {
		if _, exists := paths[key]; !exists {
			paths[key] = digest
		}
	}
	// A matching unowned setting still needs one Adopt execution to capture its prior value.
	if owned.ResourceID == "" {
		healthy = false
	}
	detail := "settings differ from [redacted]"
	if healthy {
		detail = "settings match [redacted]"
	}
	return model.Observation{Present: present, Healthy: healthy, Provider: item.Provider, Package: item.Package, Paths: paths, Detail: detail}, nil
}

func (a *Adapter) activePrune(id model.ResourceID) bool {
	if a.State == nil {
		return false
	}
	snapshot, err := a.State.Snapshot()
	if err != nil || snapshot.ActiveJournal == nil || snapshot.ActiveJournal.Status != "active" {
		return false
	}
	for _, op := range snapshot.ActiveJournal.Plan.Operations {
		if op.ResourceID == id && op.Kind == model.OperationPrune {
			return true
		}
	}
	return false
}

func (a *Adapter) Verify(ctx context.Context, item model.Resource) (model.Observation, error) {
	observed, err := a.Inspect(ctx, item)
	if err != nil {
		return model.Observation{}, err
	}
	if item.Provider == ProviderKarabiner {
		return observed, nil
	}
	if !observed.Present || !observed.Healthy {
		return observed, nil
	}
	return observed, nil
}

func (a *Adapter) Plan(ctx context.Context, item model.Resource, _ model.Observation, owned model.Ownership) ([]model.Operation, error) {
	d, err := a.declaration(item)
	if err != nil {
		return nil, err
	}
	if d.handler == HandlerKarabinerOpener {
		observed, err := a.inspectKarabiner(ctx, item)
		if err != nil {
			return nil, err
		}
		if observed.Healthy {
			return nil, nil
		}
		return []model.Operation{operation(item, model.OperationInstall)}, nil
	}
	if owned.ResourceID == "" {
		owned, err = a.ownership(item)
		if err != nil {
			return nil, err
		}
	}
	if owned.ResourceID != "" {
		if !owned.PriorUnknown {
			if err := validatePriorKeys(d, owned.PriorValues); err != nil {
				return nil, err
			}
		} else if len(owned.PriorValues) != 0 {
			return nil, errors.New("integration: unknown prior receipt cannot contain prior values")
		}
		if err := validateManagedKeys(d, owned.Paths); err != nil {
			return nil, err
		}
	}
	targets, err := a.targets(d)
	if err != nil {
		return nil, err
	}
	changed, receiptUpdate := false, false
	authorization := &model.IntegrationAuthorization{Version: 1, Fields: make(map[string]model.IntegrationFieldAuthorization)}
	for _, target := range targets {
		doc, _, err := a.readDocument(d, target)
		if err != nil {
			return nil, err
		}
		for pointer, desired := range d.fields {
			key := fieldKey(target.relative, pointer)
			current, err := doc.get(pointer)
			if err != nil {
				return nil, err
			}
			desiredState := fieldValue{Exists: true, Value: desired}
			desiredDigest, err := digestValue(desiredState)
			if err != nil {
				return nil, err
			}
			matchesDesired, err := sameField(current, desiredState)
			if err != nil {
				return nil, err
			}
			currentState, err := pathState(current)
			if err != nil {
				return nil, err
			}
			fieldAuth := model.IntegrationFieldAuthorization{Current: currentState, DesiredDigest: desiredDigest}
			if owned.ResourceID == "" {
				changed = changed || !matchesDesired
				authorization.Fields[key] = fieldAuth
				continue
			}
			last, hasLast := owned.Paths[key]
			fieldAuth.LastManagedDigest = last
			if !hasLast {
				prior, hasPrior, decodeErr := decodePrior(owned.PriorValues[key])
				if decodeErr != nil {
					return nil, decodeErr
				}
				if !hasPrior && d.pathGlob == "" {
					return nil, fmt.Errorf("integration: missing ownership receipt for %s%s", target.relative, pointer)
				}
				matchesPrior, err := sameField(current, prior)
				if err != nil {
					return nil, err
				}
				if hasPrior && !matchesPrior && !matchesDesired {
					return nil, fmt.Errorf("integration conflict at %s%s: field changed during interrupted takeover", target.relative, pointer)
				}
				receiptUpdate = true
				changed = changed || !matchesDesired
			} else {
				currentDigest, err := digestValue(current)
				if err != nil {
					return nil, err
				}
				if currentDigest != last && !matchesDesired {
					return nil, fmt.Errorf("integration conflict at %s%s: field differs from last managed value", target.relative, pointer)
				}
				changed = changed || !matchesDesired
				receiptUpdate = receiptUpdate || last != desiredDigest
			}
			authorization.Fields[key] = fieldAuth
		}
	}
	if !changed && !receiptUpdate && owned.ResourceID != "" {
		return nil, nil
	}
	if changed && d.handler == HandlerJetendardOrca && a.appRunning("Orca") {
		return nil, errors.New("integration deferred: Orca is running; quit Orca and retry")
	}
	kind := model.OperationInstall
	if !changed && owned.ResourceID == "" {
		kind = model.OperationAdopt
	} else if owned.ResourceID != "" {
		kind = model.OperationUpgrade
	}
	op := operation(item, kind)
	op.IntegrationAuthorization = authorization
	return []model.Operation{op}, nil
}

func (a *Adapter) PlanHistorical(_ context.Context, item model.Resource, _ model.Observation, owned model.Ownership) ([]model.Operation, error) {
	d, err := a.declaration(item)
	if err != nil {
		return nil, err
	}
	if d.handler == HandlerKarabinerOpener {
		return []model.Operation{operation(item, model.OperationPrune)}, nil
	}
	if owned.ResourceID != item.ID || owned.Provider != item.Provider || owned.Package != item.Package {
		return nil, errors.New("integration: historical ownership identity mismatch")
	}
	targets, err := a.targets(d)
	if err != nil {
		return nil, err
	}
	if !owned.PriorUnknown {
		if err := validatePriorKeys(d, owned.PriorValues); err != nil {
			return nil, err
		}
	} else if len(owned.PriorValues) != 0 {
		return nil, errors.New("integration: unknown prior receipt cannot contain prior values")
	}
	if err := validateManagedKeys(d, owned.Paths); err != nil {
		return nil, err
	}
	needsMutation := false
	authorization := &model.IntegrationAuthorization{Version: 1, Fields: make(map[string]model.IntegrationFieldAuthorization)}
	for _, target := range targets {
		doc, _, err := a.readDocument(d, target)
		if err != nil {
			return nil, err
		}
		for pointer, desired := range d.fields {
			key := fieldKey(target.relative, pointer)
			current, err := doc.get(pointer)
			if err != nil {
				return nil, err
			}
			desiredDigest, err := digestValue(fieldValue{Exists: true, Value: desired})
			if err != nil {
				return nil, err
			}
			last, hasLast := owned.Paths[key]
			if !hasLast || last != desiredDigest {
				return nil, fmt.Errorf("integration: last managed receipt mismatch for %s%s", target.relative, pointer)
			}
			currentDigest, err := digestValue(current)
			if err != nil {
				return nil, err
			}
			matchesPrior := false
			if owned.PriorUnknown {
				if current.Exists && currentDigest != last {
					return nil, fmt.Errorf("integration conflict at %s%s: refusing to prune a user edit", target.relative, pointer)
				}
			} else {
				prior, ok, err := decodePrior(owned.PriorValues[key])
				if err != nil {
					return nil, err
				}
				if !ok {
					return nil, fmt.Errorf("integration: missing prior receipt for %s%s", target.relative, pointer)
				}
				matchesPrior, err = sameField(current, prior)
				if err != nil {
					return nil, err
				}
				if currentDigest != last && !matchesPrior {
					return nil, fmt.Errorf("integration conflict at %s%s: refusing to prune a user edit", target.relative, pointer)
				}
			}
			needsMutation = needsMutation || (owned.PriorUnknown && current.Exists) || (!owned.PriorUnknown && !matchesPrior)
			currentState, err := pathState(current)
			if err != nil {
				return nil, err
			}
			authorization.Fields[key] = model.IntegrationFieldAuthorization{Current: currentState, DesiredDigest: desiredDigest, LastManagedDigest: last}
		}
	}
	if needsMutation && d.handler == HandlerJetendardOrca && a.appRunning("Orca") {
		return nil, errors.New("integration deferred: Orca is running; quit Orca and retry")
	}
	op := operation(item, model.OperationPrune)
	op.Removes = []string{item.Package}
	op.IntegrationAuthorization = authorization
	return []model.Operation{op}, nil
}

func (a *Adapter) Execute(context.Context, model.Operation) model.OperationResult {
	return model.OperationResult{Detail: "integration: signed resource is required", FinishedAt: time.Now().UTC()}
}

func (a *Adapter) ExecuteResource(ctx context.Context, item model.Resource, op model.Operation) model.OperationResult {
	result := model.OperationResult{OperationID: op.ID, ResourceID: op.ResourceID, FinishedAt: time.Now().UTC()}
	if err := a.execute(ctx, item, op); err != nil {
		result.Detail = err.Error()
		return result
	}
	result.Success = true
	result.Detail = "integration fields [redacted] verified"
	return result
}

func (a *Adapter) execute(ctx context.Context, item model.Resource, op model.Operation) error {
	if op.ResourceID != item.ID || op.Provider != item.Provider || op.Package != item.Package {
		return errors.New("integration: operation identity mismatch")
	}
	if err := a.authorize(op); err != nil {
		return err
	}
	d, err := a.declaration(item)
	if err != nil {
		return err
	}
	if d.handler == HandlerKarabinerOpener {
		if op.Kind == model.OperationPrune {
			a.markPruned(item.ID)
			return nil
		}
		if a.Karabiner == nil {
			return errors.New("integration: Karabiner action is unavailable")
		}
		if err := a.Karabiner.Open(ctx); err != nil {
			return err
		}
		return nil
	}
	if op.Kind == model.OperationPrune {
		if !sameStrings(op.Removes, []string{item.Package}) {
			return errors.New("integration: prune authority mismatch")
		}
		owned, err := a.ownership(item)
		if err != nil {
			return err
		}
		if err := a.validateMutation(d, op, owned, true); err != nil {
			return err
		}
		needsMutation, err := a.requiresRestore(d, owned)
		if err != nil {
			return err
		}
		if needsMutation && d.handler == HandlerJetendardOrca && a.appRunning("Orca") {
			return errors.New("integration deferred: Orca is running; quit Orca and retry")
		}
		if err := a.restore(d, owned, op); err != nil {
			return err
		}
		a.markPruned(item.ID)
		return nil
	}
	if len(op.Removes) != 0 {
		return errors.New("integration: non-prune operation cannot remove packages")
	}
	targets, err := a.targets(d)
	if err != nil {
		return err
	}
	needsMutation, err := a.requiresApply(d, targets)
	if err != nil {
		return err
	}
	if needsMutation && d.handler == HandlerJetendardOrca && a.appRunning("Orca") {
		return errors.New("integration deferred: Orca is running; quit Orca and retry")
	}
	owned, err := a.ownership(item)
	if err != nil {
		return err
	}
	if err := a.validateMutation(d, op, owned, false); err != nil {
		return err
	}
	if (owned.ResourceID == "" || d.pathGlob != "") && !owned.PriorUnknown {
		priors, err := a.captureMissing(d, targets, owned.PriorValues)
		if err != nil {
			return err
		}
		if owned.ResourceID == "" {
			owned = model.Ownership{ResourceID: item.ID, Provider: item.Provider, Package: item.Package, Paths: map[string]string{}, PriorValues: priors}
		} else {
			owned.PriorValues = priors
		}
		if err := a.State.PutOwnership(owned); err != nil {
			return fmt.Errorf("integration: persist prior values: %w", err)
		}
	}
	if err := a.apply(d, targets, op, owned); err != nil {
		return err
	}
	if owned.Paths == nil {
		owned.Paths = make(map[string]string)
	}
	for key, field := range op.IntegrationAuthorization.Fields {
		owned.Paths[key] = field.DesiredDigest
	}
	if err := a.State.PutOwnership(owned); err != nil {
		return fmt.Errorf("integration: persist managed field identities: %w", err)
	}
	return nil
}

func (a *Adapter) validateMutation(d declaration, op model.Operation, owned model.Ownership, prune bool) error {
	auth := op.IntegrationAuthorization
	if auth == nil || auth.Version != 1 {
		return errors.New("integration: operation field authorization is required")
	}
	targets, err := a.targets(d)
	if err != nil {
		return err
	}
	expected := make(map[string]struct{})
	for _, target := range targets {
		doc, _, err := a.readDocument(d, target)
		if err != nil {
			return err
		}
		for pointer, desired := range d.fields {
			key := fieldKey(target.relative, pointer)
			expected[key] = struct{}{}
			field, ok := auth.Fields[key]
			if !ok {
				return fmt.Errorf("integration conflict: operation omits %s", key)
			}
			desiredDigest, err := digestValue(fieldValue{Exists: true, Value: desired})
			if err != nil {
				return err
			}
			if field.DesiredDigest != desiredDigest {
				return errors.New("integration: operation desired value does not match signed declaration")
			}
			stored, hasStored := owned.Paths[key]
			if prune {
				if !hasStored || stored != field.LastManagedDigest {
					return errors.New("integration: prune last-managed receipt mismatch")
				}
			} else if hasStored && stored != field.LastManagedDigest && stored != desiredDigest {
				return errors.New("integration: operation last-managed receipt mismatch")
			}
			current, err := doc.get(pointer)
			if err != nil {
				return err
			}
			if err := validateCurrentMutation(key, current, field, owned, prune); err != nil {
				return fmt.Errorf("integration conflict at %s%s: field changed after plan", target.relative, pointer)
			}
		}
	}
	if len(expected) != len(auth.Fields) {
		return errors.New("integration: operation authorizes undeclared fields")
	}
	return nil
}

func validateCurrentMutation(key string, current fieldValue, field model.IntegrationFieldAuthorization, owned model.Ownership, prune bool) error {
	allowed, err := samePathState(current, field.Current)
	if err != nil {
		return err
	}
	if prune {
		prior, ok, err := decodePrior(owned.PriorValues[key])
		if err != nil {
			return err
		}
		if ok {
			matches, err := sameField(current, prior)
			if err != nil {
				return err
			}
			allowed = allowed || matches
		}
	} else {
		digest, err := digestValue(current)
		if err != nil {
			return err
		}
		allowed = allowed || digest == field.DesiredDigest
	}
	if !allowed {
		return errors.New("field changed")
	}
	return nil
}

func (a *Adapter) requiresApply(d declaration, targets []target) (bool, error) {
	for _, target := range targets {
		doc, _, err := a.readDocument(d, target)
		if err != nil {
			return false, err
		}
		for pointer, desired := range d.fields {
			current, err := doc.get(pointer)
			if err != nil {
				return false, err
			}
			matches, err := sameField(current, fieldValue{Exists: true, Value: desired})
			if err != nil {
				return false, err
			}
			if !matches {
				return true, nil
			}
		}
	}
	return false, nil
}
func (a *Adapter) requiresRestore(d declaration, owned model.Ownership) (bool, error) {
	targets, err := a.targets(d)
	if err != nil {
		return false, err
	}
	for _, target := range targets {
		doc, _, err := a.readDocument(d, target)
		if err != nil {
			return false, err
		}
		for pointer := range d.fields {
			if owned.PriorUnknown {
				current, err := doc.get(pointer)
				if err != nil {
					return false, err
				}
				if current.Exists {
					return true, nil
				}
				continue
			}
			prior, ok, err := decodePrior(owned.PriorValues[fieldKey(target.relative, pointer)])
			if err != nil {
				return false, err
			}
			if !ok {
				return false, errors.New("integration: missing prior receipt")
			}
			current, err := doc.get(pointer)
			if err != nil {
				return false, err
			}
			matches, err := sameField(current, prior)
			if err != nil {
				return false, err
			}
			if !matches {
				return true, nil
			}
		}
	}
	return false, nil
}

func (a *Adapter) captureMissing(d declaration, targets []target, existing map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	priors := make(map[string]json.RawMessage, len(existing))
	for key, value := range existing {
		priors[key] = append(json.RawMessage(nil), value...)
	}
	for _, target := range targets {
		doc, _, err := a.readDocument(d, target)
		if err != nil {
			return nil, err
		}
		for pointer := range d.fields {
			key := fieldKey(target.relative, pointer)
			if _, ok := priors[key]; ok {
				continue
			}
			value, err := doc.get(pointer)
			if err != nil {
				return nil, err
			}
			raw, err := encodePrior(value)
			if err != nil {
				return nil, err
			}
			priors[key] = raw
		}
	}
	return priors, nil
}

func (a *Adapter) apply(d declaration, targets []target, op model.Operation, owned model.Ownership) error {
	type change struct {
		target target
		doc    *document
	}
	changes := make([]change, 0, len(targets))
	for _, target := range targets {
		doc, _, err := a.readDocument(d, target)
		if err != nil {
			return err
		}
		dirty := false
		for pointer, value := range d.fields {
			current, err := doc.get(pointer)
			if err != nil {
				return err
			}
			key := fieldKey(target.relative, pointer)
			field, ok := op.IntegrationAuthorization.Fields[key]
			if !ok {
				return errors.New("integration: missing field authorization")
			}
			if err := validateCurrentMutation(key, current, field, owned, false); err != nil {
				return fmt.Errorf("integration conflict at %s%s: field changed immediately before mutation", target.relative, pointer)
			}
			matches, err := sameField(current, fieldValue{Exists: true, Value: value})
			if err != nil {
				return err
			}
			if matches {
				continue
			}
			dirty = true
			if err := doc.set(pointer, value); err != nil {
				return err
			}
		}
		if dirty {
			changes = append(changes, change{target, doc})
		}
	}
	for _, change := range changes {
		contents, err := change.doc.bytes()
		if err != nil {
			return err
		}
		if err := a.atomicWrite(change.target.absolute, contents); err != nil {
			return err
		}
	}
	return nil
}

func (a *Adapter) restore(d declaration, owned model.Ownership, op model.Operation) error {
	targets, err := a.targets(d)
	if err != nil {
		return err
	}
	type change struct {
		target target
		doc    *document
	}
	var changes []change
	for _, target := range targets {
		doc, exists, err := a.readDocument(d, target)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		dirty := false
		for pointer := range d.fields {
			current, err := doc.get(pointer)
			if err != nil {
				return err
			}
			key := fieldKey(target.relative, pointer)
			field, ok := op.IntegrationAuthorization.Fields[key]
			if !ok {
				return errors.New("integration: missing field authorization")
			}
			if err := validateCurrentMutation(key, current, field, owned, true); err != nil {
				return fmt.Errorf("integration conflict at %s%s: field changed immediately before prune", target.relative, pointer)
			}
			if owned.PriorUnknown {
				if current.Exists {
					if err := doc.remove(pointer); err != nil {
						return err
					}
					dirty = true
				}
				continue
			}
			prior, ok, err := decodePrior(owned.PriorValues[key])
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("integration: missing prior receipt for %s%s", target.relative, pointer)
			}
			if prior.Exists {
				var err error
				if len(prior.Raw) > 0 {
					err = doc.setRaw(pointer, prior.Raw)
				} else {
					err = doc.set(pointer, prior.Value)
				}
				if err != nil {
					return err
				}
			} else {
				if err := doc.remove(pointer); err != nil {
					return err
				}
			}
			dirty = true
		}
		if owned.PriorUnknown && dirty {
			if err := archiveUnknownPrior(target.absolute); err != nil {
				return err
			}
		}
		if dirty {
			changes = append(changes, change{target, doc})
		}
	}
	for _, change := range changes {
		contents, err := change.doc.bytes()
		if err != nil {
			return err
		}
		if err := a.atomicWrite(change.target.absolute, contents); err != nil {
			return err
		}
	}
	return nil
}

func archiveUnknownPrior(path string) error {
	contents, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("integration: read unknown-prior backup source: %w", err)
	}
	backup := path + unknownPriorBackupSuffix
	file, err := os.OpenFile(backup, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		info, statErr := os.Lstat(backup)
		if statErr != nil || !info.Mode().IsRegular() {
			return errors.New("integration: unknown-prior backup path is unsafe")
		}
		existing, readErr := os.ReadFile(backup)
		if readErr != nil || !bytes.Equal(existing, contents) {
			return errors.New("integration: unknown-prior backup does not match current source")
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("integration: create unknown-prior backup: %w", err)
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.Remove(backup)
		}
	}()
	if _, err := file.Write(contents); err != nil {
		_ = file.Close()
		return fmt.Errorf("integration: write unknown-prior backup: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("integration: sync unknown-prior backup: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("integration: close unknown-prior backup: %w", err)
	}
	complete = true
	return nil
}

func (a *Adapter) declaration(item model.Resource) (declaration, error) {
	if item.Type != model.ResourceIntegration {
		return declaration{}, errors.New("integration: resource type mismatch")
	}
	d := declaration{handler: item.Metadata[MetadataHandler], format: item.Metadata[MetadataFormat], path: item.Metadata[MetadataPath], pathGlob: item.Metadata[MetadataPathGlob]}
	allowed := false
	switch item.Provider {
	case ProviderJSONFields:
		allowed = d.handler == HandlerFields || d.handler == HandlerJetendardZed || d.handler == HandlerJetendardOrca
	case ProviderPlistFields:
		allowed = d.handler == HandlerFields
	case ProviderKarabiner:
		allowed = d.handler == HandlerKarabinerOpener
	}
	if !allowed {
		return declaration{}, fmt.Errorf("integration: unsupported compiled handler %q for provider %q", d.handler, item.Provider)
	}
	if d.handler == HandlerKarabinerOpener {
		if item.Metadata[MetadataFields] != "" || d.path != "" || d.pathGlob != "" {
			return declaration{}, errors.New("integration: Karabiner opener cannot declare owned fields")
		}
		return d, nil
	}
	if (d.path == "") == (d.pathGlob == "") {
		return declaration{}, errors.New("integration: exactly one path or pathGlob is required")
	}
	if d.format == "" {
		if item.Provider == ProviderPlistFields {
			d.format = "plist"
		} else {
			d.format = "json"
		}
	}
	if item.Provider == ProviderJSONFields && d.format != "json" && d.format != "jsonc" {
		return declaration{}, errors.New("integration: JSON provider format mismatch")
	}
	if item.Provider == ProviderPlistFields && d.format != "plist" {
		return declaration{}, errors.New("integration: plist provider format mismatch")
	}
	decoder := json.NewDecoder(strings.NewReader(item.Metadata[MetadataFields]))
	decoder.UseNumber()
	if err := decoder.Decode(&d.fields); err != nil {
		return declaration{}, fmt.Errorf("integration: malformed field declaration: %w", err)
	}
	if len(d.fields) == 0 {
		return declaration{}, errors.New("integration: no fields declared")
	}
	for pointer := range d.fields {
		if _, err := pointerParts(pointer); err != nil {
			return declaration{}, err
		}
	}
	return d, nil
}

func (a *Adapter) targets(d declaration) ([]target, error) {
	if a.Home == "" {
		return nil, errors.New("integration: home is required")
	}
	relatives := []string{d.path}
	if d.pathGlob != "" {
		pattern, err := a.safePath(d.pathGlob, true)
		if err != nil {
			return nil, err
		}
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, err
		}
		relatives = nil
		for _, match := range matches {
			rel, err := filepath.Rel(a.Home, match)
			if err != nil {
				return nil, err
			}
			relatives = append(relatives, filepath.ToSlash(rel))
		}
		sort.Strings(relatives)
	}
	result := make([]target, 0, len(relatives))
	for _, relative := range relatives {
		absolute, err := a.safePath(relative, false)
		if err != nil {
			return nil, err
		}
		result = append(result, target{relative: filepath.ToSlash(filepath.Clean(relative)), absolute: absolute})
	}
	return result, nil
}

func (a *Adapter) safePath(relative string, allowGlob bool) (string, error) {
	if relative == "" || filepath.IsAbs(relative) {
		return "", fmt.Errorf("integration: unsafe path %q", relative)
	}
	clean := filepath.Clean(relative)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("integration: unsafe path %q", relative)
	}
	if !allowGlob && strings.ContainsAny(clean, "*?[") {
		return "", fmt.Errorf("integration: glob is not allowed in path %q", relative)
	}
	abs := filepath.Join(a.Home, clean)
	limit := abs
	if allowGlob {
		for strings.ContainsAny(filepath.Base(limit), "*?[") {
			limit = filepath.Dir(limit)
		}
	} else {
		limit = filepath.Dir(limit)
	}
	for current := limit; ; current = filepath.Dir(current) {
		if current == a.Home {
			break
		}
		info, err := os.Lstat(current)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("integration: symlink parent %s", current)
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", errors.New("integration: path escapes home")
		}
	}
	return abs, nil
}

type document struct {
	format, text string
	root         map[string]any
	plist        *plistSpanDocument
}

func (a *Adapter) readDocument(d declaration, target target) (*document, bool, error) {
	contents, err := os.ReadFile(target.absolute)
	exists := err == nil
	if errors.Is(err, os.ErrNotExist) {
		contents = nil
		err = nil
	} else if err != nil {
		return nil, false, err
	}
	if exists {
		info, err := os.Lstat(target.absolute)
		if err != nil {
			return nil, false, err
		}
		if !info.Mode().IsRegular() {
			return nil, false, fmt.Errorf("integration: config is not a regular file: %s", target.relative)
		}
	}
	doc := &document{format: d.format}
	switch d.format {
	case "json":
		if !exists {
			doc.root = make(map[string]any)
		} else {
			doc.root, err = decodeJSON(contents)
		}
	case "jsonc":
		if !exists {
			doc.text = "{\n}\n"
		} else {
			doc.text = string(contents)
		}
		_, err = parseJSONC(doc.text)
	case "plist":
		if !exists {
			doc.text = "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<plist version=\"1.0\">\n<dict>\n</dict>\n</plist>\n"
		} else {
			doc.text = string(contents)
		}
		doc.plist, err = parsePlistSpans(doc.text)
	}
	if err != nil {
		return nil, false, fmt.Errorf("integration: malformed %s config %s: %w", d.format, target.relative, err)
	}
	return doc, exists, nil
}
func (d *document) get(pointer string) (fieldValue, error) {
	if d.format == "jsonc" {
		return jsoncGet(d.text, pointer)
	}
	if d.format == "plist" {
		return d.plist.get(pointer)
	}
	return getField(d.root, pointer)
}
func (d *document) set(pointer string, value any) error {
	if raw, ok := value.(json.RawMessage); ok {
		var decoded any
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.UseNumber()
		if err := decoder.Decode(&decoded); err != nil {
			return err
		}
		value = decoded
	}
	if d.format == "jsonc" {
		text, err := jsoncSet(d.text, pointer, value)
		if err == nil {
			d.text = text
		}
		return err
	}
	if d.format == "plist" {
		text, err := d.plist.set(pointer, value)
		if err == nil {
			d.text = text
			d.plist, err = parsePlistSpans(text)
		}
		return err
	}
	return setField(d.root, pointer, value)
}
func (d *document) setRaw(pointer string, raw []byte) error {
	if d.format != "plist" {
		return d.set(pointer, json.RawMessage(raw))
	}
	text, err := d.plist.setRaw(pointer, raw)
	if err == nil {
		d.text = text
		d.plist, err = parsePlistSpans(text)
	}
	return err
}
func (d *document) remove(pointer string) error {
	if d.format == "jsonc" {
		text, err := jsoncRemove(d.text, pointer)
		d.text = text
		return err
	}
	if d.format == "plist" {
		text, err := d.plist.remove(pointer)
		if err == nil {
			d.text = text
			d.plist, err = parsePlistSpans(text)
		}
		return err
	}
	return removeField(d.root, pointer)
}
func (d *document) bytes() ([]byte, error) {
	switch d.format {
	case "jsonc":
		return []byte(d.text), nil
	case "plist":
		return []byte(d.text), nil
	default:
		return encodeJSON(d.root)
	}
}

func (a *Adapter) atomicWrite(path string, contents []byte) error {
	if _, err := a.safePath(mustRelative(a.Home, path), false); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return errors.New("integration: target is not regular")
		}
		mode = info.Mode().Perm()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".terrapod-integration-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if err := file.Chmod(mode); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(contents); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func (a *Adapter) ownership(item model.Resource) (model.Ownership, error) {
	if a.State == nil {
		return model.Ownership{}, errors.New("integration: state store is required")
	}
	snapshot, err := a.State.Snapshot()
	if err != nil {
		return model.Ownership{}, err
	}
	owned := snapshot.Ownership[item.ID]
	if owned.ResourceID != "" && (owned.ResourceID != item.ID || owned.Provider != item.Provider || owned.Package != item.Package) {
		return model.Ownership{}, errors.New("integration: ownership identity mismatch")
	}
	return owned, nil
}
func (a *Adapter) authorize(op model.Operation) error {
	if a.State == nil {
		return errors.New("integration: state store is required")
	}
	snapshot, err := a.State.Snapshot()
	if err != nil {
		return err
	}
	if snapshot.ActiveJournal == nil || snapshot.ActiveJournal.Status != "active" {
		return errors.New("integration: active journal authority is required")
	}
	matches := 0
	for _, planned := range snapshot.ActiveJournal.Plan.Operations {
		if reflect.DeepEqual(planned, op) {
			matches++
		}
	}
	if matches != 1 {
		return errors.New("integration: operation is not authorized by active journal")
	}
	return nil
}
func (a *Adapter) appRunning(name string) bool {
	if a.AppRunning == nil {
		return false
	}
	return a.AppRunning(name)
}
func (a *Adapter) markPruned(id model.ResourceID) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.pruned == nil {
		a.pruned = make(map[model.ResourceID]bool)
	}
	a.pruned[id] = true
}

func (a *Adapter) inspectKarabiner(ctx context.Context, item model.Resource) (model.Observation, error) {
	if a.Karabiner == nil {
		return model.Observation{Present: true, Healthy: true, Provider: item.Provider, Package: item.Package, Detail: "Karabiner action unavailable; no general state is owned"}, nil
	}
	raw, err := a.Karabiner.Guidance(ctx)
	if err != nil {
		return model.Observation{}, err
	}
	need, err := karabinerNeedsAction(raw)
	if err != nil {
		return model.Observation{}, err
	}
	return model.Observation{Present: true, Healthy: !need, Provider: item.Provider, Package: item.Package, Detail: "Karabiner guidance [redacted]"}, nil
}
func karabinerNeedsAction(raw []byte) (bool, error) {
	if len(raw) == 0 {
		return false, nil
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return false, fmt.Errorf("integration: malformed Karabiner guidance: %w", err)
	}
	for _, key := range []string{"current_setup", "current_alert"} {
		if value, ok := root[key].(string); ok && value != "" && value != "none" {
			return true, nil
		}
	}
	for _, pointer := range []string{
		"/core_service_daemon_state/bundle_permission_check_result/accessibility_process_trusted",
		"/core_service_daemon_state/bundle_permission_check_result/iohid_listen_event_allowed",
		"/core_service_daemon_state/current_process_permission_check_result/accessibility_process_trusted",
		"/core_service_daemon_state/current_process_permission_check_result/iohid_listen_event_allowed",
		"/core_service_daemon_state/driver_activated",
		"/core_service_daemon_state/driver_connected",
		"/core_service_daemon_state/virtual_hid_device_service_client_connected",
		"/core_service_daemon_state/virtual_hid_keyboard_ready",
		"/guidance_context/core_agents_enabled", "/guidance_context/core_agents_running",
		"/guidance_context/core_daemons_enabled", "/guidance_context/core_daemons_running",
		"/guidance_context/services_enabled", "/guidance_context/services_running",
	} {
		value, err := getField(root, pointer)
		if err != nil {
			return false, err
		}
		if value.Exists && value.Value == false {
			return true, nil
		}
	}
	return false, nil
}

func operation(item model.Resource, kind model.OperationKind) model.Operation {
	return model.Operation{ID: "integration:" + string(item.ID) + ":" + string(kind), ResourceID: item.ID, Kind: kind, Provider: item.Provider, Package: item.Package, Detail: "apply fields [redacted]"}
}
func fieldKey(path, pointer string) string {
	return filepath.ToSlash(filepath.Clean(path)) + "#" + pointer
}
func encodePrior(value fieldValue) (json.RawMessage, error) {
	var raw json.RawMessage
	typeName := ""
	if value.Exists {
		switch value.Value.(type) {
		case []byte:
			typeName = "data"
		case time.Time:
			typeName = "date"
		}
		if len(value.Raw) > 0 {
			typeName = "plist-raw"
			value.Value = base64.StdEncoding.EncodeToString(value.Raw)
		}
		contents, err := json.Marshal(value.Value)
		if err != nil {
			return nil, err
		}
		raw = contents
	}
	return json.Marshal(priorValue{Exists: value.Exists, Type: typeName, Value: raw})
}
func decodePrior(raw json.RawMessage) (fieldValue, bool, error) {
	if len(raw) == 0 {
		return fieldValue{}, false, nil
	}
	var prior priorValue
	if err := json.Unmarshal(raw, &prior); err != nil {
		return fieldValue{}, false, err
	}
	if !prior.Exists {
		return fieldValue{}, true, nil
	}
	var value any
	decoder := json.NewDecoder(strings.NewReader(string(prior.Value)))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return fieldValue{}, false, err
	}
	switch prior.Type {
	case "data":
		encoded, ok := value.(string)
		if !ok {
			return fieldValue{}, false, errors.New("integration: invalid data prior")
		}
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return fieldValue{}, false, err
		}
		value = decoded
	case "date":
		encoded, ok := value.(string)
		if !ok {
			return fieldValue{}, false, errors.New("integration: invalid date prior")
		}
		parsed, err := time.Parse(time.RFC3339, encoded)
		if err != nil {
			return fieldValue{}, false, err
		}
		value = parsed
	case "plist-raw":
		encoded, ok := value.(string)
		if !ok {
			return fieldValue{}, false, errors.New("integration: invalid plist prior")
		}
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return fieldValue{}, false, err
		}
		parsed, err := parsePlistSpans("<?xml version=\"1.0\"?><plist><dict><key>x</key>" + string(decoded) + "</dict></plist>")
		if err != nil {
			return fieldValue{}, false, err
		}
		field, err := parsed.get("/x")
		if err != nil {
			return fieldValue{}, false, err
		}
		value = field.Value
		return fieldValue{Exists: true, Value: value, Raw: decoded}, true, nil
	case "":
	default:
		return fieldValue{}, false, errors.New("integration: invalid prior type")
	}
	return fieldValue{Exists: true, Value: value}, true, nil
}

func validatePriorKeys(d declaration, values map[string]json.RawMessage) error {
	for key, raw := range values {
		split := strings.LastIndex(key, "#")
		if split < 0 {
			return errors.New("integration: malformed prior key")
		}
		relative, pointer := key[:split], key[split+1:]
		if _, ok := d.fields[pointer]; !ok {
			return fmt.Errorf("integration: prior field %q is outside signed declaration", pointer)
		}
		if d.path != "" && relative != filepath.ToSlash(filepath.Clean(d.path)) {
			return errors.New("integration: prior path is outside signed declaration")
		}
		if d.pathGlob != "" {
			matched, err := filepath.Match(filepath.FromSlash(d.pathGlob), filepath.FromSlash(relative))
			if err != nil || !matched {
				return errors.New("integration: prior path is outside signed glob")
			}
		}
		if _, _, err := decodePrior(raw); err != nil {
			return err
		}
	}
	return nil
}
func validateManagedKeys(d declaration, values map[string]string) error {
	for key, digest := range values {
		split := strings.LastIndex(key, "#")
		if split < 0 || len(digest) != 71 || !strings.HasPrefix(digest, "sha256:") {
			return errors.New("integration: malformed last-managed receipt")
		}
		if _, err := hex.DecodeString(digest[7:]); err != nil {
			return errors.New("integration: malformed last-managed digest")
		}
		relative, pointer := key[:split], key[split+1:]
		if _, ok := d.fields[pointer]; !ok {
			return errors.New("integration: last-managed field is outside signed declaration")
		}
		if d.path != "" && relative != filepath.ToSlash(filepath.Clean(d.path)) {
			return errors.New("integration: last-managed path is outside signed declaration")
		}
		if d.pathGlob != "" {
			matched, err := filepath.Match(filepath.FromSlash(d.pathGlob), filepath.FromSlash(relative))
			if err != nil || !matched {
				return errors.New("integration: last-managed path is outside signed glob")
			}
		}
	}
	return nil
}
func sameField(a, b fieldValue) (bool, error) {
	if a.Exists != b.Exists {
		return false, nil
	}
	if !a.Exists {
		return true, nil
	}
	return sameValue(a.Value, b.Value)
}
func sameValue(a, b any) (bool, error) {
	left, err := json.Marshal(a)
	if err != nil {
		return false, fmt.Errorf("canonicalize value: %w", err)
	}
	right, err := json.Marshal(b)
	if err != nil {
		return false, fmt.Errorf("canonicalize value: %w", err)
	}
	return string(left) == string(right), nil
}
func digestValue(value fieldValue) (string, error) {
	raw, err := json.Marshal(struct {
		Exists bool `json:"exists"`
		Value  any  `json:"value,omitempty"`
	}{value.Exists, value.Value})
	if err != nil {
		return "", fmt.Errorf("canonicalize field value: %w", err)
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
func pathState(value fieldValue) (model.ManagedFilePathState, error) {
	digest, err := digestValue(value)
	if err != nil {
		return model.ManagedFilePathState{}, err
	}
	return model.ManagedFilePathState{Exists: value.Exists, Digest: digest}, nil
}
func samePathState(value fieldValue, state model.ManagedFilePathState) (bool, error) {
	digest, err := digestValue(value)
	if err != nil {
		return false, err
	}
	return value.Exists == state.Exists && digest == state.Digest, nil
}
func mustRelative(root, path string) string { relative, _ := filepath.Rel(root, path); return relative }
func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
