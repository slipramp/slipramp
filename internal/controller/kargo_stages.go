package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/dynamic"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/promotion"
)

const (
	kargoStageListLimit             = 500
	kargoFreightListLimit           = 500
	kargoStageObservationPeriod     = 30 * time.Second
	maximumKargoHealthStatusLength  = 200
	promotionIntegrationLabel       = "slipramp.com/integration"
	promotionApplicationAnnotation  = "slipramp.com/promotion-application"
	promotionEnvironmentAnnotation  = "slipramp.com/promotion-environment"
	promotionRepositoryAnnotation   = "slipramp.com/promotion-repository-url"
	promotionTargetBranchAnnotation = "slipramp.com/promotion-target-branch"
	promotionSourcePathsAnnotation  = "slipramp.com/promotion-source-paths"
)

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

type kargoStageSnapshotReporter interface {
	ReplaceKargoStageSnapshot(context.Context, string, api.KargoStageSnapshot) error
}

type kargoStagePageLister interface {
	List(context.Context, string, metav1.ListOptions) (*unstructured.UnstructuredList, error)
}

type dynamicKargoStageLister struct {
	resources dynamic.NamespaceableResourceInterface
}

func (l dynamicKargoStageLister) List(
	ctx context.Context,
	namespace string,
	options metav1.ListOptions,
) (*unstructured.UnstructuredList, error) {
	return l.resources.Namespace(namespace).List(ctx, options)
}

type kargoStageObserver struct {
	integration string
	namespaces  []string
	lister      kargoStagePageLister
	freight     kargoStagePageLister
	reporter    kargoStageSnapshotReporter
	logger      *slog.Logger
	interval    time.Duration
	newID       func() uuid.UUID
	now         func() time.Time
}

func newKargoStageObservers(
	configs []promotion.ObserverConfig,
	kubeClient dynamic.Interface,
	reporter kargoStageSnapshotReporter,
	logger *slog.Logger,
) []*kargoStageObserver {
	observers := make([]*kargoStageObserver, 0, len(configs))
	lister := dynamicKargoStageLister{resources: kubeClient.Resource(kargoStageResource)}
	freight := dynamicKargoStageLister{resources: kubeClient.Resource(kargoFreightResource)}
	for _, config := range configs {
		observers = append(observers, &kargoStageObserver{
			integration: config.Name,
			namespaces:  slices.Clone(config.StageNamespaces),
			lister:      lister,
			freight:     freight,
			reporter:    reporter,
			logger: logger.With(
				"component", "kargo-stage-observer",
				"integration", config.Name,
			),
			interval: kargoStageObservationPeriod,
			newID:    uuid.New,
			now:      time.Now,
		})
	}

	return observers
}

func (o *kargoStageObserver) Run(ctx context.Context) error {
	var pending *api.KargoStageSnapshot
	for {
		if pending == nil {
			snapshot, err := o.observe(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				o.logger.Warn("observe Kargo Stages", "error", err)
				if !waitForRetry(ctx, o.interval) {
					return nil
				}
				continue
			}
			pending = &snapshot
		}

		err := o.reporter.ReplaceKargoStageSnapshot(ctx, o.integration, *pending)
		if err == nil {
			pending = nil
		} else if ctx.Err() != nil {
			return nil
		} else if errors.Is(err, errControllerCredentialRejected) {
			return err
		} else {
			o.logger.Warn("submit Kargo Stage snapshot", "snapshot_id", pending.SnapshotID, "error", err)
			if permanentServerError(err) {
				pending = nil
			}
		}

		if !waitForRetry(ctx, o.interval) {
			return nil
		}
	}
}

func (o *kargoStageObserver) observe(ctx context.Context) (api.KargoStageSnapshot, error) {
	stages := make([]api.PromotionStageObservation, 0)
	for _, namespace := range o.namespaces {
		directFreight, err := o.latestDirectFreight(ctx, namespace)
		if err != nil {
			return api.KargoStageSnapshot{}, err
		}

		continueToken := ""
		for {
			page, err := o.lister.List(ctx, namespace, metav1.ListOptions{
				Limit:    kargoStageListLimit,
				Continue: continueToken,
			})
			if err != nil {
				return api.KargoStageSnapshot{}, fmt.Errorf("list namespace %q: %w", namespace, err)
			}

			for index := range page.Items {
				item := &page.Items[index]
				if item.GetNamespace() != namespace {
					return api.KargoStageSnapshot{}, fmt.Errorf(
						"kargo Stage %q was returned outside configured namespace %q",
						item.GetName(),
						namespace,
					)
				}
				if item.GetLabels()[promotionIntegrationLabel] != o.integration {
					continue
				}
				observed, err := normalizeKargoStageWithDirectFreight(item, directFreight)
				if err != nil {
					return api.KargoStageSnapshot{}, fmt.Errorf(
						"normalize Kargo Stage %s/%s: %w",
						namespace,
						item.GetName(),
						err,
					)
				}
				stages = append(stages, observed)
			}

			continueToken = page.GetContinue()
			if continueToken == "" {
				break
			}
		}
	}

	sort.Slice(stages, func(i, j int) bool {
		if stages[i].Project != stages[j].Project {
			return stages[i].Project < stages[j].Project
		}
		return stages[i].Name < stages[j].Name
	})

	return api.KargoStageSnapshot{
		SnapshotID: o.newID(),
		ObservedAt: o.now().UTC(),
		Stages:     stages,
	}, nil
}

