package kargo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/slipramp/slipramp/internal/api"
)

type observedKargoFreight struct {
	item         api.PromotionFreightItem
	discoveredAt time.Time
}

func (o *StageSnapshotter) candidateFreight(
	ctx context.Context,
	namespace string,
) (map[string][]api.PromotionFreightItem, error) {
	observedByOrigin := make(map[string][]observedKargoFreight)
	err := o.visitResources(ctx, kargoFreightResource, namespace, func(item *unstructured.Unstructured) error {
		observed, err := normalizeKargoFreight(item)
		if err != nil {
			return err
		}
		origin := observed.item.Origin
		observedByOrigin[origin] = append(observedByOrigin[origin], observed)
		return nil
	})
	if err != nil {
		return nil, err
	}

	items := make(map[string][]api.PromotionFreightItem, len(observedByOrigin))
	for origin, observed := range observedByOrigin {
		sort.Slice(observed, func(i, j int) bool {
			if !observed[i].discoveredAt.Equal(observed[j].discoveredAt) {
				return observed[i].discoveredAt.After(observed[j].discoveredAt)
			}
			return observed[i].item.Name > observed[j].item.Name
		})
		items[origin] = make([]api.PromotionFreightItem, 0, len(observed))
		for _, candidate := range observed {
			items[origin] = append(items[origin], candidate.item)
		}
	}

	return items, nil
}

type kargoFreight struct {
	DiscoveredAt *metav1.Time `json:"discoveredAt"`
	Origin       struct {
		Kind string `json:"kind"`
		Name string `json:"name"`
	} `json:"origin"`
	kargoFreightArtifacts
	Status struct {
		CurrentlyIn map[string]struct {
			Since *metav1.Time `json:"since"`
		} `json:"currentlyIn"`
		VerifiedIn map[string]struct {
			LongestCompletedSoak *metav1.Duration `json:"longestSoak"`
		} `json:"verifiedIn"`
		ApprovedFor map[string]json.RawMessage `json:"approvedFor"`
	} `json:"status"`
}

type kargoFreightReference struct {
	Name string `json:"name"`
	kargoFreightArtifacts
}

type kargoFreightArtifacts struct {
	Commits []struct {
		RepositoryURL string `json:"repoURL"`
		ID            string `json:"id"`
		Tag           string `json:"tag"`
	} `json:"commits"`
	Images []struct {
		RepositoryURL string `json:"repoURL"`
		Tag           string `json:"tag"`
		Digest        string `json:"digest"`
	} `json:"images"`
	Charts []struct {
		RepositoryURL string `json:"repoURL"`
		Name          string `json:"name"`
		Version       string `json:"version"`
	} `json:"charts"`
}

func normalizeKargoFreight(item *unstructured.Unstructured) (observedKargoFreight, error) {
	encoded, err := json.Marshal(item.Object)
	if err != nil {
		return observedKargoFreight{}, fmt.Errorf("encode unstructured Freight: %w", err)
	}
	var raw kargoFreight
	if err := json.Unmarshal(encoded, &raw); err != nil {
		return observedKargoFreight{}, fmt.Errorf("decode Freight: %w", err)
	}

	kind := strings.TrimSpace(raw.Origin.Kind)
	name := strings.TrimSpace(raw.Origin.Name)
	if item.GetName() == "" || kind == "" || name == "" {
		return observedKargoFreight{}, errors.New("name and origin are required")
	}
	discoveredAt := item.GetCreationTimestamp().Time
	if raw.DiscoveredAt != nil {
		discoveredAt = raw.DiscoveredAt.Time
	}
	if discoveredAt.IsZero() {
		return observedKargoFreight{}, errors.New("discovery time is required")
	}

	freight := raw.item(kind+"/"+name, item.GetName())
	freight.DiscoveredAt = new(discoveredAt)
	approvedFor := make([]string, 0, len(raw.Status.ApprovedFor))
	for stage := range raw.Status.ApprovedFor {
		approvedFor = append(approvedFor, stage)
	}
	sort.Strings(approvedFor)
	freight.ApprovedFor = &approvedFor
	verifiedIn := make([]api.PromotionFreightVerification, 0, len(raw.Status.VerifiedIn))
	for stage, evidence := range raw.Status.VerifiedIn {
		longestSoak := time.Duration(0)
		if evidence.LongestCompletedSoak != nil {
			longestSoak = evidence.LongestCompletedSoak.Duration
		}
		verifiedIn = append(verifiedIn, api.PromotionFreightVerification{
			Stage: stage, LongestCompletedSoak: longestSoak.String(),
		})
	}
	sort.Slice(verifiedIn, func(i, j int) bool { return verifiedIn[i].Stage < verifiedIn[j].Stage })
	freight.VerifiedIn = &verifiedIn
	currentlyIn := make([]api.PromotionFreightOccupancy, 0, len(raw.Status.CurrentlyIn))
	for stage, evidence := range raw.Status.CurrentlyIn {
		if evidence.Since == nil || evidence.Since.IsZero() {
			continue
		}
		currentlyIn = append(currentlyIn, api.PromotionFreightOccupancy{Stage: stage, Since: evidence.Since.Time})
	}
	sort.Slice(currentlyIn, func(i, j int) bool { return currentlyIn[i].Stage < currentlyIn[j].Stage })
	freight.CurrentlyIn = &currentlyIn

	return observedKargoFreight{
		item:         freight,
		discoveredAt: discoveredAt,
	}, nil
}

