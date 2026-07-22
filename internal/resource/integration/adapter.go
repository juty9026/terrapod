package integration

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/juty9026/terrapod/internal/execx"
	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/state"
)

const (
	ProviderJSONFields  = "json-fields"
	ProviderPlistFields = "plist-fields"
	ProviderKarabiner   = "karabiner"

	MetadataHandler  = "integration.handler"
	MetadataPath     = "integration.path"
	MetadataPathGlob = "integration.pathGlob"
	MetadataFields   = "integration.fields"
	MetadataFormat   = "integration.format"

	HandlerFields          = "fields"
	HandlerJetendardZed    = "jetendard-zed"
	HandlerJetendardOrca   = "jetendard-orca"
	HandlerKarabinerOpener = "karabiner-opener"
)

type Karabiner interface {
	Guidance(context.Context) ([]byte, error)
	Open(context.Context) error
}

type CommandRunner interface {
	Run(context.Context, execx.Request) (execx.Result, error)
}

type KarabinerClient struct {
	Runner            CommandRunner
	CLIPath, OpenPath string
}

func (c KarabinerClient) Guidance(ctx context.Context) ([]byte, error) {
	if c.Runner == nil {
		return nil, errors.New("integration: command runner is required")
	}
	cli := c.CLIPath
	if cli == "" {
		cli = "/Library/Application Support/org.pqrs/Karabiner-Elements/bin/karabiner_cli"
	}
	result, err := c.Runner.Run(ctx, execx.Request{Path: cli, Args: []string{"--show-settings-window-guidance"}})
	if err != nil {
		return nil, fmt.Errorf("integration: inspect Karabiner guidance: %w", err)
	}
	return result.Stdout, nil
}
func (c KarabinerClient) Open(ctx context.Context) error {
	if c.Runner == nil {
		return errors.New("integration: command runner is required")
	}
	open := c.OpenPath
	if open == "" {
		open = "/usr/bin/open"
	}
	_, err := c.Runner.Run(ctx, execx.Request{Path: open, Args: []string{"-a", "Karabiner-Elements"}})
	if err != nil {
		return fmt.Errorf("integration: open Karabiner-Elements: %w", err)
	}
	return nil
}

type Adapter struct {
	Home       string
	State      *state.Store
	AppRunning func(string) bool
	Karabiner  Karabiner

	mu     sync.Mutex
	pruned map[model.ResourceID]bool
}

type declaration struct {
	handler, format, path, pathGlob string
	fields                          map[string]any
}
type target struct{ relative, absolute string }
type priorValue struct {
	Exists bool            `json:"exists"`
	Type   string          `json:"type,omitempty"`
	Value  json.RawMessage `json:"value,omitempty"`
}

func (a *Adapter) Inspect(ctx context.Context, item model.Resource) (model.Observation, error) {
	d, err := a.declaration(item)
	if err != nil {
		return model.Observation{}, err
	}
	if d.handler == HandlerKarabinerOpener {
		return a.inspectKarabiner(ctx, item)
	}
	a.mu.Lock()
	wasPruned := a.pruned != nil && a.pruned[item.ID]
	a.mu.Unlock()
	if wasPruned {
		return model.Observation{Provider: item.Provider, Package: item.Package, Healthy: true}, nil
	}
	targets, err := a.targets(d)
	if err != nil {
		return model.Observation{}, err
	}
	owned, err := a.ownership(item)
	if err != nil {
		return model.Observation{}, err
	}
	present := len(targets) > 0 || d.pathGlob != ""
	healthy := true
	paths := make(map[string]string)
	for _, target := range targets {
		doc, exists, err := a.readDocument(d, target)
		if err != nil {
			return model.Observation{}, err
		}
		if !exists {
			present = false
			healthy = false
			continue
		}
		for pointer, desired := range d.fields {
			current, err := doc.get(pointer)
			if err != nil {
				return model.Observation{}, fmt.Errorf("integration: %s: %w", target.relative, err)
			}
			if !current.Exists || !sameValue(current.Value, desired) {
				healthy = false
			}
			paths[fieldKey(target.relative, pointer)] = digestValue(current)
		}
	}
	// A matching unowned setting still needs one Adopt execution to capture its prior value.
	if owned.ResourceID == "" {
		healthy = false
	}
	detail := "settings differ from [redacted]"
	if healthy {
		detail = "settings match [redacted]"
	}
	return model.Observation{Present: present, Healthy: healthy, Provider: item.Provider, Package: item.Package, Paths: paths, Detail: detail}, nil
}

