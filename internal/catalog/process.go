package catalog

import (
	"maps"
	"path"
)

const DefaultRepositoryMount = "/workspace"

// ProcessDirectories describes the Runtime repository mount and the child
// process directory. Runtime workingDirectory names the mount, not a second
// directory relative to the Target.
func ProcessDirectories(runtimeDirectory, targetPath string) (string, string) {
	mount := runtimeDirectory
	if mount == "" {
		mount = DefaultRepositoryMount
	}

	return mount, path.Join(mount, targetPath)
}

// MergeResources applies per-key overrides without mutating either input.
func MergeResources(base, overrides ResourceRequirements) ResourceRequirements {
	requests := maps.Clone(base.Requests)
	if requests == nil && overrides.Requests != nil {
		requests = make(map[string]string, len(overrides.Requests))
	}
	maps.Copy(requests, overrides.Requests)

	limits := maps.Clone(base.Limits)
	if limits == nil && overrides.Limits != nil {
		limits = make(map[string]string, len(overrides.Limits))
	}
	maps.Copy(limits, overrides.Limits)

	return ResourceRequirements{Requests: requests, Limits: limits}
}
