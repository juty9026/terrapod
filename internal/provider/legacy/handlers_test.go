package legacy

import (
	"context"
	"reflect"
	"testing"

	"github.com/juty9026/terrapod/internal/execx"
	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/provider"
)

type queueRunner struct {
	results  []execx.Result
	errors   []error
	requests []execx.Request
}

func (r *queueRunner) Run(_ context.Context, request execx.Request) (execx.Result, error) {
	r.requests = append(r.requests, request)
	i := len(r.requests) - 1
	var result execx.Result
	var err error
	if i < len(r.results) {
		result = r.results[i]
	}
	if i < len(r.errors) {
		err = r.errors[i]
	}
	return result, err
}

type providerFixture struct {
	inspected model.Resource
	operation model.Operation
}

func (f *providerFixture) Name() string { return "apt" }
func (f *providerFixture) Inspect(_ context.Context, resource model.Resource) (model.Observation, error) {
	f.inspected = resource
	return model.Observation{Present: true, Provider: "apt", Package: resource.Package}, nil
}
func (f *providerFixture) Simulate(_ context.Context, operation model.Operation) (provider.ChangeSet, error) {
	f.operation = operation
	return provider.ChangeSet{Removes: []string{operation.Package}}, nil
}
func (f *providerFixture) Execute(_ context.Context, operation model.Operation) error {
	f.operation = operation
	return nil
}
func (f *providerFixture) Verify(context.Context, model.Resource) (model.Observation, error) {
	return model.Observation{}, nil
}

func TestAPTHandlerAdaptsExactHistoricalBootstrapPackage(t *testing.T) {
	fixture := &providerFixture{}
	handler, err := NewAPTHandler(fixture)
	if err != nil {
		t.Fatal(err)
	}
	r := model.Resource{ID: "core.gum", Type: model.ResourcePackage, Provider: "homebrew-formula", Package: "gum", Commands: []string{"gum"}, VersionPolicy: model.VersionTracked, Metadata: map[string]string{"legacy.apt.package": "gum"}}
	d := Declaration{Kind: APT, Package: "gum"}
	receipt, err := handler.Inspect(context.Background(), r, d)
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Present || receipt.Paths["gum"] != "/usr/bin/gum" || fixture.inspected.Metadata["bootstrapOnly"] != "true" {
		t.Fatalf("receipt=%#v inspected=%#v", receipt, fixture.inspected)
	}
	changes, err := handler.SimulateRemoval(context.Background(), r, d)
	if err != nil || !reflect.DeepEqual(changes.Removes, []string{"gum"}) {
		t.Fatalf("changes=%#v error=%v", changes, err)
	}
}

func TestMiseHandlerUsesExactAllowlistedToolAndVersion(t *testing.T) {
	runner := &queueRunner{results: []execx.Result{
		{Stdout: []byte(`{"aqua:BurntSushi/ripgrep":[{"version":"14.1.1","installed":true}]}`)},
		{Stdout: []byte("/data/mise/installs/aqua-BurntSushi-ripgrep/14.1.1\n")},
		{Stdout: []byte("/data/mise/installs/aqua-BurntSushi-ripgrep/14.1.1/bin/rg\n")},
		{},
	}}
	handler, err := NewMiseHandler("/opt/homebrew/bin/mise", "/data/mise", runner, fakePaths{})
	if err != nil {
		t.Fatal(err)
	}
	d := Declaration{Kind: Mise, Package: "aqua:BurntSushi/ripgrep"}
	receipt, err := handler.Inspect(context.Background(), resource(nil), d)
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Present || receipt.Paths["rg"] == "" {
		t.Fatalf("receipt=%#v", receipt)
	}
	if err := handler.Remove(context.Background(), resource(nil), d); err != nil {
		t.Fatal(err)
	}
	want := []string{"uninstall", "--yes", "aqua:BurntSushi/ripgrep@14.1.1"}
	if got := runner.requests[3].Args; !reflect.DeepEqual(got, want) {
		t.Fatalf("uninstall args=%v want=%v", got, want)
	}
}

func TestMiseHandlerRejectsUnknownTool(t *testing.T) {
	handler, err := NewMiseHandler("/opt/homebrew/bin/mise", "/data/mise", &queueRunner{}, fakePaths{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = handler.Inspect(context.Background(), resource(nil), Declaration{Kind: Mise, Package: "aqua:unknown/tool"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestHomebrewHandlerRequiresPackageReceiptAndTargetsUninstall(t *testing.T) {
	runner := &queueRunner{results: []execx.Result{{Stdout: []byte(`{"formulae":[{"name":"ripgrep","full_name":"ripgrep","installed":[{"version":"14.1.1"}]}],"casks":[]}`)}, {}}}
	handler, err := NewHomebrewHandler("/custom/homebrew/bin/brew", runner, fakePaths{})
	if err != nil {
		t.Fatal(err)
	}
	d := Declaration{Kind: Homebrew, Package: "ripgrep"}
	receipt, err := handler.Inspect(context.Background(), resource(nil), d)
	if err != nil || !receipt.Present || receipt.Paths["rg"] != "/custom/homebrew/bin/rg" {
		t.Fatalf("receipt=%#v error=%v", receipt, err)
	}
	if err := handler.Remove(context.Background(), resource(nil), d); err != nil {
		t.Fatal(err)
	}
	if got, want := runner.requests[1].Args, []string{"uninstall", "--formula", "ripgrep"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("args=%v want=%v", got, want)
	}

	absentRunner := &queueRunner{results: []execx.Result{{Stdout: []byte(`{"formulae":[{"name":"ripgrep","full_name":"ripgrep","installed":[]}],"casks":[]}`)}}}
	absent, err := NewHomebrewHandler("/custom/homebrew/bin/brew", absentRunner, fakePaths{})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err = absent.Inspect(context.Background(), resource(nil), d)
	if err != nil || receipt.Present {
		t.Fatalf("prefix-only receipt=%#v error=%v", receipt, err)
	}
}

func TestHomebrewHandlerTargetsCaskWithoutForceOrZap(t *testing.T) {
	runner := &queueRunner{results: []execx.Result{{Stdout: []byte(`{"formulae":[],"casks":[{"token":"codex","full_token":"homebrew/cask/codex","installed":"1.2.3"}]}`)}, {}}}
	handler, err := NewHomebrewHandler("/custom/homebrew/bin/brew", runner, fakePaths{})
	if err != nil {
		t.Fatal(err)
	}
	r := model.Resource{ID: "optional-ai.codex", Type: model.ResourcePackage, Provider: "homebrew-cask", Package: "codex", Commands: []string{"codex"}}
	d := Declaration{Kind: Homebrew, Package: "codex"}
	if _, err := handler.Inspect(context.Background(), r, d); err != nil {
		t.Fatal(err)
	}
	if err := handler.Remove(context.Background(), r, d); err != nil {
		t.Fatal(err)
	}
	if got, want := runner.requests[1].Args, []string{"uninstall", "--cask", "codex"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("args=%v want=%v", got, want)
	}
}

func TestHomebrewHandlerRejectsStandardPrefixAndUnsafeKind(t *testing.T) {
	if _, err := NewHomebrewHandler("/opt/homebrew/bin/brew", &queueRunner{}, fakePaths{}); err == nil {
		t.Fatal("accepted standard Homebrew")
	}
}
