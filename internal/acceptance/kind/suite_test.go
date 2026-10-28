package kindacceptance

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/api"
)

const (
	acceptanceEnvironment = "SLIPRAMP_RUN_KIND_ACCEPTANCE"
	kindNodeImage         = "kindest/node:v1.37.0@sha256:a1ed56cfb0e7b93589bdf97c8cd566405a265939e3620fc4f5de89adff580ae5"
	dexImage              = "ghcr.io/dexidp/dex:v2.45.1@sha256:8499afd690c437f52301efd2b05b2455da5bd2dfc20332cd697dc9937f808462"
	systemNamespace       = "slipramp-system"
	runNamespace          = "slipramp-runs"
)

type suite struct {
	rootDirectory      string
	temporaryDirectory string
	kindBin            string
	helmBin            string
	kubectlBin         string
	clusterName        string
	serverPort         int
	webPort            int
	oidcPort           int
	keepCluster        bool
	timeout            time.Duration
	serverURL          string
	webURL             string
	oidcHost           string
	kubectlArguments   []string
	cliBin             string
	processContext     context.Context
	cancelProcesses    context.CancelFunc

	clusterCreated bool
	forwards       map[string]*forward

	fixtureDirectory      string
	fixtureCommit         string
	candidateCommit       string
	approvedReviewCommit  string
	rejectedReviewCommit  string
	productAPIKey         string
	productClient         *api.ClientWithResponses
	sessionHTTPClient     *http.Client
	repositoryID          uuid.UUID
	catalogRevision       api.CatalogRevision
	targetID              uuid.UUID
	previewRunID          uuid.UUID
	applyRunID            uuid.UUID
	rerunID               uuid.UUID
	canceledRunID         uuid.UUID
	controllerRecoveryRun uuid.UUID
	serverRecoveryRun     uuid.UUID
	rustFSRecoveryRun     uuid.UUID
	rustFSCancellationRun uuid.UUID
	postgresRecoveryRun   uuid.UUID
	postgresQueuedRun     uuid.UUID
	approvedReviewRun     uuid.UUID
	rejectedReviewRun     uuid.UUID
}

type scenario struct {
	name string
	run  func(*testing.T)
}

func TestKindAcceptance(t *testing.T) {
	if os.Getenv(acceptanceEnvironment) != "1" {
		t.Skipf("set %s=1 to run the disposable kind acceptance suite", acceptanceEnvironment)
	}

	s := newSuite(t)
	t.Cleanup(func() { s.cleanup(t) })
	s.provision(t)

	scenarios := []scenario{
		{name: "operator CLI", run: s.testOperatorCLI},
		{name: "cancellation", run: s.testCancellation},
		{name: "Controller restart recovery", run: s.testControllerRestartRecovery},
		{name: "Server restart recovery", run: s.testServerRestartRecovery},
		{name: "RustFS outage recovery", run: s.testRustFSOutageRecovery},
		{name: "cancellation during RustFS outage", run: s.testCancellationDuringRustFSOutage},
		{name: "PostgreSQL outage recovery", run: s.testPostgresOutageRecovery},
		{name: "Kargo Promotions", run: s.testKargoPromotions},
		{name: "Run reviews", run: s.testRunReviews},
		{name: "operational Inbox", run: s.testInbox},
	}
	for _, scenario := range scenarios {
		if !t.Run(scenario.name, scenario.run) {
			return
		}
	}

	t.Log("kind acceptance passed")
	t.Logf("Repository commit:       %s", s.fixtureCommit)
	t.Logf("Preview Run:             %s", s.previewRunID)
	t.Logf("Apply Run:               %s", s.applyRunID)
	t.Logf("CLI rerun:               %s", s.rerunID)
	t.Logf("Canceled Run:            %s", s.canceledRunID)
	t.Logf("Controller recovery Run: %s", s.controllerRecoveryRun)
	t.Logf("Server recovery Run:     %s", s.serverRecoveryRun)
	t.Logf("RustFS recovery Run:     %s", s.rustFSRecoveryRun)
	t.Logf("RustFS cancellation Run: %s", s.rustFSCancellationRun)
	t.Logf("PostgreSQL recovery Run: %s", s.postgresRecoveryRun)
	t.Logf("PostgreSQL queued Run:   %s", s.postgresQueuedRun)
	t.Logf("Approved review Run:     %s", s.approvedReviewRun)
	t.Logf("Rejected review Run:     %s", s.rejectedReviewRun)
}

func newSuite(t *testing.T) *suite {
	t.Helper()

	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate acceptance suite source")
	}
	rootDirectory, err := filepath.Abs(filepath.Join(filepath.Dir(sourceFile), "../../.."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}

	temporaryDirectory := t.TempDir()
	serverPort := environmentInt(t, "SLIPRAMP_ACCEPTANCE_SERVER_PORT", 18080)
	webPort := environmentInt(t, "SLIPRAMP_ACCEPTANCE_WEB_PORT", 13000)
	oidcPort := environmentInt(t, "SLIPRAMP_ACCEPTANCE_OIDC_PORT", 18081)
	timeoutSeconds := environmentInt(t, "SLIPRAMP_ACCEPTANCE_TIMEOUT_SECONDS", 180)
	clusterName := environmentString("SLIPRAMP_ACCEPTANCE_CLUSTER_NAME", "slipramp-acceptance")
	suiteContext, cancel := context.WithCancel(context.Background())

	return &suite{
		rootDirectory:      rootDirectory,
		temporaryDirectory: temporaryDirectory,
		kindBin:            environmentString("SLIPRAMP_KIND_BIN", filepath.Join(rootDirectory, ".bin/kind")),
		helmBin:            environmentString("SLIPRAMP_HELM_BIN", filepath.Join(rootDirectory, ".bin/helm")),
		kubectlBin:         environmentString("SLIPRAMP_KUBECTL_BIN", "kubectl"),
		clusterName:        clusterName,
		serverPort:         serverPort,
		webPort:            webPort,
		oidcPort:           oidcPort,
		keepCluster:        environmentString("SLIPRAMP_ACCEPTANCE_KEEP_CLUSTER", "false") == "true",
		timeout:            time.Duration(timeoutSeconds) * time.Second,
		serverURL:          "http://127.0.0.1:" + strconv.Itoa(serverPort),
		webURL:             "http://127.0.0.1:" + strconv.Itoa(webPort),
		oidcHost:           "acceptance-dex.slipramp-system.svc.cluster.local",
		kubectlArguments:   []string{"--context", "kind-" + clusterName},
		cliBin:             filepath.Join(temporaryDirectory, "slipramp"),
		processContext:     suiteContext,
		cancelProcesses:    cancel,
		forwards:           make(map[string]*forward),
	}
}

func environmentString(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func environmentInt(t *testing.T, name string, fallback int) int {
	t.Helper()

	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		t.Fatalf("%s must be a positive integer, got %q", name, value)
	}
	return parsed
}
