package repository

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitBranchScannerComparesRepositoryBranchesAndAcknowledgedRender(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git is not installed")
	}

	workingDirectory := filepath.Join(t.TempDir(), "working")
	remoteDirectory := filepath.Join(t.TempDir(), "remote.git")
	runTestGit(t, "init", "--initial-branch=main", workingDirectory)
	runTestGit(t, "-C", workingDirectory, "config", "user.name", "Slipramp Test")
	runTestGit(t, "-C", workingDirectory, "config", "user.email", "slipramp@example.com")
	writeTestFile(t, workingDirectory, "apps/example/base/deployment.yaml", "version: one\n")
	writeTestFile(t, workingDirectory, "README.md", "initial\n")
	runTestGit(t, "-C", workingDirectory, "add", ".")
	runTestGit(t, "-C", workingDirectory, "commit", "--quiet", "-m", "initial")
	acknowledgedSource := strings.TrimSpace(runTestGit(t, "-C", workingDirectory, "rev-parse", "HEAD"))

	runTestGit(t, "-C", workingDirectory, "branch", "stage/example/prod")
	writeTestFile(t, workingDirectory, "README.md", "updated\n")
	runTestGit(t, "-C", workingDirectory, "commit", "--quiet", "-am", "unrelated source update")
	currentSource := strings.TrimSpace(runTestGit(t, "-C", workingDirectory, "rev-parse", "HEAD"))
	runTestGit(t, "clone", "--quiet", "--bare", workingDirectory, remoteDirectory)
	acknowledgedTarget := strings.TrimSpace(runTestGit(
		t,
		"--git-dir", remoteDirectory,
		"rev-parse", "refs/heads/stage/example/prod",
	))

	scanner := NewGitBranchScanner(nil)
	record := Repository{CloneURL: remoteDirectory, Branch: "main"}
	request := BranchComparisonRequest{
		TargetBranch:             "stage/example/prod",
		AcknowledgedSourceCommit: acknowledgedSource,
		AcknowledgedTargetCommit: acknowledgedTarget,
		SourcePaths:              []string{"apps/example/base"},
	}

	comparisons, err := scanner.ScanBranches(t.Context(), record, []BranchComparisonRequest{request})
	if err != nil {
		t.Fatalf("ScanBranches() error = %v", err)
	}
	if len(comparisons) != 1 || comparisons[0].Error != "" || comparisons[0].SourceDrift ||
		comparisons[0].TargetDrift {
		t.Fatalf("no-op comparison = %#v", comparisons)
	}
	if comparisons[0].SourceBranchCommit != currentSource ||
		comparisons[0].TargetBranchCommit != acknowledgedTarget {
		t.Errorf("no-op branch revisions = %#v", comparisons[0])
	}

	writeTestFile(t, workingDirectory, "apps/example/base/deployment.yaml", "version: two\n")
	runTestGit(t, "-C", workingDirectory, "commit", "--quiet", "-am", "relevant source update")
	runTestGit(t, "-C", workingDirectory, "push", "--quiet", remoteDirectory, "main")

	comparisons, err = scanner.ScanBranches(t.Context(), record, []BranchComparisonRequest{request})
	if err != nil {
		t.Fatalf("ScanBranches() after source update error = %v", err)
	}
	if !comparisons[0].SourceDrift || comparisons[0].TargetDrift {
		t.Errorf("source drift comparison = %#v", comparisons[0])
	}

	runTestGit(t, "-C", workingDirectory, "switch", "--quiet", "stage/example/prod")
	writeTestFile(t, workingDirectory, "rendered.yaml", "external change\n")
	runTestGit(t, "-C", workingDirectory, "add", "rendered.yaml")
	runTestGit(t, "-C", workingDirectory, "commit", "--quiet", "-m", "external target update")
	runTestGit(
		t,
		"-C", workingDirectory,
		"push", "--quiet", remoteDirectory, "stage/example/prod",
	)

	comparisons, err = scanner.ScanBranches(t.Context(), record, []BranchComparisonRequest{request})
	if err != nil {
		t.Fatalf("ScanBranches() after target update error = %v", err)
	}
	if !comparisons[0].SourceDrift || !comparisons[0].TargetDrift {
		t.Errorf("source and target drift comparison = %#v", comparisons[0])
	}
}

func TestGitBranchScannerReportsMissingTargetBranchPerStage(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git is not installed")
	}

	workingDirectory := filepath.Join(t.TempDir(), "working")
	remoteDirectory := filepath.Join(t.TempDir(), "remote.git")
	runTestGit(t, "init", "--initial-branch=main", workingDirectory)
	runTestGit(t, "-C", workingDirectory, "config", "user.name", "Slipramp Test")
	runTestGit(t, "-C", workingDirectory, "config", "user.email", "slipramp@example.com")
	writeTestFile(t, workingDirectory, "manifest.yaml", "version: one\n")
	runTestGit(t, "-C", workingDirectory, "add", ".")
	runTestGit(t, "-C", workingDirectory, "commit", "--quiet", "-m", "initial")
	commit := strings.TrimSpace(runTestGit(t, "-C", workingDirectory, "rev-parse", "HEAD"))
	runTestGit(t, "clone", "--quiet", "--bare", workingDirectory, remoteDirectory)

	comparisons, err := NewGitBranchScanner(nil).ScanBranches(
		t.Context(),
		Repository{CloneURL: remoteDirectory, Branch: "main"},
		[]BranchComparisonRequest{{
			TargetBranch:             "stage/missing/prod",
			AcknowledgedSourceCommit: commit,
			AcknowledgedTargetCommit: commit,
			SourcePaths:              []string{"manifest.yaml"},
		}},
	)
	if err != nil {
		t.Fatalf("ScanBranches() error = %v", err)
	}
	if len(comparisons) != 1 || !strings.Contains(comparisons[0].Error, "does not exist") {
		t.Fatalf("missing target comparison = %#v", comparisons)
	}
}
