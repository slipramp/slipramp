package kindacceptance

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/api"
)

func (s *suite) testKargoPromotions(t *testing.T) {
	kargoTargetID := s.findTargetID(t, "kargo-promotions")
	t.Log("configuring the Kargo acceptance service account")
	kargoPrincipalID := s.createServiceAccount(t, "Kargo acceptance", "operator")
	kargoAPIKey := s.createServiceAccountAPIKey(t, kargoPrincipalID, "Kargo acceptance",
		[]string{"slipramp:promotions:submit"})
	kargoClient := s.newAPIClient(t, kargoAPIKey)
	kargoStage := s.waitForKargoStage(t)

	t.Log("testing operator-started Promotion dispatch and callback hydration")
	creationResponse, err := s.productClient.CreatePromotionWithResponse(t.Context(), kargoStage.ID, api.PromotionCreate{
		SourceStageID: new(kargoStage.ID),
		FreightName:   "acceptance-freight",
	})
	if err != nil {
		t.Fatalf("create operator Promotion: %v", err)
	}
	requireStatus(t, creationResponse.StatusCode(), creationResponse.Body, http.StatusCreated)
	if creationResponse.JSON201 == nil {
		t.Fatal("operator Promotion creation returned no Promotion")
	}
	operator := *creationResponse.JSON201
	if operator.Source != api.Slipramp || operator.Status != api.PromotionStatusCreating ||
		operator.StageID != kargoStage.ID || operator.SourceStageID == nil || *operator.SourceStageID != kargoStage.ID ||
		operator.SelectedFreight == nil || operator.SelectedFreight.Name != "acceptance-freight" ||
		operator.SelectedFreight.Origin != "Warehouse/acceptance" {
		t.Fatalf("operator Promotion creation = %#v", operator)
	}
	dispatched := s.waitForOperatorPromotionDispatch(t, operator.ID)
	if dispatched.KargoPromotionUID == nil {
		t.Fatalf("dispatched operator Promotion has no Kargo UID: %#v", dispatched)
	}

	kargoResource := s.kubectl(t, "-n", "kargo-acceptance", "get", "promotion", operator.ExternalPromotionID, "-o", "json")
	var resource struct {
		Metadata struct {
			UID         string            `json:"uid"`
			Annotations map[string]string `json:"annotations"`
		} `json:"metadata"`
		Spec struct {
			Stage   string `json:"stage"`
			Freight string `json:"freight"`
		} `json:"spec"`
	}
	decodeJSON(t, kargoResource, &resource)
	if resource.Metadata.UID != *dispatched.KargoPromotionUID ||
		resource.Metadata.Annotations["slipramp.com/promotion-id"] != operator.ID.String() ||
		resource.Spec.Stage != "acceptance" || resource.Spec.Freight != "acceptance-freight" {
		t.Fatalf("materialized Kargo Promotion = %#v", resource)
	}

	operatorCallback := api.KargoPromotionRequest{
		ExternalPromotionID: operator.ExternalPromotionID,
		Project:             "kargo-acceptance",
		Stage:               "acceptance",
		BaseRevision:        s.fixtureCommit,
		CandidateRevision:   s.candidateCommit,
		CandidateBranch:     "kargo/promotion/acceptance-operator",
	}
	hydrated := submitKargoPromotion(t, kargoClient, operatorCallback)
	if hydrated.ID != operator.ID || hydrated.Status != api.PromotionStatusApproved || hydrated.ReviewRequired {
		t.Fatalf("hydrated operator Promotion = %#v", hydrated)
	}
	operatorRead := s.getPromotion(t, operator.ID)
	if operatorRead.Source != api.Slipramp || operatorRead.Status != api.PromotionStatusApproved ||
		operatorRead.RequestedBy.PrincipalID != operator.RequestedBy.PrincipalID || operatorRead.CandidateCommit == nil ||
		operatorRead.DispatchedAt == nil || operatorRead.KargoPromotionUID == nil {
		t.Fatalf("hydrated operator Promotion read = %#v", operatorRead)
	}
	operatorRetry := submitKargoPromotion(t, kargoClient, operatorCallback)
	if operatorRetry.ID != operator.ID || operatorRetry.Status != api.PromotionStatusApproved {
		t.Fatalf("operator callback retry = %#v", operatorRetry)
	}

	t.Log("testing automatic Promotion approval without a preparation Run")
	automaticRequest := api.KargoPromotionRequest{
		ExternalPromotionID: "acceptance-automatic",
		Project:             "kargo-acceptance",
		Stage:               "acceptance",
		BaseRevision:        s.fixtureCommit,
		CandidateRevision:   s.candidateCommit,
		CandidateBranch:     "kargo/promotion/acceptance-automatic",
	}
	automatic := submitKargoPromotion(t, kargoClient, automaticRequest)
	if automatic.Status != api.PromotionStatusApproved || automatic.ReviewRequired {
		t.Fatalf("automatic Promotion = %#v", automatic)
	}
	automaticRead := s.getPromotion(t, automatic.ID)
	if automaticRead.Status != api.PromotionStatusApproved || automaticRead.PreparationRunID != nil {
		t.Fatalf("automatic Promotion read = %#v", automaticRead)
	}
	if runs := s.listRunsForTarget(t, kargoTargetID); len(runs) != 0 {
		t.Fatalf("automatic Promotion created preparation Runs: %#v", runs)
	}
	automaticRetry := submitKargoPromotion(t, kargoClient, automaticRequest)
	if automaticRetry.ID != automatic.ID || automaticRetry.Status != api.PromotionStatusApproved {
		t.Fatalf("automatic Promotion retry = %#v", automaticRetry)
	}

	t.Log("testing guarded Promotion execution and real artifact uploads")
	reason := "Acceptance review preparation"
	policyResponse, err := s.productClient.UpdatePromotionStagePolicyWithResponse(
		t.Context(),
		kargoStage.ID,
		api.PromotionStagePolicyUpdate{Mode: api.ReviewRequired, Reason: &reason},
	)
	if err != nil {
		t.Fatalf("update Kargo Stage policy: %v", err)
	}
	requireStatus(t, policyResponse.StatusCode(), policyResponse.Body, http.StatusOK)
	if policyResponse.JSON200 == nil || policyResponse.JSON200.EffectiveMode != api.ReviewRequired {
		t.Fatalf("updated Kargo Stage policy = %#v", policyResponse.JSON200)
	}

	s.checkGuardedPromotion(t, kargoClient, kargoPrincipalID, kargoTargetID,
		"acceptance-guarded", s.candidateCommit, "kargo/promotion/acceptance-guarded")
	s.checkGuardedPromotion(t, kargoClient, kargoPrincipalID, kargoTargetID,
		"acceptance-empty", s.fixtureCommit, "kargo/promotion/acceptance-empty")

	runs := s.listRunsForTarget(t, kargoTargetID)
	if len(runs) != 2 || slices.ContainsFunc(runs, func(run api.Run) bool {
		return run.Source.Promotion != nil && run.Source.Promotion.PromotionID == automatic.ID
	}) {
		t.Fatalf("guarded Promotion preparation Runs = %#v", runs)
	}

	s.waitForCandidateBranchCleanup(t,
		"kargo/promotion/acceptance-automatic",
		"kargo/promotion/acceptance-guarded")
	t.Log("candidate branch cleanup deleted expired exact refs and retained protected and untracked refs")
}

