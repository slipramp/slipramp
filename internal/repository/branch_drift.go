package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type BranchComparisonRequest struct {
	TargetBranch             string
	AcknowledgedSourceCommit string
	AcknowledgedTargetCommit string
	SourcePaths              []string
}

type BranchComparison struct {
	AcknowledgedTargetAuthoredAt *time.Time
	SourceBranchCommit           string
	SourceBranchAuthoredAt       *time.Time
	TargetBranchCommit           string
	SourceDrift                  bool
	TargetDrift                  bool
	Error                        string
}

type BranchScanner interface {
	ScanBranches(context.Context, Repository, []BranchComparisonRequest) ([]BranchComparison, error)
}

type GitBranchScanner struct {
	credentials RefreshCredentials
}

func NewGitBranchScanner(credentials RefreshCredentials) *GitBranchScanner {
	return &GitBranchScanner{credentials: credentials}
}

func (s *GitBranchScanner) ScanBranches(
	ctx context.Context,
	repository Repository,
	requests []BranchComparisonRequest,
) ([]BranchComparison, error) {
	comparisons := make([]BranchComparison, len(requests))
	if len(requests) == 0 {
		return comparisons, nil
	}

	gitConfigFile, err := s.credentials.gitConfigFile(repository.RefreshCredentialName)
	if err != nil {
		return nil, err
	}
	environment := gitEnvironment(gitConfigFile)
	if _, err := runGit(ctx, environment, "check-ref-format", "--branch", repository.Branch); err != nil {
		return nil, fmt.Errorf("validate source branch %q: %w", repository.Branch, err)
	}

	validTargetBranches := make(map[string]struct{})
	for index, request := range requests {
		if _, err := runGit(ctx, environment, "check-ref-format", "--branch", request.TargetBranch); err != nil {
			comparisons[index].Error = fmt.Sprintf("target branch %q is invalid", request.TargetBranch)
			continue
		}
		if !validCommitSHA(request.AcknowledgedSourceCommit) ||
			!validCommitSHA(request.AcknowledgedTargetCommit) {
			comparisons[index].Error = "successful-render acknowledgement contains an invalid Git object ID"
			continue
		}

		validTargetBranches[request.TargetBranch] = struct{}{}
	}

	temporary, err := os.MkdirTemp("", "slipramp-branch-drift-*")
	if err != nil {
		return nil, fmt.Errorf("create branch drift directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(temporary) }()

	repositoryPath := filepath.Join(temporary, "repository.git")
	git := func(arguments ...string) (string, error) {
		return runGit(ctx, environment, arguments...)
	}
	if _, err := git("init", "--quiet", "--bare", repositoryPath); err != nil {
		return nil, fmt.Errorf("initialize branch drift repository: %w", err)
	}
	if _, err := git("--git-dir", repositoryPath, "remote", "add", "origin", repository.CloneURL); err != nil {
		return nil, fmt.Errorf("configure branch drift remote: %w", err)
	}

	sourceRef := "refs/heads/" + repository.Branch
	targetBranches := mapsKeys(validTargetBranches)
	slices.Sort(targetBranches)
	remoteRefs := append([]string{sourceRef}, branchReferences(targetBranches)...)
	output, err := git(append(
		[]string{"--git-dir", repositoryPath, "ls-remote", "--refs", "--heads", "origin"},
		remoteRefs...,
	)...)
	if err != nil {
		return nil, fmt.Errorf("list branch drift refs: %w", err)
	}
	remoteRevisions, err := parseRemoteBranches(output)
	if err != nil {
		return nil, err
	}
	sourceCommit, ok := remoteRevisions[sourceRef]
	if !ok {
		return nil, fmt.Errorf("source branch %q does not exist", repository.Branch)
	}

	refspecs := []string{"+" + sourceRef + ":refs/slipramp/source"}
	localTargetRefs := make(map[string]string, len(targetBranches))
	for index, branch := range targetBranches {
		remoteRef := "refs/heads/" + branch
		if _, ok := remoteRevisions[remoteRef]; !ok {
			continue
		}
		localRef := fmt.Sprintf("refs/slipramp/targets/%d", index)
		localTargetRefs[branch] = localRef
		refspecs = append(refspecs, "+"+remoteRef+":"+localRef)
	}
	fetchArguments := []string{
		"--git-dir", repositoryPath,
		"fetch", "--quiet", "--no-tags", "--filter=blob:none", "origin",
	}
	if _, err := git(append(fetchArguments, refspecs...)...); err != nil {
		return nil, fmt.Errorf("fetch branch drift refs: %w", err)
	}
	fetchedSource, err := resolveCommit(git, repositoryPath, "refs/slipramp/source")
	if err != nil {
		return nil, fmt.Errorf("resolve fetched source branch: %w", err)
	}
	if fetchedSource != sourceCommit {
		return nil, errors.New("source branch changed while branch drift was scanned")
	}
	authoredAtCache := make(map[string]*time.Time)
	commitAuthoredAt := func(commit string) *time.Time {
		if value, ok := authoredAtCache[commit]; ok {
			return value
		}
		metadata, metadataErr := readCommitMetadata(ctx, environment, repositoryPath, commit)
		if metadataErr != nil {
			authoredAtCache[commit] = nil

			return nil
		}
		value := metadata.authoredAt
		authoredAtCache[commit] = &value

		return &value
	}

	for index, request := range requests {
		if comparisons[index].Error != "" {
			continue
		}
		comparisons[index].SourceBranchCommit = sourceCommit

		remoteRef := "refs/heads/" + request.TargetBranch
		targetCommit, ok := remoteRevisions[remoteRef]
		if !ok {
			comparisons[index].Error = fmt.Sprintf("target branch %q does not exist", request.TargetBranch)
			continue
		}
		localRef := localTargetRefs[request.TargetBranch]
		fetchedTarget, resolveErr := resolveCommit(git, repositoryPath, localRef)
		if resolveErr != nil {
			comparisons[index].Error = fmt.Sprintf("resolve target branch %q", request.TargetBranch)
			continue
		}
		if fetchedTarget != targetCommit {
			comparisons[index].Error = fmt.Sprintf(
				"target branch %q changed while branch drift was scanned",
				request.TargetBranch,
			)
			continue
		}
		if _, objectErr := git(
			"--git-dir", repositoryPath,
			"cat-file", "-e", request.AcknowledgedSourceCommit+"^{commit}",
		); objectErr != nil {
			comparisons[index].Error = "acknowledged source commit is not available from the source branch"
			continue
		}

		diffArguments := []string{
			"--git-dir", repositoryPath,
			"diff", "--quiet", "--no-ext-diff",
			request.AcknowledgedSourceCommit, sourceCommit,
			"--",
		}
		diffArguments = append(diffArguments, request.SourcePaths...)
		diffStatus, diffErr := runGitStatus(ctx, environment, diffArguments...)
		if diffErr != nil {
			comparisons[index].Error = "compare acknowledged source paths"
			continue
		}
		if diffStatus != 0 && diffStatus != 1 {
			comparisons[index].Error = "compare acknowledged source paths"
			continue
		}

		comparisons[index] = BranchComparison{
			AcknowledgedTargetAuthoredAt: commitAuthoredAt(request.AcknowledgedTargetCommit),
			SourceBranchCommit:           sourceCommit,
			SourceBranchAuthoredAt:       commitAuthoredAt(sourceCommit),
			TargetBranchCommit:           targetCommit,
			SourceDrift:                  diffStatus == 1,
			TargetDrift:                  targetCommit != request.AcknowledgedTargetCommit,
		}
	}

	return comparisons, nil
}

func branchReferences(branches []string) []string {
	references := make([]string, 0, len(branches))
	for _, branch := range branches {
		references = append(references, "refs/heads/"+branch)
	}

	return references
}

func parseRemoteBranches(output string) (map[string]string, error) {
	revisions := make(map[string]string)
	for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 || !validCommitSHA(fields[0]) || !strings.HasPrefix(fields[1], "refs/heads/") {
			return nil, errors.New("git returned an invalid branch reference")
		}
		revisions[fields[1]] = fields[0]
	}

	return revisions, nil
}

func resolveCommit(
	git func(...string) (string, error),
	repositoryPath string,
	reference string,
) (string, error) {
	output, err := git("--git-dir", repositoryPath, "rev-parse", "--verify", reference+"^{commit}")
	if err != nil {
		return "", err
	}
	commit := strings.TrimSpace(output)
	if !validCommitSHA(commit) {
		return "", fmt.Errorf("git returned invalid commit %q", commit)
	}

	return commit, nil
}

func runGitStatus(ctx context.Context, environment []string, arguments ...string) (int, error) {
	command := exec.CommandContext(ctx, "git", arguments...)
	command.Env = environment
	err := command.Run()
	if err == nil {
		return 0, nil
	}
	if ctx.Err() != nil {
		return -1, ctx.Err()
	}

	if exitError, ok := errors.AsType[*exec.ExitError](err); ok {
		return exitError.ExitCode(), nil
	}

	return -1, fmt.Errorf("run Git: %w", err)
}

func mapsKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}

	return keys
}

var _ BranchScanner = (*GitBranchScanner)(nil)
