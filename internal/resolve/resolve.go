package resolve

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"sort"
	"strings"

	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/provider"
	"github.com/juty9026/terrapod/internal/reconcile"
	"github.com/juty9026/terrapod/internal/state"
)

// Attempt is an opaque backend-bound capability prepared while the exact state
// lock is held.
type Attempt struct {
	issuer Backend
	token  [16]byte
}

type attemptDetails struct {
	input     reconcile.ApplyInput
	operation model.Operation
	changes   provider.ChangeSet
}

// Backend owns the typed provider capabilities and reconciliation engine used
// for one resolution. Prepare, removal, verification, and reconciliation are
// all called while Resolver holds the exclusive state lock. Reconcile must use
// the held-lock engine entry point rather than acquiring a second lock.
type Backend interface {
	Prepare(context.Context, model.ResourceID, *state.Lock) (Attempt, error)
	Describe(Attempt) (attemptDetails, error)
	AcquirePrivilege(context.Context, Attempt) error
	RemoveBlockers(context.Context, Attempt, []string) error
	VerifyBlockersAbsent(context.Context, Attempt, []string) error
	Reconcile(context.Context, Attempt, *state.Lock) (reconcile.Summary, error)
	Cancel(Attempt) error
}

type Resolver struct {
	StateDir     string
	Backend      Backend
	EffectiveUID func() int
}

type Result struct {
	Blockers  []string
	Proceeded bool
	Summary   reconcile.Summary
}

type ErrUnknownResource struct{ ID model.ResourceID }

func (e *ErrUnknownResource) Error() string { return fmt.Sprintf("resolve: unknown resource %q", e.ID) }

type ErrNotBlocked struct{ ID model.ResourceID }

func (e *ErrNotBlocked) Error() string {
	return fmt.Sprintf("resolve: resource %q is not blocked", e.ID)
}