func (s *suite) findTargetID(t *testing.T, name string) uuid.UUID {
	t.Helper()

	limit := 100
	response, err := s.productClient.ListTargetsWithResponse(t.Context(), &api.ListTargetsParams{
		RepositoryID: &s.repositoryID,
		Limit:        &limit,
	})
	if err != nil {
		t.Fatalf("list acceptance Targets: %v", err)
	}
	requireStatus(t, response.StatusCode(), response.Body, http.StatusOK)
	if response.JSON200 != nil {
		for _, target := range response.JSON200.Items {
			if target.Name == name {
				return target.ID
			}
		}
	}
	t.Fatalf("Target %q was not found: %s", name, response.Body)
	return uuid.Nil
}

func (s *suite) waitForKargoStage(t *testing.T) api.PromotionStage {
	t.Helper()

	integration := api.CatalogName("acceptance")
	deadline := time.Now().Add(s.timeout)
	var lastBody []byte
	for time.Now().Before(deadline) {
		response, err := s.productClient.ListPromotionStagesWithResponse(t.Context(), &api.ListPromotionStagesParams{
			Integration: &integration,
		})
		if err == nil {
			lastBody = response.Body
			if response.JSON200 != nil {
				for _, stage := range response.JSON200.Items {
					if stage.Name == "acceptance" && stage.Project == "kargo-acceptance" &&
						slices.ContainsFunc(stage.Freight.Items, func(freight api.PromotionFreightItem) bool {
							return freight.Name == "acceptance-freight"
						}) {
						return stage
					}
				}
			}
		}
		wait(t, time.Second)
	}
	t.Fatalf("Kargo acceptance Stage was not observed: %s", lastBody)
	return api.PromotionStage{}
}

