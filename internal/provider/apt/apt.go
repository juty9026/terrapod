package apt

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"sync"

	"github.com/juty9026/terrapod/internal/execx"
	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/provider"
)

const (
	AptGetPath    = "/usr/bin/apt-get"
	DpkgQueryPath = "/usr/bin/dpkg-query"
	dpkgFormat    = "${binary:Package}\\t${db:Status-Abbrev}\\t${Version}\\t${Essential}\\n"
)

var (
	packagePattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9+.:~-]*$`)
	installPlanPattern = regexp.MustCompile(`^Inst ([a-z0-9][a-z0-9+.:~-]*)(?: \[[^]]+\])? \(.+\)$`)
	removePlanPattern  = regexp.MustCompile(`^Remv ([a-z0-9][a-z0-9+.:~-]*) \[[^]]+\](?: \(.+\))?$`)
	planPrefixPattern  = regexp.MustCompile(`^[A-Z][A-Za-z]{3} `)
)

type Runner interface {
	Run(context.Context, execx.Request) (execx.Result, error)
}

type Adapter struct {
	runner Runner

	mu         sync.RWMutex
	essential  map[string]bool
	refresh    sync.Once
	refreshErr error
}

func New(aptGetPath, dpkgQueryPath string, runner Runner) (*Adapter, error) {
	if aptGetPath != AptGetPath {
		return nil, fmt.Errorf("apt: apt-get path must be %q", AptGetPath)
	}
	if dpkgQueryPath != DpkgQueryPath {
		return nil, fmt.Errorf("apt: dpkg-query path must be %q", DpkgQueryPath)
	}
	if isNilInterface(runner) {
		return nil, errors.New("apt: runner is required")
	}
	return &Adapter{runner: runner, essential: make(map[string]bool)}, nil
}

func (a *Adapter) Name() string { return "apt" }

func (a *Adapter) Inspect(ctx context.Context, resource model.Resource) (model.Observation, error) {
	if err := validateResource(resource); err != nil {
		return model.Observation{}, err
	}
	record, present, err := a.queryRecord(ctx, resource.Package)
	if err != nil {
		return model.Observation{}, err
	}
	if !present {
		return model.Observation{Provider: a.Name(), Package: resource.Package, Paths: map[string]string{}}, nil
	}
	a.mu.Lock()
	a.essential[resource.Package] = record.essential
	a.mu.Unlock()
	detail := ""
	healthy := record.installed
	if record.essential {
		healthy = false
		detail = "Essential package is unavailable for APT management"
	}
	return model.Observation{
		Present: record.installed, Provider: a.Name(), Package: resource.Package,
		Version: record.version, Paths: map[string]string{}, Healthy: healthy, Detail: detail,
	}, nil
}

func (a *Adapter) queryRecord(ctx context.Context, pkg string) (dpkgRecord, bool, error) {
	result, err := a.runner.Run(ctx, execx.Request{
		Path: DpkgQueryPath,
		Args: []string{"--show", "--showformat=" + dpkgFormat, pkg},
	})
	if err != nil && len(result.Stdout) == 0 {
		return dpkgRecord{}, false, nil
	}
	if err != nil {
		return dpkgRecord{}, false, fmt.Errorf("apt: inspect %q: %w", pkg, err)
	}
	record, err := parseDpkgRecord(result.Stdout, pkg)
	if err != nil {
		return dpkgRecord{}, false, fmt.Errorf("apt: parse inventory for %q: %w", pkg, err)
	}
	return record, true, nil
}

func (a *Adapter) Simulate(ctx context.Context, operation model.Operation) (provider.ChangeSet, error) {
	if err := validateOperation(operation); err != nil {
		return provider.ChangeSet{}, err
	}
	if operation.Kind == model.OperationUpgrade || operation.Kind == model.OperationPrune {
		record, present, err := a.queryRecord(ctx, operation.Package)
		if err != nil {
			return provider.ChangeSet{}, err
		}
		if (present && record.essential) || a.isEssential(operation.Package) {
			return provider.ChangeSet{}, fmt.Errorf("apt: refusing %s of Essential package %q", operation.Kind, operation.Package)
		}
	}
	args, err := simulationArgs(operation)
	if err != nil {
		return provider.ChangeSet{}, err
	}
	result, err := a.runner.Run(ctx, execx.Request{Path: AptGetPath, Args: args, Privilege: true})
	if err != nil {
		return provider.ChangeSet{}, fmt.Errorf("apt: simulate %s for %q: %w", operation.Kind, operation.Package, err)
	}
	changes, err := parsePlan(result.Stdout)
	if err != nil {
		return provider.ChangeSet{}, fmt.Errorf("apt: parse simulation for %q: %w", operation.Package, err)
	}
	if err := provider.ValidateChangeSet(changes, model.Resource{Package: operation.Package}, nil); err != nil {
		return provider.ChangeSet{}, err
	}
	if err := validatePlannedOperation(changes, operation); err != nil {
		return provider.ChangeSet{}, err
	}
	return changes, nil
}

func (a *Adapter) Execute(ctx context.Context, operation model.Operation) error {
	if _, err := a.Simulate(ctx, operation); err != nil {
		return err
	}
	args, err := executionArgs(operation)
	if err != nil {
		return err
	}
	if _, err := a.runner.Run(ctx, execx.Request{Path: AptGetPath, Args: args, Privilege: true}); err != nil {
		return fmt.Errorf("apt: execute %s for %q: %w", operation.Kind, operation.Package, err)
	}
	return nil
}

func (a *Adapter) Verify(ctx context.Context, resource model.Resource) (model.Observation, error) {
	observation, err := a.Inspect(ctx, resource)
	if err != nil {
		return model.Observation{}, err
	}
	if !observation.Present || !observation.Healthy {
		return observation, fmt.Errorf("apt: verification failed for %q: %s", resource.Package, observation.Detail)
	}
	return observation, nil
}

func (a *Adapter) RefreshMetadata(ctx context.Context) error {
	a.refresh.Do(func() {
		_, a.refreshErr = a.runner.Run(ctx, execx.Request{Path: AptGetPath, Args: []string{"update"}, Privilege: true})
		if a.refreshErr != nil {
			a.refreshErr = fmt.Errorf("apt: refresh metadata: %w", a.refreshErr)
		}
	})
	return a.refreshErr
}

func (a *Adapter) isEssential(pkg string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.essential[pkg]
}

type dpkgRecord struct {
	installed, essential bool
	version              string
}

func parseDpkgRecord(output []byte, target string) (dpkgRecord, error) {
	text := strings.TrimSuffix(string(output), "\n")
	if text == "" || strings.Contains(text, "\n") {
		return dpkgRecord{}, errors.New("expected exactly one record")
	}
	fields := strings.Split(text, "\t")
	if len(fields) != 4 {
		return dpkgRecord{}, errors.New("expected exactly one record with four fields")
	}
	if fields[0] != target {
		return dpkgRecord{}, fmt.Errorf("binary package %q does not match target %q", fields[0], target)
	}
	if fields[1] != "ii " {
		return dpkgRecord{}, fmt.Errorf("package status %q is not installed", fields[1])
	}
	if fields[2] == "" || strings.TrimSpace(fields[2]) != fields[2] {
		return dpkgRecord{}, errors.New("invalid installed version")
	}
	if fields[3] != "yes" && fields[3] != "no" {
		return dpkgRecord{}, fmt.Errorf("invalid Essential value %q", fields[3])
	}
	return dpkgRecord{installed: true, version: fields[2], essential: fields[3] == "yes"}, nil
}

func parsePlan(output []byte) (provider.ChangeSet, error) {
	var changes provider.ChangeSet
	seen := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSuffix(string(output), "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "Inst ") {
			match := installPlanPattern.FindStringSubmatch(line)
			if match == nil {
				return provider.ChangeSet{}, fmt.Errorf("malformed Inst plan line %q", line)
			}
			kind := "install"
			if strings.Contains(strings.SplitN(line, " (", 2)[0], " [") {
				kind = "upgrade"
			}
			if previous, ok := seen[match[1]]; ok {
				return provider.ChangeSet{}, fmt.Errorf("duplicate plan mutation for %q (%s and %s)", match[1], previous, kind)
			}
			seen[match[1]] = kind
			if kind == "upgrade" {
				changes.Upgrades = append(changes.Upgrades, match[1])
			} else {
				changes.Installs = append(changes.Installs, match[1])
			}
			continue
		}
		if strings.HasPrefix(line, "Remv ") {
			match := removePlanPattern.FindStringSubmatch(line)
			if match == nil {
				return provider.ChangeSet{}, fmt.Errorf("malformed Remv plan line %q", line)
			}
			if previous, ok := seen[match[1]]; ok {
				return provider.ChangeSet{}, fmt.Errorf("duplicate plan mutation for %q (%s and remove)", match[1], previous)
			}
			seen[match[1]] = "remove"
			changes.Removes = append(changes.Removes, match[1])
			continue
		}
		if strings.HasPrefix(line, "Conf ") {
			continue
		}
		if planPrefixPattern.MatchString(line) {
			return provider.ChangeSet{}, fmt.Errorf("unknown plan mutation line %q", line)
		}
	}
	return changes, nil
}

func simulationArgs(operation model.Operation) ([]string, error) {
	switch operation.Kind {
	case model.OperationInstall, model.OperationAdopt:
		return []string{"-s", "install", "--", operation.Package}, nil
	case model.OperationUpgrade:
		return []string{"-s", "install", "--only-upgrade", "--", operation.Package}, nil
	case model.OperationPrune:
		return []string{"-s", "remove", "--", operation.Package}, nil
	default:
		return nil, fmt.Errorf("apt: unsupported operation %q", operation.Kind)
	}
}

func executionArgs(operation model.Operation) ([]string, error) {
	switch operation.Kind {
	case model.OperationInstall, model.OperationAdopt:
		return []string{"install", "-y", "--", operation.Package}, nil
	case model.OperationUpgrade:
		return []string{"install", "--only-upgrade", "-y", "--", operation.Package}, nil
	case model.OperationPrune:
		return []string{"remove", "-y", "--", operation.Package}, nil
	default:
		return nil, fmt.Errorf("apt: unsupported operation %q", operation.Kind)
	}
}

func validatePlannedOperation(changes provider.ChangeSet, operation model.Operation) error {
	if operation.Kind != model.OperationPrune && len(changes.Removes) != 0 {
		return fmt.Errorf("apt: %s plan unexpectedly removes packages", operation.Kind)
	}
	if operation.Kind == model.OperationInstall || operation.Kind == model.OperationAdopt {
		for _, pkg := range changes.Upgrades {
			if pkg == operation.Package {
				return fmt.Errorf("apt: refusing opportunistic upgrade of target %q during normal apply", pkg)
			}
		}
	}
	return nil
}

func validateResource(resource model.Resource) error {
	if resource.Provider != "apt" {
		return fmt.Errorf("apt: resource provider %q does not match apt", resource.Provider)
	}
	if !packagePattern.MatchString(resource.Package) {
		return fmt.Errorf("apt: unsafe package token %q", resource.Package)
	}
	if resource.VersionPolicy != model.VersionTracked {
		return errors.New("apt: package must use tracked version policy")
	}
	if resource.Metadata["bootstrapOnly"] != "true" {
		return errors.New("apt: package must be bootstrapOnly")
	}
	if len(resource.Commands) != 0 {
		return errors.New("apt: catalog commands are not executable authority")
	}
	return nil
}

func validateOperation(operation model.Operation) error {
	if operation.Provider != "apt" {
		return fmt.Errorf("apt: operation provider %q does not match apt", operation.Provider)
	}
	if !packagePattern.MatchString(operation.Package) {
		return fmt.Errorf("apt: unsafe package token %q", operation.Package)
	}
	if !operation.RequiresPrivilege {
		return errors.New("apt: operation requires privilege")
	}
	return nil
}

func isNilInterface(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	}
	return false
}
