package cli

import (
	"fmt"
	"maps"
	"os"
	"slices"

	"github.com/spf13/cobra"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/placement"
)

type catalogValidateOptions struct {
	format         string
	placementsPath string
	strict         bool
}

func (a App) newCatalogValidateCommand(exitCode *int) *cobra.Command {
	options := catalogValidateOptions{}
	command := &cobra.Command{
		Use:   "validate [repository-root]",
		Short: "Validate a repository catalog",
		Args: func(_ *cobra.Command, arguments []string) error {
			if len(arguments) > 1 {
				return fmt.Errorf("catalog validate accepts at most one repository root")
			}

			return nil
		},
		Run: func(_ *cobra.Command, arguments []string) {
			*exitCode = a.runCatalogValidate(options, arguments)
		},
	}
	command.Flags().StringVar(&options.format, "format", "text", "output format: text, json, or sarif")
	command.Flags().StringVar(
		&options.placementsPath,
		"placements",
		"",
		"PlacementConfig file for deployment validation",
	)
	command.Flags().BoolVar(&options.strict, "strict", false, "fail when validation produces warnings")

	return command
}

func (a App) runCatalogValidate(options catalogValidateOptions, arguments []string) int {
	if options.format != "text" && options.format != "json" && options.format != "sarif" {
		return a.invocationError(fmt.Sprintf("unsupported output format %q", options.format))
	}

	root := "."
	if len(arguments) == 1 {
		root = arguments[0]
	}
	root, err := a.resolvePath(root)
	if err != nil {
		return a.invocationError(fmt.Sprintf("resolve repository root: %v", err))
	}
	if err := requireRepositoryRoot(root); err != nil {
		return a.invocationError(err.Error())
	}

	catalogOptions := catalog.Options{}
	var placements placement.Set
	if options.placementsPath != "" {
		resolved, err := a.resolvePath(options.placementsPath)
		if err != nil {
			return a.invocationError(fmt.Sprintf("resolve Placement file: %v", err))
		}
		placements, err = placement.LoadFile(resolved)
		if err != nil {
			return a.invocationError(err.Error())
		}
		catalogOptions.Placements = &placements
	}

	result, err := catalog.Process(os.DirFS(root), catalogOptions)
	if err != nil {
		return a.invocationError(err.Error())
	}

	summary := summarize(result, catalogOptions.Placements)
	switch options.format {
	case "text":
		err = renderText(a.Stdout, result, summary)
	case "json":
		err = renderJSON(a.Stdout, result, summary, options.strict)
	case "sarif":
		err = renderSARIF(a.Stdout, result)
	}
	if err != nil {
		return a.internalError("write validation output", err)
	}

	if result.Diagnostics.HasErrors() || options.strict && result.Diagnostics.HasWarnings() {
		return ExitInvalid
	}

	return ExitValid
}

type validationSummary struct {
	Runtimes   int      `json:"runtimes"`
	Targets    int      `json:"targets"`
	Actions    int      `json:"actions"`
	Placements []string `json:"placements,omitempty"`
	Errors     int      `json:"errors"`
	Warnings   int      `json:"warnings"`
}

func summarize(result catalog.Result, placements *placement.Set) validationSummary {
	actions := 0
	referenced := map[string]struct{}{}
	for _, target := range result.Catalog.Targets {
		actions += len(target.Spec.Actions)
		if target.Spec.Placement != "" {
			referenced[target.Spec.Placement] = struct{}{}
		}
		for actionName := range target.Spec.Actions {
			effective, _ := target.EffectiveAction(actionName)
			if effective.Placement != "" {
				referenced[effective.Placement] = struct{}{}
			}
		}
	}

	placementNames := slices.Sorted(maps.Keys(referenced))
	if placements != nil {
		placementNames = placements.Names()
	}
	errorsCount, warnings := result.Diagnostics.Counts()

	return validationSummary{
		Runtimes:   len(result.Catalog.Runtimes),
		Targets:    len(result.Catalog.Targets),
		Actions:    actions,
		Placements: placementNames,
		Errors:     errorsCount,
		Warnings:   warnings,
	}
}
