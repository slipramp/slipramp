package catalog

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"slices"
	"strings"
)

func ValidateRepository(files fs.FS, input Catalog) (Diagnostics, error) {
	repositoryFiles, err := discoverRepositoryFiles(files)
	if err != nil {
		return nil, err
	}

	diagnostics := Diagnostics{}
	for _, target := range input.Targets {
		targetPathValid := target.Spec.Path == "" || validateRepositoryPath("target path", target.Spec.Path) == nil
		if target.Spec.Path != "" && targetPathValid {
			if _, err := fs.Stat(files, target.Spec.Path); err != nil && isMissing(err) {
				item := diagnostic(
					input,
					TargetKind,
					target.Metadata.Name,
					target.Source,
					"spec.path",
					"catalog.target.path-not-found",
					SeverityWarning,
					fmt.Errorf("target path %q does not exist in the checkout", target.Spec.Path),
				)
				item.Hint = "The path may be generated later; create it or ignore this warning when that is intentional."
				diagnostics = append(diagnostics, item)
			} else if err != nil {
				return nil, fmt.Errorf("inspect target path %q: %w", target.Spec.Path, err)
			}
		}

		for index, pattern := range target.Spec.Watch {
			if validateRepositoryPattern("target watch pattern", pattern) != nil {
				continue
			}
			if slices.ContainsFunc(repositoryFiles, func(candidate string) bool {
				return matchRepositoryPattern(pattern, candidate)
			}) {
				continue
			}

			item := diagnostic(
				input,
				TargetKind,
				target.Metadata.Name,
				target.Source,
				fmt.Sprintf("spec.watch.%d", index),
				"catalog.watch.no-matches",
				SeverityWarning,
				fmt.Errorf("target watch pattern %q matches no repository files", pattern),
			)
			item.Hint = "Generated or intentionally future-facing paths are valid; otherwise check the pattern."
			diagnostics = append(diagnostics, item)
		}

		for _, actionName := range slices.Sorted(maps.Keys(target.Spec.Actions)) {
			action := target.Spec.Actions[actionName]
			if len(action.Command) == 0 {
				continue
			}
			if !targetPathValid {
				continue
			}
			commandPath, relative := repositoryCommandPath(target.Spec.Path, action.Command[0])
			if !relative {
				continue
			}

			if _, err := fs.Stat(files, commandPath); err != nil && isMissing(err) {
				item := diagnostic(
					input,
					TargetKind,
					target.Metadata.Name,
					target.Source,
					"spec.actions."+actionName+".command.0",
					"catalog.command.path-not-found",
					SeverityWarning,
					fmt.Errorf("action %q command path %q does not exist in the checkout", actionName, commandPath),
				)
				item.Hint = "The command may be generated later; create it or ignore this warning when that is intentional."
				diagnostics = append(diagnostics, item)
			} else if err != nil {
				return nil, fmt.Errorf("inspect action %q command path %q: %w", actionName, commandPath, err)
			}

			if len(target.Spec.Watch) != 0 && !TargetAffected(target, []string{commandPath}) {
				item := diagnostic(
					input,
					TargetKind,
					target.Metadata.Name,
					target.Source,
					"spec.actions."+actionName+".command.0",
					"catalog.command.not-watched",
					SeverityWarning,
					fmt.Errorf("action %q command file %q is not covered by the Target watch patterns", actionName, commandPath),
				)
				item.Hint = "Add a watch pattern if changes to this repository command should affect the Target."
				diagnostics = append(diagnostics, item)
			}
		}
	}

	return diagnostics, nil
}

func discoverRepositoryFiles(files fs.FS) ([]string, error) {
	paths := []string{}
	err := fs.WalkDir(files, ".", func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if filePath == ".git" {
				return fs.SkipDir
			}

			return nil
		}
		paths = append(paths, filePath)

		return nil
	})

	return paths, err
}

func repositoryCommandPath(targetPath, command string) (string, bool) {
	if path.IsAbs(command) || !strings.Contains(command, "/") {
		return "", false
	}

	cleaned := path.Clean(command)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", false
	}

	return path.Join(targetPath, cleaned), true
}

func isMissing(err error) bool {
	return errors.Is(err, fs.ErrNotExist)
}