func (a *Adapter) Verify(ctx context.Context, item model.Resource) (model.Observation, error) {
	observed, err := a.Inspect(ctx, item)
	if err != nil {
		return model.Observation{}, err
	}
	if item.Provider == ProviderKarabiner {
		return observed, nil
	}
	if !observed.Present || !observed.Healthy {
		return observed, nil
	}
	return observed, nil
}

func (a *Adapter) Plan(ctx context.Context, item model.Resource, _ model.Observation, owned model.Ownership) ([]model.Operation, error) {
	d, err := a.declaration(item)
	if err != nil {
		return nil, err
	}
	if d.handler == HandlerKarabinerOpener {
		observed, err := a.inspectKarabiner(ctx, item)
		if err != nil {
			return nil, err
		}
		if observed.Healthy {
			return nil, nil
		}
		return []model.Operation{operation(item, model.OperationInstall)}, nil
	}
	if owned.ResourceID == "" {
		owned, err = a.ownership(item)
		if err != nil {
			return nil, err
		}
	}
	if owned.ResourceID != "" {
		if err := validatePriorKeys(d, owned.PriorValues); err != nil {
			return nil, err
		}
	}
	targets, err := a.targets(d)
	if err != nil {
		return nil, err
	}
	changed, receiptMissing := false, false
	for _, target := range targets {
		doc, _, err := a.readDocument(d, target)
		if err != nil {
			return nil, err
		}
		for pointer, desired := range d.fields {
			current, err := doc.get(pointer)
			if err != nil {
				return nil, err
			}
			matchesDesired := sameField(current, fieldValue{Exists: true, Value: desired})
			if owned.ResourceID == "" {
				changed = changed || !matchesDesired
				continue
			}
			_, hasPrior := owned.PriorValues[fieldKey(target.relative, pointer)]
			if !hasPrior {
				if d.pathGlob == "" {
					return nil, fmt.Errorf("integration: missing prior receipt for %s%s", target.relative, pointer)
				}
				receiptMissing = true
				changed = changed || !matchesDesired
				continue
			}
			if matchesDesired {
				continue
			}
			changed = true
			prior, ok, err := decodePrior(owned.PriorValues[fieldKey(target.relative, pointer)])
			if err != nil {
				return nil, err
			}
			if !ok || !sameField(current, prior) {
				return nil, fmt.Errorf("integration conflict at %s%s: field changed after Terrapod apply", target.relative, pointer)
			}
		}
	}
	if !changed && !receiptMissing && owned.ResourceID != "" {
		return nil, nil
	}
	if changed && d.handler == HandlerJetendardOrca && a.appRunning("Orca") {
		return nil, errors.New("integration deferred: Orca is running; quit Orca and retry")
	}
	kind := model.OperationInstall
	if !changed {
		kind = model.OperationAdopt
	} else if owned.ResourceID != "" {
		kind = model.OperationRestore
	}
	return []model.Operation{operation(item, kind)}, nil
}

func (a *Adapter) PlanHistorical(_ context.Context, item model.Resource, _ model.Observation, owned model.Ownership) ([]model.Operation, error) {
	d, err := a.declaration(item)
	if err != nil {
		return nil, err
	}
	if d.handler == HandlerKarabinerOpener {
		return []model.Operation{operation(item, model.OperationPrune)}, nil
	}
	if owned.ResourceID != item.ID || owned.Provider != item.Provider || owned.Package != item.Package {
		return nil, errors.New("integration: historical ownership identity mismatch")
	}
	targets, err := a.targets(d)
	if err != nil {
		return nil, err
	}
	if err := validatePriorKeys(d, owned.PriorValues); err != nil {
		return nil, err
	}
	needsMutation := false
	for _, target := range targets {
		doc, _, err := a.readDocument(d, target)
		if err != nil {
			return nil, err
		}
		for pointer, desired := range d.fields {
			current, err := doc.get(pointer)
			if err != nil {
				return nil, err
			}
			prior, ok, err := decodePrior(owned.PriorValues[fieldKey(target.relative, pointer)])
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, fmt.Errorf("integration: missing prior receipt for %s%s", target.relative, pointer)
			}
			if !sameField(current, fieldValue{Exists: true, Value: desired}) && !sameField(current, prior) {
				return nil, fmt.Errorf("integration conflict at %s%s: refusing to prune a user edit", target.relative, pointer)
			}
			needsMutation = needsMutation || !sameField(current, prior)
		}
	}
	if needsMutation && d.handler == HandlerJetendardOrca && a.appRunning("Orca") {
		return nil, errors.New("integration deferred: Orca is running; quit Orca and retry")
	}
	op := operation(item, model.OperationPrune)
	op.Removes = []string{item.Package}
	return []model.Operation{op}, nil
}

