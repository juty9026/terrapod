package integration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/state"
)

func TestJSONFieldsLifecyclePreservesUnrelatedAndRestoresPrior(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	path := filepath.Join(home, ".config", "app", "settings.json")
	mustWrite(t, path, []byte(`{"theme":"dark","font":"Monaco"}`), 0o644)
	store, err := state.Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	a := &Adapter{Home: home, State: store}
	item := jsonItem("integration.test", ".config/app/settings.json", map[string]any{"/font": "Jetendard"})

	observed, err := a.Inspect(context.Background(), item)
	if err != nil {
		t.Fatal(err)
	}
	ops, err := a.Plan(context.Background(), item, observed, model.Ownership{})
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 1 || ops[0].Kind != model.OperationInstall || strings.Contains(ops[0].Detail, "Jetendard") || !strings.Contains(ops[0].Detail, "[redacted]") {
		t.Fatalf("unexpected redacted plan: %#v", ops)
	}
	if result := executeAuthorized(t, a, store, item, ops[0]); !result.Success {
		t.Fatal(result.Detail)
	}

	var got map[string]any
	contents, _ := os.ReadFile(path)
	if err := json.Unmarshal(contents, &got); err != nil {
		t.Fatal(err)
	}
	if got["theme"] != "dark" || got["font"] != "Jetendard" {
		t.Fatalf("unrelated/desired fields: %#v", got)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("mode changed to %o", info.Mode().Perm())
	}

	snapshot, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	owned := snapshot.Ownership[item.ID]
	if len(owned.PriorValues) != 1 || !strings.Contains(string(owned.PriorValues[fieldKey(".config/app/settings.json", "/font")]), "Monaco") {
		t.Fatalf("prior receipt missing: %#v", owned.PriorValues)
	}
	completeJournal(t, store)
	prune, err := a.PlanHistorical(context.Background(), item, model.Observation{}, owned)
	if err != nil {
		t.Fatal(err)
	}
	if result := executeAuthorized(t, a, store, item, prune[0]); !result.Success {
		t.Fatal(result.Detail)
	}
	contents, _ = os.ReadFile(path)
	if err := json.Unmarshal(contents, &got); err != nil {
		t.Fatal(err)
	}
	if got["font"] != "Monaco" || got["theme"] != "dark" {
		t.Fatalf("prior not restored: %#v", got)
	}
}

func TestAbsentFieldPruneRemovesOnlyTouchedField(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	path := filepath.Join(home, "settings.json")
	mustWrite(t, path, []byte(`{"keep":true}`), 0o600)
	store, _ := state.Open(filepath.Join(t.TempDir(), "state"))
	a := &Adapter{Home: home, State: store}
	item := jsonItem("integration.absent", "settings.json", map[string]any{"/nested/font": "Jetendard"})
	op := mustSinglePlan(t, a, item, model.Ownership{})
	if result := executeAuthorized(t, a, store, item, op); !result.Success {
		t.Fatal(result.Detail)
	}
	owned := mustOwnership(t, store, item.ID)
	completeJournal(t, store)
	prune, err := a.PlanHistorical(context.Background(), item, model.Observation{}, owned)
	if err != nil {
		t.Fatal(err)
	}
	if result := executeAuthorized(t, a, store, item, prune[0]); !result.Success {
		t.Fatal(result.Detail)
	}
	var got map[string]any
	contents, _ := os.ReadFile(path)
	_ = json.Unmarshal(contents, &got)
	if got["keep"] != true {
		t.Fatalf("unrelated lost: %#v", got)
	}
	if _, exists := got["nested"]; exists {
		t.Fatalf("empty created ancestor remains: %#v", got)
	}
}

func TestPostApplyEditIsConflict(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	path := filepath.Join(home, "settings.json")
	mustWrite(t, path, []byte(`{"font":"Monaco"}`), 0o600)
	store, _ := state.Open(filepath.Join(t.TempDir(), "state"))
	a := &Adapter{Home: home, State: store}
	item := jsonItem("integration.conflict", "settings.json", map[string]any{"/font": "Jetendard"})
	op := mustSinglePlan(t, a, item, model.Ownership{})
	if result := executeAuthorized(t, a, store, item, op); !result.Success {
		t.Fatal(result.Detail)
	}
	owned := mustOwnership(t, store, item.ID)
	mustWrite(t, path, []byte(`{"font":"User Choice"}`), 0o600)
	if _, err := a.Plan(context.Background(), item, model.Observation{}, owned); err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("wanted conflict, got %v", err)
	}
}

