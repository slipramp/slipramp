package kargo

import (
	"encoding/json"
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	"github.com/slipramp/slipramp/internal/api"
)

func TestNormalizeKargoStage(t *testing.T) {
	t.Parallel()

	stage := unstructuredResource(t, `{
      "apiVersion":"kargo.akuity.io/v1alpha1",
      "kind":"Stage",
      "metadata":{
        "namespace":"kargo-core",
        "name":"external-dns-prod",
        "uid":"stage-uid",
        "resourceVersion":"42",
        "labels":{"app":"ignored","env":"ignored"},
        "annotations":{
          "slipramp.com/promotion-application":"external-dns-phi",
          "slipramp.com/promotion-environment":"prod",
          "slipramp.com/promotion-repository-url":"https://example.com/ops.git",
          "slipramp.com/promotion-target-branch":"environments/production",
          "slipramp.com/promotion-source-paths":"[\"terraform/dns\",\"config/production.yaml\"]"
        }
      },
      "spec":{
        "requestedFreight":[{
          "origin":{"kind":"Warehouse","name":"external-dns"},
          "sources":{"stages":["external-dns-qa"],"direct":false}
        }]
      },
      "status":{
		"autoPromotionEnabled":true,
		"health":{
		  "status":"Healthy",
		  "output":[{"applicationStatuses":[
		    {"Name":"other","Namespace":"argocd"},
		    {"Name":"external-dns-phi","Namespace":"argocd"}
		  ]}]
		},
        "freightHistory":[{"items":{
          "Warehouse/external-dns":{
            "name":"99d3a4",
            "commits":[{"repoURL":"https://example.com/ops.git","id":"source-sha"}],
            "images":[{"repoURL":"ghcr.io/example/external-dns","tag":"0.19.0"}],
            "charts":[{"repoURL":"https://charts.example.com","name":"external-dns","version":"1.16.1"}]
          }
        }}],
        "lastPromotion":{"status":{
          "phase":"Succeeded",
          "state":{
            "promo-process::clone":{"commits":{"./src":"source-sha","target":"target-sha"}},
            "promo-process::commit":{"commit":"target-sha"}
          }
        }}
      }
    }`)

	observed, err := normalizeKargoStage(stage, nil)
	if err != nil {
		t.Fatalf("normalizeKargoStage() error = %v", err)
	}
	if observed.Project != "kargo-core" || observed.Name != "external-dns-prod" ||
		observed.KubernetesUID != "stage-uid" || observed.ResourceVersion != "42" ||
		valueOrEmpty(observed.Application) != "external-dns-phi" || valueOrEmpty(observed.Environment) != "prod" ||
		!observed.AutoPromotionEnabled {
		t.Errorf("identity = %#v", observed)
	}
	if valueOrEmpty(observed.HealthStatus) != "Healthy" ||
		valueOrEmpty(observed.ArgoApplicationNamespace) != "argocd" {
		t.Errorf("health = %#v, Argo namespace = %v", observed.HealthStatus, observed.ArgoApplicationNamespace)
	}
	if valueOrEmpty(observed.RepositoryURL) != "https://example.com/ops.git" ||
		valueOrEmpty(observed.TargetBranch) != "environments/production" {
		t.Errorf("branch details = %#v", observed)
	}
	wantPaths := []string{
		"terraform/dns",
		"config/production.yaml",
	}
	if observed.SourcePaths == nil || !reflect.DeepEqual(*observed.SourcePaths, wantPaths) {
		t.Errorf("source paths = %#v, want %#v", observed.SourcePaths, wantPaths)
	}
	if len(observed.RequestedFreight) != 1 ||
		observed.RequestedFreight[0].Origin != "Warehouse/external-dns" ||
		!reflect.DeepEqual(observed.RequestedFreight[0].SourceStages, []string{"external-dns-qa"}) {
		t.Errorf("requested Freight = %#v", observed.RequestedFreight)
	}
	if len(observed.Freight.Items) != 1 || observed.Freight.Items[0].Origin != "Warehouse/external-dns" ||
		len(observed.Freight.Items[0].Artifacts) != 3 ||
		observed.Freight.Items[0].Artifacts[0].Kind != api.PromotionFreightArtifactKindChart {
		t.Fatalf("Freight = %#v", observed.Freight)
	}
	if observed.SuccessfulRenderAcknowledgement == nil ||
		valueOrEmpty(observed.SuccessfulRenderAcknowledgement.SourceRevision) != "source-sha" ||
		valueOrEmpty(observed.SuccessfulRenderAcknowledgement.ResultRevision) != "target-sha" {
		t.Errorf("successful render acknowledgement = %#v", observed.SuccessfulRenderAcknowledgement)
	}

	for _, paths := range []string{
		`"terraform/dns"`, `[]`, `["../outside"]`, `["/absolute"]`, `["config", "config"]`,
	} {
		t.Run("invalid source paths "+paths, func(t *testing.T) {
			t.Parallel()

			invalid := stage.DeepCopy()
			annotations := invalid.GetAnnotations()
			annotations[promotionSourcePathsAnnotation] = paths
			invalid.SetAnnotations(annotations)
			if _, err := normalizeKargoStage(invalid, nil); err == nil {
				t.Fatalf("normalizeKargoStage() accepted invalid source paths %s", paths)
			}
		})
	}
}