func (a *Adapter) Execute(context.Context, model.Operation) model.OperationResult {
	return model.OperationResult{Detail: "integration: signed resource is required", FinishedAt: time.Now().UTC()}
}

func (a *Adapter) ExecuteResource(ctx context.Context, item model.Resource, op model.Operation) model.OperationResult {
	result := model.OperationResult{OperationID: op.ID, ResourceID: op.ResourceID, FinishedAt: time.Now().UTC()}
	if err := a.execute(ctx, item, op); err != nil {
		result.Detail = err.Error()
		return result
	}
	result.Success = true
	result.Detail = "integration fields [redacted] verified"
	return result
}

func (a *Adapter) execute(ctx context.Context, item model.Resource, op model.Operation) error {
	if op.ResourceID != item.ID || op.Provider != item.Provider || op.Package != item.Package {
		return errors.New("integration: operation identity mismatch")
	}
	if err := a.authorize(op); err != nil {
		return err
	}
	d, err := a.declaration(item)
	if err != nil {
		return err
	}
	if d.handler == HandlerKarabinerOpener {
		if op.Kind == model.OperationPrune {
			a.markPruned(item.ID)
			return nil
		}
		if a.Karabiner == nil {
			return errors.New("integration: Karabiner action is unavailable")
		}
		if err := a.Karabiner.Open(ctx); err != nil {
			return err
		}
		return nil
	}
	if op.Kind == model.OperationPrune {
		if !sameStrings(op.Removes, []string{item.Package}) {
			return errors.New("integration: prune authority mismatch")
		}
		owned, err := a.ownership(item)
		if err != nil {
			return err
		}
		needsMutation, err := a.requiresRestore(d, owned)
		if err != nil {
			return err
		}
		if needsMutation && d.handler == HandlerJetendardOrca && a.appRunning("Orca") {
			return errors.New("integration deferred: Orca is running; quit Orca and retry")
		}
		if err := a.restore(d, owned); err != nil {
			return err
		}
		a.markPruned(item.ID)
		return nil
	}
	if len(op.Removes) != 0 {
		return errors.New("integration: non-prune operation cannot remove packages")
	}
	targets, err := a.targets(d)
	if err != nil {
		return err
	}
	needsMutation, err := a.requiresApply(d, targets)
	if err != nil {
		return err
	}
	if needsMutation && d.handler == HandlerJetendardOrca && a.appRunning("Orca") {
		return errors.New("integration deferred: Orca is running; quit Orca and retry")
	}
	owned, err := a.ownership(item)
	if err != nil {
		return err
	}
	if owned.ResourceID == "" || d.pathGlob != "" {
		priors, err := a.captureMissing(d, targets, owned.PriorValues)
		if err != nil {
			return err
		}
		if owned.ResourceID == "" {
			owned = model.Ownership{ResourceID: item.ID, Provider: item.Provider, Package: item.Package, Paths: map[string]string{}, PriorValues: priors}
		} else {
			owned.PriorValues = priors
		}
		if err := a.State.PutOwnership(owned); err != nil {
			return fmt.Errorf("integration: persist prior values: %w", err)
		}
	}
	return a.apply(d, targets)
}

