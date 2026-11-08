package kargo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
)

const (
	kargoStageListLimit             = 500
	kargoFreightListLimit           = 500
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

// StageSnapshotter lists the configured Kargo resources and constructs a
// complete Stage snapshot for one integration.
type StageSnapshotter struct {
	integration string
	namespaces  []string
	lister      kargoStagePageLister
	freight     kargoStagePageLister
	newID       func() uuid.UUID
	now         func() time.Time
}

func NewStageSnapshotters(config Config, kubeClient dynamic.Interface) []*StageSnapshotter {
	snapshotters := make([]*StageSnapshotter, 0, len(config.Integrations))
	lister := dynamicKargoStageLister{resources: kubeClient.Resource(kargoStageResource)}
	freight := dynamicKargoStageLister{resources: kubeClient.Resource(kargoFreightResource)}
	for _, integration := range config.Integrations {
		snapshotters = append(snapshotters, &StageSnapshotter{
			integration: integration.Name,
			namespaces:  slices.Clone(integration.StageNamespaces),
			lister:      lister,
			freight:     freight,
			newID:       uuid.New,
			now:         time.Now,
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
				observed, err := normalizeKargoStageWithCandidateFreight(item, candidateFreight)
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

func (o *StageSnapshotter) candidateFreight(
	ctx context.Context,
	namespace string,
) (map[string][]api.PromotionFreightItem, error) {
	observedByOrigin := make(map[string][]observedKargoFreight)
	if o.freight == nil {
		return map[string][]api.PromotionFreightItem{}, nil
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
			observedByOrigin[observed.item.Origin] = append(observedByOrigin[observed.item.Origin], observed)
		}

		continueToken = page.GetContinue()
		if continueToken == "" {
			break
		}
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

type kargoStage struct {
	Spec struct {
		RequestedFreight []struct {
			Origin struct {
				Kind string `json:"kind"`
				Name string `json:"name"`
			} `json:"origin"`
			Sources struct {
				Direct               bool     `json:"direct"`
				Stages               []string `json:"stages"`
				AvailabilityStrategy string   `json:"availabilityStrategy"`
				RequiredSoakTime     string   `json:"requiredSoakTime"`
			} `json:"sources"`
		} `json:"requestedFreight"`
		PromotionTemplate *struct {
			Spec struct {
				Steps []json.RawMessage `json:"steps"`
			} `json:"spec"`
		} `json:"promotionTemplate"`
	} `json:"spec"`
	Status struct {
		Conditions           []metav1.Condition `json:"conditions"`
		AutoPromotionEnabled bool               `json:"autoPromotionEnabled"`
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
		CurrentPromotion *kargoPromotionReference `json:"currentPromotion"`
		LastPromotion    *kargoPromotionReference `json:"lastPromotion"`
	} `json:"status"`
}

type kargoPromotionReference struct {
	Name   string `json:"name"`
	Status struct {
		Phase string                     `json:"phase"`
		State map[string]json.RawMessage `json:"state"`
	} `json:"status"`
	FinishedAt *metav1.Time `json:"finishedAt"`
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
	return normalizeKargoStageWithCandidateFreight(item, nil)
}

func normalizeKargoStageWithCandidateFreight(
	item *unstructured.Unstructured,
	candidateFreight map[string][]api.PromotionFreightItem,
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
	var sourcePaths *[]string
	if value := strings.TrimSpace(annotations[promotionSourcePathsAnnotation]); value != "" {
		parsed, parseErr := kargoSourcePaths(value)
		if parseErr != nil {
			return api.PromotionStageObservation{}, parseErr
		}
		sourcePaths = &parsed
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
		Application:          optionalNonemptyString(application),
		Environment:          optionalNonemptyString(strings.TrimSpace(annotations[promotionEnvironmentAnnotation])),
		RepositoryURL:        optionalNonemptyString(strings.TrimSpace(annotations[promotionRepositoryAnnotation])),
		TargetBranch:         optionalNonemptyString(strings.TrimSpace(annotations[promotionTargetBranchAnnotation])),
		SourcePaths:          sourcePaths,
		RequestedFreight:     requestedFreight,
		Freight:              currentKargoFreight(raw),
		CandidateFreight:     candidateKargoFreight(raw, candidateFreight),
		AutoPromotionEnabled: raw.Status.AutoPromotionEnabled,
		ControlFlow:          raw.Spec.PromotionTemplate == nil || len(raw.Spec.PromotionTemplate.Spec.Steps) == 0,
		Conditions:           kargoStageConditions(raw),
		CurrentPromotion:     observedStagePromotionReference(raw.Status.CurrentPromotion),
		LastPromotion:        observedStagePromotionReference(raw.Status.LastPromotion),
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
	} {
		if value == "" {
			return api.PromotionStageObservation{}, fmt.Errorf("%s is required", subject)
		}
	}
	return observed, nil
}

func kargoStageConditions(stage kargoStage) []api.PromotionStageCondition {
	conditions := make([]api.PromotionStageCondition, 0, len(stage.Status.Conditions))
	for _, condition := range stage.Status.Conditions {
		var transitionedAt *time.Time
		if !condition.LastTransitionTime.IsZero() {
			value := condition.LastTransitionTime.Time
			transitionedAt = &value
		}
		conditions = append(conditions, api.PromotionStageCondition{
			Type:               condition.Type,
			Status:             string(condition.Status),
			Reason:             optionalNonemptyString(strings.TrimSpace(condition.Reason)),
			Message:            optionalNonemptyString(strings.TrimSpace(condition.Message)),
			LastTransitionTime: transitionedAt,
		})
	}
	sort.Slice(conditions, func(i, j int) bool { return conditions[i].Type < conditions[j].Type })

	return conditions
}

func observedStagePromotionReference(value *kargoPromotionReference) *api.PromotionStagePromotionReference {
	if value == nil || strings.TrimSpace(value.Name) == "" {
		return nil
	}
	var finishedAt *time.Time
	if value.FinishedAt != nil && !value.FinishedAt.IsZero() {
		observed := value.FinishedAt.Time
		finishedAt = &observed
	}

	return &api.PromotionStagePromotionReference{
		Name:       value.Name,
		Phase:      optionalNonemptyString(strings.TrimSpace(value.Status.Phase)),
		FinishedAt: finishedAt,
	}
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
	freight := kargoFreightItem(kind+"/"+name, reference)
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

func candidateKargoFreight(
	stage kargoStage,
	candidates map[string][]api.PromotionFreightItem,
) api.PromotionFreight {
	result := api.PromotionFreight{Items: []api.PromotionFreightItem{}}
	for _, request := range stage.Spec.RequestedFreight {
		origin := strings.TrimSpace(request.Origin.Kind) + "/" + strings.TrimSpace(request.Origin.Name)
		result.Items = append(result.Items, candidates[origin]...)
	}
	sort.SliceStable(result.Items, func(i, j int) bool { return result.Items[i].Origin < result.Items[j].Origin })

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
		strategy := api.PromotionFreightRequestAvailabilityStrategyOneOf
		if raw.Sources.AvailabilityStrategy == "All" {
			strategy = api.PromotionFreightRequestAvailabilityStrategyAll
		} else if raw.Sources.AvailabilityStrategy != "" && raw.Sources.AvailabilityStrategy != "OneOf" {
			return nil, fmt.Errorf("requested Freight availability strategy %q is invalid", raw.Sources.AvailabilityStrategy)
		}
		var requiredSoakTime *string
		if value := strings.TrimSpace(raw.Sources.RequiredSoakTime); value != "" {
			if _, err := time.ParseDuration(value); err != nil {
				return nil, fmt.Errorf("requested Freight soak time %q is invalid: %w", value, err)
			}
			requiredSoakTime = &value
		}
		requests = append(requests, api.PromotionFreightRequest{
			Origin:               kind + "/" + name,
			Direct:               raw.Sources.Direct,
			SourceStages:         uniqueStrings(sources),
			AvailabilityStrategy: strategy,
			RequiredSoakTime:     requiredSoakTime,
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

func optionalNonemptyString(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	return &value
}
