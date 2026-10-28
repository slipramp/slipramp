package placement

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	corev1 "k8s.io/api/core/v1"

	"github.com/slipramp/slipramp/internal/cluster"
)

// Set owns an immutable collection of validated placement configurations.
type Set struct {
	configurations           map[string]Config
	clusterMaximumActiveRuns map[string]int32
	checksum                 string
}

func NewSet(configurations map[string]Config) (Set, error) {
	return NewSetWithClusterLimits(configurations, nil)
}

func NewSetWithClusterLimits(
	configurations map[string]Config,
	clusterMaximumActiveRuns map[string]int32,
) (Set, error) {
	validatedClusterLimits := make(map[string]int32, len(clusterMaximumActiveRuns))
	for _, name := range slices.Sorted(maps.Keys(clusterMaximumActiveRuns)) {
		if err := cluster.ValidateName(name); err != nil {
			return Set{}, err
		}
		maximum := clusterMaximumActiveRuns[name]
		if maximum < 1 {
			return Set{}, fmt.Errorf("cluster %q maximum active Runs must be positive", name)
		}
		validatedClusterLimits[name] = maximum
	}

	validated := make(map[string]Config, len(configurations))
	for _, name := range slices.Sorted(maps.Keys(configurations)) {
		if err := ValidateName(name); err != nil {
			return Set{}, err
		}

		cfg := configurations[name]
		if err := cfg.Validate(); err != nil {
			return Set{}, fmt.Errorf("validate placement %q: %w", name, err)
		}
		if clusterMaximum := validatedClusterLimits[cfg.ClusterName]; clusterMaximum > 0 &&
			cfg.MaximumActiveRuns > clusterMaximum {
			return Set{}, fmt.Errorf(
				"validate placement %q: maximum active Runs must not exceed cluster %q maximum of %d",
				name,
				cfg.ClusterName,
				clusterMaximum,
			)
		}

		validated[name] = cloneConfig(cfg)
	}

	checksum, err := configurationChecksum(validated)
	if err != nil {
		return Set{}, fmt.Errorf("checksum placement configuration: %w", err)
	}

	return Set{
		configurations:           validated,
		clusterMaximumActiveRuns: validatedClusterLimits,
		checksum:                 checksum,
	}, nil
}

func (s Set) Len() int {
	return len(s.configurations)
}

func (s Set) Names() []string {
	return slices.Sorted(maps.Keys(s.configurations))
}

func (s Set) Lookup(name string) (Config, bool) {
	cfg, ok := s.configurations[name]
	if !ok {
		return Config{}, false
	}

	return cloneConfig(cfg), true
}

func (s Set) ClusterMaximumActiveRuns(name string) (int32, bool) {
	maximum, ok := s.clusterMaximumActiveRuns[name]
	return maximum, ok
}

func (s Set) ClusterNames() []string {
	return slices.Sorted(maps.Keys(s.clusterMaximumActiveRuns))
}

// Checksum identifies the validated effective Placement configuration. It is
// stable across source formatting and map insertion order.
func (s Set) Checksum() string {
	if s.checksum != "" {
		return s.checksum
	}

	checksum, _ := configurationChecksum(nil)

	return checksum
}

type checksumDocument struct {
	Placements []checksumPlacement `json:"placements"`
}

type checksumPlacement struct {
	Name               string                  `json:"name"`
	ClusterName        string                  `json:"cluster"`
	JobNamespace       string                  `json:"jobNamespace"`
	ServiceAccountName string                  `json:"serviceAccountName,omitempty"`
	TimeoutNanoseconds int64                   `json:"timeoutNanoseconds,omitempty"`
	Resources          checksumResources       `json:"resources"`
	NodeSelector       map[string]string       `json:"nodeSelector,omitempty"`
	Credentials        map[string]string       `json:"credentials,omitempty"`
	PodTemplate        *corev1.PodTemplateSpec `json:"podTemplate,omitempty"`
}

type checksumResources struct {
	Requests map[string]string `json:"requests,omitempty"`
	Limits   map[string]string `json:"limits,omitempty"`
}

func configurationChecksum(configurations map[string]Config) (string, error) {
	document := checksumDocument{Placements: make([]checksumPlacement, 0, len(configurations))}
	for _, name := range slices.Sorted(maps.Keys(configurations)) {
		cfg := configurations[name]
		credentials := make(map[string]string, len(cfg.Credentials))
		for credentialName, source := range cfg.Credentials {
			credentials[credentialName] = source.SecretRef.Name
		}

		document.Placements = append(document.Placements, checksumPlacement{
			Name:               name,
			ClusterName:        cfg.ClusterName,
			JobNamespace:       cfg.JobNamespace,
			ServiceAccountName: cfg.ServiceAccountName,
			TimeoutNanoseconds: cfg.Timeout.Nanoseconds(),
			Resources: checksumResources{
				Requests: cfg.Resources.Requests,
				Limits:   cfg.Resources.Limits,
			},
			NodeSelector: cfg.NodeSelector,
			Credentials:  credentials,
			PodTemplate:  cfg.PodTemplate,
		})
	}

	encoded, err := json.Marshal(document)
	if err != nil {
		return "", err
	}

	digest := sha256.Sum256(encoded)

	return hex.EncodeToString(digest[:]), nil
}

func cloneConfig(cfg Config) Config {
	cfg.Resources.Requests = maps.Clone(cfg.Resources.Requests)
	cfg.Resources.Limits = maps.Clone(cfg.Resources.Limits)
	cfg.NodeSelector = maps.Clone(cfg.NodeSelector)
	cfg.Credentials = maps.Clone(cfg.Credentials)
	cfg.PodTemplate = cfg.PodTemplate.DeepCopy()

	return cfg
}
