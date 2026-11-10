package kargo

import (
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	kubetesting "k8s.io/client-go/testing"

	"github.com/slipramp/slipramp/internal/promotion"
)

func TestKargoStageObserverReportsCandidateFreightNewestFirst(t *testing.T) {
	t.Parallel()

	stage := minimalUnstructuredStage(t, "kargo-core", "external-dns-prod")
	spec := stage.Object["spec"].(map[string]any)
	spec["requestedFreight"] = []any{map[string]any{
		"origin":  map[string]any{"kind": "Warehouse", "name": "external-dns"},
		"sources": map[string]any{"direct": true},
	}}

	newest := unstructuredFreight(t, "kargo-core", "freight-new", "2026-10-21T11:00:00Z", "2.0.0")
	newest.Object["status"] = map[string]any{
		"approvedFor": map[string]any{"prod": map[string]any{}},
		"verifiedIn":  map[string]any{"test": map[string]any{"longestSoak": "45m"}},
		"currentlyIn": map[string]any{"test": map[string]any{"since": "2026-10-21T09:00:00Z"}},
	}
	client := newTestKargoDynamicClient(stage, newest,
		unstructuredFreight(t, "kargo-core", "freight-old", "2026-10-21T10:00:00Z", "1.0.0"),
	)
	observer := &StageSnapshotter{client: client, namespaces: []string{"kargo-core"}}
	snapshot, err := observer.Snapshot(t.Context())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if len(snapshot.Stages) != 1 || len(snapshot.Stages[0].CandidateFreight.Items) != 2 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	item := snapshot.Stages[0].CandidateFreight.Items[0]
	if item.Name != "freight-new" || item.Origin != "Warehouse/external-dns" ||
		len(item.Artifacts) != 1 || item.Artifacts[0].Version != "2.0.0" {
		t.Errorf("direct Freight = %#v", item)
	}
	if item.ApprovedFor == nil || !reflect.DeepEqual(*item.ApprovedFor, []string{"prod"}) ||
		item.VerifiedIn == nil || len(*item.VerifiedIn) != 1 ||
		(*item.VerifiedIn)[0].LongestCompletedSoak != "45m0s" ||
		item.CurrentlyIn == nil || len(*item.CurrentlyIn) != 1 {
		t.Errorf("eligibility evidence = %#v", item)
	}
}

func TestStageSnapshotListsEveryPageInAssignedNamespaces(t *testing.T) {
	t.Parallel()

	client := newTestKargoDynamicClient()
	var requests []string
	client.PrependReactor("list", "stages", func(action kubetesting.Action) (bool, runtime.Object, error) {
		namespace := action.GetNamespace()
		options := action.(kubetesting.ListActionImpl).GetListOptions()
		if options.Limit != kargoListLimit || options.LabelSelector != "" {
			t.Fatalf("unexpected Stage list options: %#v", options)
		}
		requests = append(requests, namespace+"/"+options.Continue)
		item := minimalUnstructuredStage(t, namespace, "first")
		page := &unstructured.UnstructuredList{}
		if namespace == "core" && options.Continue == "" {
			page.SetContinue("next")
			item.SetLabels(map[string]string{"slipramp.com/integration": "other"})
		} else if options.Continue == "next" {
			item.SetName("second")
		}
		page.Items = []unstructured.Unstructured{*item}
		return true, page, nil
	})
	observers := NewStageSnapshotters(Config{Integrations: []promotion.ObserverConfig{
		{Name: "prod", StageNamespaces: []string{"core", "platform"}},
	}}, client)
	snapshot, err := observers[0].Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"core/", "core/next", "platform/"}; !reflect.DeepEqual(requests, want) {
		t.Fatalf("list requests = %v, want %v", requests, want)
	}
	if len(snapshot.Stages) != 3 || snapshot.SnapshotID == uuid.Nil || snapshot.ObservedAt.IsZero() {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	stage := snapshot.Stages[0]
	if stage.Project != "core" || stage.Name != "first" || !stage.ControlFlow ||
		stage.Application != nil || stage.Environment != nil || stage.RepositoryURL != nil ||
		stage.TargetBranch != nil || stage.SourcePaths != nil {
		t.Errorf("unannotated Stage = %#v", stage)
	}
}

func TestStageSnapshotRejectsPartialInventory(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name     string
		resource string
		item     *unstructured.Unstructured
	}{
		{name: "Freight list failure", resource: "freights"},
		{name: "Stage list failure", resource: "stages"},
		{name: "Stage outside namespace", resource: "stages", item: minimalUnstructuredStage(t, "other", "app")},
		{name: "invalid Stage", resource: "stages", item: minimalUnstructuredStage(t, "platform", "")},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client := newTestKargoDynamicClient(minimalUnstructuredStage(t, "core", "first"))
			client.PrependReactor("list", test.resource, func(action kubetesting.Action) (bool, runtime.Object, error) {
				if action.GetNamespace() != "platform" {
					return false, nil, nil
				}
				if test.item == nil {
					return true, nil, errors.New("Kubernetes API unavailable")
				}
				return true, &unstructured.UnstructuredList{Items: []unstructured.Unstructured{*test.item}}, nil
			})
			observer := &StageSnapshotter{client: client, namespaces: []string{"core", "platform"}}
			snapshot, err := observer.Snapshot(t.Context())
			if err == nil || snapshot.SnapshotID != uuid.Nil || !snapshot.ObservedAt.IsZero() || len(snapshot.Stages) != 0 {
				t.Fatalf("Snapshot() = %#v, %v", snapshot, err)
			}
		})
	}
}