type observedKargoFreight struct {
	item         api.PromotionFreightItem
	discoveredAt time.Time
}

func (o *kargoStageObserver) latestDirectFreight(
	ctx context.Context,
	namespace string,
) (map[string]api.PromotionFreightItem, error) {
	latest := make(map[string]observedKargoFreight)
	if o.freight == nil {
		return map[string]api.PromotionFreightItem{}, nil
	}

	continueToken := ""
	for {
		page, err := o.freight.List(ctx, namespace, metav1.ListOptions{
			Limit:    kargoFreightListLimit,
			Continue: continueToken,
		})
		if err != nil {
			return nil, fmt.Errorf("list Kargo Freight in namespace %q: %w", namespace, err)
		}

		for index := range page.Items {
			item := &page.Items[index]
			if item.GetNamespace() != namespace {
				return nil, fmt.Errorf(
					"kargo Freight %q was returned outside configured namespace %q",
					item.GetName(),
					namespace,
				)
			}
			observed, normalizeErr := normalizeKargoFreight(item)
			if normalizeErr != nil {
				return nil, fmt.Errorf(
					"normalize Kargo Freight %s/%s: %w",
					namespace,
					item.GetName(),
					normalizeErr,
				)
			}
			current, ok := latest[observed.item.Origin]
			if !ok || observed.discoveredAt.After(current.discoveredAt) ||
				(observed.discoveredAt.Equal(current.discoveredAt) && observed.item.Name > current.item.Name) {
				latest[observed.item.Origin] = observed
			}
		}

		continueToken = page.GetContinue()
		if continueToken == "" {
			break
		}
	}

	items := make(map[string]api.PromotionFreightItem, len(latest))
	for origin, observed := range latest {
		items[origin] = observed.item
	}

	return items, nil
}

type kargoStage struct {
	Spec struct {
		RequestedFreight []struct {
			Origin struct {
				Kind string `json:"kind"`
				Name string `json:"name"`
			} `json:"origin"`
			Sources struct {
				Direct bool     `json:"direct"`
				Stages []string `json:"stages"`
			} `json:"sources"`
		} `json:"requestedFreight"`
	} `json:"spec"`
	Status struct {
		AutoPromotionEnabled bool `json:"autoPromotionEnabled"`
		Health               struct {
			Status string `json:"status"`
			Output []struct {
				ApplicationStatuses []struct {
					Name      string `json:"Name"`
					Namespace string `json:"Namespace"`
				} `json:"applicationStatuses"`
			} `json:"output"`
		} `json:"health"`
		FreightHistory []struct {
			Items map[string]kargoFreightReference `json:"items"`
		} `json:"freightHistory"`
		LastPromotion *struct {
			Status struct {
				Phase string                     `json:"phase"`
				State map[string]json.RawMessage `json:"state"`
			} `json:"status"`
		} `json:"lastPromotion"`
	} `json:"status"`
}

