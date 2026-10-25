package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/slipramp/slipramp/internal/catalog"
	"github.com/slipramp/slipramp/internal/placement"
)

type parameterAssignments map[string]string

func (p parameterAssignments) String() string { return "" }

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

func (a App) runCatalogExplain(arguments []string) int {
	flags := flag.NewFlagSet("slipramp catalog explain", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", ".", "repository root (defaults to current directory)")
	placementsPath := flags.String("placements", "", "PlacementConfig file for deployment validation")
	format := flags.String("format", "text", "output format: text or json")
	parameters := parameterAssignments{}
	flags.Var(parameters, "set", "parameter NAME=VALUE (repeatable)")

	// The public command places identities before flags. Also accept the usual
	// standard-library flag ordering before both identities.
	identities := []string{}
	if len(arguments) >= 2 && !strings.HasPrefix(arguments[0], "-") && !strings.HasPrefix(arguments[1], "-") {
		identities = arguments[:2]
		arguments = arguments[2:]
	}
	if err := flags.Parse(arguments); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			if err := printCatalogExplainUsage(a.Stdout); err != nil {
				return a.internalError("write usage", err)
			}
			return ExitValid
		}
		return a.invocationError(err.Error())
	}
	identities = append(identities, flags.Args()...)
	if len(identities) != 2 {
		return a.invocationError("catalog explain requires TARGET ACTION [flags]")
	}
	if *format != "text" && *format != "json" {
		return a.invocationError(fmt.Sprintf("unsupported output format %q", *format))
	}
	resolvedRoot, err := a.resolvePath(*root)
	if err != nil {
		return a.invocationError(fmt.Sprintf("resolve repository root: %v", err))
	}
	if err := requireRepositoryRoot(resolvedRoot); err != nil {
		return a.invocationError(err.Error())
	}
	options := catalog.Options{}
	if *placementsPath != "" {
		resolved, err := a.resolvePath(*placementsPath)
		if err != nil {
			return a.invocationError(fmt.Sprintf("resolve Placement file: %v", err))
		}
		placements, err := placement.LoadFile(resolved)
		if err != nil {
			return a.invocationError(err.Error())
		}
		options.Placements = &placements
	}
	result, err := catalog.Process(os.DirFS(resolvedRoot), options)
	if err != nil {
		return a.invocationError(err.Error())
	}
	if result.Diagnostics.HasErrors() {
		summary := summarize(result, options.Placements)
		if *format == "json" {
			err = renderJSON(a.Stdout, result, summary, false)
		} else {
			err = renderText(a.Stdout, result, summary)
		}
		if err != nil {
			return a.internalError("write validation output", err)
		}
		return ExitInvalid
	}
	explanation, err := result.Explain(identities[0], identities[1], parameters)
	if err != nil {
		if _, writeErr := fmt.Fprintf(a.Stderr, "slipramp: %v\n", err); writeErr != nil {
			return ExitInternal
		}
		return ExitInvalid
	}
	if *format == "json" {
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

func printCatalogExplainUsage(output io.Writer) error {
	_, err := fmt.Fprintln(output, `usage: slipramp catalog explain TARGET ACTION [flags]

  --root PATH        Repository root (default: current directory)
  --placements PATH  PlacementConfig file for deployment validation
  --set NAME=VALUE   Parameter value (repeatable; names must be unique)
  --format text|json Output format (default: text)`)

	return err
}
