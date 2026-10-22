package kargotools

import (
	"errors"
	"sort"
	"strings"
)

type ArgoDiff struct {
	Resources []ResourceDiff `json:"resources"`
}

type ResourceDiff struct {
	Group               string `json:"group,omitempty"`
	Kind                string `json:"kind"`
	Namespace           string `json:"namespace,omitempty"`
	Name                string `json:"name"`
	Diff                string `json:"diff"`
	VersionMetadataOnly bool   `json:"versionMetadataOnly,omitempty"`
}

func ParseArgoDiff(output []byte) (ArgoDiff, error) {
	result := ArgoDiff{Resources: []ResourceDiff{}}
	if strings.TrimSpace(string(output)) == "" {
		return result, nil
	}

	var current *ResourceDiff
	body := []string{}
	flush := func() error {
		if current == nil {
			return nil
		}
		current.Diff = strings.Trim(strings.Join(body, "\n"), "\r\n")
		if strings.TrimSpace(current.Diff) == "" {
			return errors.New("argo CD resource diff has an empty body")
		}
		current.VersionMetadataOnly = versionMetadataOnly(current.Diff)
		result.Resources = append(result.Resources, *current)
		body = nil
		return nil
	}

	for line := range strings.SplitSeq(strings.Trim(string(output), "\r\n"), "\n") {
		resource, ok := parseArgoDiffHeader(line)
		if ok {
			if err := flush(); err != nil {
				return ArgoDiff{}, err
			}
			current = &resource
			continue
		}
		if current != nil {
			body = append(body, strings.TrimSuffix(line, "\r"))
		}
	}
	if err := flush(); err != nil {
		return ArgoDiff{}, err
	}
	if len(result.Resources) == 0 {
		return ArgoDiff{}, errors.New("argo CD returned non-empty output without resource diff headers")
	}

	sort.Slice(result.Resources, func(left, right int) bool {
		first := result.Resources[left]
		second := result.Resources[right]
		firstKey := first.Group + "\x00" + first.Kind + "\x00" + first.Namespace + "\x00" + first.Name
		secondKey := second.Group + "\x00" + second.Kind + "\x00" + second.Namespace + "\x00" + second.Name
		return firstKey < secondKey
	})

	return result, nil
}

func parseArgoDiffHeader(line string) (ResourceDiff, bool) {
	if !strings.HasPrefix(line, "===== ") {
		return ResourceDiff{}, false
	}
	header := strings.TrimPrefix(line, "===== ")
	withoutSuffix := strings.TrimRight(header, "=")
	if len(header)-len(withoutSuffix) < 5 {
		return ResourceDiff{}, false
	}
	fields := strings.Fields(withoutSuffix)
	if len(fields) != 2 {
		return ResourceDiff{}, false
	}
	groupKind := strings.SplitN(fields[0], "/", 2)
	if len(groupKind) != 2 || groupKind[1] == "" {
		return ResourceDiff{}, false
	}
	resource := ResourceDiff{Group: groupKind[0], Kind: groupKind[1]}
	namespaceName := strings.SplitN(fields[1], "/", 2)
	if len(namespaceName) == 2 {
		resource.Namespace = namespaceName[0]
		resource.Name = namespaceName[1]
	} else {
		resource.Name = namespaceName[0]
	}
	if resource.Name == "" {
		return ResourceDiff{}, false
	}

	return resource, true
}

func versionMetadataOnly(diff string) bool {
	versionAnnotations := map[string]struct{}{
		"app.kubernetes.io/version": {},
		"helm.sh/chart":             {},
	}
	foundChange := false
	for line := range strings.SplitSeq(diff, "\n") {
		var changed string
		switch {
		case strings.HasPrefix(line, "< "), strings.HasPrefix(line, "> "):
			changed = line[2:]
		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"),
			strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
			changed = line[1:]
		default:
			continue
		}
		foundChange = true
		key, _, ok := strings.Cut(strings.TrimSpace(changed), ":")
		key = strings.Trim(strings.TrimSpace(key), `"'`)
		if !ok {
			return false
		}
		if _, ok := versionAnnotations[key]; !ok {
			return false
		}
	}

	return foundChange
}
