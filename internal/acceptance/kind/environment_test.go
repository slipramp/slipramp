package kindacceptance

import (
	"bufio"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func (s *suite) provision(t *testing.T) {
	t.Helper()

	s.requireCommands(t)
	s.buildImagesAndCLI(t)
	s.createCluster(t)
	s.createFixture(t)
	s.deployDependencies(t)
	s.deployFixtures(t)
	s.installSlipramp(t)
	s.startForward(t, "server", "slipramp-server", s.serverPort, 8080)
	s.startForward(t, "web", "slipramp-web", s.webPort, 3000)
	s.startForward(t, "oidc", "acceptance-dex", s.oidcPort, 5556)
	s.authenticate(t)
	s.registerRepository(t)
}

func (s *suite) requireCommands(t *testing.T) {
	t.Helper()

	for _, command := range []string{"docker", "git", "go", "ssh-keygen", s.kubectlBin} {
		if _, err := exec.LookPath(command); err != nil {
			t.Fatalf("required command %q is not available: %v", command, err)
		}
	}
	for _, path := range []string{s.kindBin, s.helmBin} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("required binary %q is not available: %v", path, err)
		}
		if info.Mode()&0o111 == 0 {
			t.Fatalf("required binary %q is not executable", path)
		}
	}

	clusters := string(s.mustRun(t, s.kindBin, "get", "clusters"))
	for cluster := range strings.FieldsSeq(clusters) {
		if cluster == s.clusterName {
			t.Fatalf("kind cluster already exists: %s", s.clusterName)
		}
	}
}

func (s *suite) buildImagesAndCLI(t *testing.T) {
	t.Helper()

	t.Log("building the Slipramp CLI")
	s.mustRunVisible(t, "go", "-C", s.rootDirectory, "build", "-trimpath", "-o", s.cliBin, "./cmd/slipramp")
	version := strings.TrimSpace(string(s.mustRun(t, s.cliBin, "version")))
	if !slices.Contains(strings.Split(version, "\n"), "slipramp dev") {
		t.Fatalf("CLI version output omitted %q: %q", "slipramp dev", version)
	}

	t.Log("building Slipramp acceptance images")
	for _, target := range []string{"acceptance-git", "checkout", "controller", "harness", "kargo-tools", "server", "web"} {
		s.mustRunVisible(t, "docker", "build",
			"--target", target,
			"--tag", "slipramp-"+target+":acceptance",
			s.rootDirectory,
		)
	}

	t.Log("pulling acceptance dependency images")
	for _, image := range dependencyImages() {
		s.mustRunVisible(t, "docker", "pull", image)
	}
	s.mustRunVisible(t, "docker", "pull", dexImage)
	s.mustRun(t, "docker", "tag", dexImage, "slipramp-acceptance-dex:acceptance")
}

func dependencyImages() []string {
	return []string{
		"postgres:18.6-alpine3.24",
		"rustfs/rustfs:1.0.1",
		"rustfs/rc:v0.1.36",
	}
}

func (s *suite) createCluster(t *testing.T) {
	t.Helper()

	t.Logf("creating kind cluster %s", s.clusterName)
	s.mustRunVisible(t, s.kindBin,
		"create", "cluster",
		"--name", s.clusterName,
		"--image", kindNodeImage,
		"--wait", "2m",
	)
	s.clusterCreated = true

	images := []string{
		"slipramp-acceptance-dex:acceptance",
		"slipramp-acceptance-git:acceptance",
		"slipramp-checkout:acceptance",
		"slipramp-controller:acceptance",
		"slipramp-harness:acceptance",
		"slipramp-kargo-tools:acceptance",
		"slipramp-server:acceptance",
		"slipramp-web:acceptance",
	}
	images = append(images, dependencyImages()...)
	arguments := []string{"load", "docker-image", "--name", s.clusterName}
	arguments = append(arguments, images...)
	t.Log("loading images into kind")
	s.mustRunVisible(t, s.kindBin, arguments...)
}