func (a *Adapter) requiresApply(d declaration, targets []target) (bool, error) {
	for _, target := range targets {
		doc, _, err := a.readDocument(d, target)
		if err != nil {
			return false, err
		}
		for pointer, desired := range d.fields {
			current, err := doc.get(pointer)
			if err != nil {
				return false, err
			}
			if !sameField(current, fieldValue{Exists: true, Value: desired}) {
				return true, nil
			}
		}
	}
	return false, nil
}
func (a *Adapter) requiresRestore(d declaration, owned model.Ownership) (bool, error) {
	targets, err := a.targets(d)
	if err != nil {
		return false, err
	}
	for _, target := range targets {
		doc, _, err := a.readDocument(d, target)
		if err != nil {
			return false, err
		}
		for pointer := range d.fields {
			prior, ok, err := decodePrior(owned.PriorValues[fieldKey(target.relative, pointer)])
			if err != nil {
				return false, err
			}
			if !ok {
				return false, errors.New("integration: missing prior receipt")
			}
			current, err := doc.get(pointer)
			if err != nil {
				return false, err
			}
			if !sameField(current, prior) {
				return true, nil
			}
		}
	}
	return false, nil
}

func (a *Adapter) captureMissing(d declaration, targets []target, existing map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	priors := make(map[string]json.RawMessage, len(existing))
	for key, value := range existing {
		priors[key] = append(json.RawMessage(nil), value...)
	}
	for _, target := range targets {
		doc, _, err := a.readDocument(d, target)
		if err != nil {
			return nil, err
		}
		for pointer := range d.fields {
			key := fieldKey(target.relative, pointer)
			if _, ok := priors[key]; ok {
				continue
			}
			value, err := doc.get(pointer)
			if err != nil {
				return nil, err
			}
			raw, err := encodePrior(value)
			if err != nil {
				return nil, err
			}
			priors[key] = raw
		}
	}
	return priors, nil
}

func (a *Adapter) apply(d declaration, targets []target) error {
	type change struct {
		target target
		doc    *document
	}
	changes := make([]change, 0, len(targets))
	for _, target := range targets {
		doc, _, err := a.readDocument(d, target)
		if err != nil {
			return err
		}
		dirty := false
		for pointer, value := range d.fields {
			current, err := doc.get(pointer)
			if err != nil {
				return err
			}
			if sameField(current, fieldValue{Exists: true, Value: value}) {
				continue
			}
			dirty = true
			if err := doc.set(pointer, value); err != nil {
				return err
			}
		}
		if dirty {
			changes = append(changes, change{target, doc})
		}
	}
	for _, change := range changes {
		contents, err := change.doc.bytes()
		if err != nil {
			return err
		}
		if err := a.atomicWrite(change.target.absolute, contents); err != nil {
			return err
		}
	}
	return nil
}

func (a *Adapter) restore(d declaration, owned model.Ownership) error {
	targets, err := a.targets(d)
	if err != nil {
		return err
	}
	type change struct {
		target target
		doc    *document
	}
	var changes []change
	for _, target := range targets {
		doc, exists, err := a.readDocument(d, target)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		for pointer := range d.fields {
			prior, ok, err := decodePrior(owned.PriorValues[fieldKey(target.relative, pointer)])
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("integration: missing prior receipt for %s%s", target.relative, pointer)
			}
			if prior.Exists {
				if err := doc.set(pointer, prior.Value); err != nil {
					return err
				}
			} else {
				if err := doc.remove(pointer); err != nil {
					return err
				}
			}
		}
		changes = append(changes, change{target, doc})
	}
	for _, change := range changes {
		contents, err := change.doc.bytes()
		if err != nil {
			return err
		}
		if err := a.atomicWrite(change.target.absolute, contents); err != nil {
			return err
		}
	}
	return nil
}

