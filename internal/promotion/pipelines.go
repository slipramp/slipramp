package promotion

import (
	"fmt"
	"maps"
	"slices"
)

type ProjectPipeline struct {
	Name       string
	StageNames []string
}

// ProjectPipelines groups the Stages of a single Project by connected Freight sources.
// Pipelines and their member Stages are ordered by Stage name.
func ProjectPipelines(stages []Stage) []ProjectPipeline {
	applicationsByStage := make(map[string]string, len(stages))
	for _, stage := range stages {
		applicationsByStage[stage.Name] = stage.Application
	}

	neighbors := make(map[string][]string, len(stages))
	for _, stage := range stages {
		for _, request := range stage.RequestedFreight {
			for _, source := range request.SourceStages {
				if _, ok := applicationsByStage[source]; ok {
					neighbors[stage.Name] = append(neighbors[stage.Name], source)
					neighbors[source] = append(neighbors[source], stage.Name)
				}
			}
		}
	}

	var pipelines []ProjectPipeline
	visited := make(map[string]bool, len(stages))
	for _, root := range slices.Sorted(maps.Keys(applicationsByStage)) {
		if visited[root] {
			continue
		}
		visited[root] = true
		var members []string
		applications := make(map[string]struct{})
		pending := []string{root}
		for len(pending) > 0 {
			name := pending[len(pending)-1]
			pending = pending[:len(pending)-1]
			members = append(members, name)
			if application := applicationsByStage[name]; application != "" {
				applications[application] = struct{}{}
			}
			for _, neighbor := range neighbors[name] {
				if !visited[neighbor] {
					visited[neighbor] = true
					pending = append(pending, neighbor)
				}
			}
		}
		slices.Sort(members)
		pipelines = append(pipelines, ProjectPipeline{
			Name:       pipelineName(slices.Sorted(maps.Keys(applications)), members, len(pipelines)+1),
			StageNames: members,
		})
	}
	return pipelines
}

func pipelineName(applications []string, members []string, ordinal int) string {
	switch {
	case len(applications) > 1:
		return fmt.Sprintf("%s + %d more", applications[0], len(applications)-1)
	case len(applications) == 1:
		return applications[0]
	case len(members) == 1:
		return members[0]
	default:
		return fmt.Sprintf("Pipeline %d", ordinal)
	}
}