func (s *suite) waitForOperatorPromotionDispatch(t *testing.T, promotionID uuid.UUID) api.Promotion {
	t.Helper()

	deadline := time.Now().Add(s.timeout)
	var last api.Promotion
	for time.Now().Before(deadline) {
		response, err := s.productClient.GetPromotionWithResponse(t.Context(), promotionID)
		if err == nil && response.JSON200 != nil {
			last = *response.JSON200
			if last.Source == api.Slipramp && last.Status == api.PromotionStatusCreating &&
				last.DispatchAttemptCount >= 1 && last.DispatchedAt != nil && last.KargoPromotionUID != nil {
				return last
			}
			if last.Status != api.PromotionStatusCreating {
				t.Fatalf("operator Promotion %s left creating before its callback: %#v", promotionID, last)
			}
		}
		wait(t, time.Second)
	}
	t.Fatalf("operator Promotion %s was not dispatched: %#v", promotionID, last)
	return api.Promotion{}
}

func submitKargoPromotion(
	t *testing.T,
	client *api.ClientWithResponses,
	request api.KargoPromotionRequest,
) api.KargoPromotionResponse {
	t.Helper()

	response, err := client.SubmitKargoPromotionWithResponse(t.Context(), "acceptance", request)
	if err != nil {
		t.Fatalf("submit Kargo Promotion %s: %v", request.ExternalPromotionID, err)
	}
	requireStatus(t, response.StatusCode(), response.Body, http.StatusOK)
	if response.JSON200 == nil {
		t.Fatalf("submit Kargo Promotion %s returned no response", request.ExternalPromotionID)
	}
	return *response.JSON200
}

func (s *suite) getPromotion(t *testing.T, promotionID uuid.UUID) api.Promotion {
	t.Helper()

	response, err := s.productClient.GetPromotionWithResponse(t.Context(), promotionID)
	if err != nil {
		t.Fatalf("get Promotion %s: %v", promotionID, err)
	}
	requireStatus(t, response.StatusCode(), response.Body, http.StatusOK)
	if response.JSON200 == nil {
		t.Fatalf("get Promotion %s returned no Promotion", promotionID)
	}
	return *response.JSON200
}

func (s *suite) waitForPendingPromotion(t *testing.T, promotionID uuid.UUID) api.Promotion {
	t.Helper()

	deadline := time.Now().Add(s.timeout)
	var last api.Promotion
	for time.Now().Before(deadline) {
		response, err := s.productClient.GetPromotionWithResponse(t.Context(), promotionID)
		if err == nil && response.JSON200 != nil {
			last = *response.JSON200
			if last.Status == api.PromotionStatusPending {
				return last
			}
			if last.Status != api.PromotionStatusPreparing {
				t.Fatalf("Promotion %s reached %s instead of pending: %#v", promotionID, last.Status, last)
			}
		}
		wait(t, time.Second)
	}
	t.Fatalf("Promotion %s did not reach pending: %#v", promotionID, last)
	return api.Promotion{}
}

