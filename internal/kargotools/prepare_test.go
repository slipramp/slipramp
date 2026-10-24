package kargotools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slipramp/slipramp/internal/argodiff"
)

func TestPrepareWritesReadOnlyGitAndStructuredArgoArtifacts(t *testing.T) {
	gitBinary, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed")
	}

	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	work := filepath.Join(root, "work")
	runGit(t, gitBinary, "", "init", "--bare", origin)
	runGit(t, gitBinary, "", "init", work)
	runGit(t, gitBinary, work, "config", "user.name", "Slipramp Test")
	runGit(t, gitBinary, work, "config", "user.email", "slipramp@example.com")
	writeTestFile(t, filepath.Join(work, "manifest.yaml"), "version: one\n")
	runGit(t, gitBinary, work, "add", "manifest.yaml")
	runGit(t, gitBinary, work, "commit", "-m", "base")
	baseRevision := runGit(t, gitBinary, work, "rev-parse", "HEAD")
	runGit(t, gitBinary, work, "remote", "add", "origin", origin)

	writeTestFile(t, filepath.Join(work, "manifest.yaml"), "version: two\n")
	runGit(t, gitBinary, work, "commit", "-am", "candidate")
	candidateRevision := runGit(t, gitBinary, work, "rev-parse", "HEAD")
	runGit(t, gitBinary, work, "push", "origin", "HEAD:refs/heads/kargo/promotion/test")

	argoCapture := filepath.Join(root, "argo-arguments")
	t.Setenv("ARGO_CAPTURE", argoCapture)
	argoBinary := filepath.Join(root, "argocd")
	writeExecutable(t, argoBinary, `#!/bin/sh
test "$ARGOCD_AUTH_TOKEN" = "prod-token"
printf '%s\n' "$*" > "$ARGO_CAPTURE"
cat <<'EOF'
===== apps/Deployment default/example ======
@@ -1 +1 @@
-image: old
+image: new
EOF
`)

	targetsPath := filepath.Join(root, "targets.json")
	writeTestFile(t, targetsPath, `{"prod":{"server":"argocd.example.com","authToken":"prod-token","grpcWeb":true}}`)
	gitDiffPath := filepath.Join(root, "artifacts", "git.diff")
	argoDiffPath := filepath.Join(root, "artifacts", "argo.json")
	config := validConfig(baseRevision, candidateRevision)
	config.RepositoryURL = origin
	config.ArgoTargetsPath = targetsPath
	config.GitDiffPath = gitDiffPath
	config.ArgoDiffPath = argoDiffPath
	config.GitBinary = gitBinary
	config.ArgoBinary = argoBinary

	if err := NewRunner().Prepare(context.Background(), config); err != nil {
		t.Fatal(err)
	}

	gitDiff := readTestFile(t, gitDiffPath)
	if !strings.Contains(gitDiff, "-version: one") || !strings.Contains(gitDiff, "+version: two") {
		t.Fatalf("Git diff artifact = %q", gitDiff)
	}
	if !strings.HasSuffix(gitDiff, "\n") {
		t.Fatalf("Git diff artifact lacks a final newline: %q", gitDiff)
	}

	argoDiff, err := argodiff.Decode([]byte(readTestFile(t, argoDiffPath)))
	if err != nil {
		t.Fatal(err)
	}
	if len(argoDiff.Resources) != 1 || argoDiff.Resources[0].Name != "example" ||
		argoDiff.Resources[0].Diff != "@@ -1 +1 @@\n-image: old\n+image: new" {
		t.Fatalf("Argo CD diff artifact = %#v", argoDiff)
	}

	argoArguments := readTestFile(t, argoCapture)
	for _, expected := range []string{
		"app diff example",
		"--revision " + candidateRevision,
		"--server argocd.example.com",
		"--grpc-web",
		"--server-side-diff",
	} {
		if !strings.Contains(argoArguments, expected) {
			t.Errorf("Argo CD arguments %q do not contain %q", argoArguments, expected)
		}
	}
	if strings.Contains(argoArguments, "prod-token") || strings.Contains(argoArguments, "--auth-token") {
		t.Fatalf("Argo CD token was exposed in arguments: %q", argoArguments)
	}

	remoteRevision := runGit(t, gitBinary, "", "--git-dir", origin, "rev-parse", "refs/heads/kargo/promotion/test")
	if remoteRevision != candidateRevision {
		t.Fatalf("candidate branch changed to %s", remoteRevision)
	}
	if output := runGit(
		t,
		gitBinary,
		"",
		"--git-dir",
		origin,
		"for-each-ref",
		"--format=%(refname)",
		"refs/heads/stage/example/prod",
	); output != "" {
		t.Fatalf("target branch was created: %q", output)
	}
}

