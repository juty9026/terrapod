package model

import (
	"encoding/json"
	"fmt"
	"regexp"
	"time"
)

type ResourceID string

type Profile string

const (
	ProfileMacOSTerminal Profile = "macos-terminal"
	ProfileVPSShell      Profile = "vps-shell"
)

func (p Profile) Supported() bool {
	return p == ProfileMacOSTerminal || p == ProfileVPSShell
}

type ResourceType string

const (
	ResourcePackage        ResourceType = "package"
	ResourceManagedFiles   ResourceType = "managed-files"
	ResourceGitCheckout    ResourceType = "git-checkout"
	ResourceArchive        ResourceType = "archive"
	ResourceIntegration    ResourceType = "integration"
	ResourceManagementCore ResourceType = "management-core"
)

type VersionPolicy string

const (
	VersionTracked VersionPolicy = "tracked"
	VersionPinned  VersionPolicy = "pinned"
)

type ResourceState string

const (
	ResourceReady       ResourceState = "ready"
	ResourceUnavailable ResourceState = "unavailable"
)

type OperationKind string

const (
	OperationAdopt    OperationKind = "adopt"
	OperationInstall  OperationKind = "install"
	OperationUpgrade  OperationKind = "upgrade"
	OperationTransfer OperationKind = "transfer"
	OperationPrune    OperationKind = "prune"
	OperationRestore  OperationKind = "restore"
	OperationVerify   OperationKind = "verify"
)

type Config struct {
	Version  int            `json:"version"`
	Terrapod map[string]any `json:"terrapod"`
}

type ConfigField struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Required bool   `json:"required"`
	Default  any    `json:"default,omitempty"`
}

type ConfigSchema struct {
	Version int           `json:"version"`
	Fields  []ConfigField `json:"fields"`
}

type Resource struct {
	ID            ResourceID        `json:"id"`
	Type          ResourceType      `json:"type"`
	Profiles      []Profile         `json:"profiles"`
	DependsOn     []ResourceID      `json:"dependsOn"`
	VersionPolicy VersionPolicy     `json:"versionPolicy"`
	Provider      string            `json:"provider"`
	Package       string            `json:"package"`
	Commands      []string          `json:"commands"`
	Metadata      map[string]string `json:"metadata"`
}

var resourceIDPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z][a-z0-9-]*)+$`)

func (r Resource) Validate() error {
	if !resourceIDPattern.MatchString(string(r.ID)) {
		return fmt.Errorf("invalid resource ID %q", r.ID)
	}
	if r.VersionPolicy != VersionTracked && r.VersionPolicy != VersionPinned {
		return fmt.Errorf("invalid version policy %q", r.VersionPolicy)
	}
	return nil
}

type Catalog struct {
	Version   int          `json:"version"`
	Release   string       `json:"release"`
	Config    ConfigSchema `json:"config"`
	Resources []Resource   `json:"resources"`
}

type Ownership struct {
	ResourceID    ResourceID                 `json:"resourceId"`
	CatalogDigest string                     `json:"catalogDigest"`
	Provider      string                     `json:"provider"`
	Package       string                     `json:"package"`
	Paths         map[string]string          `json:"paths"`
	PriorValues   map[string]json.RawMessage `json:"priorValues"`
}

type Observation struct {
	Present  bool
	Provider string
	Package  string
	Version  string
	Paths    map[string]string
	Healthy  bool
	Detail   string
}

type Operation struct {
	ID                string
	ResourceID        ResourceID
	Kind              OperationKind
	RequiresPrivilege bool
	Removes           []string
	Detail            string
}

type OperationResult struct {
	OperationID string
	ResourceID  ResourceID
	Success     bool
	Detail      string
	FinishedAt  time.Time
}

type Plan struct {
	ID          string
	Release     string
	Operations  []Operation
	Unavailable map[ResourceID]string
}

type Snapshot struct {
	Ownership       map[ResourceID]Ownership
	ActiveJournal   *Journal
	AppliedCatalogs []string
}

type Journal struct {
	ID        string
	Plan      Plan
	Results   []OperationResult
	StartedAt time.Time
	Status    string
}
