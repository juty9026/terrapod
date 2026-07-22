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
	handler, err := newAPTHandler(fixture, &queueRunner{results: []execx.Result{{Stdout: []byte("/usr/bin/gum\n")}}}, fakePaths{})
	if err != nil {
		t.Fatal(err)
	}
	r := model.Resource{ID: "core.gum", Type: model.ResourcePackage, Provider: "homebrew-formula", Package: "gum", Commands: []string{"gum"}, VersionPolicy: model.VersionTracked, Metadata: map[string]string{"legacy.apt.package": "gum", "legacy.apt.profile": "vps-shell"}}
	d := Declaration{Kind: APT, Package: "gum"}
	receipt, err := handler.inspect(context.Background(), r, d)
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Present || receipt.Paths["gum"] != "/usr/bin/gum" || fixture.inspected.Metadata["bootstrapOnly"] != "true" {
		t.Fatalf("receipt=%#v inspected=%#v", receipt, fixture.inspected)
	}
	changes, err := handler.simulateRemoval(context.Background(), r, d)
	if err != nil || !reflect.DeepEqual(changes.Removes, []string{"gum"}) {
		t.Fatalf("changes=%#v error=%v", changes, err)
	}
	unrelated, _ := newAPTHandler(&providerFixture{}, &queueRunner{results: []execx.Result{{Stdout: []byte("/usr/bin/not-gum\n")}}}, fakePaths{})
	unrelatedReceipt, err := unrelated.inspect(context.Background(), r, d)
	if err != nil {
		t.Fatal(err)
	}
	if len(unrelatedReceipt.Paths) != 0 {
		t.Fatalf("unrelated APT receipt trusted: %#v", unrelatedReceipt)
	}
}

func TestMiseHandlerUsesExactAllowlistedToolAndVersion(t *testing.T) {
	runner := &queueRunner{results: []execx.Result{
		{Stdout: []byte(`{"aqua:BurntSushi/ripgrep":[{"version":"14.1.1","installed":true}]}`)},
		{Stdout: []byte("/data/mise/installs/aqua-BurntSushi-ripgrep/14.1.1\n")},
		{Stdout: []byte("/data/mise/installs/aqua-BurntSushi-ripgrep/14.1.1/bin/rg\n")},
		{},
	}}
	handler, err := newMiseHandler("/opt/homebrew/bin/mise", "/data/mise", runner, fakePaths{})
	if err != nil {
		t.Fatal(err)
	}
	d := Declaration{Kind: Mise, Package: "aqua:BurntSushi/ripgrep"}
	miseResource := resource(map[string]string{"legacy.mise.package": "aqua:BurntSushi/ripgrep"})
	receipt, err := handler.inspect(context.Background(), miseResource, d)
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Present || receipt.Paths["rg"] == "" {
		t.Fatalf("receipt=%#v", receipt)
	}
	if err := handler.remove(context.Background(), miseResource, d); err != nil {
		t.Fatal(err)
	}
	want := []string{"uninstall", "--yes", "aqua:BurntSushi/ripgrep@14.1.1"}
	if got := runner.requests[3].Args; !reflect.DeepEqual(got, want) {
		t.Fatalf("uninstall args=%v want=%v", got, want)
	}
}

