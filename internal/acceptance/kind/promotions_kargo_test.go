package kindacceptance

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/api"
)

func (s *suite) testKargoIntegration(t *testing.T) {
	t.Log("configuring Kargo credentials")
	kargoPrincipalID := s.createServiceAccount(t, "Kargo acceptance", "operator")
	kargoAPIKey := s.createServiceAccountAPIKey(t, kargoPrincipalID, "Kargo acceptance",
		[]string{"slipramp:promotions:submit"})
	s.applyKargoGitCredentials(t)
	s.applyKargoCallbackSecret(t, kargoAPIKey)
	s.kubectl(t, "apply", "-f", filepath.Join(s.rootDirectory, "deploy/e2e/kind/kargo-integration.yaml"))

	stage, freightName := s.waitForKargoFreight(t)
	reason := "Kargo acceptance review"
	policyResponse, err := s.productClient.UpdatePromotionStagePolicyWithResponse(
		t.Context(),
		stage.ID,
		api.PromotionStagePolicyUpdate{Mode: api.ReviewRequired, Reason: &reason},
	)
	if err != nil {
		t.Fatalf("require review for Kargo Stage: %v", err)
	}
	requireStatus(t, policyResponse.StatusCode(), policyResponse.Body, http.StatusOK)

	t.Logf("starting a Kargo Promotion with Freight %s", freightName)
	creationResponse, err := s.productClient.CreatePromotionWithResponse(t.Context(), stage.ID, api.PromotionCreate{
		FreightName: freightName,
	})
	if err != nil {
		t.Fatalf("create Kargo Promotion: %v", err)
	}
	requireStatus(t, creationResponse.StatusCode(), creationResponse.Body, http.StatusCreated)
	if creationResponse.JSON201 == nil {
		t.Fatal("Kargo Promotion creation returned no Promotion")
	}
	promotion := *creationResponse.JSON201
	if promotion.Source != api.Slipramp || promotion.Status != api.PromotionStatusCreating ||
		promotion.SelectedFreight == nil || promotion.SelectedFreight.Name != freightName {
		t.Fatalf("Kargo Promotion creation = %#v", promotion)
	}

	dispatched := s.waitForOperatorPromotionDispatch(t, promotion.ID)
	if dispatched.KargoPromotionUID == nil {
		t.Fatalf("Kargo Promotion has no Kargo UID: %#v", dispatched)
	}

	pending := s.waitForKargoCallbackAndPendingPromotion(t, promotion.ID)
	if pending.BaseCommit == nil || pending.BaseCommit.Sha != s.baseCommit || pending.CandidateCommit == nil ||
		pending.CandidateCommit.Sha == s.baseCommit || pending.CandidateBranch == nil ||
		!strings.HasPrefix(*pending.CandidateBranch, "kargo/promotion/") || pending.PreparationRunID == nil ||
		pending.GitDiffArtifactID == nil || pending.ArgoDiffArtifactID == nil {
		t.Fatalf("Kargo Promotion pending state = %#v", pending)
	}

	comment := "Approved by Kargo acceptance"
	decisionResponse, err := s.productClient.DecidePromotionWithResponse(t.Context(), promotion.ID,
		api.PromotionDecisionUpdate{
			Decision: api.PromotionDecisionUpdateDecisionApproved,
			Comment:  &comment,
		})
	if err != nil {
		t.Fatalf("approve Kargo Promotion: %v", err)
	}
	requireStatus(t, decisionResponse.StatusCode(), decisionResponse.Body, http.StatusOK)
	if decisionResponse.JSON200 == nil || decisionResponse.JSON200.Status != api.PromotionStatusApproved {
		t.Fatalf("approved Kargo Promotion = %#v", decisionResponse.JSON200)
	}

	s.waitForKargoPromotionSucceeded(t, pending.ExternalPromotionID)
	targetCommit := strings.TrimSpace(string(s.kubectl(t, "-n", systemNamespace, "exec",
		"deployment/acceptance-git", "-c", "git", "--",
		"git", "--git-dir=/srv/repository.git", "rev-parse", "refs/heads/stage/acceptance/acceptance")))
	if targetCommit != pending.CandidateCommit.Sha {
		t.Fatalf("target branch commit = %s, want candidate %s", targetCommit, pending.CandidateCommit.Sha)
	}
	targetManifest := string(s.kubectl(t, "-n", systemNamespace, "exec",
		"deployment/acceptance-git", "-c", "git", "--",
		"git", "--git-dir=/srv/repository.git", "show",
		"refs/heads/stage/acceptance/acceptance:review/manifest.yaml"))
	if targetManifest != "image: acceptance:new\n" {
		t.Fatalf("promoted target manifest = %q", targetManifest)
	}

	final := s.getPromotion(t, promotion.ID)
	if final.Status != api.PromotionStatusApproved || final.Decision == nil ||
		final.Decision.Decision != api.PromotionDecisionDecisionApproved || final.KargoPromotionUID == nil ||
		*final.KargoPromotionUID != *dispatched.KargoPromotionUID {
		t.Fatalf("final Kargo Promotion = %#v", final)
	}
	t.Logf("Kargo Promotion %s pushed candidate %s to its target branch",
		pending.ExternalPromotionID, targetCommit)
}

