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
	deps.FinalizeSource = func(context.Context) error {
		order = append(order, "source")
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
	deps.Prepare = func(context.Context) (CurrentPrepared, error) {
		order = append(order, "prepare")
		return CurrentPrepared{Plan: wantPlan}, nil
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
	deps.Resume = func(_ context.Context, plan model.Plan, _ *state.Lock) (reconcile.Summary, error) {
		order = append(order, "resume")
		if !reflect.DeepEqual(plan, wantPlan) {
			t.Fatalf("resume plan=%#v want=%#v", plan, wantPlan)
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
			return CurrentPrepared{Plan: model.Plan{ID: "p", Unavailable: map[model.ResourceID]string{}}}, nil
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
		Resume: func(context.Context, model.Plan, *state.Lock) (reconcile.Summary, error) {
			*order = append(*order, "resume")
			return reconcile.Summary{Unavailable: map[model.ResourceID]string{}}, nil
		},
		FinalizeSource: func(context.Context) error { *order = append(*order, "source"); return nil },
	}
}
