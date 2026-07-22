package resolve

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/juty9026/terrapod/internal/execx"
	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/provider"
	"github.com/juty9026/terrapod/internal/provider/apt"
	"github.com/juty9026/terrapod/internal/reconcile"
	"github.com/juty9026/terrapod/internal/resource"
	"github.com/juty9026/terrapod/internal/state"
)

func TestPackageBackendRealAPTAndHeldEnginePreserveTransferOrderAndPrivilegeOnce(t *testing.T) {
	fixture := newPackageIntegration(t, false)
	var output bytes.Buffer
	result, err := fixture.resolver.Resolve(context.Background(), fixture.item.ID, strings.NewReader("yes\n"), &output)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Proceeded || !reflect.DeepEqual(result.Blockers, []string{"dependent-a"}) || fixture.privilege.calls != 1 {
		t.Fatalf("result=%#v privilege calls=%d", result, fixture.privilege.calls)
	}
	wantOrder := []string{"apt-remove-blockers", "install-desired", "verify-desired", "remove-legacy-root", "verify-legacy-absent", "verify-desired"}
	if !orderedSubsequence(fixture.events, wantOrder) {
		t.Fatalf("events=%v, want ordered %v", fixture.events, wantOrder)
	}
	if fixture.aptInstalled["dependent-a"] || fixture.aptInstalled["mise"] {
		t.Fatalf("APT inventory after reconciliation=%v", fixture.aptInstalled)
	}
	snapshot, err := fixture.store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	owned := snapshot.Ownership[fixture.item.ID]
	if owned.CatalogDigest != "signed-digest" || owned.Provider != fixture.item.Provider || snapshot.ActiveJournal != nil {
		t.Fatalf("snapshot=%#v", snapshot)
	}
}

func TestPackageBackendDefaultCancellationHasNoMutationOrDeclineState(t *testing.T) {
	fixture := newPackageIntegration(t, false)
	var output bytes.Buffer
	result, err := fixture.resolver.Resolve(context.Background(), fixture.item.ID, strings.NewReader("\n"), &output)
	if err != nil {
		t.Fatal(err)
	}
	if result.Proceeded || !fixture.aptInstalled["dependent-a"] || !fixture.aptInstalled["mise"] || fixture.desired.present || fixture.privilege.calls != 0 {
		t.Fatalf("cancellation mutated state: result=%#v apt=%v desired=%v privilege=%d", result, fixture.aptInstalled, fixture.desired.present, fixture.privilege.calls)
	}
	snapshot, err := fixture.store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Ownership) != 0 || snapshot.ActiveJournal != nil {
		t.Fatalf("cancellation persisted manager state: %#v", snapshot)
	}
}

func TestPackageBackendEssentialRefusesBeforePromptAndMutation(t *testing.T) {
	fixture := newPackageIntegration(t, true)
	var output bytes.Buffer
	_, err := fixture.resolver.Resolve(context.Background(), fixture.item.ID, strings.NewReader("yes\n"), &output)
	if err == nil || !strings.Contains(err.Error(), "Essential") {
		t.Fatalf("Resolve error=%v", err)
	}
	if output.Len() != 0 || !fixture.aptInstalled["dependent-a"] || !fixture.aptInstalled["mise"] || fixture.desired.present || fixture.privilege.calls != 0 {
		t.Fatalf("essential refusal prompted or mutated: output=%q events=%v", output.String(), fixture.events)
	}
}

func TestPackageBackendAttemptCancelAndReplayAreRejected(t *testing.T) {
	fixture := newPackageIntegration(t, false)
	backend := fixture.resolver.Backend.(*PackageBackend)
	lock, err := state.Acquire(fixture.resolver.StateDir, "tpod resolve core.mise")
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	attempt, err := backend.Prepare(context.Background(), fixture.item.ID, lock)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.Cancel(attempt); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Describe(attempt); err == nil {
		t.Fatal("canceled backend attempt described")
	}
	if err := backend.RemoveBlockers(context.Background(), attempt, []string{"dependent-a"}, lock); err == nil {
		t.Fatal("canceled backend attempt executed")
	}
}