func (s *suite) applyKargoGitCredentials(t *testing.T) {
	t.Helper()

	repositoryURL := "ssh://git@acceptance-git.slipramp-system.svc.cluster.local:2222/srv/repository.git"
	privateKey := readFile(t, filepath.Join(s.temporaryDirectory, "client-key"))
	manifest, err := json.Marshal(map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]any{
			"name":      "acceptance-git",
			"namespace": "kargo-acceptance",
			"labels": map[string]string{
				"kargo.akuity.io/cred-type": "git",
			},
		},
		"type": "Opaque",
		"data": map[string]string{
			"repoURL":       base64.StdEncoding.EncodeToString([]byte(repositoryURL)),
			"sshPrivateKey": base64.StdEncoding.EncodeToString(privateKey),
		},
	})
	if err != nil {
		t.Fatalf("encode Kargo Git credentials: %v", err)
	}
	s.kubectlApplyInput(t, manifest)
}

func (s *suite) applyKargoCallbackSecret(t *testing.T, token string) {
	t.Helper()

	manifest, err := json.Marshal(map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]any{
			"name":      "slipramp-promotions",
			"namespace": "kargo-acceptance",
			"labels": map[string]string{
				"kargo.akuity.io/cred-type": "generic",
			},
		},
		"type": "Opaque",
		"data": map[string]string{
			"token": base64.StdEncoding.EncodeToString([]byte(token)),
		},
	})
	if err != nil {
		t.Fatalf("encode Kargo callback Secret: %v", err)
	}
	s.kubectlApplyInput(t, manifest)
}

func (s *suite) waitForKargoFreight(t *testing.T) (api.PromotionPipelineStage, string) {
	t.Helper()

	integration := api.CatalogName("acceptance")
	deadline := time.Now().Add(s.timeout)
	var lastBody []byte
	for time.Now().Before(deadline) {
		response, err := s.productClient.ListPromotionPipelinesWithResponse(t.Context(), &api.ListPromotionPipelinesParams{
			Integration: &integration,
		})
		if err == nil {
			lastBody = response.Body
			if response.JSON200 != nil {
				for _, pipeline := range response.JSON200.Items {
					if pipeline.Project != "kargo-acceptance" {
						continue
					}
					for _, stage := range pipeline.Stages {
						if stage.Name != "acceptance" {
							continue
						}
						for _, freight := range stage.AvailableFreight.Items {
							if freight.Origin != "Warehouse/acceptance" || !slices.ContainsFunc(
								freight.Artifacts,
								func(artifact api.PromotionFreightArtifact) bool {
									return artifact.Kind == api.PromotionFreightArtifactKindGit &&
										artifact.Version == s.fixtureCommit
								},
							) {
								continue
							}
							return stage, freight.Name
						}
					}
				}
			}
		}
		wait(t, time.Second)
	}
	t.Fatalf("Kargo Freight was not observed: %s", lastBody)
	return api.PromotionPipelineStage{}, ""
}

func (s *suite) waitForKargoCallbackAndPendingPromotion(t *testing.T, promotionID uuid.UUID) api.Promotion {
	t.Helper()

	deadline := time.Now().Add(s.timeout)
	var last api.Promotion
	for time.Now().Before(deadline) {
		response, err := s.productClient.GetPromotionWithResponse(t.Context(), promotionID)
		if err == nil && response.JSON200 != nil {
			last = *response.JSON200
			switch last.Status {
			case api.PromotionStatusPending:
				return last
			case api.PromotionStatusCreating, api.PromotionStatusPreparing:
			default:
				t.Fatalf("Kargo Promotion %s reached %s instead of pending: %#v", promotionID, last.Status, last)
			}
		}
		wait(t, time.Second)
	}
	t.Fatalf("Kargo Promotion %s did not reach pending: %#v", promotionID, last)
	return api.Promotion{}
}

func (s *suite) waitForKargoPromotionSucceeded(t *testing.T, name string) {
	t.Helper()

	deadline := time.Now().Add(s.timeout)
	var lastPhase string
	var lastMessage string
	for time.Now().Before(deadline) {
		result := s.kubectlResult(t, "-n", "kargo-acceptance", "get", "promotion", name,
			"-o", "jsonpath={.status.phase}{'\\n'}{.status.message}")
		if result.exitCode == 0 {
			parts := strings.SplitN(string(result.stdout), "\n", 2)
			lastPhase = strings.TrimSpace(parts[0])
			if len(parts) == 2 {
				lastMessage = strings.TrimSpace(parts[1])
			}
			if lastPhase == "Succeeded" {
				return
			}
			if slices.Contains([]string{"Failed", "Errored", "Aborted"}, lastPhase) {
				t.Fatalf("Kargo Promotion %s reached %s: %s", name, lastPhase, lastMessage)
			}
		}
		wait(t, time.Second)
	}
	t.Fatalf("Kargo Promotion %s did not succeed; last phase %q: %s", name, lastPhase, lastMessage)
}
