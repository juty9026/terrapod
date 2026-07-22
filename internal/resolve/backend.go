package resolve

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync"

	"github.com/juty9026/terrapod/internal/legacydecl"
	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/provider"
	"github.com/juty9026/terrapod/internal/provider/apt"
	"github.com/juty9026/terrapod/internal/reconcile"
	"github.com/juty9026/terrapod/internal/state"
)

type RebuildFunc func(context.Context) (reconcile.ApplyInput, error)

type PackageBackend struct {
	rebuild   RebuildFunc
	engine    *reconcile.Engine
	apt       *apt.Adapter
	privilege provider.Privilege
	mu        sync.Mutex
	attempts  map[[16]byte]*preparedAttempt
}

type preparedAttempt struct {
	details   attemptDetails
	apt       *apt.Resolution
	blockers  []string
	privilege *oncePrivilege
	engine    *reconcile.Engine
	removed   bool
	verified  bool
}

func NewPackageBackend(rebuild RebuildFunc, engine *reconcile.Engine, aptAdapter *apt.Adapter, privilege provider.Privilege) (*PackageBackend, error) {
	if rebuild == nil || engine == nil || aptAdapter == nil || isNilInterface(privilege) {
		return nil, errors.New("resolve: rebuild, engine, APT adapter, and privilege are required")
	}
	return &PackageBackend{rebuild: rebuild, engine: engine, apt: aptAdapter, privilege: privilege, attempts: make(map[[16]byte]*preparedAttempt)}, nil
}

func (b *PackageBackend) Prepare(ctx context.Context, id model.ResourceID, lock *state.Lock) (Attempt, error) {
	if err := lock.ValidateHeld(b.engine.LockDir); err != nil {
		return Attempt{}, fmt.Errorf("resolve: validate prepare lock: %w", err)
	}
	input, err := b.rebuild(ctx)
	if err != nil {
		return Attempt{}, fmt.Errorf("resolve: rebuild reconciliation: %w", err)
	}
	item, operation, root, err := b.selectAPTConflict(id, input)
	if err != nil {
		return Attempt{}, err
	}
	if _, ok := b.engine.Registry.Lookup(item.Type, item.Provider); !ok {
		return Attempt{}, fmt.Errorf("resolve: no signed adapter for resource %q", id)
	}
	input = scopeInput(input, id, operation)
	prune := model.Operation{ID: "resolve-" + operation.ID, ResourceID: item.ID, Kind: model.OperationPrune, Provider: "apt", Package: root, RequiresPrivilege: true, Removes: []string{root}}
	aptCapability, changes, err := b.apt.PrepareResolution(ctx, prune)
	if err != nil {
		var noConflict *apt.ErrNoResolutionConflict
		if errors.As(err, &noConflict) {
			return Attempt{}, &ErrNotBlocked{ID: id}
		}
		return Attempt{}, err
	}
	blockers := blockersFrom(changes.Removes, operation.Removes)
	if len(blockers) == 0 {
		_ = b.apt.CancelResolution(aptCapability)
		return Attempt{}, &ErrNotBlocked{ID: id}
	}
	token, err := randomToken()
	if err != nil {
		_ = b.apt.CancelResolution(aptCapability)
		return Attempt{}, err
	}
	once := &oncePrivilege{underlying: b.privilege}
	engineCopy := *b.engine
	engineCopy.Privilege = once
	details := attemptDetails{input: input, operation: operation, changes: changes}
	attempt := Attempt{issuer: b, token: token}
	b.mu.Lock()
	if _, duplicate := b.attempts[token]; duplicate {
		b.mu.Unlock()
		_ = b.apt.CancelResolution(aptCapability)
		return Attempt{}, errors.New("resolve: duplicate package attempt")
	}
	b.attempts[token] = &preparedAttempt{details: details, apt: aptCapability, blockers: blockers, privilege: once, engine: &engineCopy}
	b.mu.Unlock()
	return attempt, nil
}

func (b *PackageBackend) Describe(attempt Attempt) (attemptDetails, error) {
	prepared, err := b.lookup(attempt)
	if err != nil {
		return attemptDetails{}, err
	}
	return cloneDetails(prepared.details), nil
}

func (b *PackageBackend) AcquirePrivilege(ctx context.Context, attempt Attempt) error {
	prepared, err := b.lookup(attempt)
	if err != nil {
		return err
	}
	if err := prepared.privilege.Acquire(ctx); err != nil {
		b.revoke(attempt)
		return err
	}
	return nil
}

func (b *PackageBackend) RemoveBlockers(ctx context.Context, attempt Attempt, blockers []string) error {
	prepared, err := b.lookup(attempt)
	if err != nil {
		return err
	}
	if !exactSorted(blockers, prepared.blockers) {
		b.revoke(attempt)
		return errors.New("resolve: confirmed blockers do not match package capability")
	}
	if err := b.apt.ExecuteResolution(ctx, prepared.apt, blockers); err != nil {
		b.revoke(attempt)
		return err
	}
	b.mu.Lock()
	if current := b.attempts[attempt.token]; current == prepared {
		prepared.removed = true
	}
	b.mu.Unlock()
	return nil
}

func (b *PackageBackend) VerifyBlockersAbsent(ctx context.Context, attempt Attempt, blockers []string) error {
	prepared, err := b.lookup(attempt)
	if err != nil {
		return err
	}
	if !prepared.removed || !exactSorted(blockers, prepared.blockers) {
		b.revoke(attempt)
		return errors.New("resolve: package blockers were not removed by this attempt")
	}
	if err := b.apt.VerifyResolutionAbsent(ctx, prepared.apt); err != nil {
		b.revoke(attempt)
		return err
	}
	b.mu.Lock()
	if current := b.attempts[attempt.token]; current == prepared {
		prepared.verified = true
	}
	b.mu.Unlock()
	return nil
}