func (s *suite) createFixture(t *testing.T) {
	t.Helper()

	t.Log("building the local Git fixture")
	s.fixtureDirectory = filepath.Join(s.temporaryDirectory, "repository")
	copyTree(t, filepath.Join(s.rootDirectory, "deploy/e2e/kind/repository"), s.fixtureDirectory)

	s.git(t, "init", "--quiet", "--initial-branch=main")
	s.git(t, "config", "user.email", "acceptance@slipramp.test")
	s.git(t, "config", "user.name", "Slipramp Acceptance")
	s.git(t, "add", ".")
	s.git(t, "commit", "--quiet", "-m", "Acceptance fixture")
	s.fixtureCommit = strings.TrimSpace(string(s.git(t, "rev-parse", "HEAD")))
	s.git(t, "branch", "stage/acceptance/acceptance")
	s.git(t, "branch", "kargo/promotion/acceptance-empty")
	s.git(t, "checkout", "--quiet", "-b", "kargo/promotion/acceptance")
	writeFile(t, filepath.Join(s.fixtureDirectory, "review/manifest.yaml"), []byte("image: acceptance:new\n"), 0o644)
	s.git(t, "commit", "--quiet", "-am", "Promotion candidate")
	s.candidateCommit = strings.TrimSpace(string(s.git(t, "rev-parse", "HEAD")))
	for _, branch := range []string{
		"kargo/promotion/acceptance-operator",
		"kargo/promotion/acceptance-automatic",
		"kargo/promotion/acceptance-guarded",
	} {
		s.git(t, "branch", branch)
	}
	s.git(t, "checkout", "--quiet", "main")
	s.git(t, "checkout", "--quiet", "-b", "acceptance/run-review-approved")
	writeFile(t, filepath.Join(s.fixtureDirectory, "scripts/review.txt"), []byte("approved\n"), 0o644)
	s.git(t, "commit", "--quiet", "-am", "Approved Run review fixture")
	s.approvedReviewCommit = strings.TrimSpace(string(s.git(t, "rev-parse", "HEAD")))
	s.git(t, "checkout", "--quiet", "-b", "acceptance/run-review-rejected")
	writeFile(t, filepath.Join(s.fixtureDirectory, "scripts/review.txt"), []byte("rejected\n"), 0o644)
	s.git(t, "commit", "--quiet", "-am", "Rejected Run review fixture")
	s.rejectedReviewCommit = strings.TrimSpace(string(s.git(t, "rev-parse", "HEAD")))
	s.git(t, "checkout", "--quiet", "main")
	s.git(t, "bundle", "create", filepath.Join(s.temporaryDirectory, "repository.bundle"), "--all")

	s.createGitCredentials(t)
}

func (s *suite) git(t *testing.T, args ...string) []byte {
	t.Helper()

	arguments := append([]string{"-C", s.fixtureDirectory}, args...)
	return s.mustRun(t, "git", arguments...)
}

func (s *suite) createGitCredentials(t *testing.T) {
	t.Helper()

	clientKey := filepath.Join(s.temporaryDirectory, "client-key")
	hostKey := filepath.Join(s.temporaryDirectory, "host-key")
	s.mustRun(t, "ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", "slipramp-acceptance-client", "-f", clientKey)
	s.mustRun(t, "ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", "slipramp-acceptance-host", "-f", hostKey)

	hostPublic := readFile(t, hostKey+".pub")
	fields := strings.Fields(string(hostPublic))
	if len(fields) < 2 {
		t.Fatalf("invalid generated host public key: %q", hostPublic)
	}
	gitHost := "acceptance-git.slipramp-system.svc.cluster.local"
	knownHosts := fmt.Sprintf("[%s]:2222 %s %s\n", gitHost, fields[0], fields[1])
	writeFile(t, filepath.Join(s.temporaryDirectory, "known_hosts"), []byte(knownHosts), 0o600)

	gitConfig := "[core]\n" +
		"    sshCommand = ssh -i /var/run/slipramp/repository-credentials/acceptance/ssh-privatekey" +
		" -o BatchMode=yes -o IdentitiesOnly=yes" +
		" -o UserKnownHostsFile=/var/run/slipramp/repository-credentials/acceptance/known_hosts" +
		" -o StrictHostKeyChecking=yes\n"
	writeFile(t, filepath.Join(s.temporaryDirectory, "gitconfig"), []byte(gitConfig), 0o600)

	sshdConfig := `Port 2222
ListenAddress 0.0.0.0
HostKey /etc/ssh/keys/ssh_host_ed25519_key
AuthorizedKeysFile .ssh/authorized_keys
PasswordAuthentication no
KbdInteractiveAuthentication no
PermitRootLogin no
AllowUsers git
AllowTcpForwarding no
X11Forwarding no
PidFile /tmp/sshd.pid
`
	writeFile(t, filepath.Join(s.temporaryDirectory, "sshd_config"), []byte(sshdConfig), 0o600)
}

func (s *suite) deployDependencies(t *testing.T) {
	t.Helper()

	t.Log("deploying PostgreSQL and RustFS")
	s.kubectl(t, "apply", "-f", filepath.Join(s.rootDirectory, "deploy/e2e/kind/dependencies.yaml"))
	for _, deployment := range []string{"postgres", "rustfs"} {
		s.kubectl(t, "-n", systemNamespace, "rollout", "status", "deployment/"+deployment, "--timeout="+s.timeout.String())
	}
	s.kubectl(t, "apply", "-f", filepath.Join(s.rootDirectory, "deploy/e2e/kind/rustfs-init.yaml"))
	s.kubectl(t, "-n", systemNamespace, "wait", "--for=condition=complete", "job/rustfs-init", "--timeout="+s.timeout.String())
}

