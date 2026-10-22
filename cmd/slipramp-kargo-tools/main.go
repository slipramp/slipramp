package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/slipramp/slipramp/internal/kargotools"
)

const (
	gitUsernameEnvironment = "SLIPRAMP_KARGO_GIT_USERNAME"
	gitPasswordEnvironment = "SLIPRAMP_KARGO_GIT_PASSWORD"
)

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stderr))
}

func run(ctx context.Context, arguments []string, stderr io.Writer) int {
	if len(arguments) == 0 || arguments[0] != "prepare" {
		_, _ = fmt.Fprintln(stderr, "usage: slipramp-kargo-tools prepare [options]")
		return 2
	}

	flags := flag.NewFlagSet("slipramp-kargo-tools prepare", flag.ContinueOnError)
	flags.SetOutput(stderr)
	config := kargotools.Config{
		GitUsername: os.Getenv(gitUsernameEnvironment),
		GitPassword: os.Getenv(gitPasswordEnvironment),
	}
	flags.StringVar(&config.PromotionID, "promotion-id", "", "Slipramp Promotion UUID")
	flags.StringVar(&config.ExternalPromotionID, "external-promotion-id", "", "external Kargo Promotion identity")
	flags.StringVar(&config.Project, "project", "", "Kargo project")
	flags.StringVar(&config.Stage, "stage", "", "Kargo Stage")
	flags.StringVar(&config.Application, "application", "", "Argo CD Application")
	flags.StringVar(&config.Environment, "environment", "", "Argo CD target environment")
	flags.StringVar(&config.RepositoryName, "repository-name", "", "Slipramp Repository name")
	flags.StringVar(&config.RepositoryURL, "repository-url", "", "Git repository URL")
	flags.StringVar(&config.BaseRevision, "base-revision", "", "full base Git object ID")
	flags.StringVar(&config.CandidateRevision, "candidate-revision", "", "full candidate Git object ID")
	flags.StringVar(&config.CandidateBranch, "candidate-branch", "", "candidate Git branch")
	flags.StringVar(&config.TargetBranch, "target-branch", "", "target Git branch")
	flags.StringVar(&config.SourcePaths, "source-paths", "", "JSON array of Stage source paths")
	flags.StringVar(
		&config.ArgoTargetsPath,
		"argo-targets",
		kargotools.DefaultArgoTargetsPath,
		"path to the Argo CD target configuration",
	)
	flags.StringVar(&config.GitDiffPath, "git-diff-output", kargotools.DefaultGitDiffPath, "Git diff artifact path")
	flags.StringVar(
		&config.ArgoDiffPath,
		"argo-diff-output",
		kargotools.DefaultArgoDiffPath,
		"Argo CD diff artifact path",
	)
	if err := flags.Parse(arguments[1:]); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		_, _ = fmt.Fprintf(stderr, "unexpected positional arguments: %v\n", flags.Args())
		return 2
	}

	if err := kargotools.NewRunner().Prepare(ctx, config); err != nil {
		_, _ = fmt.Fprintf(stderr, "prepare Kargo review: %v\n", err)
		return 1
	}

	_, _ = fmt.Fprintln(stderr, "prepared Git and Argo CD review artifacts")
	return 0
}
