package repository

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestGitCommitHistoryFetcherIndexesInitialIncrementalAndRewrittenHistory(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git is not installed")
	}

	workingDirectory := filepath.Join(t.TempDir(), "working")
	remoteDirectory := filepath.Join(t.TempDir(), "remote.git")
	runTestGit(t, "init", "--initial-branch=main", workingDirectory)
	configureHistoryTestAuthor(t, workingDirectory)

	var commits []string
	for index, contents := range []string{"one\n", "two\n", "three\n"} {
		writeTestFile(t, workingDirectory, "history.txt", contents)
		runTestGit(t, "-C", workingDirectory, "add", "history.txt")
		runTestGit(t, "-C", workingDirectory, "commit", "--quiet", "-m", "commit "+string(rune('1'+index)))
		commits = append(
			commits,
			trimTestGitOutput(runTestGit(t, "-C", workingDirectory, "rev-parse", "HEAD")),
		)
	}
	runTestGit(t, "clone", "--quiet", "--bare", workingDirectory, remoteDirectory)

	repositoryID := uuid.New()
	fetcher := NewGitCommitHistoryFetcher(nil)
	record := Repository{
		CloneURL:       remoteDirectory,
		Branch:         "main",
		CommitIndexing: CommitIndexingTrackedBranchHistory,
		ID:             repositoryID,
	}

	initial, err := fetcher.FetchCommitHistory(context.Background(), record, commits[2], nil)
	if err != nil {
		t.Fatalf("initial FetchCommitHistory() error = %v", err)
	}
	if !initial.Replace || initial.HeadSHA != commits[2] || len(initial.Commits) != 3 {
		t.Fatalf("initial history = %#v", initial)
	}
	for _, commit := range initial.Commits {
		if commit.RepositoryID != repositoryID || commit.Title == nil ||
			commit.AuthorName == nil || commit.AuthoredAt == nil || commit.CommittedAt == nil {
			t.Errorf("initial commit = %#v", commit)
		}
	}

	writeTestFile(t, workingDirectory, "history.txt", "four\n")
	runTestGit(t, "-C", workingDirectory, "commit", "--quiet", "-am", "commit 4")
	commitFour := trimTestGitOutput(runTestGit(t, "-C", workingDirectory, "rev-parse", "HEAD"))
	runTestGit(t, "-C", workingDirectory, "push", "--quiet", remoteDirectory, "main")

	incremental, err := fetcher.FetchCommitHistory(
		context.Background(),
		record,
		commitFour,
		&commits[2],
	)
	if err != nil {
		t.Fatalf("incremental FetchCommitHistory() error = %v", err)
	}
	if incremental.Replace || incremental.HeadSHA != commitFour ||
		len(incremental.Commits) != 1 || incremental.Commits[0].SHA != commitFour {
		t.Errorf("incremental history = %#v", incremental)
	}

	rewrittenDirectory := filepath.Join(t.TempDir(), "rewritten")
	runTestGit(t, "init", "--initial-branch=main", rewrittenDirectory)
	configureHistoryTestAuthor(t, rewrittenDirectory)
	writeTestFile(t, rewrittenDirectory, "rewritten.txt", "replacement\n")
	runTestGit(t, "-C", rewrittenDirectory, "add", "rewritten.txt")
	runTestGit(t, "-C", rewrittenDirectory, "commit", "--quiet", "-m", "rewrite history")
	rewrittenHead := trimTestGitOutput(runTestGit(t, "-C", rewrittenDirectory, "rev-parse", "HEAD"))
	runTestGit(t, "-C", rewrittenDirectory, "push", "--quiet", "--force", remoteDirectory, "main")

	rewritten, err := fetcher.FetchCommitHistory(
		context.Background(),
		record,
		rewrittenHead,
		&commitFour,
	)
	if err != nil {
		t.Fatalf("rewritten FetchCommitHistory() error = %v", err)
	}
	if !rewritten.Replace || rewritten.HeadSHA != rewrittenHead ||
		len(rewritten.Commits) != 1 || rewritten.Commits[0].SHA != rewrittenHead {
		t.Errorf("rewritten history = %#v", rewritten)
	}
}

func configureHistoryTestAuthor(t *testing.T, directory string) {
	t.Helper()

	runTestGit(t, "-C", directory, "config", "user.name", "Slipramp Test")
	runTestGit(t, "-C", directory, "config", "user.email", "slipramp@example.com")
}

func trimTestGitOutput(output string) string {
	return strings.TrimSpace(output)
}
