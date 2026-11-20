package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/slipramp/slipramp/internal/catalog"
)

func renderText(output io.Writer, result catalog.Result, summary validationSummary) error {
	for _, item := range result.Diagnostics {
		location := item.Location.Path
		if item.Location.Line > 0 {
			location += fmt.Sprintf(":%d", item.Location.Line)
			if item.Location.Column > 0 {
				location += fmt.Sprintf(":%d", item.Location.Column)
			}
		}
		if _, err := fmt.Fprintf(
			output,
			"%s: %s [%s]\n%s\n",
			location,
			item.Severity,
			item.Code,
			item.Message,
		); err != nil {
			return err
		}
		if item.Hint != "" {
			if _, err := fmt.Fprintf(output, "hint: %s\n", item.Hint); err != nil {
				return err
			}
		}
		for _, related := range item.Related {
			if _, err := fmt.Fprintf(
				output,
				"related: %s:%d:%d: %s\n",
				related.Location.Path,
				related.Location.Line,
				related.Location.Column,
				related.Message,
			); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(output); err != nil {
			return err
		}
	}

	if !result.Diagnostics.HasErrors() {
		if result.Level == catalog.ValidationLevelPortable {
			if _, err := fmt.Fprintf(
				output,
				"Catalog is portable-valid: %d runtimes, %d targets, %d actions.\n",
				summary.Runtimes,
				summary.Targets,
				summary.Actions,
			); err != nil {
				return err
			}
			if len(summary.Placements) > 0 {
				if _, err := fmt.Fprintf(
					output,
					"Deployment checks were not run for placements: %s.\n",
					strings.Join(summary.Placements, ", "),
				); err != nil {
					return err
				}
			}
			if len(summary.GateSources) > 0 {
				if _, err := fmt.Fprintf(
					output,
					"Deployment checks were not run for Gate sources: %s.\n",
					strings.Join(summary.GateSources, ", "),
				); err != nil {
					return err
				}
			}
		} else {
			if err := renderDeploymentSummary(output, summary); err != nil {
				return err
			}
			if !summary.placementsConfigured && len(summary.Placements) > 0 {
				if _, err := fmt.Fprintf(
					output,
					"Deployment checks were not run for placements: %s.\n",
					strings.Join(summary.Placements, ", "),
				); err != nil {
					return err
				}
			}
			if !summary.gateSourcesConfigured && len(summary.GateSources) > 0 {
				if _, err := fmt.Fprintf(
					output,
					"Deployment checks were not run for Gate sources: %s.\n",
					strings.Join(summary.GateSources, ", "),
				); err != nil {
					return err
				}
			}
		}
	}

	_, err := fmt.Fprintf(output, "\n%d errors, %d warnings\n", summary.Errors, summary.Warnings)

	return err
}

func renderDeploymentSummary(output io.Writer, summary validationSummary) error {
	configuration := "installation configuration"
	switch {
	case summary.placementsConfigured && summary.gateSourcesConfigured:
		configuration = fmt.Sprintf(
			"%d placements and %d Gate sources",
			len(summary.Placements),
			len(summary.GateSources),
		)
	case summary.placementsConfigured:
		configuration = fmt.Sprintf("%d placements", len(summary.Placements))
	case summary.gateSourcesConfigured:
		configuration = fmt.Sprintf("%d Gate sources", len(summary.GateSources))
	}

	_, err := fmt.Fprintf(
		output,
		"Catalog is valid against %s: %d runtimes, %d targets, %d actions.\n",
		configuration,
		summary.Runtimes,
		summary.Targets,
		summary.Actions,
	)

	return err
}
