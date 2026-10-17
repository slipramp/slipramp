package catalog

import (
	"path"
	"strings"
)

// TargetAffected reports whether any changed repository path matches one of
// the target's watch patterns. A segment containing only ** matches zero or
// more complete path segments.
func TargetAffected(target Target, changedPaths []string) bool {
	for _, pattern := range target.Spec.Watch {
		for _, changedPath := range changedPaths {
			if matchRepositoryPattern(pattern, changedPath) {
				return true
			}
		}
	}

	return false
}

func matchRepositoryPattern(pattern, candidate string) bool {
	patternParts := strings.Split(pattern, "/")
	candidateParts := strings.Split(candidate, "/")
	memo := map[[2]int]bool{}
	visited := map[[2]int]bool{}

	var match func(int, int) bool
	match = func(patternIndex, candidateIndex int) bool {
		state := [2]int{patternIndex, candidateIndex}
		if visited[state] {
			return memo[state]
		}
		visited[state] = true

		matched := false
		switch {
		case patternIndex == len(patternParts):
			matched = candidateIndex == len(candidateParts)
		case patternParts[patternIndex] == "**":
			matched = match(patternIndex+1, candidateIndex)
			if !matched && candidateIndex < len(candidateParts) {
				matched = match(patternIndex, candidateIndex+1)
			}
		case candidateIndex < len(candidateParts):
			segmentMatched, err := path.Match(
				patternParts[patternIndex],
				candidateParts[candidateIndex],
			)
			matched = err == nil && segmentMatched &&
				match(patternIndex+1, candidateIndex+1)
		}

		memo[state] = matched

		return matched
	}

	return match(0, 0)
}
