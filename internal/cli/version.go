package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/slipramp/slipramp/internal/catalog"
)

func (a App) newVersionCommand(exitCode *int) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version and catalog compatibility information",
		Args: func(_ *cobra.Command, arguments []string) error {
			if len(arguments) != 0 {
				return fmt.Errorf("version does not accept arguments")
			}

			return nil
		},
		Run: func(_ *cobra.Command, _ []string) {
			*exitCode = a.runVersion()
		},
	}
}

func (a App) runVersion() int {
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