func (s *suite) deployFixtures(t *testing.T) {
	t.Helper()

	t.Log("deploying the acceptance fixtures")
	s.kubectl(t, "apply", "-f", filepath.Join(s.rootDirectory, "deploy/e2e/kind/kargo-api.yaml"))
	s.kubectl(t,
		"wait", "--for=condition=Established",
		"crd/stages.kargo.akuity.io", "crd/promotions.kargo.akuity.io",
		"--timeout="+s.timeout.String(),
	)
	s.kubectl(t, "apply", "-f", filepath.Join(s.rootDirectory, "deploy/e2e/kind/kargo-stage.yaml"))

	s.createConfigMap(t, systemNamespace, "acceptance-repository",
		"repository.bundle="+filepath.Join(s.temporaryDirectory, "repository.bundle"))
	s.createConfigMap(t, systemNamespace, "acceptance-git-client-public",
		"authorized_key="+filepath.Join(s.temporaryDirectory, "client-key.pub"))
	s.createConfigMap(t, systemNamespace, "acceptance-git-sshd",
		"sshd_config="+filepath.Join(s.temporaryDirectory, "sshd_config"))
	s.createSecretFiles(t, systemNamespace, "acceptance-git-host-keys",
		"ssh_host_ed25519_key="+filepath.Join(s.temporaryDirectory, "host-key"),
		"ssh_host_ed25519_key.pub="+filepath.Join(s.temporaryDirectory, "host-key.pub"))
	s.createSecretFiles(t, systemNamespace, "acceptance-git-client",
		"gitconfig="+filepath.Join(s.temporaryDirectory, "gitconfig"),
		"ssh-privatekey="+filepath.Join(s.temporaryDirectory, "client-key"),
		"known_hosts="+filepath.Join(s.temporaryDirectory, "known_hosts"))
	s.createSecretFiles(t, runNamespace, "acceptance-git-client",
		"ssh-privatekey="+filepath.Join(s.temporaryDirectory, "client-key"),
		"known_hosts="+filepath.Join(s.temporaryDirectory, "known_hosts"))
	s.createSecretLiterals(t, runNamespace, "acceptance-argocd",
		`targets.json={"acceptance":{"server":"acceptance-argocd","authToken":"acceptance-argo-token"}}`)

	s.kubectl(t, "apply", "-f", filepath.Join(s.rootDirectory, "deploy/e2e/kind/platform.yaml"))
	s.kubectl(t, "-n", systemNamespace, "set", "env", "deployment/acceptance-dex",
		"SLIPRAMP_ACCEPTANCE_OIDC_REDIRECT_URL="+s.webURL+"/api/v1/auth/callback")
	for _, deployment := range []string{"acceptance-git", "acceptance-dex"} {
		s.kubectl(t, "-n", systemNamespace, "rollout", "status", "deployment/"+deployment, "--timeout="+s.timeout.String())
	}
}

func (s *suite) installSlipramp(t *testing.T) {
	t.Helper()

	t.Log("installing Slipramp through the supported Helm charts")
	s.createSecretLiterals(t, systemNamespace, "slipramp-server-secrets",
		"database-url=postgres://slipramp:slipramp-acceptance@postgres:5432/slipramp?sslmode=disable",
		"s3-access-key-id=slipramp",
		"s3-secret-access-key=slipramp-acceptance",
		`controller-enrollment-tokens={"kind":"acceptance-enrollment-token"}`,
		"artifact-token-secret=acceptance-artifact-token-secret-32",
		"oidc-client-secret=acceptance-client-secret")
	s.createSecretLiterals(t, systemNamespace, "slipramp-controller-credentials",
		"enrollment-token=acceptance-enrollment-token",
		"controller-token=acceptance-controller-token")

	s.mustRunVisible(t, s.helmBin,
		"upgrade", "--install", "slipramp",
		filepath.Join(s.rootDirectory, "deploy/charts/slipramp"),
		"--kube-context", "kind-"+s.clusterName,
		"--namespace", systemNamespace,
		"--values", filepath.Join(s.rootDirectory, "deploy/e2e/kind/slipramp-values.yaml"),
		"--set-string", "server.oidc.redirectURL="+s.webURL+"/api/v1/auth/callback",
		"--wait", "--timeout", s.timeout.String(),
	)
	s.mustRunVisible(t, s.helmBin,
		"upgrade", "--install", "slipramp-controller",
		filepath.Join(s.rootDirectory, "deploy/charts/slipramp-controller"),
		"--kube-context", "kind-"+s.clusterName,
		"--namespace", systemNamespace,
		"--values", filepath.Join(s.rootDirectory, "deploy/e2e/kind/controller-values.yaml"),
		"--wait", "--timeout", s.timeout.String(),
	)
	for _, deployment := range []string{"slipramp-server", "slipramp-web", "slipramp-controller"} {
		s.kubectl(t, "-n", systemNamespace, "rollout", "status", "deployment/"+deployment, "--timeout="+s.timeout.String())
	}
}