func TestMalformedJSONAndIntermediateAreRejectedWithoutMutation(t *testing.T) {
	t.Parallel()
	for _, body := range []string{`{`, `{"terminal":null}`, `{"keep":[1,,2]}`} {
		home := t.TempDir()
		path := filepath.Join(home, "settings.json")
		mustWrite(t, path, []byte(body), 0o600)
		store, _ := state.Open(filepath.Join(t.TempDir(), "state"))
		a := &Adapter{Home: home, State: store}
		item := jsonItem("integration.invalid", "settings.json", map[string]any{"/terminal/font": "Jetendard"})
		if _, err := a.Inspect(context.Background(), item); err == nil {
			t.Fatalf("%q accepted", body)
		}
		got, _ := os.ReadFile(path)
		if string(got) != body {
			t.Fatal("malformed file mutated")
		}
	}
}

func TestMatchingAdoptDoesNotRewriteConfig(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	path := filepath.Join(home, "settings.json")
	body := []byte("{ \"font\" : \"Jetendard\", \"keep\": true }\n")
	mustWrite(t, path, body, 0o600)
	store, _ := state.Open(filepath.Join(t.TempDir(), "state"))
	a := &Adapter{Home: home, State: store}
	item := jsonItem("integration.adopt", "settings.json", map[string]any{"/font": "Jetendard"})
	op := mustSinglePlan(t, a, item, model.Ownership{})
	if op.Kind != model.OperationAdopt {
		t.Fatalf("kind=%s", op.Kind)
	}
	if result := executeAuthorized(t, a, store, item, op); !result.Success {
		t.Fatal(result.Detail)
	}
	if got := mustRead(t, path); string(got) != string(body) {
		t.Fatalf("adopt rewrote config: %q", got)
	}
}

func TestDynamicProfileCapturesLaterPriorWithoutRewrite(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	first := filepath.Join(home, "profiles/one/settings.json")
	mustWrite(t, first, []byte(`{"font":"Monaco"}`), 0o600)
	store, _ := state.Open(filepath.Join(t.TempDir(), "state"))
	a := &Adapter{Home: home, State: store}
	item := jsonItem("integration.dynamic", "", map[string]any{"/font": "Jetendard"})
	delete(item.Metadata, MetadataPath)
	item.Metadata[MetadataPathGlob] = "profiles/*/settings.json"
	op := mustSinglePlan(t, a, item, model.Ownership{})
	if result := executeAuthorized(t, a, store, item, op); !result.Success {
		t.Fatal(result.Detail)
	}
	completeJournal(t, store)
	second := filepath.Join(home, "profiles/two/settings.json")
	body := []byte("{ \"font\" : \"Jetendard\", \"keep\": 2 }\n")
	mustWrite(t, second, body, 0o600)
	owned := mustOwnership(t, store, item.ID)
	op = mustSinglePlan(t, a, item, owned)
	if op.Kind != model.OperationAdopt {
		t.Fatalf("kind=%s", op.Kind)
	}
	if result := executeAuthorized(t, a, store, item, op); !result.Success {
		t.Fatal(result.Detail)
	}
	if got := mustRead(t, second); string(got) != string(body) {
		t.Fatalf("matching new profile rewritten: %q", got)
	}
	owned = mustOwnership(t, store, item.ID)
	if len(owned.PriorValues) != 2 {
		t.Fatalf("priors=%#v", owned.PriorValues)
	}
}

func TestJSONCFieldsPreserveComments(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	path := filepath.Join(home, ".config/zed/settings.json")
	body := "// keep\n{\n  \"theme\": \"dark\", // inline\n  \"terminal\": {\"shell\": \"zsh\"},\n}\n"
	mustWrite(t, path, []byte(body), 0o600)
	store, _ := state.Open(filepath.Join(t.TempDir(), "state"))
	a := &Adapter{Home: home, State: store}
	item := jsonItem("integration.zed", ".config/zed/settings.json", map[string]any{"/buffer_font_family": "Jetendard", "/terminal/font_family": "Jetendard"})
	item.Metadata[MetadataFormat] = "jsonc"
	op := mustSinglePlan(t, a, item, model.Ownership{})
	if result := executeAuthorized(t, a, store, item, op); !result.Success {
		t.Fatal(result.Detail)
	}
	got, _ := os.ReadFile(path)
	for _, want := range []string{"// keep", "// inline", `"shell": "zsh"`, `"font_family": "Jetendard"`} {
		if !strings.Contains(string(got), want) {
			t.Fatalf("missing %q in %s", want, got)
		}
	}
}

