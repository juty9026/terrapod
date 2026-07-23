package migrate

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/reconcile"
	"github.com/juty9026/terrapod/internal/state"
)

func TestRunCurrentPreflightsAndPrintsBeforeMutation(t *testing.T) {
	var order []string
	deps := currentFixture(t, &order)
	result, err := RunCurrent(context.Background(), deps, func(model.Plan) error {
		order = append(order, "print")
		return nil
	})
	if err != nil || len(result.Summary.Unavailable) != 0 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	want := []string{"prepare", "preflight", "print", "config", "activate", "import", "reconcile", "source"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("order=%v want=%v", order, want)
	}
	order = nil
	result, err = RunCurrent(context.Background(), deps, func(model.Plan) error {
		t.Fatal("completed migration printed another plan")
		return nil
	})
	if err != nil || !result.AlreadyComplete || len(order) != 0 {
		t.Fatalf("second result=%#v order=%v err=%v", result, order, err)
	}
}

func TestRunCurrentUnavailablePreflightMakesNoMutation(t *testing.T) {
	var order []string
	deps := currentFixture(t, &order)
	deps.Prepare = func(context.Context) (CurrentPrepared, error) {
		order = append(order, "prepare")
		return CurrentPrepared{Plan: model.Plan{ID: "p", Unavailable: map[model.ResourceID]string{"core.git": "conflict"}}}, nil
	}
	result, err := RunCurrent(context.Background(), deps, func(model.Plan) error {
		order = append(order, "print")
		return nil
	})
	if err == nil || result.Summary.Unavailable["core.git"] != "conflict" || !reflect.DeepEqual(order, []string{"prepare", "preflight", "print"}) {
		t.Fatalf("result=%#v order=%v err=%v", result, order, err)
	}
}

func TestRunCurrentRetriesOnlySourceAfterReconciliation(t *testing.T) {
	var order []string
	deps := currentFixture(t, &order)
	fail := true
	expectedBinding := CurrentBinding{Release: "1.2.3", ManifestDigest: "manifest-digest", CatalogDigest: "catalog-digest"}
	deps.FinalizeSource = func(_ context.Context, _ reconcile.ApplyInput, binding CurrentBinding, _ *state.Lock) error {
		order = append(order, "source")
		if binding != expectedBinding {
			t.Fatalf("source binding=%#v want=%#v", binding, expectedBinding)
		}
		if fail {
			return errors.New("interrupted")
		}
		return nil
	}
	if _, err := RunCurrent(context.Background(), deps, func(model.Plan) error {
		order = append(order, "print")
		return nil
	}); err == nil {
		t.Fatal("source interruption succeeded")
	}
	fail = false
	order = nil
	result, err := RunCurrent(context.Background(), deps, func(model.Plan) error {
		t.Fatal("retry repeated preflight")
		return nil
	})
	if err != nil || result.AlreadyComplete || !reflect.DeepEqual(order, []string{"source"}) {
		t.Fatalf("retry result=%#v order=%v err=%v", result, order, err)
	}
}

func TestRunCurrentResumesPersistedPlanAfterPartialReconciliation(t *testing.T) {
	var order []string
	deps := currentFixture(t, &order)
	wantPlan := model.Plan{ID: "p", Release: "1.2.3", Unavailable: map[model.ResourceID]string{}}
	wantInput := reconcile.ApplyInput{
		Plan: wantPlan, CatalogDigest: "catalog-digest", Profile: model.ProfileMacOSTerminal,
		HistoricalResources: map[model.ResourceID]reconcile.HistoricalResource{
			"legacy.tool": {Resource: model.Resource{ID: "legacy.tool"}, CatalogDigest: "legacy-digest"},
		},
	}
	deps.Prepare = func(context.Context) (CurrentPrepared, error) {
		order = append(order, "prepare")
		return CurrentPrepared{
			Plan: wantPlan, ApplyInput: wantInput,
			Binding: CurrentBinding{Release: "1.2.3", ManifestDigest: "manifest-digest", CatalogDigest: "catalog-digest"},
		}, nil
	}
	deps.Reconcile = func(context.Context, CurrentPrepared, *state.Lock) (reconcile.Summary, error) {
		order = append(order, "reconcile-partial")
		return reconcile.Summary{}, errors.New("interrupted")
	}
	if _, err := RunCurrent(context.Background(), deps, func(model.Plan) error {
		order = append(order, "print")
		return nil
	}); err == nil {
		t.Fatal("partial reconciliation succeeded")
	}

	order = nil
	deps.Prepare = func(context.Context) (CurrentPrepared, error) {
		t.Fatal("retry recomputed migration inspection")
		return CurrentPrepared{}, nil
	}
	deps.Resume = func(_ context.Context, input reconcile.ApplyInput, _ CurrentBinding, _ *state.Lock) (reconcile.Summary, error) {
		order = append(order, "resume")
		if !reflect.DeepEqual(input, wantInput) {
			t.Fatalf("resume input=%#v want=%#v", input, wantInput)
		}
		return reconcile.Summary{Unavailable: map[model.ResourceID]string{}}, nil
	}
	result, err := RunCurrent(context.Background(), deps, func(model.Plan) error {
		t.Fatal("retry printed a newly computed plan")
		return nil
	})
	if err != nil || result.AlreadyComplete || !reflect.DeepEqual(order, []string{"resume", "source"}) {
		t.Fatalf("retry result=%#v order=%v err=%v", result, order, err)
	}
}

func currentFixture(t *testing.T, order *[]string) CurrentDependencies {
	t.Helper()
	root := t.TempDir()
	return CurrentDependencies{
		LockDir: filepath.Join(root, "state"), CompletionPath: filepath.Join(root, "state", "migration-current.json"),
		Prepare: func(context.Context) (CurrentPrepared, error) {
			*order = append(*order, "prepare")
			plan := model.Plan{ID: "p", Release: "1.2.3", Unavailable: map[model.ResourceID]string{}}
			return CurrentPrepared{
				Plan: plan, ApplyInput: reconcile.ApplyInput{Plan: plan, CatalogDigest: "catalog-digest", Profile: model.ProfileMacOSTerminal},
				Binding: CurrentBinding{Release: "1.2.3", ManifestDigest: "manifest-digest", CatalogDigest: "catalog-digest"},
			}, nil
		},
		Preflight: func(context.Context, CurrentPrepared, *state.Lock) error {
			*order = append(*order, "preflight")
			return nil
		},
		CommitConfig: func(context.Context, CurrentPrepared) error { *order = append(*order, "config"); return nil },
		Activate:     func(context.Context, CurrentPrepared) error { *order = append(*order, "activate"); return nil },
		Import:       func(context.Context, CurrentPrepared) error { *order = append(*order, "import"); return nil },
		Reconcile: func(context.Context, CurrentPrepared, *state.Lock) (reconcile.Summary, error) {
			*order = append(*order, "reconcile")
			return reconcile.Summary{Unavailable: map[model.ResourceID]string{}}, nil
		},
		Resume: func(context.Context, reconcile.ApplyInput, CurrentBinding, *state.Lock) (reconcile.Summary, error) {
			*order = append(*order, "resume")
			return reconcile.Summary{Unavailable: map[model.ResourceID]string{}}, nil
		},
		FinalizeSource: func(context.Context, reconcile.ApplyInput, CurrentBinding, *state.Lock) error {
			*order = append(*order, "source")
			return nil
		},
	}
}
