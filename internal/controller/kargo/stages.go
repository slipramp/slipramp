package kargo

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"slices"
	"sort"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/slipramp/slipramp/internal/api"
)

const (
	maximumKargoHealthStatusLength  = 200
	promotionApplicationAnnotation  = "slipramp.com/promotion-application"
	promotionEnvironmentAnnotation  = "slipramp.com/promotion-environment"
	promotionRepositoryAnnotation   = "slipramp.com/promotion-repository-url"
	promotionTargetBranchAnnotation = "slipramp.com/promotion-target-branch"
	promotionSourcePathsAnnotation  = "slipramp.com/promotion-source-paths"
)

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

func normalizeKargoStage(
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
	candidates := api.PromotionFreight{Items: []api.PromotionFreightItem{}}
	for _, request := range requestedFreight {
		candidates.Items = append(candidates.Items, candidateFreight[request.Origin]...)
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
		Freight:              currentKargoFreight(raw, candidateFreight),
		CandidateFreight:     candidates,
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
			SourceStages:         slices.Compact(sources),
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

func optionalNonemptyString(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	return &value
}