func currentKargoFreight(
	stage kargoStage,
	candidateFreight map[string][]api.PromotionFreightItem,
) api.PromotionFreight {
	summary := api.PromotionFreight{Items: []api.PromotionFreightItem{}}
	if len(stage.Status.FreightHistory) == 0 {
		return summary
	}

	for origin, reference := range stage.Status.FreightHistory[0].Items {
		freight := reference.item(origin, reference.Name)
		for _, candidate := range candidateFreight[origin] {
			if candidate.Name == reference.Name {
				freight.DiscoveredAt = candidate.DiscoveredAt
				break
			}
		}
		summary.Items = append(summary.Items, freight)
	}
	sort.Slice(summary.Items, func(i, j int) bool { return summary.Items[i].Origin < summary.Items[j].Origin })

	return summary
}

func (f kargoFreightArtifacts) item(origin, name string) api.PromotionFreightItem {
	artifacts := make(
		[]api.PromotionFreightArtifact,
		0,
		len(f.Commits)+len(f.Images)+len(f.Charts),
	)
	for _, commit := range f.Commits {
		artifacts = append(artifacts, api.PromotionFreightArtifact{
			Kind:       api.PromotionFreightArtifactKindGit,
			Repository: optionalFreightRepository(commit.RepositoryURL),
			Version:    firstNonempty(commit.Tag, commit.ID),
		})
	}
	for _, image := range f.Images {
		artifacts = append(artifacts, api.PromotionFreightArtifact{
			Kind:       api.PromotionFreightArtifactKindImage,
			Repository: optionalFreightRepository(image.RepositoryURL),
			Version:    firstNonempty(image.Tag, image.Digest),
		})
	}
	for _, chart := range f.Charts {
		artifacts = append(artifacts, api.PromotionFreightArtifact{
			Kind:       api.PromotionFreightArtifactKindChart,
			Repository: optionalFreightRepository(firstNonempty(chart.Name, chart.RepositoryURL)),
			Version:    chart.Version,
		})
	}
	sort.Slice(artifacts, func(i, j int) bool {
		if artifacts[i].Kind != artifacts[j].Kind {
			return artifacts[i].Kind < artifacts[j].Kind
		}
		leftRepository := freightRepository(artifacts[i].Repository)
		rightRepository := freightRepository(artifacts[j].Repository)
		if leftRepository != rightRepository {
			return leftRepository < rightRepository
		}
		return artifacts[i].Version < artifacts[j].Version
	})
	return api.PromotionFreightItem{Origin: origin, Name: name, Artifacts: artifacts}
}

func optionalFreightRepository(value string) *string {
	if value == "" {
		return nil
	}

	return &value
}

func freightRepository(value *string) string {
	if value == nil {
		return ""
	}

	return *value
}

func firstNonempty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}

	return ""
}
