package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/slipramp/slipramp/internal/cli/remote"
)

func (a App) commandContext() context.Context {
	if a.Context != nil {
		return a.Context
	}

	return context.Background()
}

func (a App) environment(name string) string {
	if a.Getenv != nil {
		return a.Getenv(name)
	}

	return os.Getenv(name)
}

func (a App) remoteClient(serverFlag string) (*remote.Client, error) {
	serverURL := serverFlag
	if strings.TrimSpace(serverURL) == "" {
		serverURL = a.environment("SLIPRAMP_URL")
	}

	return remote.New(serverURL, a.environment("SLIPRAMP_API_KEY"), a.HTTPClient)
}

func (a App) remoteError(err error) int {
	if _, writeErr := fmt.Fprintf(a.Stderr, "slipramp: %v\n", err); writeErr != nil {
		return ExitInternal
	}

	return ExitRemote
}

func renderRemoteJSON(output io.Writer, value any) error {
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")

	return encoder.Encode(value)
}

func validateRemoteFormat(format string) error {
	if format != "text" && format != "json" {
		return fmt.Errorf("unsupported output format %q", format)
	}

	return nil
}
