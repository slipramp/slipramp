// Package catalog discovers and decodes repository-defined Slipramp resources.
package catalog

const (
	Directory  = ".slipramp"
	APIVersion = "slipramp.com/v1alpha1"

	RuntimeKind = "Runtime"
	TargetKind  = "Target"

	ParameterTypeString  = "string"
	ParameterTypeBoolean = "boolean"
	ParameterTypeNumber  = "number"
	ParameterTypeEnum    = "enum"

	ArtifactCompatibilityExactCatalogRevision = "exact-catalog-revision"
)

type Catalog struct {
	Runtimes []Runtime `json:"runtimes"`
	Targets  []Target  `json:"targets"`

	locations sourceLocations
}

type Source struct {
	Path     string `json:"path"`
	Document int    `json:"document"`
}

type Metadata struct {
	Name  string `json:"name"            yaml:"name"`
	Title string `json:"title,omitempty" yaml:"title,omitempty"`
}

type Runtime struct {
	APIVersion string      `json:"apiVersion" yaml:"apiVersion"`
	Kind       string      `json:"kind"       yaml:"kind"`
	Metadata   Metadata    `json:"metadata"   yaml:"metadata"`
	Spec       RuntimeSpec `json:"spec"       yaml:"spec"`
	Source     Source      `json:"source"     yaml:"-"`
}

type RuntimeSpec struct {
	Image            string                       `json:"image"                      yaml:"image"`
	WorkingDirectory string                       `json:"workingDirectory,omitempty" yaml:"workingDirectory,omitempty"`
	Environment      map[string]string            `json:"environment,omitempty"      yaml:"environment,omitempty"`
	Credentials      map[string]RuntimeCredential `json:"credentials,omitempty"      yaml:"credentials,omitempty"`
}

type RuntimeCredential struct {
	Inject CredentialInjection `json:"inject" yaml:"inject"`
}

type CredentialInjection struct {
	Environment     map[string]CredentialEnvironmentVariable `json:"environment,omitempty"     yaml:"environment,omitempty"`
	EnvironmentFrom bool                                     `json:"environmentFrom,omitempty" yaml:"environmentFrom,omitempty"`
	Files           []CredentialFile                         `json:"files,omitempty"           yaml:"files,omitempty"`
	Directory       *CredentialDirectory                     `json:"directory,omitempty"       yaml:"directory,omitempty"`
}

type CredentialEnvironmentVariable struct {
	Key string `json:"key" yaml:"key"`
}

type CredentialFile struct {
	Key  string `json:"key"            yaml:"key"`
	Path string `json:"path"           yaml:"path"`
	Mode uint32 `json:"mode,omitempty" yaml:"mode,omitempty"`
}

type CredentialDirectory struct {
	Path string `json:"path"           yaml:"path"`
	Mode uint32 `json:"mode,omitempty" yaml:"mode,omitempty"`
}

type Target struct {
	APIVersion string     `json:"apiVersion" yaml:"apiVersion"`
	Kind       string     `json:"kind"       yaml:"kind"`
	Metadata   Metadata   `json:"metadata"   yaml:"metadata"`
	Spec       TargetSpec `json:"spec"       yaml:"spec"`
	Source     Source     `json:"source"     yaml:"-"`
}

type TargetSpec struct {
	Path        string               `json:"path,omitempty"        yaml:"path,omitempty"`
	Watch       []string             `json:"watch,omitempty"       yaml:"watch,omitempty"`
	Runtime     string               `json:"runtime,omitempty"     yaml:"runtime,omitempty"`
	Placement   string               `json:"placement,omitempty"   yaml:"placement,omitempty"`
	Credentials map[string]string    `json:"credentials,omitempty" yaml:"credentials,omitempty"`
	Timeout     string               `json:"timeout,omitempty"     yaml:"timeout,omitempty"`
	Resources   ResourceRequirements `json:"resources,omitzero"    yaml:"resources,omitempty"`
	Actions     map[string]Action    `json:"actions"               yaml:"actions"`
}

type Action struct {
	Title            string               `json:"title,omitempty"            yaml:"title,omitempty"`
	Description      string               `json:"description,omitempty"      yaml:"description,omitempty"`
	DocumentationURL string               `json:"documentationUrl,omitempty" yaml:"documentationUrl,omitempty"`
	Intent           string               `json:"intent,omitempty"           yaml:"intent,omitempty"`
	Command          []string             `json:"command"                    yaml:"command"`
	Runtime          string               `json:"runtime,omitempty"          yaml:"runtime,omitempty"`
	Placement        string               `json:"placement,omitempty"        yaml:"placement,omitempty"`
	Credentials      map[string]string    `json:"credentials,omitempty"      yaml:"credentials,omitempty"`
	Timeout          string               `json:"timeout,omitempty"          yaml:"timeout,omitempty"`
	Resources        ResourceRequirements `json:"resources,omitzero"         yaml:"resources,omitempty"`
	Parameters       map[string]Parameter `json:"parameters,omitempty"       yaml:"parameters,omitempty"`
	Artifacts        map[string]Artifact  `json:"artifacts,omitempty"        yaml:"artifacts,omitempty"`
	Inputs           map[string]Input     `json:"inputs,omitempty"           yaml:"inputs,omitempty"`
	Triggers         []Trigger            `json:"triggers,omitempty"         yaml:"triggers,omitempty"`
}

// Trigger declares one named source of automatic action Runs.
type Trigger struct {
	Name       string         `json:"name"                 yaml:"name"`
	Git        *GitTrigger    `json:"git,omitempty"        yaml:"git,omitempty"`
	Parameters map[string]any `json:"parameters,omitempty" yaml:"parameters,omitempty"`
}

// GitTrigger uses the containing target's watch patterns. It is intentionally
// empty until Git-specific filtering has an accepted product contract.
type GitTrigger struct{}

type ResourceRequirements struct {
	Requests map[string]string `json:"requests,omitempty" yaml:"requests,omitempty"`
	Limits   map[string]string `json:"limits,omitempty"   yaml:"limits,omitempty"`
}

type Parameter struct {
	Title       string             `json:"title,omitempty"       yaml:"title,omitempty"`
	Description string             `json:"description,omitempty" yaml:"description,omitempty"`
	Type        string             `json:"type"                  yaml:"type"`
	Required    bool               `json:"required,omitempty"    yaml:"required,omitempty"`
	Values      []string           `json:"values,omitempty"      yaml:"values,omitempty"`
	Environment string             `json:"environment,omitempty" yaml:"environment,omitempty"`
	Argument    *ParameterArgument `json:"argument,omitempty"    yaml:"argument,omitempty"`
}

type ParameterArgument struct {
	Name string `json:"name,omitempty" yaml:"name,omitempty"`
}

type Artifact struct {
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	Path        string `json:"path"                  yaml:"path"`
	MediaType   string `json:"mediaType,omitempty"   yaml:"mediaType,omitempty"`
	Sensitive   bool   `json:"sensitive,omitempty"   yaml:"sensitive,omitempty"`
}

type Input struct {
	Artifact  ArtifactReference `json:"artifact"  yaml:"artifact"`
	MountPath string            `json:"mountPath" yaml:"mountPath"`
}

type ArtifactReference struct {
	FromAction    string `json:"fromAction"              yaml:"fromAction"`
	Compatibility string `json:"compatibility,omitempty" yaml:"compatibility,omitempty"`
}