func (r *Resolver) Resolve(ctx context.Context, id model.ResourceID, input io.Reader, output io.Writer) (result Result, retErr error) {
	if err := validateID(id); err != nil {
		return result, err
	}
	geteuid := r.EffectiveUID
	if geteuid == nil {
		geteuid = os.Geteuid
	}
	if geteuid() == 0 {
		return result, errors.New("resolve: must run as a non-root user")
	}
	if r.StateDir == "" || r.Backend == nil {
		return result, errors.New("resolve: state directory and backend are required")
	}
	if input == nil || output == nil {
		return result, errors.New("resolve: prompt input and output are required")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}

	lock, err := state.Acquire(r.StateDir, "tpod resolve "+string(id))
	if err != nil {
		return result, fmt.Errorf("resolve: acquire state lock: %w", err)
	}
	defer func() { retErr = errors.Join(retErr, lock.Release()) }()

	attempt, err := r.Backend.Prepare(ctx, id, lock)
	if err != nil {
		return result, err
	}
	defer func() { retErr = errors.Join(retErr, r.Backend.Cancel(attempt)) }()
	details, err := r.Backend.Describe(attempt)
	if err != nil {
		return result, err
	}
	item, err := validateAttempt(id, details)
	if err != nil {
		return result, err
	}
	blockers, err := unmanagedBlockers(item, details)
	if err != nil {
		return result, err
	}
	if len(blockers) == 0 {
		return result, &ErrNotBlocked{ID: id}
	}
	result.Blockers = blockers
	if err := renderPrompt(output, id, blockers); err != nil {
		return result, err
	}
	confirmed, err := readConfirmation(input)
	if err != nil {
		return result, err
	}
	if !confirmed {
		return result, nil
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if details.operation.RequiresPrivilege {
		if err := r.Backend.AcquirePrivilege(ctx, attempt); err != nil {
			return result, fmt.Errorf("resolve: acquire privilege: %w", err)
		}
	}
	if err := r.Backend.RemoveBlockers(ctx, attempt, blockers); err != nil {
		return result, fmt.Errorf("resolve: remove blockers: %w", err)
	}
	if err := r.Backend.VerifyBlockersAbsent(ctx, attempt, blockers); err != nil {
		return result, fmt.Errorf("resolve: verify blockers absent: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	result.Summary, err = r.Backend.Reconcile(ctx, attempt, lock)
	if err != nil {
		return result, fmt.Errorf("resolve: reconcile %q: %w", id, err)
	}
	result.Proceeded = true
	return result, nil
}

func validateID(id model.ResourceID) error {
	item := model.Resource{ID: id, VersionPolicy: model.VersionTracked}
	if err := item.Validate(); err != nil {
		return fmt.Errorf("resolve: %q is not a stable resource ID", id)
	}
	return nil
}

func validateAttempt(id model.ResourceID, details attemptDetails) (model.Resource, error) {
	input, operation := details.input, details.operation
	if input.CatalogDigest == "" || input.Plan.ID == "" || !input.Profile.Supported() {
		return model.Resource{}, errors.New("resolve: prepared attempt lacks verified reconciliation input")
	}
	var item model.Resource
	for _, candidate := range input.CurrentResources {
		if candidate.ID != id {
			continue
		}
		if item.ID != "" {
			return model.Resource{}, errors.New("resolve: prepared input contains a duplicate resource")
		}
		item = candidate
	}
	if item.ID == "" {
		return model.Resource{}, &ErrUnknownResource{ID: id}
	}
	enabled := false
	for _, candidate := range input.EnabledIDs {
		if candidate == id {
			if enabled {
				return model.Resource{}, errors.New("resolve: prepared input contains a duplicate enabled resource")
			}
			enabled = true
		}
	}
	if !enabled {
		return model.Resource{}, &ErrUnknownResource{ID: id}
	}
	if item.Type != model.ResourcePackage || operation.Kind != model.OperationTransfer {
		return model.Resource{}, &ErrNotBlocked{ID: id}
	}
	if operation.ID == "" || operation.ResourceID != item.ID || operation.Provider != item.Provider || operation.Package != item.Package {
		return model.Resource{}, errors.New("resolve: prepared operation does not match signed resource")
	}
	found := false
	for _, planned := range input.Plan.Operations {
		if reflect.DeepEqual(planned, operation) {
			if found {
				return model.Resource{}, errors.New("resolve: prepared plan contains a duplicate blocked operation")
			}
			found = true
		}
	}
	if !found {
		return model.Resource{}, errors.New("resolve: prepared operation is not in the rebuilt plan")
	}
	return item, nil
}

func unmanagedBlockers(item model.Resource, details attemptDetails) ([]string, error) {
	err := provider.ValidateChangeSet(details.changes, item, details.operation.Removes)
	if err == nil {
		return nil, nil
	}
	var unmanaged *provider.ErrUnmanagedRemoval
	if !errors.As(err, &unmanaged) {
		return nil, err
	}
	unique := make(map[string]struct{}, len(unmanaged.IDs))
	for _, id := range unmanaged.IDs {
		if id == "" {
			return nil, errors.New("resolve: simulation contains an empty blocker")
		}
		unique[id] = struct{}{}
	}
	blockers := make([]string, 0, len(unique))
	for id := range unique {
		blockers = append(blockers, id)
	}
	sort.Strings(blockers)
	return blockers, nil
}

func renderPrompt(output io.Writer, id model.ResourceID, blockers []string) error {
	if _, err := fmt.Fprintln(output, "Unmanaged blockers:"); err != nil {
		return err
	}
	for _, blocker := range blockers {
		if _, err := fmt.Fprintf(output, "  %s\n", blocker); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(output, "Remove the listed blockers and reconcile %s? [y/N]", id)
	return err
}

func readConfirmation(input io.Reader) (bool, error) {
	answer, err := bufio.NewReader(input).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("resolve: read confirmation: %w", err)
	}
	answer = strings.TrimSpace(answer)
	return answer == "y" || answer == "Y" || answer == "yes" || answer == "YES", nil
}
