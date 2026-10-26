package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/slipramp/slipramp/internal/catalog"
)

func (a App) newCatalogCommand(exitCode *int) *cobra.Command {
	command := &cobra.Command{
		Use:   "catalog",
		Short: "Inspect and validate a repository catalog",
		RunE: func(_ *cobra.Command, _ []string) error {
			return fmt.Errorf("catalog requires the validate or explain command")
		},
	}
	command.AddCommand(
		a.newCatalogValidateCommand(exitCode),
		a.newCatalogExplainCommand(exitCode),
	)

	return command
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
