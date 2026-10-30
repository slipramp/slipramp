package controller

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	"github.com/slipramp/slipramp/internal/api"
)

type kargoStagePageListerFunc func(
	context.Context,
	string,
	metav1.ListOptions,
) (*unstructured.UnstructuredList, error)

func (f kargoStagePageListerFunc) List(
	ctx context.Context,
	namespace string,
	options metav1.ListOptions,
) (*unstructured.UnstructuredList, error) {
	return f(ctx, namespace, options)
}

type kargoStageSnapshotReporterFunc func(context.Context, string, api.KargoStageSnapshot) error

func (f kargoStageSnapshotReporterFunc) ReplaceKargoStageSnapshot(
	ctx context.Context,
	integration string,
	snapshot api.KargoStageSnapshot,
) error {
	return f(ctx, integration, snapshot)
}

func TestNormalizeKargoStage(t *testing.T) {
	t.Parallel()

	stage := unstructuredStage(t, `{
      "apiVersion":"kargo.akuity.io/v1alpha1",
      "kind":"Stage",
      "metadata":{
        "namespace":"kargo-core",
        "name":"external-dns-prod",
        "uid":"stage-uid",
        "resourceVersion":"42",
        "labels":{"app":"ignored","env":"prod"},
        "annotations":{"slipramp.com/promotion-source-paths":"k8s/shared\n../ignored"}
      },
      "spec":{
        "requestedFreight":[{
          "origin":{"kind":"Warehouse","name":"external-dns"},
          "sources":{"stages":["external-dns-qa"],"direct":false}
        }],
        "promotionTemplate":{"spec":{"vars":[
          {"name":"app","value":"external-dns"},
          {"name":"argoApp","value":"external-dns-phi"},
          {"name":"overlay","value":"prod"},
          {"name":"kustomizePath","value":"network/external-dns"},
          {"name":"stageBranchKustomization","value":"k8s/kargo/custom-stage.yaml"},
          {"name":"gitopsRepo","value":"https://example.com/ops.git"}
        ]}}
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

	observed, err := normalizeKargoStage(stage)
	if err != nil {
		t.Fatalf("normalizeKargoStage() error = %v", err)
	}
	if observed.Project != "kargo-core" || observed.Name != "external-dns-prod" ||
		observed.KubernetesUID != "stage-uid" || observed.ResourceVersion != "42" ||
		observed.Application != "external-dns-phi" || observed.Environment != "prod" ||
		!observed.AutoPromotionEnabled {
		t.Errorf("identity = %#v", observed)
	}
	if valueOrEmpty(observed.HealthStatus) != "Healthy" ||
		valueOrEmpty(observed.ArgoApplicationNamespace) != "argocd" {
		t.Errorf("health = %#v, Argo namespace = %v", observed.HealthStatus, observed.ArgoApplicationNamespace)
	}
	if observed.RepositoryURL != "https://example.com/ops.git" ||
		observed.TargetBranch != "stage/external-dns/prod" {
		t.Errorf("branch details = %#v", observed)
	}
	wantPaths := []string{
		"k8s/network/external-dns/base",
		"k8s/network/external-dns/overlays/prod",
		"k8s/kargo/custom-stage.yaml",
		"k8s/shared",
	}
	if !reflect.DeepEqual(observed.SourcePaths, wantPaths) {
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
}

func TestKargoStageObserverReportsNewestDirectFreight(t *testing.T) {
	t.Parallel()

	stage := minimalUnstructuredStage(t, "kargo-core", "external-dns-prod")
	spec := stage.Object["spec"].(map[string]any)
	spec["requestedFreight"] = []any{map[string]any{
		"origin":  map[string]any{"kind": "Warehouse", "name": "external-dns"},
		"sources": map[string]any{"direct": true},
	}}

	freightLister := kargoStagePageListerFunc(func(
		_ context.Context,
		namespace string,
		_ metav1.ListOptions,
	) (*unstructured.UnstructuredList, error) {
		return &unstructured.UnstructuredList{Items: []unstructured.Unstructured{
			*unstructuredFreight(t, namespace, "freight-old", "2026-10-21T10:00:00Z", "1.0.0"),
			*unstructuredFreight(t, namespace, "freight-new", "2026-10-21T11:00:00Z", "2.0.0"),
		}}, nil
	})
	observer := &kargoStageObserver{
		integration: "prod",
		namespaces:  []string{"kargo-core"},
		lister: kargoStagePageListerFunc(func(
			_ context.Context,
			_ string,
			_ metav1.ListOptions,
		) (*unstructured.UnstructuredList, error) {
			return &unstructured.UnstructuredList{Items: []unstructured.Unstructured{*stage}}, nil
		}),
		freight: freightLister,
		logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		newID:   uuid.New,
		now:     time.Now,
	}

	snapshot, err := observer.observe(t.Context())
	if err != nil {
		t.Fatalf("observe() error = %v", err)
	}
	if len(snapshot.Stages) != 1 || len(snapshot.Stages[0].DirectFreight.Items) != 1 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	item := snapshot.Stages[0].DirectFreight.Items[0]
	if item.Name != "freight-new" || item.Origin != "Warehouse/external-dns" ||
		len(item.Artifacts) != 1 || item.Artifacts[0].Version != "2.0.0" {
		t.Errorf("direct Freight = %#v", item)
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

	observed, err := normalizeKargoStage(stage)
	if err != nil {
		t.Fatalf("normalizeKargoStage() error = %v", err)
	}
	if observed.SuccessfulRenderAcknowledgement != nil {
		t.Errorf("failed Promotion acknowledgement = %#v", observed.SuccessfulRenderAcknowledgement)
	}
}

func TestKargoStageObserverPaginatesConfiguredNamespaces(t *testing.T) {
	t.Parallel()

	type request struct {
		namespace string
		limit     int64
		continued string
	}
	requests := make([]request, 0)
	lister := kargoStagePageListerFunc(func(
		_ context.Context,
		namespace string,
		options metav1.ListOptions,
	) (*unstructured.UnstructuredList, error) {
		requests = append(requests, request{namespace, options.Limit, options.Continue})
		if namespace == "kargo-core" && options.Continue == "" {
			return &unstructured.UnstructuredList{
				Object: map[string]any{"metadata": map[string]any{"continue": "next"}},
				Items:  []unstructured.Unstructured{*minimalUnstructuredStage(t, namespace, "first")},
			}, nil
		}
		if namespace == "kargo-core" && options.Continue == "next" {
			return &unstructured.UnstructuredList{
				Items: []unstructured.Unstructured{*minimalUnstructuredStage(t, namespace, "second")},
			}, nil
		}
		if namespace == "kargo-platform" {
			return &unstructured.UnstructuredList{
				Items: []unstructured.Unstructured{*minimalUnstructuredStage(t, namespace, "third")},
			}, nil
		}

		return nil, errors.New("unexpected namespace or continuation")
	})
	observedAt := time.Date(2026, time.October, 21, 12, 0, 0, 0, time.UTC)
	observer := &kargoStageObserver{
		integration: "prod",
		namespaces:  []string{"kargo-core", "kargo-platform"},
		lister:      lister,
		logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		newID:       func() uuid.UUID { return uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1902") },
		now:         func() time.Time { return observedAt },
	}

	snapshot, err := observer.observe(t.Context())
	if err != nil {
		t.Fatalf("observe() error = %v", err)
	}
	if len(snapshot.Stages) != 3 || !snapshot.ObservedAt.Equal(observedAt) {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	wantRequests := []request{
		{"kargo-core", kargoStageListLimit, ""},
		{"kargo-core", kargoStageListLimit, "next"},
		{"kargo-platform", kargoStageListLimit, ""},
	}
	if !reflect.DeepEqual(requests, wantRequests) {
		t.Errorf("list requests = %#v, want %#v", requests, wantRequests)
	}
}

func TestKargoStageObserverRejectsPartialAndCrossNamespaceLists(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		lister kargoStagePageLister
	}{
		{
			name: "partial list failure",
			lister: kargoStagePageListerFunc(func(
				_ context.Context,
				namespace string,
				_ metav1.ListOptions,
			) (*unstructured.UnstructuredList, error) {
				if namespace == "kargo-platform" {
					return nil, errors.New("Kubernetes API unavailable")
				}
				return &unstructured.UnstructuredList{
					Items: []unstructured.Unstructured{*minimalUnstructuredStage(t, namespace, "first")},
				}, nil
			}),
		},
		{
			name: "cross namespace item",
			lister: kargoStagePageListerFunc(func(
				_ context.Context,
				_ string,
				_ metav1.ListOptions,
			) (*unstructured.UnstructuredList, error) {
				return &unstructured.UnstructuredList{
					Items: []unstructured.Unstructured{*minimalUnstructuredStage(t, "not-configured", "first")},
				}, nil
			}),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			observer := &kargoStageObserver{
				integration: "prod",
				namespaces:  []string{"kargo-core", "kargo-platform"},
				lister:      test.lister,
				logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
				newID:       uuid.New,
				now:         time.Now,
			}
			if _, err := observer.observe(t.Context()); err == nil {
				t.Fatal("observe() error = nil")
			}
		})
	}
}

func TestKargoStageObserverRetriesTheSameSnapshotAfterAmbiguousFailure(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var reported []api.KargoStageSnapshot
	reporter := kargoStageSnapshotReporterFunc(func(
		_ context.Context,
		integration string,
		snapshot api.KargoStageSnapshot,
	) error {
		if integration != "prod" {
			t.Errorf("integration = %q, want prod", integration)
		}
		reported = append(reported, snapshot)
		if len(reported) == 1 {
			return errors.New("ambiguous network failure")
		}
		cancel()

		return nil
	})
	observer := &kargoStageObserver{
		integration: "prod",
		namespaces:  []string{"kargo-core"},
		lister: kargoStagePageListerFunc(func(
			_ context.Context,
			namespace string,
			_ metav1.ListOptions,
		) (*unstructured.UnstructuredList, error) {
			return &unstructured.UnstructuredList{
				Items: []unstructured.Unstructured{*minimalUnstructuredStage(t, namespace, "first")},
			}, nil
		}),
		reporter: reporter,
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		interval: time.Millisecond,
		newID: func() uuid.UUID {
			return uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1904")
		},
		now: func() time.Time {
			return time.Date(2026, time.October, 21, 12, 0, 0, 0, time.UTC)
		},
	}

	if err := observer.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(reported) != 2 || !reflect.DeepEqual(reported[0], reported[1]) {
		t.Fatalf("reported snapshots = %#v", reported)
	}
}

func unstructuredStage(t *testing.T, source string) *unstructured.Unstructured {
	t.Helper()

	var object map[string]any
	if err := json.Unmarshal([]byte(source), &object); err != nil {
		t.Fatalf("decode Stage fixture: %v", err)
	}

	return &unstructured.Unstructured{Object: object}
}

func minimalUnstructuredStage(t *testing.T, namespace string, name string) *unstructured.Unstructured {
	t.Helper()

	stage := unstructuredStage(t, `{
      "apiVersion":"kargo.akuity.io/v1alpha1",
      "kind":"Stage",
      "metadata":{"labels":{"env":"prod"}},
      "spec":{"promotionTemplate":{"spec":{"vars":[
        {"name":"app","value":"application"},
        {"name":"overlay","value":"prod"},
        {"name":"gitopsRepo","value":"https://example.com/ops.git"}
      ]}}}
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

	freight := unstructuredStage(t, `{
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