func (a *Adapter) declaration(item model.Resource) (declaration, error) {
	if item.Type != model.ResourceIntegration {
		return declaration{}, errors.New("integration: resource type mismatch")
	}
	d := declaration{handler: item.Metadata[MetadataHandler], format: item.Metadata[MetadataFormat], path: item.Metadata[MetadataPath], pathGlob: item.Metadata[MetadataPathGlob]}
	allowed := false
	switch item.Provider {
	case ProviderJSONFields:
		allowed = d.handler == HandlerFields || d.handler == HandlerJetendardZed || d.handler == HandlerJetendardOrca
	case ProviderPlistFields:
		allowed = d.handler == HandlerFields
	case ProviderKarabiner:
		allowed = d.handler == HandlerKarabinerOpener
	}
	if !allowed {
		return declaration{}, fmt.Errorf("integration: unsupported compiled handler %q for provider %q", d.handler, item.Provider)
	}
	if d.handler == HandlerKarabinerOpener {
		if item.Metadata[MetadataFields] != "" || d.path != "" || d.pathGlob != "" {
			return declaration{}, errors.New("integration: Karabiner opener cannot declare owned fields")
		}
		return d, nil
	}
	if (d.path == "") == (d.pathGlob == "") {
		return declaration{}, errors.New("integration: exactly one path or pathGlob is required")
	}
	if d.format == "" {
		if item.Provider == ProviderPlistFields {
			d.format = "plist"
		} else {
			d.format = "json"
		}
	}
	if item.Provider == ProviderJSONFields && d.format != "json" && d.format != "jsonc" {
		return declaration{}, errors.New("integration: JSON provider format mismatch")
	}
	if item.Provider == ProviderPlistFields && d.format != "plist" {
		return declaration{}, errors.New("integration: plist provider format mismatch")
	}
	decoder := json.NewDecoder(strings.NewReader(item.Metadata[MetadataFields]))
	decoder.UseNumber()
	if err := decoder.Decode(&d.fields); err != nil {
		return declaration{}, fmt.Errorf("integration: malformed field declaration: %w", err)
	}
	if len(d.fields) == 0 {
		return declaration{}, errors.New("integration: no fields declared")
	}
	for pointer := range d.fields {
		if _, err := pointerParts(pointer); err != nil {
			return declaration{}, err
		}
	}
	return d, nil
}

func (a *Adapter) targets(d declaration) ([]target, error) {
	if a.Home == "" {
		return nil, errors.New("integration: home is required")
	}
	relatives := []string{d.path}
	if d.pathGlob != "" {
		pattern, err := a.safePath(d.pathGlob, true)
		if err != nil {
			return nil, err
		}
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, err
		}
		relatives = nil
		for _, match := range matches {
			rel, err := filepath.Rel(a.Home, match)
			if err != nil {
				return nil, err
			}
			relatives = append(relatives, filepath.ToSlash(rel))
		}
		sort.Strings(relatives)
	}
	result := make([]target, 0, len(relatives))
	for _, relative := range relatives {
		absolute, err := a.safePath(relative, false)
		if err != nil {
			return nil, err
		}
		result = append(result, target{relative: filepath.ToSlash(filepath.Clean(relative)), absolute: absolute})
	}
	return result, nil
}

func (a *Adapter) safePath(relative string, allowGlob bool) (string, error) {
	if relative == "" || filepath.IsAbs(relative) {
		return "", fmt.Errorf("integration: unsafe path %q", relative)
	}
	clean := filepath.Clean(relative)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("integration: unsafe path %q", relative)
	}
	if !allowGlob && strings.ContainsAny(clean, "*?[") {
		return "", fmt.Errorf("integration: glob is not allowed in path %q", relative)
	}
	abs := filepath.Join(a.Home, clean)
	limit := abs
	if allowGlob {
		for strings.ContainsAny(filepath.Base(limit), "*?[") {
			limit = filepath.Dir(limit)
		}
	} else {
		limit = filepath.Dir(limit)
	}
	for current := limit; ; current = filepath.Dir(current) {
		if current == a.Home {
			break
		}
		info, err := os.Lstat(current)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("integration: symlink parent %s", current)
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", errors.New("integration: path escapes home")
		}
	}
	return abs, nil
}

type document struct {
	format, text string
	root         map[string]any
}