func TestJSONCRemovePreservesUnrelatedBytes(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, text, pointer, want string }{
		{"first", "{\n  // before\n  \"owned\": \"x\", // after\n  \"keep\": true,\n}\n", "/owned", "{\n  // before\n   // after\n  \"keep\": true,\n}\n"},
		{"middle", "{\"first\":1, /* left */ \"owned\":2, /* right */ \"last\":3}", "/owned", "{\"first\":1, /* left */  /* right */ \"last\":3}"},
		{"last trailing", "{\n  \"keep\": true,\n  \"owned\": \"x\",\n}\n", "/owned", "{\n  \"keep\": true,\n  \n}\n"},
		{"last no trailing", "{\"keep\":true, \"owned\":false}", "/owned", "{\"keep\":true}"},
		{"nested sibling", "{\n  \"terminal\": {\n    // sibling stays\n    \"shell\": \"zsh\",\n    \"font\": \"x\",\n  },\n}\n", "/terminal/font", "{\n  \"terminal\": {\n    // sibling stays\n    \"shell\": \"zsh\",\n    \n  },\n}\n"},
		{"comment keeps empty parent", "{\"terminal\": {/* user */ \"font\":\"x\"},\"keep\":1}", "/terminal/font", "{\"terminal\": {/* user */ },\"keep\":1}"},
		{"created empty parent removed", "{\"terminal\": {\"font\":\"x\"},\"keep\":1}", "/terminal/font", "{\"keep\":1}"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got, err := jsoncRemove(tt.text, tt.pointer)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("got  %q\nwant %q", got, tt.want)
			}
			if _, err := parseJSONC(got); err != nil {
				t.Fatalf("invalid result: %v\n%s", err, got)
			}
		})
	}
}

func TestPlistFieldsRoundTrip(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	path := filepath.Join(home, "settings.plist")
	body := `<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict><key>keep</key><true/><key>font</key><string>Monaco</string></dict></plist>`
	mustWrite(t, path, []byte(body), 0o600)
	store, _ := state.Open(filepath.Join(t.TempDir(), "state"))
	a := &Adapter{Home: home, State: store}
	item := jsonItem("integration.plist", "settings.plist", map[string]any{"/font": "Jetendard"})
	item.Provider = ProviderPlistFields
	item.Metadata[MetadataFormat] = "plist"
	op := mustSinglePlan(t, a, item, model.Ownership{})
	if result := executeAuthorized(t, a, store, item, op); !result.Success {
		t.Fatal(result.Detail)
	}
	observed, err := a.Verify(context.Background(), item)
	if err != nil || !observed.Healthy {
		t.Fatalf("verify: %#v %v", observed, err)
	}
	owned := mustOwnership(t, store, item.ID)
	completeJournal(t, store)
	prune, _ := a.PlanHistorical(context.Background(), item, model.Observation{}, owned)
	if result := executeAuthorized(t, a, store, item, prune[0]); !result.Success {
		t.Fatal(result.Detail)
	}
	values, err := decodePlist(mustRead(t, path))
	if err != nil {
		t.Fatal(err)
	}
	if values["font"] != "Monaco" || values["keep"] != true {
		t.Fatalf("plist restore: %#v", values)
	}
}

func TestAppRunningDefersChangedOrcaProfiles(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	path := filepath.Join(home, "Library/Application Support/orca/profiles/one/orca-data.json")
	mustWrite(t, path, []byte(`{"settings":{"terminalFontFamily":"Monaco"},"keep":1}`), 0o600)
	store, _ := state.Open(filepath.Join(t.TempDir(), "state"))
	a := &Adapter{Home: home, State: store, AppRunning: func(string) bool { return true }}
	item := model.Resource{ID: "integration.jetendard-orca", Type: model.ResourceIntegration, Profiles: []model.Profile{model.ProfileMacOSTerminal}, VersionPolicy: model.VersionTracked, Provider: ProviderJSONFields, Package: "jetendard-orca", Metadata: map[string]string{MetadataHandler: HandlerJetendardOrca, MetadataPathGlob: "Library/Application Support/orca/profiles/*/orca-data.json", MetadataFields: `{"/settings/terminalFontFamily":"Jetendard"}`}}
	observed, err := a.Inspect(context.Background(), item)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Plan(context.Background(), item, observed, model.Ownership{}); err == nil || !strings.Contains(err.Error(), "deferred") {
		t.Fatalf("wanted defer, got %v", err)
	}
	if string(mustRead(t, path)) != `{"settings":{"terminalFontFamily":"Monaco"},"keep":1}` {
		t.Fatal("running app mutation")
	}
}