func TestPackageBackendRequiresSignedPrivilegeAndAcquisitionBeforeRemoval(t *testing.T) {
	fixture := newPackageIntegration(t, false)
	backend := fixture.resolver.Backend.(*PackageBackend)
	lock, err := state.Acquire(fixture.resolver.StateDir, "tpod resolve core.mise")
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	attempt, err := backend.Prepare(context.Background(), fixture.item.ID, lock)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.RemoveBlockers(context.Background(), attempt, []string{"dependent-a"}, lock); err == nil {
		t.Fatal("removal bypassed privilege acquisition")
	}

	fixture = newPackageIntegration(t, false)
	backend = fixture.resolver.Backend.(*PackageBackend)
	original := backend.rebuild
	backend.rebuild = func(ctx context.Context) (reconcile.ApplyInput, error) {
		input, err := original(ctx)
		input.Plan.Operations[0].RequiresPrivilege = false
		return input, err
	}
	lock, err = state.Acquire(fixture.resolver.StateDir, "tpod resolve core.mise")
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	if _, err := backend.Prepare(context.Background(), fixture.item.ID, lock); err == nil || !strings.Contains(err.Error(), "privilege") {
		t.Fatalf("unprivileged signed operation error = %v", err)
	}
}

func TestPackageBackendConcurrentPhasesHaveExactlyOneWinner(t *testing.T) {
	t.Run("remove", func(t *testing.T) {
		fixture, backend, lock, attempt := preparedIntegration(t)
		if err := backend.AcquirePrivilege(context.Background(), attempt); err != nil {
			t.Fatal(err)
		}
		errs := concurrently(2, func() error {
			return backend.RemoveBlockers(context.Background(), attempt, []string{"dependent-a"}, lock)
		})
		if successCount(errs) != 1 || eventCount(fixture.events, "apt-remove-blockers") != 1 {
			t.Fatalf("remove errors=%v events=%v", errs, fixture.events)
		}
	})

	t.Run("verify", func(t *testing.T) {
		_, backend, lock, attempt := preparedIntegration(t)
		if err := backend.AcquirePrivilege(context.Background(), attempt); err != nil {
			t.Fatal(err)
		}
		if err := backend.RemoveBlockers(context.Background(), attempt, []string{"dependent-a"}, lock); err != nil {
			t.Fatal(err)
		}
		errs := concurrently(2, func() error {
			return backend.VerifyBlockersAbsent(context.Background(), attempt, []string{"dependent-a"})
		})
		if successCount(errs) != 1 {
			t.Fatalf("verify errors=%v", errs)
		}
	})

	t.Run("reconcile", func(t *testing.T) {
		fixture, backend, lock, attempt := preparedIntegration(t)
		if err := backend.AcquirePrivilege(context.Background(), attempt); err != nil {
			t.Fatal(err)
		}
		if err := backend.RemoveBlockers(context.Background(), attempt, []string{"dependent-a"}, lock); err != nil {
			t.Fatal(err)
		}
		if err := backend.VerifyBlockersAbsent(context.Background(), attempt, []string{"dependent-a"}); err != nil {
			t.Fatal(err)
		}
		errs := concurrently(2, func() error {
			_, err := backend.Reconcile(context.Background(), attempt, lock)
			return err
		})
		if successCount(errs) != 1 || eventCount(fixture.events, "install-desired") != 1 {
			t.Fatalf("reconcile errors=%v events=%v", errs, fixture.events)
		}
	})
}

func TestPackageBackendRejectsReleasedLockAtRemovalBoundary(t *testing.T) {
	fixture, backend, lock, attempt := preparedIntegration(t)
	if err := backend.AcquirePrivilege(context.Background(), attempt); err != nil {
		t.Fatal(err)
	}
	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}
	if err := backend.RemoveBlockers(context.Background(), attempt, []string{"dependent-a"}, lock); err == nil {
		t.Fatal("released lock allowed mutation")
	}
	if !fixture.aptInstalled["dependent-a"] || eventCount(fixture.events, "apt-remove-blockers") != 0 {
		t.Fatalf("released-lock mutation: apt=%v events=%v", fixture.aptInstalled, fixture.events)
	}
}

type packageIntegration struct {
	resolver     *Resolver
	item         model.Resource
	store        *state.Store
	desired      *integrationTransferAdapter
	privilege    *integrationPrivilege
	aptInstalled map[string]bool
	events       []string
}

