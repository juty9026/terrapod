package resolve

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/provider"
	"github.com/juty9026/terrapod/internal/reconcile"
	"github.com/juty9026/terrapod/internal/state"
)

func TestResolveRejectsUnknownAndNonBlockedResource(t *testing.T) {
	for _, test := range []struct {
		name string
		id   model.ResourceID
		err  error
		want string
	}{
		{"unknown", "core.missing", &ErrUnknownResource{ID: "core.missing"}, "unknown resource"},
		{"not blocked", "core.alpha", &ErrNotBlocked{ID: "core.alpha"}, "is not blocked"},
	} {
		t.Run(test.name, func(t *testing.T) {
			backend := &fakeBackend{prepareErr: test.err}
			resolver := testResolver(t, backend)
			var output bytes.Buffer
			_, err := resolver.Resolve(context.Background(), test.id, strings.NewReader("yes\n"), &output)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Resolve() error = %v, want %q", err, test.want)
			}
			if backend.removeCalls != 0 || backend.reconcileCalls != 0 || output.Len() != 0 {
				t.Fatalf("unexpected effects: remove=%d reconcile=%d output=%q", backend.removeCalls, backend.reconcileCalls, output.String())
			}
		})
	}
}

func TestResolveDisplaysEveryExactBlockerAndDefaultsToCancellation(t *testing.T) {
	backend := blockedBackend()
	resolver := testResolver(t, backend)
	var output bytes.Buffer

	result, err := resolver.Resolve(context.Background(), "core.alpha", strings.NewReader("\n"), &output)
	if err != nil {
		t.Fatal(err)
	}
	if result.Proceeded || backend.removeCalls != 0 || backend.verifyCalls != 0 || backend.reconcileCalls != 0 {
		t.Fatalf("cancel mutated: result=%#v backend=%#v", result, backend)
	}
	if !reflect.DeepEqual(result.Blockers, []string{"dependent-a", "dependent-z"}) {
		t.Fatalf("blockers = %v", result.Blockers)
	}
	want := "Unmanaged blockers:\n  dependent-a\n  dependent-z\nRemove the listed blockers and reconcile core.alpha? [y/N]"
	if output.String() != want {
		t.Fatalf("output = %q, want %q", output.String(), want)
	}
	if backend.cancelCalls != 1 {
		t.Fatalf("prepared capability cancellation calls = %d, want 1", backend.cancelCalls)
	}
	if matches, _ := filepath.Glob(filepath.Join(resolver.StateDir, "*declin*")); len(matches) != 0 {
		t.Fatalf("persistent decline marker created: %v", matches)
	}
}

func TestResolveAcceptsOnlyExplicitYesAndVerifiesBeforeReconcile(t *testing.T) {
	for _, answer := range []string{"y\n", "Y\n", "yes\n", "YES\n"} {
		t.Run(strings.TrimSpace(answer), func(t *testing.T) {
			backend := blockedBackend()
			resolver := testResolver(t, backend)
			var output bytes.Buffer

			result, err := resolver.Resolve(context.Background(), "core.alpha", strings.NewReader(answer), &output)
			if err != nil {
				t.Fatal(err)
			}
			if !result.Proceeded || !reflect.DeepEqual(backend.events, []string{"prepare", "privilege", "remove", "verify", "reconcile", "cancel"}) {
				t.Fatalf("result=%#v events=%v", result, backend.events)
			}
		})
	}

	for _, answer := range []string{"n\n", "no\n", "Yes\n", "force\n"} {
		t.Run("reject-"+strings.TrimSpace(answer), func(t *testing.T) {
			backend := blockedBackend()
			resolver := testResolver(t, backend)
			if result, err := resolver.Resolve(context.Background(), "core.alpha", strings.NewReader(answer), &bytes.Buffer{}); err != nil || result.Proceeded || backend.removeCalls != 0 {
				t.Fatalf("Resolve(%q) = %#v, %v; remove=%d", answer, result, err, backend.removeCalls)
			}
		})
	}
}

func TestResolveRefusesEssentialSimulationAndNeverPrompts(t *testing.T) {
	backend := &fakeBackend{prepareErr: errors.New("apt: refusing plan containing Essential package \"init\"")}
	resolver := testResolver(t, backend)
	var output bytes.Buffer

	_, err := resolver.Resolve(context.Background(), "core.alpha", strings.NewReader("yes\n"), &output)
	if err == nil || !strings.Contains(err.Error(), "Essential") {
		t.Fatalf("Resolve() error = %v", err)
	}
	if output.Len() != 0 || backend.removeCalls != 0 {
		t.Fatalf("essential refusal prompted or mutated: output=%q remove=%d", output.String(), backend.removeCalls)
	}
}

