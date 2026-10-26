package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/placement"
)

type parameterAssignments map[string]string

func (p parameterAssignments) String() string { return "" }

func (p parameterAssignments) Type() string { return "NAME=VALUE" }

func (p parameterAssignments) Set(assignment string) error {
	name, value, ok := strings.Cut(assignment, "=")
	if !ok || name == "" {
		return errors.New("--set requires NAME=VALUE")
	}
	if _, duplicate := p[name]; duplicate {
		return fmt.Errorf("parameter %q was supplied more than once", name)
	}
	p[name] = value

	return nil
}

type catalogExplainOptions struct {
	root           string
	placementsPath string
	format         string
	parameters     parameterAssignments
}

func (a App) newCatalogExplainCommand(exitCode *int) *cobra.Command {
	options := catalogExplainOptions{parameters: parameterAssignments{}}
	command := &cobra.Command{
		Use:   "explain TARGET ACTION",
		Short: "Explain the effective process for a Target Action",
		Args: func(_ *cobra.Command, arguments []string) error {
			if len(arguments) != 2 {
				return errors.New("catalog explain requires TARGET ACTION [flags]")
			}

			return nil
		},
		Run: func(_ *cobra.Command, arguments []string) {
			*exitCode = a.runCatalogExplain(options, arguments)
		},
	}
	command.Flags().StringVar(&options.root, "root", ".", "repository root (defaults to current directory)")
	command.Flags().StringVar(
		&options.placementsPath,
		"placements",
		"",
		"PlacementConfig file for deployment validation",
	)
	command.Flags().Var(&options.parameters, "set", "parameter NAME=VALUE (repeatable)")
	command.Flags().StringVar(&options.format, "format", "text", "output format: text or json")

	return command
}

func (a App) runCatalogExplain(options catalogExplainOptions, identities []string) int {
	if options.format != "text" && options.format != "json" {
		return a.invocationError(fmt.Sprintf("unsupported output format %q", options.format))
	}
	resolvedRoot, err := a.resolvePath(options.root)
	if err != nil {
		return a.invocationError(fmt.Sprintf("resolve repository root: %v", err))
	}
	if err := requireRepositoryRoot(resolvedRoot); err != nil {
		return a.invocationError(err.Error())
	}
	catalogOptions := catalog.Options{}
	if options.placementsPath != "" {
		resolved, err := a.resolvePath(options.placementsPath)
		if err != nil {
			return a.invocationError(fmt.Sprintf("resolve Placement file: %v", err))
		}
		placements, err := placement.LoadFile(resolved)
		if err != nil {
			return a.invocationError(err.Error())
		}
		catalogOptions.Placements = &placements
	}
	result, err := catalog.Process(os.DirFS(resolvedRoot), catalogOptions)
	if err != nil {
		return a.invocationError(err.Error())
	}
	if result.Diagnostics.HasErrors() {
		summary := summarize(result, catalogOptions.Placements)
		if options.format == "json" {
			err = renderJSON(a.Stdout, result, summary, false)
		} else {
			err = renderText(a.Stdout, result, summary)
		}
		if err != nil {
			return a.internalError("write validation output", err)
		}
		return ExitInvalid
	}
	explanation, err := result.Explain(identities[0], identities[1], options.parameters)
	if err != nil {
		if _, writeErr := fmt.Fprintf(a.Stderr, "slipramp: %v\n", err); writeErr != nil {
			return ExitInternal
		}
		return ExitInvalid
	}
	if options.format == "json" {
		encoder := json.NewEncoder(a.Stdout)
		encoder.SetIndent("", "  ")
		err = encoder.Encode(explanation)
	} else {
		err = renderExplanationText(a.Stdout, explanation)
	}
	if err != nil {
		return a.internalError("write explanation output", err)
	}

	return ExitValid
}