func newPackageIntegration(t *testing.T, essential bool) *packageIntegration {
	t.Helper()
	fixture := &packageIntegration{aptInstalled: map[string]bool{"mise": true, "dependent-a": true}}
	aptAdapter, err := apt.New(apt.AptGetPath, apt.DpkgQueryPath, integrationAPTRunner{fixture: fixture, essential: essential})
	if err != nil {
		t.Fatal(err)
	}
	fixture.item = model.Resource{ID: "core.mise", Type: model.ResourcePackage, Provider: "homebrew-formula", Package: "mise", VersionPolicy: model.VersionTracked, Metadata: map[string]string{"legacy.apt.package": "mise", "legacy.apt.profile": "vps-shell"}}
	operation := model.Operation{ID: "transfer-mise", ResourceID: fixture.item.ID, Kind: model.OperationTransfer, Provider: fixture.item.Provider, Package: fixture.item.Package, Removes: []string{"mise"}, RequiresPrivilege: true}
	input := reconcile.ApplyInput{Plan: model.Plan{ID: "plan", Operations: []model.Operation{operation}, Unavailable: map[model.ResourceID]string{}}, CurrentResources: []model.Resource{fixture.item}, EnabledIDs: []model.ResourceID{fixture.item.ID}, HistoricalResources: map[model.ResourceID]reconcile.HistoricalResource{}, CatalogDigest: "signed-digest", Profile: model.ProfileVPSShell}
	fixture.desired = &integrationTransferAdapter{fixture: fixture, operation: operation}
	registry := resource.NewRegistry()
	if err := registry.Register(fixture.item.Type, fixture.item.Provider, fixture.desired); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	fixture.store, err = state.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	fixture.privilege = &integrationPrivilege{}
	engine := &reconcile.Engine{Registry: registry, State: fixture.store, LockDir: dir, EffectiveUID: func() int { return 501 }}
	backend, err := NewPackageBackend(func(context.Context) (reconcile.ApplyInput, error) { return input, nil }, engine, aptAdapter, fixture.privilege)
	if err != nil {
		t.Fatal(err)
	}
	fixture.resolver = &Resolver{StateDir: dir, Backend: backend, EffectiveUID: func() int { return 501 }}
	return fixture
}

func preparedIntegration(t *testing.T) (*packageIntegration, *PackageBackend, *state.Lock, Attempt) {
	t.Helper()
	fixture := newPackageIntegration(t, false)
	backend := fixture.resolver.Backend.(*PackageBackend)
	lock, err := state.Acquire(fixture.resolver.StateDir, "tpod resolve core.mise")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Release() })
	attempt, err := backend.Prepare(context.Background(), fixture.item.ID, lock)
	if err != nil {
		t.Fatal(err)
	}
	return fixture, backend, lock, attempt
}

func concurrently(count int, call func() error) []error {
	start := make(chan struct{})
	errs := make([]error, count)
	var wait sync.WaitGroup
	for index := range count {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			errs[index] = call()
		}()
	}
	close(start)
	wait.Wait()
	return errs
}

func successCount(errs []error) int {
	count := 0
	for _, err := range errs {
		if err == nil {
			count++
		}
	}
	return count
}

func eventCount(events []string, target string) int {
	count := 0
	for _, event := range events {
		if event == target {
			count++
		}
	}
	return count
}

type integrationAPTRunner struct {
	fixture   *packageIntegration
	essential bool
}