func TestResolveStopsWhenRemovalVerificationFails(t *testing.T) {
	backend := blockedBackend()
	backend.verifyErr = errors.New("dependent-z remains installed")
	resolver := testResolver(t, backend)

	_, err := resolver.Resolve(context.Background(), "core.alpha", strings.NewReader("yes\n"), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "remains installed") {
		t.Fatalf("Resolve() error = %v", err)
	}
	if backend.reconcileCalls != 0 || !reflect.DeepEqual(backend.events, []string{"prepare", "privilege", "remove", "verify", "cancel"}) {
		t.Fatalf("reconciled without verification: calls=%d events=%v", backend.reconcileCalls, backend.events)
	}
}

func TestResolveRebuildAndSimulationRunWhileExclusiveLockIsHeld(t *testing.T) {
	backend := blockedBackend()
	resolver := testResolver(t, backend)
	backend.onPrepare = func() {
		if _, err := os.Stat(filepath.Join(resolver.StateDir, "lock", "owner.json")); err != nil {
			t.Errorf("prepare ran without lock: %v", err)
		}
	}

	if _, err := resolver.Resolve(context.Background(), "core.alpha", strings.NewReader("\n"), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
}

func TestResolveRejectsRootAndInvalidStableIDBeforePreparing(t *testing.T) {
	backend := blockedBackend()
	resolver := testResolver(t, backend)
	resolver.EffectiveUID = func() int { return 0 }
	if _, err := resolver.Resolve(context.Background(), "core.alpha", strings.NewReader("yes\n"), &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "non-root") {
		t.Fatalf("root error = %v", err)
	}
	resolver.EffectiveUID = func() int { return 501 }
	if _, err := resolver.Resolve(context.Background(), "not-stable", strings.NewReader("yes\n"), &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "stable resource ID") {
		t.Fatalf("ID error = %v", err)
	}
	if backend.prepareCalls != 0 {
		t.Fatalf("prepare calls = %d", backend.prepareCalls)
	}
}

type fakeBackend struct {
	details        attemptDetails
	prepareErr     error
	verifyErr      error
	prepareCalls   int
	removeCalls    int
	verifyCalls    int
	reconcileCalls int
	cancelCalls    int
	events         []string
	onPrepare      func()
}

func blockedBackend() *fakeBackend {
	item := model.Resource{ID: "core.alpha", Type: model.ResourcePackage, Provider: "fixture", Package: "alpha", VersionPolicy: model.VersionTracked}
	operation := model.Operation{ID: "transfer-alpha", ResourceID: item.ID, Kind: model.OperationTransfer, Provider: item.Provider, Package: item.Package, Removes: []string{"legacy-alpha"}, RequiresPrivilege: true}
	input := reconcile.ApplyInput{Plan: model.Plan{ID: "plan", Operations: []model.Operation{operation}}, CurrentResources: []model.Resource{item}, EnabledIDs: []model.ResourceID{item.ID}, CatalogDigest: "signed-digest", Profile: model.ProfileVPSShell}
	return &fakeBackend{details: attemptDetails{input: input, operation: operation, changes: provider.ChangeSet{Installs: []string{"alpha"}, Removes: []string{"dependent-z", "legacy-alpha", "dependent-a", "dependent-z"}}}}
}

func (f *fakeBackend) Prepare(_ context.Context, _ model.ResourceID, _ *state.Lock) (Attempt, error) {
	f.prepareCalls++
	f.events = append(f.events, "prepare")
	if f.onPrepare != nil {
		f.onPrepare()
	}
	return Attempt{issuer: f, token: [16]byte{1}}, f.prepareErr
}
func (f *fakeBackend) Describe(Attempt) (attemptDetails, error) { return f.details, nil }
func (f *fakeBackend) AcquirePrivilege(context.Context, Attempt) error {
	f.events = append(f.events, "privilege")
	return nil
}
func (f *fakeBackend) RemoveBlockers(_ context.Context, _ Attempt, blockers []string) error {
	f.removeCalls++
	f.events = append(f.events, "remove")
	if !reflect.DeepEqual(blockers, []string{"dependent-a", "dependent-z"}) {
		return errors.New("wrong blockers")
	}
	return nil
}
func (f *fakeBackend) VerifyBlockersAbsent(_ context.Context, _ Attempt, _ []string) error {
	f.verifyCalls++
	f.events = append(f.events, "verify")
	return f.verifyErr
}
func (f *fakeBackend) Reconcile(_ context.Context, _ Attempt, _ *state.Lock) (reconcile.Summary, error) {
	f.reconcileCalls++
	f.events = append(f.events, "reconcile")
	return reconcile.Summary{Ready: []model.ResourceID{"core.alpha"}, Unavailable: map[model.ResourceID]string{}}, nil
}
func (f *fakeBackend) Cancel(_ Attempt) error {
	f.cancelCalls++
	f.events = append(f.events, "cancel")
	return nil
}

func testResolver(t *testing.T, backend Backend) *Resolver {
	t.Helper()
	return &Resolver{StateDir: t.TempDir(), Backend: backend, EffectiveUID: func() int { return 501 }}
}