func (s *suite) checkGuardedPromotion(
	t *testing.T,
	kargoClient *api.ClientWithResponses,
	kargoPrincipalID, kargoTargetID uuid.UUID,
	externalID, candidateRevision, candidateBranch string,
) {
	t.Helper()

	request := api.KargoPromotionRequest{
		ExternalPromotionID: externalID,
		Project:             "kargo-acceptance",
		Stage:               "acceptance",
		BaseRevision:        s.fixtureCommit,
		CandidateRevision:   candidateRevision,
		CandidateBranch:     candidateBranch,
	}
	created := submitKargoPromotion(t, kargoClient, request)
	if !created.ReviewRequired ||
		(created.Status != api.PromotionStatusPreparing && created.Status != api.PromotionStatusPending) {
		t.Fatalf("guarded Promotion creation = %#v", created)
	}
	promotion := s.waitForPendingPromotion(t, created.ID)
	if promotion.PreparationRunID == nil || promotion.GitDiffArtifactID == nil || promotion.ArgoDiffArtifactID == nil ||
		promotion.PreparationRun == nil || promotion.GitDiffArtifact == nil || promotion.ArgoDiffArtifact == nil ||
		promotion.CandidateCommit == nil {
		t.Fatalf("guarded Promotion omitted preparation records: %#v", promotion)
	}
	run := *promotion.PreparationRun
	if !promotion.ReviewRequired || promotion.Status != api.PromotionStatusPending ||
		promotion.RequestedBy.PrincipalID != kargoPrincipalID || promotion.CandidateCommit.Sha != candidateRevision ||
		run.Phase != api.RunPhaseFinished || run.Outcome == nil || *run.Outcome != api.RunOutcomeSucceeded ||
		run.Commit.Sha != s.fixtureCommit || run.CatalogRevisionID != s.catalogRevision.ID ||
		run.Source.Type != api.RunSourceTypePromotion || run.Source.Promotion == nil ||
		run.Source.Promotion.PromotionID != promotion.ID || run.CreatedBy == nil ||
		run.CreatedBy.PrincipalID != kargoPrincipalID {
		t.Fatalf("guarded Promotion preparation = %#v", promotion)
	}
	assertPromotionArtifact(t, *promotion.GitDiffArtifact, run.ID, s.fixtureCommit, s.catalogRevision.ID,
		"text/x-diff; charset=utf-8")
	assertPromotionArtifact(t, *promotion.ArgoDiffArtifact, run.ID, s.fixtureCommit, s.catalogRevision.ID,
		"application/vnd.slipramp.argocd-diff+json")

	s.waitForJobDeletion(t, run.ID)
	actualGit := s.downloadArtifact(t, *promotion.GitDiffArtifactID)
	actualArgo := s.downloadArtifact(t, *promotion.ArgoDiffArtifactID)
	expectedGit := s.git(t, "diff", "--no-ext-diff", "--no-color", "--find-renames", s.fixtureCommit, candidateRevision)
	if !bytes.Equal(actualGit, expectedGit) {
		t.Fatalf("Promotion %s Git diff mismatch\nwant:\n%s\ngot:\n%s", externalID, expectedGit, actualGit)
	}
	var argoDiff struct {
		Resources []struct {
			Group     string `json:"group"`
			Kind      string `json:"kind"`
			Namespace string `json:"namespace"`
			Name      string `json:"name"`
			Diff      string `json:"diff"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(actualArgo, &argoDiff); err != nil {
		t.Fatalf("decode Promotion %s Argo CD diff: %v", externalID, err)
	}
	if len(argoDiff.Resources) != 1 {
		t.Fatalf("Promotion %s Argo CD resources = %#v", externalID, argoDiff.Resources)
	}
	resource := argoDiff.Resources[0]
	if resource.Group != "apps" || resource.Kind != "Deployment" || resource.Namespace != "default" ||
		resource.Name != "acceptance" || resource.Diff != "@@ -1 +1 @@\n-image: acceptance:old\n+image: acceptance:new" {
		t.Fatalf("Promotion %s Argo CD resource = %#v", externalID, resource)
	}
	if candidateRevision == s.fixtureCommit {
		if len(actualGit) != 0 || promotion.GitDiffArtifact.SizeBytes != 0 || promotion.GitDiffArtifact.Sha256 == nil ||
			*promotion.GitDiffArtifact.Sha256 != hex.EncodeToString(sha256.New().Sum(nil)) {
			t.Fatalf("empty Git diff Artifact = %#v with %d bytes", promotion.GitDiffArtifact, len(actualGit))
		}
	} else if len(actualGit) == 0 {
		t.Fatalf("Promotion %s produced an empty Git diff", externalID)
	}

	retry := submitKargoPromotion(t, kargoClient, request)
	if retry.ID != promotion.ID || retry.Status != api.PromotionStatusPending {
		t.Fatalf("guarded Promotion retry = %#v", retry)
	}
	runs := s.listRunsForTarget(t, kargoTargetID)
	matchingRuns := 0
	for _, candidate := range runs {
		if candidate.Source.Promotion != nil && candidate.Source.Promotion.PromotionID == promotion.ID {
			matchingRuns++
			if candidate.ID != run.ID {
				t.Fatalf("guarded Promotion retry created Run %s, want %s", candidate.ID, run.ID)
			}
		}
	}
	if matchingRuns != 1 {
		t.Fatalf("guarded Promotion %s has %d preparation Runs", promotion.ID, matchingRuns)
	}
	s.assertLogContains(t, run.ID, "prepared Git and Argo CD review artifacts")
	t.Logf("Promotion %s reached pending with retained diff artifacts", externalID)
}

func assertPromotionArtifact(
	t *testing.T,
	artifact api.Artifact,
	runID uuid.UUID,
	commit string,
	catalogRevisionID uuid.UUID,
	mediaType string,
) {
	t.Helper()
	if artifact.Kind != api.Output || artifact.State != api.ArtifactStateSealed || !artifact.Sensitive ||
		artifact.RetentionState != api.ArtifactRetentionStateRetained || artifact.Provenance.ProducerRunID != runID ||
		artifact.Provenance.Commit.Sha != commit || artifact.Provenance.CatalogRevisionID != catalogRevisionID ||
		artifact.MediaType != mediaType {
		t.Fatalf("Promotion Artifact = %#v", artifact)
	}
}

func (s *suite) listRunsForTarget(t *testing.T, targetID uuid.UUID) []api.Run {
	t.Helper()

	limit := 100
	response, err := s.productClient.ListRunsWithResponse(t.Context(), &api.ListRunsParams{
		TargetID: &targetID,
		Limit:    &limit,
	})
	if err != nil {
		t.Fatalf("list Runs for Target %s: %v", targetID, err)
	}
	requireStatus(t, response.StatusCode(), response.Body, http.StatusOK)
	if response.JSON200 == nil {
		t.Fatalf("list Runs for Target %s returned no list", targetID)
	}
	return response.JSON200.Items
}

func (s *suite) waitForCandidateBranchCleanup(t *testing.T, deletedBranch, protectedBranch string) {
	t.Helper()

	deadline := time.Now().Add(s.timeout)
	for time.Now().Before(deadline) {
		if !s.candidateBranchExists(t, deletedBranch) && s.candidateBranchExists(t, protectedBranch) &&
			s.candidateBranchExists(t, "kargo/promotion/acceptance") {
			return
		}
		wait(t, time.Second)
	}
	refs := s.kubectl(t, "-n", systemNamespace, "exec", "deployment/acceptance-git", "--",
		"git", "--git-dir=/srv/repository.git", "show-ref")
	t.Fatalf("candidate branch cleanup did not reach the expected safe state:\n%s", refs)
}

func (s *suite) candidateBranchExists(t *testing.T, branch string) bool {
	t.Helper()

	result := s.kubectlResult(t, "-n", systemNamespace, "exec", "deployment/acceptance-git", "--",
		"git", "--git-dir=/srv/repository.git", "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	if result.exitCode != 0 && result.exitCode != 1 {
		t.Fatalf("check candidate branch %s failed with exit code %d: %s", branch, result.exitCode, result.stderr)
	}
	return result.exitCode == 0
}