func (a *Adapter) readDocument(d declaration, target target) (*document, bool, error) {
	contents, err := os.ReadFile(target.absolute)
	exists := err == nil
	if errors.Is(err, os.ErrNotExist) {
		contents = nil
		err = nil
	} else if err != nil {
		return nil, false, err
	}
	if exists {
		info, err := os.Lstat(target.absolute)
		if err != nil {
			return nil, false, err
		}
		if !info.Mode().IsRegular() {
			return nil, false, fmt.Errorf("integration: config is not a regular file: %s", target.relative)
		}
	}
	doc := &document{format: d.format}
	switch d.format {
	case "json":
		if !exists {
			doc.root = make(map[string]any)
		} else {
			doc.root, err = decodeJSON(contents)
		}
	case "jsonc":
		if !exists {
			doc.text = "{\n}\n"
		} else {
			doc.text = string(contents)
		}
		_, err = parseJSONC(doc.text)
	case "plist":
		if !exists {
			doc.root = make(map[string]any)
		} else {
			doc.root, err = decodePlist(contents)
		}
	}
	if err != nil {
		return nil, false, fmt.Errorf("integration: malformed %s config %s: %w", d.format, target.relative, err)
	}
	return doc, exists, nil
}
func (d *document) get(pointer string) (fieldValue, error) {
	if d.format == "jsonc" {
		return jsoncGet(d.text, pointer)
	}
	return getField(d.root, pointer)
}
func (d *document) set(pointer string, value any) error {
	if raw, ok := value.(json.RawMessage); ok {
		var decoded any
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.UseNumber()
		if err := decoder.Decode(&decoded); err != nil {
			return err
		}
		value = decoded
	}
	if d.format == "jsonc" {
		text, err := jsoncSet(d.text, pointer, value)
		if err == nil {
			d.text = text
		}
		return err
	}
	return setField(d.root, pointer, value)
}
func (d *document) remove(pointer string) error {
	if d.format == "jsonc" {
		text, err := jsoncRemove(d.text, pointer)
		d.text = text
		return err
	}
	return removeField(d.root, pointer)
}
func (d *document) bytes() ([]byte, error) {
	switch d.format {
	case "jsonc":
		return []byte(d.text), nil
	case "plist":
		return encodePlist(d.root)
	default:
		return encodeJSON(d.root)
	}
}

func (a *Adapter) atomicWrite(path string, contents []byte) error {
	if _, err := a.safePath(mustRelative(a.Home, path), false); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return errors.New("integration: target is not regular")
		}
		mode = info.Mode().Perm()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".terrapod-integration-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if err := file.Chmod(mode); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(contents); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func (a *Adapter) ownership(item model.Resource) (model.Ownership, error) {
	if a.State == nil {
		return model.Ownership{}, errors.New("integration: state store is required")
	}
	snapshot, err := a.State.Snapshot()
	if err != nil {
		return model.Ownership{}, err
	}
	owned := snapshot.Ownership[item.ID]
	if owned.ResourceID != "" && (owned.ResourceID != item.ID || owned.Provider != item.Provider || owned.Package != item.Package) {
		return model.Ownership{}, errors.New("integration: ownership identity mismatch")
	}
	return owned, nil
}
func (a *Adapter) authorize(op model.Operation) error {
	if a.State == nil {
		return errors.New("integration: state store is required")
	}
	snapshot, err := a.State.Snapshot()
	if err != nil {
		return err
	}
	if snapshot.ActiveJournal == nil || snapshot.ActiveJournal.Status != "active" {
		return errors.New("integration: active journal authority is required")
	}
	matches := 0
	for _, planned := range snapshot.ActiveJournal.Plan.Operations {
		if reflect.DeepEqual(planned, op) {
			matches++
		}
	}
	if matches != 1 {
		return errors.New("integration: operation is not authorized by active journal")
	}
	return nil
}
func (a *Adapter) appRunning(name string) bool {
	if a.AppRunning == nil {
		return false
	}
	return a.AppRunning(name)
}
func (a *Adapter) markPruned(id model.ResourceID) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.pruned == nil {
		a.pruned = make(map[model.ResourceID]bool)
	}
	a.pruned[id] = true
}

func (a *Adapter) inspectKarabiner(ctx context.Context, item model.Resource) (model.Observation, error) {
	if a.Karabiner == nil {
		return model.Observation{Present: true, Healthy: true, Provider: item.Provider, Package: item.Package, Detail: "Karabiner action unavailable; no general state is owned"}, nil
	}
	raw, err := a.Karabiner.Guidance(ctx)
	if err != nil {
		return model.Observation{}, err
	}
	need, err := karabinerNeedsAction(raw)
	if err != nil {
		return model.Observation{}, err
	}
	return model.Observation{Present: true, Healthy: !need, Provider: item.Provider, Package: item.Package, Detail: "Karabiner guidance [redacted]"}, nil
}
func karabinerNeedsAction(raw []byte) (bool, error) {
	if len(raw) == 0 {
		return false, nil
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return false, fmt.Errorf("integration: malformed Karabiner guidance: %w", err)
	}
	for _, key := range []string{"current_setup", "current_alert"} {
		if value, ok := root[key].(string); ok && value != "" && value != "none" {
			return true, nil
		}
	}
	for _, pointer := range []string{
		"/core_service_daemon_state/bundle_permission_check_result/accessibility_process_trusted",
		"/core_service_daemon_state/bundle_permission_check_result/iohid_listen_event_allowed",
		"/core_service_daemon_state/current_process_permission_check_result/accessibility_process_trusted",
		"/core_service_daemon_state/current_process_permission_check_result/iohid_listen_event_allowed",
		"/core_service_daemon_state/driver_activated",
		"/core_service_daemon_state/driver_connected",
		"/core_service_daemon_state/virtual_hid_device_service_client_connected",
		"/core_service_daemon_state/virtual_hid_keyboard_ready",
		"/guidance_context/core_agents_enabled", "/guidance_context/core_agents_running",
		"/guidance_context/core_daemons_enabled", "/guidance_context/core_daemons_running",
		"/guidance_context/services_enabled", "/guidance_context/services_running",
	} {
		value, err := getField(root, pointer)
		if err != nil {
			return false, err
		}
		if value.Exists && value.Value == false {
			return true, nil
		}
	}
	return false, nil
}

