package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/slipramp/slipramp/internal/api"
)

type artifactListOptions struct {
	serverURL string
	format    string
}

type artifactDownloadOptions struct {
	serverURL string
	output    string
}

func (a App) newArtifactCommand(exitCode *int) *cobra.Command {
	command := &cobra.Command{
		Use:   "artifact",
		Short: "Inspect and download Artifacts",
		RunE: func(_ *cobra.Command, _ []string) error {
			return errors.New("artifact requires the list or download command")
		},
	}
	command.AddCommand(
		a.newArtifactListCommand(exitCode),
		a.newArtifactDownloadCommand(exitCode),
	)

	return command
}

func (a App) newArtifactListCommand(exitCode *int) *cobra.Command {
	options := artifactListOptions{}
	command := &cobra.Command{
		Use:   "list RUN_ID",
		Short: "List a Run's Artifacts",
		Args:  requireRunIdentity,
		Run: func(_ *cobra.Command, arguments []string) {
			*exitCode = a.runArtifactList(options, arguments[0])
		},
	}
	flags := command.Flags()
	flags.StringVar(&options.serverURL, "server", "", "Slipramp Server URL (default: SLIPRAMP_URL)")
	flags.StringVar(&options.format, "format", "text", "output format: text or json")

	return command
}

func (a App) runArtifactList(options artifactListOptions, identity string) int {
	if err := validateRemoteFormat(options.format); err != nil {
		return a.invocationError(err.Error())
	}
	runID, err := parseRunIdentity(identity)
	if err != nil {
		return a.invocationError(err.Error())
	}
	client, err := a.remoteClient(options.serverURL)
	if err != nil {
		return a.invocationError(err.Error())
	}
	response, err := client.API.ListRunArtifacts(a.commandContext(), runID)
	if err != nil {
		return a.remoteError(client.NetworkError("list Artifacts", err))
	}
	defer func() { _ = response.Body.Close() }()
	parsed, err := api.ParseListRunArtifactsResponse(response)
	if err != nil {
		return a.remoteError(client.InvalidResponse("list Artifacts", err))
	}
	if parsed.StatusCode() != http.StatusOK || parsed.JSON200 == nil {
		return a.remoteError(client.ResponseError("list Artifacts", parsed.StatusCode(), parsed.Body))
	}

	if options.format == "json" {
		if err := renderRemoteJSON(a.Stdout, parsed.JSON200); err != nil {
			return a.internalError("write Artifact list", err)
		}

		return ExitValid
	}
	if err := renderArtifactListText(a.Stdout, *parsed.JSON200); err != nil {
		return a.internalError("write Artifact list", err)
	}

	return ExitValid
}

func renderArtifactListText(output io.Writer, list api.ArtifactList) error {
	table := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(
		table,
		"ID\tNAME\tKIND\tSTATE\tRETENTION\tSIZE\tMEDIA TYPE\tSENSITIVE\tSHA256\tCREATED",
	); err != nil {
		return err
	}
	for _, artifact := range list.Items {
		if _, err := fmt.Fprintf(
			table,
			"%s\t%s\t%s\t%s\t%s\t%d\t%s\t%t\t%s\t%s\n",
			artifact.ID,
			artifact.Name,
			artifact.Kind,
			artifact.State,
			artifact.RetentionState,
			artifact.SizeBytes,
			artifact.MediaType,
			artifact.Sensitive,
			optionalString(artifact.Sha256),
			renderTime(&artifact.CreatedAt),
		); err != nil {
			return err
		}
	}

	return table.Flush()
}

func (a App) newArtifactDownloadCommand(exitCode *int) *cobra.Command {
	options := artifactDownloadOptions{}
	command := &cobra.Command{
		Use:   "download ARTIFACT_ID",
		Short: "Download and verify a retained Artifact",
		Args: func(_ *cobra.Command, arguments []string) error {
			if len(arguments) != 1 {
				return errors.New("exactly one Artifact ID is required")
			}

			return nil
		},
		Run: func(_ *cobra.Command, arguments []string) {
			*exitCode = a.runArtifactDownload(options, arguments[0])
		},
	}
	flags := command.Flags()
	flags.StringVar(&options.serverURL, "server", "", "Slipramp Server URL (default: SLIPRAMP_URL)")
	flags.StringVar(&options.output, "output", "", "destination path, or - for standard output (required)")

	return command
}

func (a App) runArtifactDownload(options artifactDownloadOptions, identity string) int {
	artifactID, err := uuid.Parse(identity)
	if err != nil {
		return a.invocationError("Artifact ID must be a UUID")
	}
	if options.output == "" {
		return a.invocationError("--output is required")
	}
	client, err := a.remoteClient(options.serverURL)
	if err != nil {
		return a.invocationError(err.Error())
	}
	response, err := client.API.DownloadArtifact(a.commandContext(), artifactID)
	if err != nil {
		return a.remoteError(client.NetworkError("download Artifact", err))
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		if readErr != nil {
			return a.remoteError(client.InvalidResponse("download Artifact", readErr))
		}

		return a.remoteError(client.ResponseError("download Artifact", response.StatusCode, body))
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/octet-stream" {
		return a.remoteError(errors.New(
			"download Artifact: invalid Server response: expected application/octet-stream",
		))
	}
	wantDigest, err := artifactResponseDigest(response.Header.Get("Digest"))
	if err != nil {
		return a.remoteError(fmt.Errorf("download Artifact: invalid Server response: %w", err))
	}

	destination := a.Stdout
	var outputFile *os.File
	outputPath := ""
	removeOutput := false
	if options.output != "-" {
		outputPath, err = a.resolvePath(options.output)
		if err != nil {
			return a.invocationError(fmt.Sprintf("resolve output path: %v", err))
		}
		outputFile, err = os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			return a.invocationError(fmt.Sprintf("output path %q already exists", outputPath))
		}
		if err != nil {
			return a.internalError("create Artifact output", err)
		}
		destination = outputFile
		removeOutput = true
		defer func() {
			if removeOutput {
				_ = os.Remove(outputPath)
			}
		}()
	}

	digest := sha256.New()
	if _, err := io.Copy(io.MultiWriter(destination, digest), response.Body); err != nil {
		if outputFile != nil {
			_ = outputFile.Close()
		}

		return a.remoteError(fmt.Errorf("download Artifact: stream response: %w", err))
	}
	if outputFile != nil {
		if err := outputFile.Close(); err != nil {
			return a.internalError("close Artifact output", err)
		}
	}
	gotDigest := hex.EncodeToString(digest.Sum(nil))
	if gotDigest != wantDigest {
		return a.remoteError(errors.New("download Artifact: response digest does not match contents"))
	}
	removeOutput = false

	return ExitValid
}

func artifactResponseDigest(value string) (string, error) {
	const prefix = "sha-256="
	if !strings.HasPrefix(value, prefix) {
		return "", errors.New("digest header must contain sha-256")
	}
	digest := strings.TrimPrefix(value, prefix)
	decoded, err := hex.DecodeString(digest)
	if err != nil || len(decoded) != sha256.Size || digest != strings.ToLower(digest) {
		return "", errors.New("digest header contains an invalid SHA-256 value")
	}

	return digest, nil
}