func (r integrationAPTRunner) Run(_ context.Context, request execx.Request) (execx.Result, error) {
	if request.Path == apt.DpkgQueryPath {
		pkg := request.Args[len(request.Args)-1]
		if !r.fixture.aptInstalled[pkg] {
			return execx.Result{}, integrationExitOne()
		}
		flag := "no"
		if r.essential && pkg == "dependent-a" {
			flag = "yes"
		}
		return execx.Result{Stdout: []byte(pkg + "\tii \t1\t" + flag + "\n")}, nil
	}
	if request.Path != apt.AptGetPath {
		return execx.Result{}, errors.New("unexpected APT request")
	}
	if reflect.DeepEqual(request.Args, []string{"-s", "remove", "--", "mise"}) {
		if request.Privilege {
			return execx.Result{}, errors.New("simulation requested privilege")
		}
		return execx.Result{Stdout: []byte("Remv mise [1]\nRemv dependent-a [1]\n0 upgraded, 0 newly installed, 2 to remove and 0 not upgraded.\n")}, nil
	}
	if reflect.DeepEqual(request.Args, []string{"-s", "remove", "--", "dependent-a"}) {
		if request.Privilege {
			return execx.Result{}, errors.New("simulation requested privilege")
		}
		return execx.Result{Stdout: []byte("Remv dependent-a [1]\n0 upgraded, 0 newly installed, 1 to remove and 0 not upgraded.\n")}, nil
	}
	if reflect.DeepEqual(request.Args, []string{"remove", "-y", "--", "dependent-a"}) {
		if !request.Privilege {
			return execx.Result{}, errors.New("mutation omitted privilege")
		}
		if !r.fixture.aptInstalled["mise"] {
			return execx.Result{}, errors.New("legacy root removed before blocker")
		}
		r.fixture.aptInstalled["dependent-a"] = false
		r.fixture.events = append(r.fixture.events, "apt-remove-blockers")
		return execx.Result{}, nil
	}
	return execx.Result{}, errors.New("unexpected APT argv")
}

type integrationTransferAdapter struct {
	fixture   *packageIntegration
	operation model.Operation
	present   bool
}

func (a *integrationTransferAdapter) Inspect(context.Context, model.Resource) (model.Observation, error) {
	return a.observation(), nil
}
func (a *integrationTransferAdapter) Plan(context.Context, model.Resource, model.Observation, model.Ownership) ([]model.Operation, error) {
	return []model.Operation{a.operation}, nil
}
func (a *integrationTransferAdapter) Execute(context.Context, model.Operation) model.OperationResult {
	return model.OperationResult{}
}
func (a *integrationTransferAdapter) Verify(context.Context, model.Resource) (model.Observation, error) {
	a.fixture.events = append(a.fixture.events, "verify-desired")
	if !a.present {
		return model.Observation{}, errors.New("desired absent")
	}
	return a.observation(), nil
}
func (a *integrationTransferAdapter) Simulate(context.Context, model.Resource, model.Operation) (provider.ChangeSet, error) {
	a.fixture.events = append(a.fixture.events, "simulate-transfer")
	if !a.fixture.aptInstalled["mise"] {
		return provider.ChangeSet{Installs: []string{"mise"}}, nil
	}
	return provider.ChangeSet{Installs: []string{"mise"}, Removes: []string{"mise"}}, nil
}
func (a *integrationTransferAdapter) CancelSimulation(model.Operation) error { return nil }
func (a *integrationTransferAdapter) InstallDesired(_ context.Context, _ model.Resource, operation model.Operation) model.OperationResult {
	a.fixture.events = append(a.fixture.events, "install-desired")
	a.present = true
	return model.OperationResult{OperationID: operation.ID, ResourceID: operation.ResourceID, Success: true}
}
func (a *integrationTransferAdapter) RemoveLegacy(_ context.Context, _ model.Resource, operation model.Operation) model.OperationResult {
	if !a.present {
		return model.OperationResult{OperationID: operation.ID, ResourceID: operation.ResourceID, Detail: "desired absent"}
	}
	a.fixture.events = append(a.fixture.events, "remove-legacy-root")
	a.fixture.aptInstalled["mise"] = false
	return model.OperationResult{OperationID: operation.ID, ResourceID: operation.ResourceID, Success: true}
}
func (a *integrationTransferAdapter) VerifyLegacyAbsent(context.Context, model.Resource, model.Operation) error {
	a.fixture.events = append(a.fixture.events, "verify-legacy-absent")
	if a.fixture.aptInstalled["mise"] {
		return errors.New("legacy root remains")
	}
	return nil
}
func (a *integrationTransferAdapter) observation() model.Observation {
	return model.Observation{Present: a.present, Healthy: a.present, Provider: "homebrew-formula", Package: "mise", Paths: map[string]string{}}
}

type integrationPrivilege struct{ calls int }

func (p *integrationPrivilege) Acquire(context.Context) error { p.calls++; return nil }

func integrationExitOne() error { return exec.Command("/usr/bin/false").Run() }

func orderedSubsequence(events, wanted []string) bool {
	index := 0
	for _, event := range events {
		if index < len(wanted) && event == wanted[index] {
			index++
		}
	}
	return index == len(wanted)
}
