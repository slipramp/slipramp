package handler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/slipramp/slipramp/internal/promotion"
)

func TestReplaceKargoStageSnapshot(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name       string
		required   string
		completed  string
		serviceErr error
		status     int
	}{
		{"authenticated cluster", "1h", "45m", nil, http.StatusNoContent},
		{"wrong cluster", "1h", "45m", promotion.ErrIntegrationCluster, http.StatusForbidden},
		{"malformed requirement", "one hour", "45m", nil, http.StatusBadRequest},
		{"malformed evidence", "1h", "yesterday", nil, http.StatusBadRequest},
		{"negative evidence", "1h", "-1s", nil, http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			service := &recordingPromotionService{err: test.serviceErr}
			handler := NewHTTP(Dependencies{
				ControllerIdentities: controllerIdentityFor("controller-token", "colo"),
				PromotionStages:      service,
			}, HTTPOptions{})
			body := fmt.Sprintf(`{
			  "snapshotId":"018f90ff-9fd8-7b5f-b7e1-f32ff54c1903","observedAt":"2026-10-21T12:00:00Z",
			  "stages":[{
			    "project":"core","name":"prod","kubernetesUid":"stage-uid","resourceVersion":"42",
			    "repositoryUrl":"https://example.com/ops.git","freight":{"items":[]},"conditions":[],
			    "autoPromotionEnabled":false,"controlFlow":false,
			    "requestedFreight":[{"origin":"Warehouse/app","direct":false,"sourceStages":["test"],
			       "availabilityStrategy":"OneOf","requiredSoakTime":%q}],
			    "candidateFreight":{"items":[{"origin":"Warehouse/app","name":"candidate",
			       "discoveredAt":"2026-10-21T11:00:00Z","artifacts":[],
			       "verifiedIn":[{"stage":"test","longestCompletedSoak":%q}]}]}
			  }]
			}`, test.required, test.completed)
			request := httptest.NewRequestWithContext(t.Context(), http.MethodPut, "/controller/v1/kargo/prod/stages", strings.NewReader(body))
			request.Header.Set("Authorization", "Bearer controller-token")
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if test.status != http.StatusNoContent {
				assertProblem(t, response, test.status)
				if test.status == http.StatusBadRequest && service.integration != "" {
					t.Fatal("invalid snapshot reached the Stage service")
				}
				return
			}
			if response.Code != http.StatusNoContent || service.cluster != "colo" || service.integration != "prod" ||
				len(service.replacement.Stages) != 1 {
				t.Fatalf("HTTP %d, submission = %#v", response.Code, service)
			}
			stage := service.replacement.Stages[0]
			if stage.RepositoryURL != "https://example.com/ops.git" || len(stage.RequestedFreight) != 1 ||
				stage.RequestedFreight[0].RequiredSoakTime != time.Hour || len(stage.CandidateFreight.Items) != 1 {
				t.Fatalf("Stage observation = %#v", stage)
			}
			if evidence := stage.CandidateFreight.Items[0].VerifiedIn; len(evidence) != 1 || evidence[0].LongestCompletedSoak != 45*time.Minute {
				t.Errorf("Freight verification = %#v", evidence)
			}
			if discoveredAt := stage.CandidateFreight.Items[0].DiscoveredAt; discoveredAt == nil ||
				!discoveredAt.Equal(time.Date(2026, time.October, 21, 11, 0, 0, 0, time.UTC)) {
				t.Errorf("Freight discovery time = %v", discoveredAt)
			}
		})
	}
}
