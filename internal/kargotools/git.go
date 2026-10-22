package kargotools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (r *Runner) gitDiff(ctx context.Context, config Config) ([]byte, error) {
	if _, err := r.runCommand(ctx, config.GitBinary, []string{
		"check-ref-format", "--branch", config.CandidateBranch,
	}, config.gitEnvironment()); err != nil {
		return nil, fmt.Errorf("validate candidate branch: %w", err)
	}
	if _, err := r.runCommand(ctx, config.GitBinary, []string{
		"check-ref-format", "--branch", config.TargetBranch,
	}, config.gitEnvironment()); err != nil {
		return nil, fmt.Errorf("validate target branch: %w", err)
	}

	temporary, err := os.MkdirTemp("", "slipramp-kargo-tools-*")
	if err != nil {
		return nil, fmt.Errorf("create temporary Git directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(temporary) }()

	repositoryPath := filepath.Join(temporary, "repository.git")
	git := func(arguments ...string) ([]byte, error) {
		return r.runCommand(ctx, config.GitBinary, arguments, config.gitEnvironment())
	}
	if _, err := git("init", "--quiet", "--bare", repositoryPath); err != nil {
		return nil, fmt.Errorf("initialize Git repository: %w", err)
	}
	if _, err := git("--git-dir", repositoryPath, "remote", "add", "origin", config.RepositoryURL); err != nil {
		return nil, fmt.Errorf("configure Git remote: %w", err)
	}

	candidateRef := "refs/heads/" + config.CandidateBranch
	remoteOutput, err := git(
		"--git-dir", repositoryPath,
		"ls-remote", "--exit-code", "--heads", "origin", candidateRef,
	)
	if err != nil {
		return nil, fmt.Errorf("resolve candidate branch: %w", err)
	}
	remoteRevision, err := parseRemoteRevision(remoteOutput, candidateRef)
	if err != nil {
		return nil, err
	}
	if remoteRevision != config.CandidateRevision {
		return nil, fmt.Errorf(
			"candidate branch %q resolves to %s instead of %s",
			config.CandidateBranch,
			remoteRevision,
			config.CandidateRevision,
		)
	}

	refspec := "+" + candidateRef + ":refs/slipramp/candidate"
	if _, err := git(
		"--git-dir", repositoryPath,
		"fetch", "--quiet", "--no-tags", "--filter=blob:none", "origin", refspec,
	); err != nil {
		return nil, fmt.Errorf("fetch candidate branch: %w", err)
	}
	localRevision, err := git("--git-dir", repositoryPath, "rev-parse", "refs/slipramp/candidate^{commit}")
	if err != nil {
		return nil, fmt.Errorf("resolve fetched candidate branch: %w", err)
	}
	if strings.TrimSpace(string(localRevision)) != config.CandidateRevision {
		return nil, errors.New("candidate branch changed while it was fetched")
	}
	if _, err := git("--git-dir", repositoryPath, "cat-file", "-e", config.BaseRevision+"^{commit}"); err != nil {
		return nil, fmt.Errorf("resolve base revision %s: %w", config.BaseRevision, err)
	}

	output, err := git(
		"--git-dir", repositoryPath,
		"diff", "--no-ext-diff", "--no-color", "--find-renames",
		config.BaseRevision, config.CandidateRevision,
	)
	if err != nil {
		return nil, fmt.Errorf("generate Git diff: %w", err)
	}

	return output, nil
}

func parseRemoteRevision(output []byte, ref string) (string, error) {
	fields := strings.Fields(string(output))
	if len(fields) != 2 || fields[1] != ref || !fullGitObjectID.MatchString(fields[0]) {
		return "", fmt.Errorf("unexpected Git response for candidate branch %q", ref)
	}

	return strings.ToLower(fields[0]), nil
}