func operation(item model.Resource, kind model.OperationKind) model.Operation {
	return model.Operation{ID: "integration:" + string(item.ID) + ":" + string(kind), ResourceID: item.ID, Kind: kind, Provider: item.Provider, Package: item.Package, Detail: "apply fields [redacted]"}
}
func fieldKey(path, pointer string) string {
	return filepath.ToSlash(filepath.Clean(path)) + "#" + pointer
}
func encodePrior(value fieldValue) (json.RawMessage, error) {
	var raw json.RawMessage
	typeName := ""
	if value.Exists {
		switch value.Value.(type) {
		case []byte:
			typeName = "data"
		case time.Time:
			typeName = "date"
		}
		contents, err := json.Marshal(value.Value)
		if err != nil {
			return nil, err
		}
		raw = contents
	}
	return json.Marshal(priorValue{Exists: value.Exists, Type: typeName, Value: raw})
}
func decodePrior(raw json.RawMessage) (fieldValue, bool, error) {
	if len(raw) == 0 {
		return fieldValue{}, false, nil
	}
	var prior priorValue
	if err := json.Unmarshal(raw, &prior); err != nil {
		return fieldValue{}, false, err
	}
	if !prior.Exists {
		return fieldValue{}, true, nil
	}
	var value any
	decoder := json.NewDecoder(strings.NewReader(string(prior.Value)))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return fieldValue{}, false, err
	}
	switch prior.Type {
	case "data":
		encoded, ok := value.(string)
		if !ok {
			return fieldValue{}, false, errors.New("integration: invalid data prior")
		}
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return fieldValue{}, false, err
		}
		value = decoded
	case "date":
		encoded, ok := value.(string)
		if !ok {
			return fieldValue{}, false, errors.New("integration: invalid date prior")
		}
		parsed, err := time.Parse(time.RFC3339, encoded)
		if err != nil {
			return fieldValue{}, false, err
		}
		value = parsed
	case "":
	default:
		return fieldValue{}, false, errors.New("integration: invalid prior type")
	}
	return fieldValue{Exists: true, Value: value}, true, nil
}

func validatePriorKeys(d declaration, values map[string]json.RawMessage) error {
	for key, raw := range values {
		split := strings.LastIndex(key, "#")
		if split < 0 {
			return errors.New("integration: malformed prior key")
		}
		relative, pointer := key[:split], key[split+1:]
		if _, ok := d.fields[pointer]; !ok {
			return fmt.Errorf("integration: prior field %q is outside signed declaration", pointer)
		}
		if d.path != "" && relative != filepath.ToSlash(filepath.Clean(d.path)) {
			return errors.New("integration: prior path is outside signed declaration")
		}
		if d.pathGlob != "" {
			matched, err := filepath.Match(filepath.FromSlash(d.pathGlob), filepath.FromSlash(relative))
			if err != nil || !matched {
				return errors.New("integration: prior path is outside signed glob")
			}
		}
		if _, _, err := decodePrior(raw); err != nil {
			return err
		}
	}
	return nil
}
func sameField(a, b fieldValue) bool {
	return a.Exists == b.Exists && (!a.Exists || sameValue(a.Value, b.Value))
}
func sameValue(a, b any) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return string(left) == string(right)
}
func digestValue(value fieldValue) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}
func mustRelative(root, path string) string { relative, _ := filepath.Rel(root, path); return relative }
func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