func (b *PackageBackend) Reconcile(ctx context.Context, attempt Attempt, lock *state.Lock) (reconcile.Summary, error) {
	prepared, err := b.lookup(attempt)
	if err != nil {
		return reconcile.Summary{}, err
	}
	if !prepared.verified {
		b.revoke(attempt)
		return reconcile.Summary{}, errors.New("resolve: blockers are not verified absent")
	}
	if err := lock.ValidateHeld(b.engine.LockDir); err != nil {
		b.revoke(attempt)
		return reconcile.Summary{}, err
	}
	summary, applyErr := prepared.engine.ApplyInputHeld(ctx, prepared.details.input, lock)
	b.revoke(attempt)
	return summary, applyErr
}

func (b *PackageBackend) Cancel(attempt Attempt) error {
	if attempt.issuer != b {
		return errors.New("resolve: package attempt belongs to another backend")
	}
	b.revoke(attempt)
	return nil
}

func (b *PackageBackend) selectAPTConflict(id model.ResourceID, input reconcile.ApplyInput) (model.Resource, model.Operation, string, error) {
	var item model.Resource
	for _, candidate := range input.CurrentResources {
		if candidate.ID == id {
			if item.ID != "" {
				return model.Resource{}, model.Operation{}, "", errors.New("resolve: duplicate current resource")
			}
			item = candidate
		}
	}
	if item.ID == "" {
		return model.Resource{}, model.Operation{}, "", &ErrUnknownResource{ID: id}
	}
	var operation model.Operation
	for _, candidate := range input.Plan.Operations {
		if candidate.ResourceID != id {
			continue
		}
		if operation.ID != "" || candidate.Kind != model.OperationTransfer {
			return model.Resource{}, model.Operation{}, "", &ErrNotBlocked{ID: id}
		}
		operation = candidate
	}
	if operation.ID == "" {
		return model.Resource{}, model.Operation{}, "", &ErrNotBlocked{ID: id}
	}
	declarations, err := legacydecl.Parse(item)
	if err != nil {
		return model.Resource{}, model.Operation{}, "", err
	}
	root := ""
	for _, declaration := range declarations {
		if declaration.Kind != legacydecl.APT || (declaration.Profile != "" && declaration.Profile != input.Profile) || !containsString(operation.Removes, declaration.Package) {
			continue
		}
		if root != "" {
			return model.Resource{}, model.Operation{}, "", errors.New("resolve: multiple APT legacy roots are unsupported")
		}
		root = declaration.Package
	}
	if root == "" {
		return model.Resource{}, model.Operation{}, "", &ErrNotBlocked{ID: id}
	}
	return item, operation, root, nil
}

func (b *PackageBackend) lookup(attempt Attempt) (*preparedAttempt, error) {
	if attempt.issuer != b {
		return nil, errors.New("resolve: invalid package attempt")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	prepared := b.attempts[attempt.token]
	if prepared == nil {
		return nil, errors.New("resolve: package attempt is consumed or revoked")
	}
	return prepared, nil
}

func (b *PackageBackend) revoke(attempt Attempt) {
	if attempt.issuer != b {
		return
	}
	b.mu.Lock()
	prepared := b.attempts[attempt.token]
	delete(b.attempts, attempt.token)
	b.mu.Unlock()
	if prepared != nil {
		_ = b.apt.CancelResolution(prepared.apt)
	}
}

type oncePrivilege struct {
	underlying provider.Privilege
	mu         sync.Mutex
	acquired   bool
}

func (p *oncePrivilege) Acquire(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.acquired {
		return nil
	}
	if err := p.underlying.Acquire(ctx); err != nil {
		return err
	}
	p.acquired = true
	return nil
}

func randomToken() ([16]byte, error) {
	var token [16]byte
	_, err := rand.Read(token[:])
	return token, err
}

func blockersFrom(removals, authorized []string) []string {
	allowed := make(map[string]struct{}, len(authorized))
	for _, pkg := range authorized {
		allowed[pkg] = struct{}{}
	}
	var blockers []string
	for _, pkg := range removals {
		if _, ok := allowed[pkg]; !ok {
			blockers = append(blockers, pkg)
		}
	}
	sort.Strings(blockers)
	return blockers
}

func exactSorted(actual, expected []string) bool {
	if len(actual) != len(expected) {
		return false
	}
	copyActual := append([]string(nil), actual...)
	sort.Strings(copyActual)
	return reflect.DeepEqual(copyActual, expected)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func cloneDetails(details attemptDetails) attemptDetails {
	details.input.CurrentResources = append([]model.Resource(nil), details.input.CurrentResources...)
	details.input.EnabledIDs = append([]model.ResourceID(nil), details.input.EnabledIDs...)
	details.input.Plan.Operations = append([]model.Operation(nil), details.input.Plan.Operations...)
	details.changes = provider.ChangeSet{Installs: append([]string(nil), details.changes.Installs...), Upgrades: append([]string(nil), details.changes.Upgrades...), Removes: append([]string(nil), details.changes.Removes...)}
	return details
}

func scopeInput(input reconcile.ApplyInput, id model.ResourceID, operation model.Operation) reconcile.ApplyInput {
	input.EnabledIDs = []model.ResourceID{id}
	input.HistoricalResources = nil
	input.Plan = model.Plan{ID: input.Plan.ID + ":resolve:" + string(id), Release: input.Plan.Release, Operations: []model.Operation{operation}, Unavailable: map[model.ResourceID]string{}}
	return input
}

func isNilInterface(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	}
	return false
}

var _ Backend = (*PackageBackend)(nil)
