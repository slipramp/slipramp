package repository

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitCandidateBranchRemoteListsAndDeletesExactTip(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git is not installed")
	}

	workingDirectory := filepath.Join(t.TempDir(), "working")
	remoteDirectory := filepath.Join(t.TempDir(), "remote.git")
	runTestGit(t, "init", "--initial-branch=main", workingDirectory)
	runTestGit(t, "-C", workingDirectory, "config", "user.name", "Slipramp Test")
	runTestGit(t, "-C", workingDirectory, "config", "user.email", "slipramp@example.com")
	writeTestFile(t, workingDirectory, "candidate.txt", "candidate\n")
	runTestGit(t, "-C", workingDirectory, "add", "candidate.txt")
	runTestGit(t, "-C", workingDirectory, "commit", "--quiet", "-m", "candidate")
	candidateRevision := strings.TrimSpace(runTestGit(t, "-C", workingDirectory, "rev-parse", "HEAD"))
	runTestGit(t, "-C", workingDirectory, "branch", "kargo/promotion/review-1")
	runTestGit(t, "clone", "--quiet", "--bare", workingDirectory, remoteDirectory)
	runTestGit(
		t,
		"-C", workingDirectory,
		"push", "--quiet", remoteDirectory,
		"kargo/promotion/review-1:kargo/promotion/review-1",
	)

	gitConfigFile := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(gitConfigFile, nil, 0o600); err != nil {
		t.Fatalf("write Git config: %v", err)
	}
	remote := NewGitCandidateBranchRemote(RefreshCredentials{"cleanup": gitConfigFile})
	record := Repository{
		Config: Config{CloneURL: remoteDirectory}, //nolint:modernize // A keyed embedded Config keeps this fixture focused.
	}

	branches, err := remote.ListCandidateBranches(context.Background(), record, "cleanup")
	if err != nil {
		t.Fatalf("ListCandidateBranches() error = %v", err)
	}
	if len(branches) != 1 || branches["kargo/promotion/review-1"] != candidateRevision {
		t.Fatalf("candidate branches = %#v", branches)
	}

	result, err := remote.DeleteCandidateBranch(
		context.Background(),
		record,
		"cleanup",
		"kargo/promotion/review-1",
		strings.Repeat("a", 40),
	)
	if err != nil || result != CandidateBranchMoved {
		t.Fatalf("DeleteCandidateBranch() moved = %q, %v", result, err)
	}

	result, err = remote.DeleteCandidateBranch(
		context.Background(),
		record,
		"cleanup",
		"kargo/promotion/review-1",
		candidateRevision,
	)
	if err != nil || result != CandidateBranchDeleted {
		t.Fatalf("DeleteCandidateBranch() deleted = %q, %v", result, err)
	}
	branches, err = remote.ListCandidateBranches(context.Background(), record, "cleanup")
	if err != nil || len(branches) != 0 {
		t.Fatalf("candidate branches after deletion = %#v, %v", branches, err)
	}

	result, err = remote.DeleteCandidateBranch(
		context.Background(),
		record,
		"cleanup",
		"kargo/promotion/review-1",
		candidateRevision,
	)
	if err != nil || result != CandidateBranchAbsent {
		t.Fatalf("DeleteCandidateBranch() absent = %q, %v", result, err)
	}
}

func TestGitCandidateBranchRemoteRejectsBranchOutsideCandidateNamespace(t *testing.T) {
	t.Parallel()

	remote := NewGitCandidateBranchRemote(nil)
	_, err := remote.DeleteCandidateBranch(
		context.Background(),
		Repository{},
		"cleanup",
		"main",
		strings.Repeat("a", 40),
	)
	if err == nil || !strings.Contains(err.Error(), candidateBranchPrefix) {
		t.Fatalf("DeleteCandidateBranch() error = %v", err)
	}
}
