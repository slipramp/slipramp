package kargo

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/slipramp/slipramp/internal/api"
)

const kargoListLimit = 500

var kargoStageResource = schema.GroupVersionResource{
	Group:    "kargo.akuity.io",
	Version:  "v1alpha1",
	Resource: "stages",
}

var kargoFreightResource = schema.GroupVersionResource{
	Group:    "kargo.akuity.io",
	Version:  "v1alpha1",
	Resource: "freights",
}

// StageSnapshotter lists the configured Kargo resources and constructs a
// complete Stage snapshot for one integration.
type StageSnapshotter struct {
	integration string
	namespaces  []string
	client      dynamic.Interface
}

func NewStageSnapshotters(config Config, kubeClient dynamic.Interface) []*StageSnapshotter {
	snapshotters := make([]*StageSnapshotter, 0, len(config.Integrations))
	for _, integration := range config.Integrations {
		snapshotters = append(snapshotters, &StageSnapshotter{
			integration: integration.Name,
			namespaces:  slices.Clone(integration.StageNamespaces),
			client:      kubeClient,
		})
	}

	return snapshotters
}

func (o *StageSnapshotter) Integration() string {
	return o.integration
}

func (o *StageSnapshotter) Snapshot(ctx context.Context) (api.KargoStageSnapshot, error) {
	stages := make([]api.PromotionStageObservation, 0)
	for _, namespace := range o.namespaces {
		candidateFreight, err := o.candidateFreight(ctx, namespace)
		if err != nil {
			return api.KargoStageSnapshot{}, err
		}

		err = o.visitResources(ctx, kargoStageResource, namespace, func(item *unstructured.Unstructured) error {
			observed, err := normalizeKargoStage(item, candidateFreight)
			if err != nil {
				return err
			}
			stages = append(stages, observed)
			return nil
		})
		if err != nil {
			return api.KargoStageSnapshot{}, err
		}
	}

	sort.Slice(stages, func(i, j int) bool {
		if stages[i].Project != stages[j].Project {
			return stages[i].Project < stages[j].Project
		}
		return stages[i].Name < stages[j].Name
	})

	return api.KargoStageSnapshot{
		SnapshotID: uuid.New(),
		ObservedAt: time.Now().UTC(),
		Stages:     stages,
	}, nil
}

func (o *StageSnapshotter) visitResources(
	ctx context.Context,
	resource schema.GroupVersionResource,
	namespace string,
	visit func(*unstructured.Unstructured) error,
) error {
	client := o.client.Resource(resource).Namespace(namespace)
	options := metav1.ListOptions{Limit: kargoListLimit}
	for {
		page, err := client.List(ctx, options)
		if err != nil {
			return fmt.Errorf("list Kargo %s in namespace %q: %w", resource.Resource, namespace, err)
		}
		for i := range page.Items {
			item := &page.Items[i]
			if item.GetNamespace() != namespace {
				return fmt.Errorf("unexpected Kargo %s/%s outside namespace %q", resource.Resource, item.GetName(), namespace)
			}
			if err := visit(item); err != nil {
				return fmt.Errorf("normalize Kargo %s %s/%s: %w", resource.Resource, namespace, item.GetName(), err)
			}
		}
		options.Continue = page.GetContinue()
		if options.Continue == "" {
			return nil
		}
	}
}
