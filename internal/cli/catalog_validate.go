package cli

import (
	"fmt"
	"maps"
	"os"
	"slices"

	"github.com/spf13/cobra"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/gate"
	"github.com/slipramp/slipramp/internal/placement"
)

type catalogValidateOptions struct {
	format          string
	placementsPath  string
	gateSourcesPath string
	strict          bool
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
	command.Flags().StringVar(
		&options.gateSourcesPath,
		"gate-sources",
		"",
		"GateSourceConfig file for deployment validation",
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
	var gateSources gate.SourceSet
	if options.gateSourcesPath != "" {
		resolved, err := a.resolvePath(options.gateSourcesPath)
		if err != nil {
			return a.invocationError(fmt.Sprintf("resolve Gate source file: %v", err))
		}
		gateSources, err = gate.LoadSourceFile(resolved)
		if err != nil {
			return a.invocationError(err.Error())
		}
		catalogOptions.GateSources = &gateSources
	}

	result, err := catalog.Process(os.DirFS(root), catalogOptions)
	if err != nil {
		return a.invocationError(err.Error())
	}

	summary := summarize(result, catalogOptions.Placements, catalogOptions.GateSources)
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
	Runtimes              int      `json:"runtimes"`
	Targets               int      `json:"targets"`
	Actions               int      `json:"actions"`
	Placements            []string `json:"placements,omitempty"`
	GateSources           []string `json:"gateSources,omitempty"`
	Errors                int      `json:"errors"`
	Warnings              int      `json:"warnings"`
	placementsConfigured  bool
	gateSourcesConfigured bool
}

func summarize(result catalog.Result, placements *placement.Set, gateSources *gate.SourceSet) validationSummary {
	actions := 0
	referencedPlacements := map[string]struct{}{}
	referencedGateSources := map[string]struct{}{}
	for _, catalogGate := range result.Catalog.Gates {
		if policy := catalogGate.Spec.Evaluator; policy != nil {
			referencedGateSources[policy.SourceName()] = struct{}{}
		}
	}
	for _, target := range result.Catalog.Targets {
		actions += len(target.Spec.Actions)
		if target.Spec.Placement != "" {
			referencedPlacements[target.Spec.Placement] = struct{}{}
		}
		for actionName := range target.Spec.Actions {
			effective, _ := target.EffectiveAction(actionName)
			if effective.Placement != "" {
				referencedPlacements[effective.Placement] = struct{}{}
			}
		}
	}

	placementNames := slices.Sorted(maps.Keys(referencedPlacements))
	if placements != nil {
		placementNames = placements.Names()
	}
	gateSourceNames := slices.Sorted(maps.Keys(referencedGateSources))
	if gateSources != nil {
		gateSourceNames = gateSources.Names()
	}
	errorsCount, warnings := result.Diagnostics.Counts()

	return validationSummary{
		Runtimes:              len(result.Catalog.Runtimes),
		Targets:               len(result.Catalog.Targets),
		Actions:               actions,
		Placements:            placementNames,
		GateSources:           gateSourceNames,
		Errors:                errorsCount,
		Warnings:              warnings,
		placementsConfigured:  placements != nil,
		gateSourcesConfigured: gateSources != nil,
	}
}
