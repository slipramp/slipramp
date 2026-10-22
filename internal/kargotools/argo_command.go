package kargotools

import (
	"context"
	"fmt"
	"os"
)

func (r *Runner) argoDiff(ctx context.Context, config Config, target ArgoTarget) ([]byte, error) {
	arguments := []string{
		"app", "diff", config.Application,
		"--revision", config.CandidateRevision,
		"--hard-refresh",
		"--exit-code=false",
		"--server", target.Server,
	}
	if target.Plaintext {
		arguments = append(arguments, "--plaintext")
	}
	if target.GRPCWeb {
		arguments = append(arguments, "--grpc-web")
	}
	arguments = append(arguments, "--server-side-diff")

	environment := setEnvironment(os.Environ(), "ARGOCD_AUTH_TOKEN", target.AuthToken)
	output, err := r.runCommand(ctx, config.ArgoBinary, arguments, environment)
	if err != nil {
		return nil, fmt.Errorf("generate Argo CD diff: %w", err)
	}

	return output, nil
}