type kargoFreight struct {
	DiscoveredAt *metav1.Time `json:"discoveredAt"`
	Origin       struct {
		Kind string `json:"kind"`
		Name string `json:"name"`
	} `json:"origin"`
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

type kargoFreightReference struct {
	Name    string `json:"name"`
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

func normalizeKargoStage(item *unstructured.Unstructured) (api.PromotionStageObservation, error) {
	return normalizeKargoStageWithDirectFreight(item, nil)
}

func normalizeKargoStageWithDirectFreight(
	item *unstructured.Unstructured,
	directFreight map[string]api.PromotionFreightItem,
) (api.PromotionStageObservation, error) {
	encoded, err := json.Marshal(item.Object)
	if err != nil {
		return api.PromotionStageObservation{}, fmt.Errorf("encode unstructured Stage: %w", err)
	}
	var raw kargoStage
	if err := json.Unmarshal(encoded, &raw); err != nil {
		return api.PromotionStageObservation{}, fmt.Errorf("decode Stage: %w", err)
	}

	annotations := item.GetAnnotations()
	application := strings.TrimSpace(annotations[promotionApplicationAnnotation])
	healthStatus, argoApplicationNamespace := kargoStageHealth(raw, application)
	sourcePaths, err := kargoSourcePaths(annotations[promotionSourcePathsAnnotation])
	if err != nil {
		return api.PromotionStageObservation{}, err
	}
	requestedFreight, err := requestedKargoFreight(raw)
	if err != nil {
		return api.PromotionStageObservation{}, err
	}
	observed := api.PromotionStageObservation{
		Project:              api.CatalogName(item.GetNamespace()),
		Name:                 item.GetName(),
		KubernetesUID:        string(item.GetUID()),
		ResourceVersion:      item.GetResourceVersion(),
		Application:          application,
		Environment:          strings.TrimSpace(annotations[promotionEnvironmentAnnotation]),
		RepositoryURL:        strings.TrimSpace(annotations[promotionRepositoryAnnotation]),
		TargetBranch:         strings.TrimSpace(annotations[promotionTargetBranchAnnotation]),
		SourcePaths:          sourcePaths,
		RequestedFreight:     requestedFreight,
		Freight:              currentKargoFreight(raw),
		DirectFreight:        directKargoFreight(raw, directFreight),
		AutoPromotionEnabled: raw.Status.AutoPromotionEnabled,
		HealthStatus:         optionalNonemptyString(healthStatus),
		ArgoApplicationNamespace: optionalNonemptyString(
			argoApplicationNamespace,
		),
	}

	observed.SuccessfulRenderAcknowledgement = kargoSuccessfulRenderAcknowledgement(raw)

	for subject, value := range map[string]string{
		"project":          string(observed.Project),
		"name":             observed.Name,
		"Kubernetes UID":   observed.KubernetesUID,
		"resource version": observed.ResourceVersion,
		"application":      observed.Application,
		"environment":      observed.Environment,
		"repository URL":   observed.RepositoryURL,
		"target branch":    observed.TargetBranch,
	} {
		if value == "" {
			return api.PromotionStageObservation{}, fmt.Errorf("%s is required", subject)
		}
	}
	if len(observed.SourcePaths) == 0 {
		return api.PromotionStageObservation{}, errors.New("at least one source path is required")
	}

	return observed, nil
}

func kargoStageHealth(stage kargoStage, application string) (string, string) {
	healthStatus := strings.TrimSpace(stage.Status.Health.Status)
	if len(healthStatus) > maximumKargoHealthStatusLength {
		healthStatus = ""
	}

	applicationNamespace := ""
	for _, output := range stage.Status.Health.Output {
		for _, status := range output.ApplicationStatuses {
			if strings.TrimSpace(status.Name) != application {
				continue
			}
			namespace := strings.TrimSpace(status.Namespace)
			if namespace == "" || len(validation.IsDNS1123Label(namespace)) > 0 {
				continue
			}
			if applicationNamespace != "" && applicationNamespace != namespace {
				return healthStatus, ""
			}
			applicationNamespace = namespace
		}
	}

	return healthStatus, applicationNamespace
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

	reference := kargoFreightReference{
		Name:    item.GetName(),
		Commits: raw.Commits,
		Images:  raw.Images,
		Charts:  raw.Charts,
	}

	return observedKargoFreight{
		item:         kargoFreightItem(kind+"/"+name, reference),
		discoveredAt: discoveredAt,
	}, nil
}

func directKargoFreight(
	stage kargoStage,
	latest map[string]api.PromotionFreightItem,
) api.PromotionFreight {
	result := api.PromotionFreight{Items: []api.PromotionFreightItem{}}
	for _, request := range stage.Spec.RequestedFreight {
		if !request.Sources.Direct {
			continue
		}
		origin := strings.TrimSpace(request.Origin.Kind) + "/" + strings.TrimSpace(request.Origin.Name)
		if item, ok := latest[origin]; ok {
			result.Items = append(result.Items, item)
		}
	}
	sort.Slice(result.Items, func(i, j int) bool { return result.Items[i].Origin < result.Items[j].Origin })

	return result
}

func requestedKargoFreight(stage kargoStage) ([]api.PromotionFreightRequest, error) {
	requests := make([]api.PromotionFreightRequest, 0, len(stage.Spec.RequestedFreight))
	for _, raw := range stage.Spec.RequestedFreight {
		kind := strings.TrimSpace(raw.Origin.Kind)
		name := strings.TrimSpace(raw.Origin.Name)
		if kind == "" || name == "" {
			return nil, errors.New("requested Freight origin kind and name are required")
		}
		sources := make([]string, 0, len(raw.Sources.Stages))
		for _, source := range raw.Sources.Stages {
			if source = strings.TrimSpace(source); source != "" {
				sources = append(sources, source)
			}
		}
		sort.Strings(sources)
		requests = append(requests, api.PromotionFreightRequest{
			Origin:       kind + "/" + name,
			Direct:       raw.Sources.Direct,
			SourceStages: uniqueStrings(sources),
		})
	}
	sort.Slice(requests, func(i, j int) bool { return requests[i].Origin < requests[j].Origin })

	return requests, nil
}

func kargoSourcePaths(value string) ([]string, error) {
	var paths []string
	if err := json.Unmarshal([]byte(value), &paths); err != nil {
		return nil, fmt.Errorf("%s must be a JSON array of repository-relative paths: %w", promotionSourcePathsAnnotation, err)
	}
	if len(paths) == 0 {
		return nil, errors.New("at least one source path is required")
	}
	seen := make(map[string]struct{}, len(paths))
	for _, value := range paths {
		if value == "" || value != strings.TrimSpace(value) || strings.HasPrefix(value, "/") ||
			path.Clean(value) != value || value == ".." || strings.HasPrefix(value, "../") {
			return nil, fmt.Errorf("source path %q must be a clean repository-relative path", value)
		}
		if _, exists := seen[value]; exists {
			return nil, fmt.Errorf("source path %q is duplicated", value)
		}
		seen[value] = struct{}{}
	}

	return paths, nil
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}

	return unique
}

func currentKargoFreight(stage kargoStage) api.PromotionFreight {
	summary := api.PromotionFreight{Items: []api.PromotionFreightItem{}}
	if len(stage.Status.FreightHistory) == 0 {
		return summary
	}

	for origin, reference := range stage.Status.FreightHistory[0].Items {
		summary.Items = append(summary.Items, kargoFreightItem(origin, reference))
	}
	sort.Slice(summary.Items, func(i, j int) bool { return summary.Items[i].Origin < summary.Items[j].Origin })

	return summary
}

func kargoFreightItem(origin string, reference kargoFreightReference) api.PromotionFreightItem {
	artifacts := make(
		[]api.PromotionFreightArtifact,
		0,
		len(reference.Commits)+len(reference.Images)+len(reference.Charts),
	)
	for _, commit := range reference.Commits {
		artifacts = append(artifacts, api.PromotionFreightArtifact{
			Kind:       api.PromotionFreightArtifactKindGit,
			Repository: optionalFreightRepository(commit.RepositoryURL),
			Version:    firstNonempty(commit.Tag, commit.ID),
		})
	}
	for _, image := range reference.Images {
		artifacts = append(artifacts, api.PromotionFreightArtifact{
			Kind:       api.PromotionFreightArtifactKindImage,
			Repository: optionalFreightRepository(image.RepositoryURL),
			Version:    firstNonempty(image.Tag, image.Digest),
		})
	}
	for _, chart := range reference.Charts {
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
	return api.PromotionFreightItem{Origin: origin, Name: reference.Name, Artifacts: artifacts}
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

func kargoSuccessfulRenderAcknowledgement(stage kargoStage) *api.SuccessfulRenderAcknowledgement {
	promotion := stage.Status.LastPromotion
	if promotion == nil || promotion.Status.Phase != "Succeeded" {
		return nil
	}

	cloneState, ok := promotionStateForAlias(promotion.Status.State, "clone")
	if !ok {
		return nil
	}
	commitState, ok := promotionStateForAlias(promotion.Status.State, "commit")
	if !ok {
		return nil
	}

	var clone struct {
		Commits map[string]string `json:"commits"`
	}
	if err := json.Unmarshal(cloneState, &clone); err != nil {
		return nil
	}
	var commit struct {
		Commit string `json:"commit"`
	}
	if err := json.Unmarshal(commitState, &commit); err != nil {
		return nil
	}

	sourceRevision := strings.TrimSpace(clone.Commits["./src"])
	targetRevision := strings.TrimSpace(clone.Commits["target"])
	resultRevision := strings.TrimSpace(commit.Commit)
	if sourceRevision == "" || targetRevision == "" || resultRevision == "" {
		return nil
	}

	return &api.SuccessfulRenderAcknowledgement{
		SourceRevision: &sourceRevision,
		TargetRevision: &targetRevision,
		ResultRevision: &resultRevision,
	}
}

func promotionStateForAlias(state map[string]json.RawMessage, alias string) (json.RawMessage, bool) {
	if value, ok := state[alias]; ok {
		return value, true
	}
	keys := make([]string, 0)
	for key := range state {
		if strings.HasSuffix(key, "::"+alias) {
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		return nil, false
	}
	sort.Strings(keys)

	return state[keys[0]], true
}

func firstNonempty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}

	return ""
}
