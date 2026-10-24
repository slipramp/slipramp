package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

const candidateBranchPrefix = "kargo/promotion/"

type CandidateBranchDeleteResult string

const (
	CandidateBranchDeleted CandidateBranchDeleteResult = "deleted"
	CandidateBranchAbsent  CandidateBranchDeleteResult = "absent"
	CandidateBranchMoved   CandidateBranchDeleteResult = "moved"
)

type CandidateBranchRemote interface {
	ListCandidateBranches(context.Context, Repository, string) (map[string]string, error)
	DeleteCandidateBranch(
		context.Context,
		Repository,
		string,
		string,
		string,
	) (CandidateBranchDeleteResult, error)
}

type GitCandidateBranchRemote struct {
	credentials RefreshCredentials
}

func NewGitCandidateBranchRemote(credentials RefreshCredentials) *GitCandidateBranchRemote {
	return &GitCandidateBranchRemote{credentials: credentials}
}

func (r *GitCandidateBranchRemote) ListCandidateBranches(
	ctx context.Context,
	record Repository,
	credentialName string,
) (map[string]string, error) {
	environment, err := r.environment(credentialName)
	if err != nil {
		return nil, err
	}
	output, err := runGit(
		ctx,
		environment,
		"ls-remote",
		"--refs",
		"--heads",
		"--",
		record.CloneURL,
		"refs/heads/"+candidateBranchPrefix+"*",
	)
	if err != nil {
		return nil, fmt.Errorf("list candidate branches: %w", err)
	}
	references, err := parseRemoteBranches(output)
	if err != nil {
		return nil, err
	}
	branches := make(map[string]string, len(references))
	for reference, revision := range references {
		branch := strings.TrimPrefix(reference, "refs/heads/")
		if !strings.HasPrefix(branch, candidateBranchPrefix) {
			return nil, fmt.Errorf("git returned candidate branch outside %q", candidateBranchPrefix)
		}
		branches[branch] = revision
	}

	return branches, nil
}

func (r *GitCandidateBranchRemote) DeleteCandidateBranch(
	ctx context.Context,
	record Repository,
	credentialName string,
	branch string,
	expectedRevision string,
) (CandidateBranchDeleteResult, error) {
	if err := validateCandidateBranch(branch); err != nil {
		return "", err
	}
	if !ValidCommitSHA(expectedRevision) {
		return "", fmt.Errorf("candidate branch expected revision %q is invalid", expectedRevision)
	}
	environment, err := r.environment(credentialName)
	if err != nil {
		return "", err
	}
	reference := "refs/heads/" + branch
	revision, exists, err := readRemoteReference(ctx, environment, record.CloneURL, reference)
	if err != nil {
		return "", err
	}
	if !exists {
		return CandidateBranchAbsent, nil
	}
	if revision != expectedRevision {
		return CandidateBranchMoved, nil
	}

	lease := "--force-with-lease=" + reference + ":" + expectedRevision
	_, deleteErr := runGit(
		ctx,
		environment,
		"push",
		"--porcelain",
		lease,
		"--",
		record.CloneURL,
		":"+reference,
	)
	if deleteErr == nil {
		return CandidateBranchDeleted, nil
	}

	// A transport failure can hide a successful deletion. Re-read the exact ref
	// so a retry remains idempotent and a concurrent update is never removed.
	revision, exists, readErr := readRemoteReference(ctx, environment, record.CloneURL, reference)
	if readErr != nil {
		return "", fmt.Errorf("delete candidate branch %q: %w", branch, errors.Join(deleteErr, readErr))
	}
	if !exists {
		return CandidateBranchDeleted, nil
	}
	if revision != expectedRevision {
		return CandidateBranchMoved, nil
	}

	return "", fmt.Errorf("delete candidate branch %q: %w", branch, deleteErr)
}

func (r *GitCandidateBranchRemote) environment(credentialName string) ([]string, error) {
	gitConfigFile, err := r.credentials.gitConfigFile(&credentialName)
	if err != nil {
		return nil, err
	}

	return gitEnvironment(gitConfigFile), nil
}

func readRemoteReference(
	ctx context.Context,
	environment []string,
	cloneURL string,
	reference string,
) (string, bool, error) {
	output, err := runGit(ctx, environment, "ls-remote", "--refs", "--heads", "--", cloneURL, reference)
	if err != nil {
		return "", false, fmt.Errorf("read candidate branch: %w", err)
	}
	references, err := parseRemoteBranches(output)
	if err != nil {
		return "", false, err
	}
	if len(references) == 0 {
		return "", false, nil
	}
	if len(references) != 1 {
		return "", false, fmt.Errorf("git returned multiple values for candidate branch")
	}
	revision, ok := references[reference]
	if !ok {
		return "", false, fmt.Errorf("git returned an unexpected candidate branch")
	}

	return revision, true, nil
}

func validateCandidateBranch(branch string) error {
	if !strings.HasPrefix(branch, candidateBranchPrefix) || len(branch) == len(candidateBranchPrefix) {
		return fmt.Errorf("candidate branch must be under %q", candidateBranchPrefix)
	}
	if err := validateBranch(branch); err != nil {
		return fmt.Errorf("candidate branch: %w", err)
	}

	return nil
}

var _ CandidateBranchRemote = (*GitCandidateBranchRemote)(nil)
