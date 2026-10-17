package repository

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitFetcherFetchesExactTrackedBranch(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git is not installed")
	}

	workingDirectory := filepath.Join(t.TempDir(), "working")
	remoteDirectory := filepath.Join(t.TempDir(), "remote.git")
	runTestGit(t, "init", "--initial-branch=main", workingDirectory)
	runTestGit(t, "-C", workingDirectory, "config", "user.name", "Slipramp Test")
	runTestGit(
		t,
		"-C",
		workingDirectory,
		"config",
		"user.email",
		"slipramp@example.com",
	)

	trackedFile := filepath.Join(workingDirectory, "tracked.txt")
	if err := os.WriteFile(trackedFile, []byte("main\n"), 0o600); err != nil {
		t.Fatalf("write tracked file: %v", err)
	}

	runTestGit(t, "-C", workingDirectory, "add", "tracked.txt")
	runTestGit(t, "-C", workingDirectory, "commit", "--quiet", "-m", "main")
	mainCommit := strings.TrimSpace(
		runTestGit(t, "-C", workingDirectory, "rev-parse", "HEAD"),
	)

	runTestGit(t, "-C", workingDirectory, "switch", "--quiet", "-c", "other")
	if err := os.WriteFile(trackedFile, []byte("other\n"), 0o600); err != nil {
		t.Fatalf("update tracked file: %v", err)
	}

	runTestGit(t, "-C", workingDirectory, "commit", "--quiet", "-am", "other")
	runTestGit(t, "clone", "--quiet", "--bare", workingDirectory, remoteDirectory)

	gitConfigFile := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(gitConfigFile, nil, 0o600); err != nil {
		t.Fatalf("write Git config: %v", err)
	}

	credentialName := "local-test"
	fetcher := NewGitFetcher(RefreshCredentials{
		credentialName: gitConfigFile,
	})
	cfg := Config{
		CloneURL:              remoteDirectory,
		Branch:                "main",
		RefreshCredentialName: &credentialName,
	}
	fetched, err := fetcher.Fetch(
		context.Background(),
		Repository{
			Config: cfg,
		},
	)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	if fetched.CommitSHA != mainCommit {
		t.Errorf("Fetch() commit = %q, want %q", fetched.CommitSHA, mainCommit)
	}

	contents, err := fs.ReadFile(fetched.Files, "tracked.txt")
	if err != nil {
		t.Fatalf("read fetched file: %v", err)
	}
	if string(contents) != "main\n" {
		t.Errorf("fetched file = %q, want %q", contents, "main\\n")
	}

	if err := fetched.Close(); err != nil {
		t.Fatalf("FetchedCommit.Close() error = %v", err)
	}
	if _, err := fs.ReadFile(fetched.Files, "tracked.txt"); err == nil {
		t.Fatal("read fetched file after Close() error = nil")
	}
}

func TestGitFetcherRejectsUnknownCredential(t *testing.T) {
	t.Parallel()

	credentialName := "missing"
	fetcher := NewGitFetcher(nil)
	cfg := Config{
		CloneURL:              "https://example.com/repository.git",
		Branch:                "main",
		RefreshCredentialName: &credentialName,
	}
	_, err := fetcher.Fetch(
		context.Background(),
		Repository{
			Config: cfg,
		},
	)
	if !errors.Is(err, ErrUnknownRefreshCredential) {
		t.Fatalf("Fetch() error = %v, want %v", err, ErrUnknownRefreshCredential)
	}
}

func TestGitEnvironmentRemovesAmbientGitOverrides(t *testing.T) {
	t.Parallel()

	environment := removeGitOverrides([]string{
		"PATH=/usr/bin",
		"GIT_DIR=/tmp/untrusted",
		"GIT_CONFIG_COUNT=1",
		"HTTPS_PROXY=http://proxy.example.com",
	})
	joined := strings.Join(environment, "\n")
	if strings.Contains(joined, "GIT_") {
		t.Errorf("filtered environment contains a Git override: %q", joined)
	}

	if !strings.Contains(joined, "PATH=") ||
		!strings.Contains(joined, "HTTPS_PROXY=") {
		t.Errorf("filtered environment dropped unrelated values: %q", joined)
	}
}

func TestRunGitDoesNotReturnStandardError(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git is not installed")
	}

	const sensitiveOutput = "credential-secret-marker"
	_, err := runGit(
		context.Background(),
		gitEnvironment(""),
		sensitiveOutput,
	)
	if err == nil {
		t.Fatal("runGit() error = nil, want an error")
	}

	if strings.Contains(err.Error(), sensitiveOutput) {
		t.Errorf("runGit() exposed standard error: %v", err)
	}
}

func runTestGit(t *testing.T, arguments ...string) string {
	t.Helper()

	command := exec.CommandContext(context.Background(), "git", arguments...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", arguments, err, output)
	}

	return string(output)
}