func TestNormalizeKargoStageExcludesFailedPromotionAcknowledgement(t *testing.T) {
	t.Parallel()

	stage := minimalUnstructuredStage(t, "kargo-security", "falco-prod")
	stage.Object["status"] = map[string]any{
		"lastPromotion": map[string]any{
			"status": map[string]any{
				"phase": "Failed",
				"state": map[string]any{
					"promo-process::clone": map[string]any{
						"commits": map[string]any{"./src": "source-sha", "target": "target-sha"},
					},
					"promo-process::commit": map[string]any{"commit": "result-sha"},
				},
			},
		},
	}

	observed, err := normalizeKargoStage(stage, nil)
	if err != nil {
		t.Fatalf("normalizeKargoStage() error = %v", err)
	}
	if observed.SuccessfulRenderAcknowledgement != nil {
		t.Errorf("failed Promotion acknowledgement = %#v", observed.SuccessfulRenderAcknowledgement)
	}
}

func unstructuredResource(t *testing.T, source string) *unstructured.Unstructured {
	t.Helper()

	var object map[string]any
	if err := json.Unmarshal([]byte(source), &object); err != nil {
		t.Fatalf("decode Kargo fixture: %v", err)
	}

	return &unstructured.Unstructured{Object: object}
}

func minimalUnstructuredStage(t *testing.T, namespace string, name string) *unstructured.Unstructured {
	t.Helper()

	stage := unstructuredResource(t, `{
      "apiVersion":"kargo.akuity.io/v1alpha1",
      "kind":"Stage",
      "spec":{}
    }`)
	stage.SetNamespace(namespace)
	stage.SetName(name)
	stage.SetUID(types.UID("uid-" + name))
	stage.SetResourceVersion("1")

	return stage
}

func unstructuredFreight(
	t *testing.T,
	namespace string,
	name string,
	discoveredAt string,
	version string,
) *unstructured.Unstructured {
	t.Helper()

	freight := unstructuredResource(t, `{
      "apiVersion":"kargo.akuity.io/v1alpha1",
      "kind":"Freight",
      "origin":{"kind":"Warehouse","name":"external-dns"},
      "images":[{"repoURL":"ghcr.io/example/external-dns","tag":"`+version+`"}],
      "discoveredAt":"`+discoveredAt+`"
    }`)
	freight.SetNamespace(namespace)
	freight.SetName(name)

	return freight
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}

	return *value
}
