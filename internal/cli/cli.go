package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/placement"
)

const (
	ExitValid      = 0
	ExitInvalid    = 1
	ExitInvocation = 2
	ExitInternal   = 3
)

type App struct {
	Stdout  io.Writer
	Stderr  io.Writer
	WorkDir string
	Version string
}

func (a App) Run(arguments []string) int {
	if len(arguments) == 0 {
		return a.invocationError("a command is required")
	}

	switch arguments[0] {
	case "version":
		return a.runVersion(arguments[1:])
	case "catalog":
		return a.runCatalog(arguments[1:])
	case "help", "-h", "--help":
		if err := a.printUsage(a.Stdout); err != nil {
			return a.internalError("write usage", err)
		}

		return ExitValid
	default:
		return a.invocationError(fmt.Sprintf("unknown command %q", arguments[0]))
	}
}

func (a App) runVersion(arguments []string) int {
	if len(arguments) != 0 {
		return a.invocationError("version does not accept arguments")
	}

	if _, err := fmt.Fprintf(
		a.Stdout,
		"slipramp %s\nsupported catalog API versions: %s\n",
		a.Version,
		catalog.APIVersion,
	); err != nil {
		return a.internalError("write version", err)
	}

	return ExitValid
}

func (a App) runCatalog(arguments []string) int {
	if len(arguments) == 0 || arguments[0] != "validate" {
		return a.invocationError("catalog requires the validate command")
	}

	return a.runCatalogValidate(arguments[1:])
}

func (a App) runCatalogValidate(arguments []string) int {
	flags := flag.NewFlagSet("slipramp catalog validate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	format := flags.String("format", "text", "output format: text, json, or sarif")
	placementsPath := flags.String("placements", "", "PlacementConfig file for deployment validation")
	strict := flags.Bool("strict", false, "fail when validation produces warnings")
	if err := flags.Parse(arguments); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			if err := a.printCatalogValidateUsage(a.Stdout); err != nil {
				return a.internalError("write usage", err)
			}

			return ExitValid
		}
		return a.invocationError(err.Error())
	}
	if flags.NArg() > 1 {
		return a.invocationError("catalog validate accepts at most one repository root")
	}
	if *format != "text" && *format != "json" && *format != "sarif" {
		return a.invocationError(fmt.Sprintf("unsupported output format %q", *format))
	}

	root := "."
	if flags.NArg() == 1 {
		root = flags.Arg(0)
	}
	root, err := a.resolvePath(root)
	if err != nil {
		return a.invocationError(fmt.Sprintf("resolve repository root: %v", err))
	}
	if err := requireRepositoryRoot(root); err != nil {
		return a.invocationError(err.Error())
	}

	options := catalog.Options{}
	var placements placement.Set
	if *placementsPath != "" {
		resolved, err := a.resolvePath(*placementsPath)
		if err != nil {
			return a.invocationError(fmt.Sprintf("resolve Placement file: %v", err))
		}
		placements, err = placement.LoadFile(resolved)
		if err != nil {
			return a.invocationError(err.Error())
		}
		options.Placements = &placements
	}

	result, err := catalog.Process(os.DirFS(root), options)
	if err != nil {
		return a.invocationError(err.Error())
	}

	summary := summarize(result, options.Placements)
	switch *format {
	case "text":
		err = renderText(a.Stdout, result, summary)
	case "json":
		err = renderJSON(a.Stdout, result, summary, *strict)
	case "sarif":
		err = renderSARIF(a.Stdout, result)
	}
	if err != nil {
		return a.internalError("write validation output", err)
	}

	if result.Diagnostics.HasErrors() || *strict && result.Diagnostics.HasWarnings() {
		return ExitInvalid
	}

	return ExitValid
}

func (a App) resolvePath(value string) (string, error) {
	if filepath.IsAbs(value) {
		return filepath.Clean(value), nil
	}

	workDir := a.WorkDir
	if workDir == "" {
		var err error
		workDir, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}

	return filepath.Abs(filepath.Join(workDir, value))
}

func requireRepositoryRoot(root string) error {
	info, err := os.Stat(root)
	if err != nil {
		return fmt.Errorf("inspect repository root %q: %w", root, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("repository root %q is not a directory", root)
	}

	catalogDirectory := filepath.Join(root, filepath.FromSlash(catalog.Directory))
	info, err = os.Stat(catalogDirectory)
	if err != nil {
		return fmt.Errorf("inspect catalog directory %q: %w", catalogDirectory, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("catalog path %q is not a directory", catalogDirectory)
	}

	return nil
}

func (a App) invocationError(message string) int {
	if _, err := fmt.Fprintf(a.Stderr, "slipramp: %s\n", message); err != nil {
		return ExitInternal
	}

	return ExitInvocation
}

func (a App) internalError(operation string, err error) int {
	_, _ = fmt.Fprintf(a.Stderr, "slipramp: %s: %v\n", operation, err)

	return ExitInternal
}

func (a App) printUsage(output io.Writer) error {
	if _, err := fmt.Fprintln(output, "usage: slipramp <command>"); err != nil {
		return err
	}
	_, err := fmt.Fprintln(output, "commands: catalog validate, version")

	return err
}

func (a App) printCatalogValidateUsage(output io.Writer) error {
	_, err := fmt.Fprintln(output, "usage: slipramp catalog validate [flags] [repository-root]")

	return err
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