func TestMiseHandlerRejectsUnknownTool(t *testing.T) {
	handler, err := newMiseHandler("/opt/homebrew/bin/mise", "/data/mise", &queueRunner{}, fakePaths{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = handler.inspect(context.Background(), resource(nil), Declaration{Kind: Mise, Package: "aqua:unknown/tool"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMiseHandlerRemovesEveryExactInstalledVersionInSortedOrder(t *testing.T) {
	runner := &queueRunner{results: []execx.Result{
		{Stdout: []byte(`{"aqua:BurntSushi/ripgrep":[{"version":"15.0.0","installed":true},{"version":"14.1.1","installed":true}]}`)},
		{Stdout: []byte("/data/mise/rg/14.1.1\n")}, {Stdout: []byte("/data/mise/rg/14.1.1/rg\n")},
		{Stdout: []byte("/data/mise/rg/15.0.0\n")}, {Stdout: []byte("/data/mise/rg/15.0.0/rg\n")}, {}, {},
	}}
	paths := fakePaths{commands: map[string]string{"rg": "/data/mise/rg/15.0.0/rg"}}
	handler, err := newMiseHandler("/opt/homebrew/bin/mise", "/data/mise", runner, paths)
	if err != nil {
		t.Fatal(err)
	}
	r := resource(map[string]string{"legacy.mise.package": "aqua:BurntSushi/ripgrep"})
	d := Declaration{Kind: Mise, Package: "aqua:BurntSushi/ripgrep"}
	receipt, err := handler.inspect(context.Background(), r, d)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Paths["rg"] != "/data/mise/rg/15.0.0/rg" {
		t.Fatalf("receipt=%#v", receipt)
	}
	if err := handler.remove(context.Background(), r, d); err != nil {
		t.Fatal(err)
	}
	wants := [][]string{{"uninstall", "--yes", "aqua:BurntSushi/ripgrep@14.1.1"}, {"uninstall", "--yes", "aqua:BurntSushi/ripgrep@15.0.0"}}
	for index, want := range wants {
		if got := runner.requests[5+index].Args; !reflect.DeepEqual(got, want) {
			t.Fatalf("args[%d]=%v want=%v", index, got, want)
		}
	}
}

func TestLegacyHandlerRootsRejectBroadAndTemporaryLocations(t *testing.T) {
	if _, err := newMiseHandler("/opt/homebrew/bin/mise", "/", &queueRunner{}, fakePaths{}); err == nil {
		t.Fatal("accepted root mise data")
	}
	paths := fakePaths{}
	c, err := New(paths, WithHomebrew("/tmp/legacy-homebrew", &queueRunner{}))
	if err == nil || c != nil {
		t.Fatal("accepted temporary Homebrew prefix")
	}
}

func TestHomebrewHandlerRequiresPackageReceiptAndTargetsUninstall(t *testing.T) {
	runner := &queueRunner{results: []execx.Result{{Stdout: []byte(`{"formulae":[{"name":"ripgrep","full_name":"ripgrep","installed":[{"version":"14.1.1"}]}],"casks":[]}`)}, {Stdout: []byte("/custom/homebrew/Cellar/ripgrep/14.1.1/bin/rg\n")}, {}}}
	handler, err := newHomebrewHandler("/custom/homebrew/bin/brew", runner, fakePaths{})
	if err != nil {
		t.Fatal(err)
	}
	d := Declaration{Kind: Homebrew, Package: "ripgrep"}
	brewResource := resource(map[string]string{"legacy.homebrew.package": "ripgrep"})
	receipt, err := handler.inspect(context.Background(), brewResource, d)
	if err != nil || !receipt.Present || receipt.Paths["rg"] != "/custom/homebrew/Cellar/ripgrep/14.1.1/bin/rg" {
		t.Fatalf("receipt=%#v error=%v", receipt, err)
	}
	if err := handler.remove(context.Background(), brewResource, d); err != nil {
		t.Fatal(err)
	}
	if got, want := runner.requests[2].Args, []string{"uninstall", "--formula", "ripgrep"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("args=%v want=%v", got, want)
	}

	absentRunner := &queueRunner{results: []execx.Result{{Stdout: []byte(`{"formulae":[{"name":"ripgrep","full_name":"ripgrep","installed":[]}],"casks":[]}`)}}}
	absent, err := newHomebrewHandler("/custom/homebrew/bin/brew", absentRunner, fakePaths{})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err = absent.inspect(context.Background(), brewResource, d)
	if err != nil || receipt.Present {
		t.Fatalf("prefix-only receipt=%#v error=%v", receipt, err)
	}
}

func TestHomebrewReceiptDoesNotTrustUnrelatedExecutable(t *testing.T) {
	runner := &queueRunner{results: []execx.Result{{Stdout: []byte(`{"formulae":[{"name":"ripgrep","full_name":"ripgrep","installed":[{"version":"14.1.1"}]}],"casks":[]}`)}, {Stdout: []byte("/custom/homebrew/Cellar/ripgrep/14.1.1/bin/not-rg\n")}}}
	handler, err := newHomebrewHandler("/custom/homebrew/bin/brew", runner, fakePaths{})
	if err != nil {
		t.Fatal(err)
	}
	r := resource(map[string]string{"legacy.homebrew.package": "ripgrep"})
	receipt, err := handler.inspect(context.Background(), r, Declaration{Kind: Homebrew, Package: "ripgrep"})
	if err != nil {
		t.Fatal(err)
	}
	if len(receipt.Paths) != 0 {
		t.Fatalf("unrelated Homebrew receipt trusted: %#v", receipt)
	}
}

func TestHomebrewHandlerTargetsCaskWithoutForceOrZap(t *testing.T) {
	runner := &queueRunner{results: []execx.Result{{Stdout: []byte(`{"formulae":[],"casks":[{"token":"codex","full_token":"homebrew/cask/codex","installed":"1.2.3"}]}`)}, {Stdout: []byte("/custom/homebrew/Caskroom/codex/1.2.3/codex\n")}, {}}}
	handler, err := newHomebrewHandler("/custom/homebrew/bin/brew", runner, fakePaths{})
	if err != nil {
		t.Fatal(err)
	}
	r := model.Resource{ID: "optional-ai.codex", Type: model.ResourcePackage, Provider: "homebrew-cask", Package: "codex", Commands: []string{"codex"}, Metadata: map[string]string{"legacy.homebrew.package": "codex"}}
	d := Declaration{Kind: Homebrew, Package: "codex"}
	if _, err := handler.inspect(context.Background(), r, d); err != nil {
		t.Fatal(err)
	}
	if err := handler.remove(context.Background(), r, d); err != nil {
		t.Fatal(err)
	}
	if got, want := runner.requests[2].Args, []string{"uninstall", "--cask", "codex"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("args=%v want=%v", got, want)
	}
}

func TestHomebrewHandlerRejectsStandardPrefixAndUnsafeKind(t *testing.T) {
	if _, err := newHomebrewHandler("/opt/homebrew/bin/brew", &queueRunner{}, fakePaths{}); err == nil {
		t.Fatal("accepted standard Homebrew")
	}
}