func (s *suite) createConfigMap(t *testing.T, namespace, name string, files ...string) {
	t.Helper()

	args := []string{"-n", namespace, "create", "configmap", name}
	for _, file := range files {
		args = append(args, "--from-file="+file)
	}
	args = append(args, "--dry-run=client", "-o", "yaml")
	s.kubectlApplyInput(t, s.kubectl(t, args...))
}

func (s *suite) createSecretFiles(t *testing.T, namespace, name string, files ...string) {
	t.Helper()

	args := []string{"-n", namespace, "create", "secret", "generic", name}
	for _, file := range files {
		args = append(args, "--from-file="+file)
	}
	args = append(args, "--dry-run=client", "-o", "yaml")
	s.kubectlApplyInput(t, s.kubectl(t, args...))
}

func (s *suite) createSecretLiterals(t *testing.T, namespace, name string, literals ...string) {
	t.Helper()

	args := []string{"-n", namespace, "create", "secret", "generic", name}
	for _, literal := range literals {
		args = append(args, "--from-literal="+literal)
	}
	args = append(args, "--dry-run=client", "-o", "yaml")
	s.kubectlApplyInput(t, s.kubectl(t, args...))
}

func (s *suite) cleanup(t *testing.T) {
	for name := range s.forwards {
		s.stopForward(t, name)
	}
	s.cancelProcesses()
	if !s.clusterCreated {
		return
	}
	if t.Failed() {
		s.dumpDiagnostics(t)
	}
	if s.keepCluster {
		t.Logf("keeping kind cluster %s with context kind-%s", s.clusterName, s.clusterName)
		return
	}

	t.Logf("deleting kind cluster %s", s.clusterName)
	context, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(context, s.kindBin, "delete", "cluster", "--name", s.clusterName)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		t.Errorf("delete kind cluster: %v", err)
	}
}

func (s *suite) dumpDiagnostics(t *testing.T) {
	t.Log("acceptance diagnostics")
	s.bestEffort(t, s.kubectlBin, append(s.kubectlArguments, "get", "pods,jobs", "-A", "-o", "wide")...)
	s.bestEffort(t, s.kubectlBin, append(s.kubectlArguments, "-n", systemNamespace, "describe", "pods")...)
	for _, deployment := range []string{
		"postgres", "rustfs", "acceptance-git", "acceptance-dex", "slipramp-server", "slipramp-web", "slipramp-controller",
	} {
		t.Logf("logs: deployment/%s", deployment)
		s.bestEffort(t, s.kubectlBin, append(s.kubectlArguments,
			"-n", systemNamespace, "logs", "deployment/"+deployment, "--all-containers", "--tail=-1")...)
	}

	result := s.bestEffort(t, s.kubectlBin, append(s.kubectlArguments,
		"-n", runNamespace, "get", "pods", "-o", "name")...)
	scanner := bufio.NewScanner(strings.NewReader(string(result)))
	for scanner.Scan() {
		pod := strings.TrimSpace(scanner.Text())
		if pod == "" {
			continue
		}
		t.Logf("logs: %s", pod)
		s.bestEffort(t, s.kubectlBin, append(s.kubectlArguments,
			"-n", runNamespace, "logs", pod, "--all-containers", "--tail=-1")...)
	}
}

func (s *suite) bestEffort(t *testing.T, name string, args ...string) []byte {
	t.Helper()

	context, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(context, name, args...)
	command.Dir = s.rootDirectory
	output, err := command.CombinedOutput()
	if len(output) > 0 {
		_, _ = os.Stderr.Write(output)
	}
	if err != nil {
		t.Logf("diagnostic command %s failed: %v", name, err)
	}
	return output
}

func copyTree(t *testing.T, source, destination string) {
	t.Helper()

	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, contents, info.Mode().Perm())
	})
	if err != nil {
		t.Fatalf("copy acceptance fixture: %v", err)
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return contents
}

func writeFile(t *testing.T, path string, contents []byte, mode fs.FileMode) {
	t.Helper()

	if err := os.WriteFile(path, contents, mode); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