func TestPrepareRejectsCandidateBranchAtDifferentRevision(t *testing.T) {
	t.Parallel()

	runner := NewRunner()
	runner.runCommand = func(_ context.Context, _ string, arguments []string, _ []string) ([]byte, error) {
		joined := strings.Join(arguments, " ")
		switch {
		case strings.Contains(joined, "check-ref-format"):
			return nil, nil
		case strings.Contains(joined, "ls-remote"):
			return []byte(strings.Repeat("b", 40) + "\trefs/heads/kargo/promotion/test\n"), nil
		case strings.HasPrefix(joined, "app diff"):
			return nil, nil
		default:
			return nil, nil
		}
	}

	root := t.TempDir()
	targetsPath := filepath.Join(root, "targets.json")
	writeTestFile(t, targetsPath, `{"prod":{"server":"argocd.example.com","authToken":"prod-token"}}`)
	config := validConfig(strings.Repeat("a", 40), strings.Repeat("c", 40))
	config.RepositoryURL = "https://example.com/operations.git"
	config.ArgoTargetsPath = targetsPath
	config.GitDiffPath = filepath.Join(root, "git.diff")
	config.ArgoDiffPath = filepath.Join(root, "argo.json")

	err := runner.Prepare(context.Background(), config)
	if err == nil || !strings.Contains(err.Error(), "resolves to") {
		t.Fatalf("Prepare() error = %v", err)
	}
	if _, err := os.Stat(config.GitDiffPath); !os.IsNotExist(err) {
		t.Fatalf("Git artifact exists after failure: %v", err)
	}
}

func TestPrepareRejectsUnknownEnvironmentBeforeExecution(t *testing.T) {
	t.Parallel()

	runner := NewRunner()
	runner.runCommand = func(context.Context, string, []string, []string) ([]byte, error) {
		t.Fatal("command executed for an unknown environment")
		return nil, nil
	}

	root := t.TempDir()
	targetsPath := filepath.Join(root, "targets.json")
	writeTestFile(t, targetsPath, `{"prod":{"server":"argocd.example.com","authToken":"prod-token"}}`)
	config := validConfig(strings.Repeat("a", 40), strings.Repeat("b", 40))
	config.Environment = "homelab"
	config.ArgoTargetsPath = targetsPath
	config.GitDiffPath = filepath.Join(root, "git.diff")
	config.ArgoDiffPath = filepath.Join(root, "argo.json")

	err := runner.Prepare(context.Background(), config)
	if err == nil || !strings.Contains(err.Error(), `not configured for environment "homelab"`) {
		t.Fatalf("Prepare() error = %v", err)
	}
}

func TestConfigAcceptsFullSHA256ObjectIDs(t *testing.T) {
	t.Parallel()

	config := validConfig(strings.Repeat("a", 64), strings.Repeat("b", 64))
	config.setDefaults()
	if err := config.validate(); err != nil {
		t.Fatalf("validate SHA-256 revisions: %v", err)
	}
}

func validConfig(baseRevision, candidateRevision string) Config {
	return Config{
		PromotionID:         "018f90ff-9fd8-7b5f-b7e1-f32ff54c1200",
		ExternalPromotionID: "external-dns-01jbe4q8",
		Project:             "kargo-core",
		Stage:               "external-dns-prod",
		Application:         "example",
		Environment:         "prod",
		RepositoryName:      "operations",
		RepositoryURL:       "https://example.com/operations.git",
		BaseRevision:        baseRevision,
		CandidateRevision:   candidateRevision,
		CandidateBranch:     "kargo/promotion/test",
		TargetBranch:        "stage/example/prod",
		SourcePaths:         `["k8s/example"]`,
	}
}

func runGit(t *testing.T, binary, directory string, arguments ...string) string {
	t.Helper()
	command := exec.CommandContext(t.Context(), binary, arguments...)
	command.Dir = directory
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", arguments, err, output)
	}

	return strings.TrimSpace(string(output))
}

func writeTestFile(t *testing.T, path, value string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeExecutable(t *testing.T, path, value string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value), 0o700); err != nil {
		t.Fatal(err)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	return string(data)
}