func TestReceiptIsPrivate(t *testing.T) {
	t.Parallel()
	stateDir := filepath.Join(t.TempDir(), "state")
	store, _ := state.Open(stateDir)
	home := t.TempDir()
	a := &Adapter{Home: home, State: store}
	item := jsonItem("integration.private", "settings.json", map[string]any{"/token": "secret"})
	op := mustSinglePlan(t, a, item, model.Ownership{})
	if result := executeAuthorized(t, a, store, item, op); !result.Success {
		t.Fatal(result.Detail)
	}
	info, err := os.Stat(filepath.Join(stateDir, "snapshot.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("receipt mode %o", info.Mode().Perm())
	}
}

func TestKarabinerOpenerOwnsNoGeneralState(t *testing.T) {
	t.Parallel()
	store, _ := state.Open(filepath.Join(t.TempDir(), "state"))
	action := &karabinerFixture{guidance: []byte(`{"current_setup":"none","core_service_daemon_state":{"driver_connected":false},"unrelated":false}`)}
	a := &Adapter{Home: t.TempDir(), State: store, Karabiner: action}
	item := model.Resource{ID: "integration.karabiner-opener", Type: model.ResourceIntegration, Profiles: []model.Profile{model.ProfileMacOSTerminal}, VersionPolicy: model.VersionTracked, Provider: ProviderKarabiner, Package: "karabiner-opener", Metadata: map[string]string{MetadataHandler: HandlerKarabinerOpener}}
	observed, err := a.Inspect(context.Background(), item)
	if err != nil {
		t.Fatal(err)
	}
	if observed.Healthy {
		t.Fatal("required false guidance was accepted")
	}
	ops, err := a.Plan(context.Background(), item, observed, model.Ownership{})
	if err != nil || len(ops) != 1 {
		t.Fatalf("plan=%#v err=%v", ops, err)
	}
	if result := executeAuthorized(t, a, store, item, ops[0]); !result.Success {
		t.Fatal(result.Detail)
	}
	if action.opens != 1 {
		t.Fatalf("opens=%d", action.opens)
	}
	if owned := mustOwnership(t, store, item.ID); owned.ResourceID != "" {
		t.Fatalf("Karabiner general state was owned: %#v", owned)
	}

	action.guidance = []byte(`{"current_setup":"none","current_alert":"none","unrelated":false}`)
	observed, err = a.Inspect(context.Background(), item)
	if err != nil || !observed.Healthy {
		t.Fatalf("unrelated false affected health: %#v %v", observed, err)
	}
}

type karabinerFixture struct {
	guidance []byte
	opens    int
}

func (f *karabinerFixture) Guidance(context.Context) ([]byte, error) {
	return append([]byte(nil), f.guidance...), nil
}
func (f *karabinerFixture) Open(context.Context) error { f.opens++; return nil }

func jsonItem(id, path string, fields map[string]any) model.Resource {
	raw, _ := json.Marshal(fields)
	return model.Resource{ID: model.ResourceID(id), Type: model.ResourceIntegration, Profiles: []model.Profile{model.ProfileMacOSTerminal}, VersionPolicy: model.VersionTracked, Provider: ProviderJSONFields, Package: "settings", Metadata: map[string]string{MetadataHandler: HandlerFields, MetadataPath: path, MetadataFields: string(raw), MetadataFormat: "json"}}
}

func mustSinglePlan(t *testing.T, a *Adapter, item model.Resource, owned model.Ownership) model.Operation {
	t.Helper()
	observed, err := a.Inspect(context.Background(), item)
	if err != nil {
		t.Fatal(err)
	}
	ops, err := a.Plan(context.Background(), item, observed, owned)
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 1 {
		t.Fatalf("ops %#v", ops)
	}
	return ops[0]
}
func mustOwnership(t *testing.T, store *state.Store, id model.ResourceID) model.Ownership {
	t.Helper()
	snapshot, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	return snapshot.Ownership[id]
}
func executeAuthorized(t *testing.T, a *Adapter, store *state.Store, item model.Resource, op model.Operation) model.OperationResult {
	t.Helper()
	if _, err := store.Begin(model.Plan{ID: "plan-" + op.ID, Operations: []model.Operation{op}, Unavailable: map[model.ResourceID]string{}}); err != nil {
		t.Fatal(err)
	}
	return a.ExecuteResource(context.Background(), item, op)
}
func completeJournal(t *testing.T, store *state.Store) {
	t.Helper()
	snapshot, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ActiveJournal != nil {
		if err := store.Complete(snapshot.ActiveJournal.ID); err != nil {
			t.Fatal(err)
		}
	}
}
func mustWrite(t *testing.T, path string, body []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, mode); err != nil {
		t.Fatal(err)
	}
}
func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return body
}
