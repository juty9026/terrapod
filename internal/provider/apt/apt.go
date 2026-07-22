package apt

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"reflect"
	"regexp"
	"strconv"
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
	binaryPackagePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9+.~-]*(?::[a-z0-9][a-z0-9-]*)?$`)
	installPlanPattern   = regexp.MustCompile(`^Inst ([a-z0-9][a-z0-9+.~-]*(?::[a-z0-9][a-z0-9-]*)?)(?: \[[^]]+\])? \(.+\)$`)
	removePlanPattern    = regexp.MustCompile(`^Remv ([a-z0-9][a-z0-9+.~-]*(?::[a-z0-9][a-z0-9-]*)?) \[[^]]+\](?: \(.+\))?$`)
	confPlanPattern      = regexp.MustCompile(`^Conf ([a-z0-9][a-z0-9+.~-]*(?::[a-z0-9][a-z0-9-]*)?)(?: \(.+\))?$`)
	summaryPattern       = regexp.MustCompile(`^([0-9]+) upgraded, ([0-9]+) newly installed, ([0-9]+) to remove and ([0-9]+) not upgraded\.$`)
	packageListPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9+.~:-]*(?: [a-z0-9][a-z0-9+.~:-]*)*$`)
)

type Runner interface {
	Run(context.Context, execx.Request) (execx.Result, error)
}

type Adapter struct {
	runner Runner

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
	return &Adapter{runner: runner}, nil
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
		Env:  map[string]string{"LC_ALL": "C"},
	})
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return dpkgRecord{}, false, fmt.Errorf("apt: inspect %q: %w", pkg, ctxErr)
		}
		if exitCode, single := singleUnwrapExitCode(err); len(result.Stdout) == 0 && single && exitCode == 1 {
			return dpkgRecord{}, false, nil
		}
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
	args, err := simulationArgs(operation)
	if err != nil {
		return provider.ChangeSet{}, err
	}
	result, err := a.runner.Run(ctx, aptGetRequest(args, true))
	if err != nil {
		return provider.ChangeSet{}, fmt.Errorf("apt: simulate %s for %q: %w", operation.Kind, operation.Package, err)
	}
	changes, err := parsePlan(result.Stdout, operation.Package)
	if err != nil {
		return provider.ChangeSet{}, fmt.Errorf("apt: parse simulation for %q: %w", operation.Package, err)
	}
	for _, pkg := range append(append([]string(nil), changes.Upgrades...), changes.Removes...) {
		record, present, err := a.queryRecord(ctx, pkg)
		if err != nil {
			return provider.ChangeSet{}, err
		}
		if present && record.essential {
			return provider.ChangeSet{}, fmt.Errorf("apt: refusing plan containing Essential package %q", pkg)
		}
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
	if _, err := a.runner.Run(ctx, aptGetRequest(args, true)); err != nil {
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
		_, a.refreshErr = a.runner.Run(ctx, aptGetRequest([]string{"update"}, true))
		if a.refreshErr != nil {
			a.refreshErr = fmt.Errorf("apt: refresh metadata: %w", a.refreshErr)
		}
	})
	return a.refreshErr
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
	if _, err := normalizeTargetPackage(fields[0], target); err != nil {
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

func parsePlan(output []byte, target string) (provider.ChangeSet, error) {
	var changes provider.ChangeSet
	seen := make(map[string]string)
	instIdentities := make(map[string]string)
	confIdentities := make(map[string]string)
	summaryFound := false
	summary := [3]int{}
	allowPackageList := false
	for _, line := range strings.Split(strings.TrimSuffix(string(output), "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if match := summaryPattern.FindStringSubmatch(line); match != nil {
			allowPackageList = false
			if summaryFound {
				return provider.ChangeSet{}, errors.New("multiple simulation summaries")
			}
			summaryFound = true
			for index := range 3 {
				value, err := strconv.Atoi(match[index+1])
				if err != nil {
					return provider.ChangeSet{}, errors.New("invalid simulation summary count")
				}
				summary[index] = value
			}
			continue
		}
		if strings.HasPrefix(line, "Inst ") {
			allowPackageList = false
			match := installPlanPattern.FindStringSubmatch(line)
			if match == nil {
				return provider.ChangeSet{}, fmt.Errorf("malformed Inst plan line %q", line)
			}
			pkg, err := normalizeTargetPackage(match[1], target)
			if err != nil {
				pkg = match[1]
			}
			kind := "install"
			if strings.Contains(strings.SplitN(line, " (", 2)[0], " [") {
				kind = "upgrade"
			}
			if previous, ok := seen[pkg]; ok {
				return provider.ChangeSet{}, fmt.Errorf("duplicate plan mutation for %q (%s and %s)", pkg, previous, kind)
			}
			seen[pkg] = kind
			instIdentities[match[1]] = pkg
			if kind == "upgrade" {
				changes.Upgrades = append(changes.Upgrades, pkg)
			} else {
				changes.Installs = append(changes.Installs, pkg)
			}
			continue
		}
		if strings.HasPrefix(line, "Remv ") {
			allowPackageList = false
			match := removePlanPattern.FindStringSubmatch(line)
			if match == nil {
				return provider.ChangeSet{}, fmt.Errorf("malformed Remv plan line %q", line)
			}
			pkg, err := normalizeTargetPackage(match[1], target)
			if err != nil {
				pkg = match[1]
			}
			if previous, ok := seen[pkg]; ok {
				return provider.ChangeSet{}, fmt.Errorf("duplicate plan mutation for %q (%s and remove)", pkg, previous)
			}
			seen[pkg] = "remove"
			changes.Removes = append(changes.Removes, pkg)
			continue
		}
		if strings.HasPrefix(line, "Conf ") {
			allowPackageList = false
			match := confPlanPattern.FindStringSubmatch(line)
			if match == nil {
				return provider.ChangeSet{}, fmt.Errorf("malformed Conf plan line %q", line)
			}
			pkg, err := normalizeTargetPackage(match[1], target)
			if err != nil {
				pkg = match[1]
			}
			if _, duplicate := confIdentities[match[1]]; duplicate {
				return provider.ChangeSet{}, fmt.Errorf("duplicate Conf for %q", match[1])
			}
			confIdentities[match[1]] = pkg
			continue
		}
		if knownHeader(line) {
			allowPackageList = false
			continue
		}
		if knownListHeader(line) {
			allowPackageList = true
			continue
		}
		if allowPackageList && packageListPattern.MatchString(line) {
			continue
		}
		return provider.ChangeSet{}, fmt.Errorf("unknown simulation output line %q", line)
	}
	if !summaryFound {
		return provider.ChangeSet{}, errors.New("missing English simulation summary")
	}
	for raw, normalized := range confIdentities {
		instNormalized, ok := instIdentities[raw]
		if !ok || instNormalized != normalized {
			return provider.ChangeSet{}, fmt.Errorf("Conf package %q does not correspond to an exact Inst package", raw)
		}
	}
	if summary[0] != len(changes.Upgrades) || summary[1] != len(changes.Installs) || summary[2] != len(changes.Removes) {
		return provider.ChangeSet{}, fmt.Errorf("simulation summary counts do not match parsed changes")
	}
	return changes, nil
}

func singleUnwrapExitCode(err error) (int, bool) {
	for err != nil {
		if _, multi := err.(interface{ Unwrap() []error }); multi {
			return 0, false
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), true
		}
		unwrapper, ok := err.(interface{ Unwrap() error })
		if !ok {
			return 0, false
		}
		err = unwrapper.Unwrap()
	}
	return 0, false
}

