package provider

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/juty9026/terrapod/internal/model"
)

type stubProvider struct{}

func (stubProvider) Name() string { return "stub" }
func (stubProvider) Inspect(context.Context, model.Resource) (model.Observation, error) {
	return model.Observation{}, nil
}
func (stubProvider) Simulate(context.Context, model.Operation) (ChangeSet, error) {
	return ChangeSet{
		Installs: []string{"dependency"},
		Upgrades: []string{"target"},
		Removes:  []string{"obsolete"},
	}, nil
}
func (stubProvider) Execute(context.Context, model.Operation) error { return nil }
func (stubProvider) Verify(context.Context, model.Resource) (model.Observation, error) {
	return model.Observation{}, nil
}

var _ Provider = stubProvider{}

func TestChangeSetSeparatesKinds(t *testing.T) {
	changes, err := (stubProvider{}).Simulate(context.Background(), model.Operation{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(changes.Installs, []string{"dependency"}) ||
		!reflect.DeepEqual(changes.Upgrades, []string{"target"}) ||
		!reflect.DeepEqual(changes.Removes, []string{"obsolete"}) {
		t.Fatalf("unexpected change set: %#v", changes)
	}
}

func TestValidateChangeSetAllowsTargetAndPlanOwnedRemovals(t *testing.T) {
	changes := ChangeSet{
		Installs: []string{"new-dependency"},
		Upgrades: []string{"existing-dependency"},
		Removes:  []string{"target", "old-dependency"},
	}
	target := model.Resource{Package: "target"}

	if err := ValidateChangeSet(changes, target, []string{"old-dependency"}); err != nil {
		t.Fatalf("ValidateChangeSet() error = %v", err)
	}
}

func TestValidateChangeSetRejectsSortedUniqueUnmanagedRemovals(t *testing.T) {
	changes := ChangeSet{Removes: []string{"zeta", "target", "alpha", "zeta", "owned"}}
	target := model.Resource{Package: "target"}

	err := ValidateChangeSet(changes, target, []string{"owned"})
	var unmanaged *ErrUnmanagedRemoval
	if !errors.As(err, &unmanaged) {
		t.Fatalf("ValidateChangeSet() error = %v, want *ErrUnmanagedRemoval", err)
	}
	if want := []string{"alpha", "zeta"}; !reflect.DeepEqual(unmanaged.IDs, want) {
		t.Fatalf("IDs = %#v, want %#v", unmanaged.IDs, want)
	}
	if !strings.Contains(err.Error(), "alpha, zeta") {
		t.Fatalf("error = %q, want deterministic IDs", err)
	}
}

func TestValidateChangeSetNeverAllowsEmptyRemovalID(t *testing.T) {
	changes := ChangeSet{Removes: []string{"", "", "managed"}}
	target := model.Resource{}

	err := ValidateChangeSet(changes, target, []string{"", "managed"})
	var unmanaged *ErrUnmanagedRemoval
	if !errors.As(err, &unmanaged) {
		t.Fatalf("ValidateChangeSet() error = %v, want *ErrUnmanagedRemoval", err)
	}
	if want := []string{""}; !reflect.DeepEqual(unmanaged.IDs, want) {
		t.Fatalf("IDs = %#v, want %#v", unmanaged.IDs, want)
	}
}

func TestOperationCarriesTypedProviderTargetInJSON(t *testing.T) {
	op := model.Operation{Provider: "homebrew", Package: "ripgrep"}
	data, err := json.Marshal(op)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got["provider"] != "homebrew" || got["package"] != "ripgrep" {
		t.Fatalf("JSON = %s, want provider and package fields", data)
	}
}