func normalizeTargetPackage(candidate, target string) (string, error) {
	if !binaryPackagePattern.MatchString(candidate) || !binaryPackagePattern.MatchString(target) {
		return "", errors.New("malformed binary package")
	}
	if candidate == target {
		return target, nil
	}
	if strings.Contains(target, ":") {
		return "", errors.New("qualified target mismatch")
	}
	base, _, ok := strings.Cut(candidate, ":")
	if ok && base == target {
		return target, nil
	}
	return "", errors.New("base package mismatch")
}

func knownHeader(line string) bool {
	for _, exact := range []string{"NOTE: This is only a simulation!", "Reading package lists...", "Building dependency tree...", "Reading state information...", "Calculating upgrade...", "Use 'sudo apt autoremove' to remove them."} {
		if line == exact {
			return true
		}
	}
	for _, prefix := range []string{"apt-get needs root privileges for real execution.", "Keep also in mind that locking is deactivated,", "so don't depend on the relevance to the real current situation!"} {
		if line == prefix {
			return true
		}
	}
	return false
}

func knownListHeader(line string) bool {
	for _, exact := range []string{"The following additional packages will be installed:", "The following NEW packages will be installed:", "The following packages will be upgraded:", "The following packages will be REMOVED:", "The following packages have been kept back:", "The following packages were automatically installed and are no longer required:", "Suggested packages:", "Recommended packages:"} {
		if line == exact {
			return true
		}
	}
	return false
}

func aptGetRequest(args []string, privilege bool) execx.Request {
	return execx.Request{Path: AptGetPath, Args: args, Env: map[string]string{"LC_ALL": "C"}, Privilege: privilege}
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
	if !binaryPackagePattern.MatchString(resource.Package) {
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
	if !binaryPackagePattern.MatchString(operation.Package) {
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
