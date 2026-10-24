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
	"k8s.io/client-go/dynamic"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/promotion"
)

const (
	kargoStageListLimit            = 500
	kargoStageObservationPeriod    = 30 * time.Second
	promotionSourcePathsAnnotation = "slipramp.com/promotion-source-paths"
)

var kargoStageResource = schema.GroupVersionResource{
	Group:    "kargo.akuity.io",
	Version:  "v1alpha1",
	Resource: "stages",
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
	for _, config := range configs {
		observers = append(observers, &kargoStageObserver{
			integration: config.Name,
			namespaces:  slices.Clone(config.StageNamespaces),
			lister:      lister,
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
				observed, err := normalizeKargoStage(item)
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
		PromotionTemplate struct {
			Spec struct {
				Variables []struct {
					Name  string `json:"name"`
					Value string `json:"value"`
				} `json:"vars"`
			} `json:"spec"`
		} `json:"promotionTemplate"`
	} `json:"spec"`
	Status struct {
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

type successfulRenderAcknowledgement struct {
	SourceRevision string `json:"sourceRevision"`
	TargetRevision string `json:"targetRevision"`
	ResultRevision string `json:"resultRevision"`
}

func normalizeKargoStage(item *unstructured.Unstructured) (api.PromotionStageObservation, error) {
	encoded, err := json.Marshal(item.Object)
	if err != nil {
		return api.PromotionStageObservation{}, fmt.Errorf("encode unstructured Stage: %w", err)
	}
	var raw kargoStage
	if err := json.Unmarshal(encoded, &raw); err != nil {
		return api.PromotionStageObservation{}, fmt.Errorf("decode Stage: %w", err)
	}

	variables := kargoVariables(raw)
	application := firstNonempty(variables["argoApp"], variables["app"], item.GetLabels()["app"], item.GetName())
	repositoryURL, targetBranch, sourcePaths := kargoBranchDetails(item, variables)
	requestedFreight, err := requestedKargoFreight(raw)
	if err != nil {
		return api.PromotionStageObservation{}, err
	}
	observed := api.PromotionStageObservation{
		Project:          api.CatalogName(item.GetNamespace()),
		Name:             item.GetName(),
		KubernetesUID:    string(item.GetUID()),
		ResourceVersion:  item.GetResourceVersion(),
		Application:      application,
		Environment:      strings.TrimSpace(item.GetLabels()["env"]),
		RepositoryURL:    repositoryURL,
		TargetBranch:     targetBranch,
		SourcePaths:      sourcePaths,
		RequestedFreight: requestedFreight,
		Freight:          currentKargoFreight(raw),
	}

	acknowledgement := kargoSuccessfulRenderAcknowledgement(raw)
	if acknowledgement != nil {
		value, objectErr := jsonObject(acknowledgement)
		if objectErr != nil {
			return api.PromotionStageObservation{}, objectErr
		}
		observed.SuccessfulRenderAcknowledgement = &value
	}

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

func kargoVariables(stage kargoStage) map[string]string {
	variables := make(map[string]string)
	for _, variable := range stage.Spec.PromotionTemplate.Spec.Variables {
		variables[variable.Name] = strings.TrimSpace(variable.Value)
	}

	return variables
}

func kargoBranchDetails(
	item *unstructured.Unstructured,
	variables map[string]string,
) (string, string, []string) {
	application := variables["app"]
	environment := variables["overlay"]
	if application == "" || environment == "" {
		return variables["gitopsRepo"], "", nil
	}

	kustomizePath := variables["kustomizePath"]
	if kustomizePath == "" {
		kustomizePath = application
	}
	kustomizePath, ok := cleanRepositoryPath(kustomizePath)
	if !ok {
		return variables["gitopsRepo"], "", nil
	}
	root := path.Join("k8s", kustomizePath)

	paths := []string{
		path.Join(root, "base"),
		path.Join(root, "overlays", environment),
	}
	stageBranchKustomization := variables["stageBranchKustomization"]
	if stageBranchKustomization == "" {
		stageBranchKustomization = "k8s/kargo/stage-branch/kustomization.yaml"
	}
	if cleaned, valid := cleanRepositoryPath(stageBranchKustomization); valid {
		paths = append(paths, cleaned)
	}
	for _, candidate := range strings.FieldsFunc(
		item.GetAnnotations()[promotionSourcePathsAnnotation],
		func(value rune) bool { return value == ',' || value == '\n' },
	) {
		if cleaned, valid := cleanRepositoryPath(candidate); valid {
			paths = append(paths, cleaned)
		}
	}

	return variables["gitopsRepo"], "stage/" + application + "/" + environment, uniqueStrings(paths)
}

func cleanRepositoryPath(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "/") {
		return "", false
	}
	cleaned := path.Clean(value)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", false
	}

	return cleaned, true
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
		summary.Items = append(summary.Items, api.PromotionFreightItem{
			Origin:    origin,
			Name:      reference.Name,
			Artifacts: artifacts,
		})
	}
	sort.Slice(summary.Items, func(i, j int) bool { return summary.Items[i].Origin < summary.Items[j].Origin })

	return summary
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

func kargoSuccessfulRenderAcknowledgement(stage kargoStage) *successfulRenderAcknowledgement {
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

	acknowledgement := &successfulRenderAcknowledgement{
		SourceRevision: strings.TrimSpace(clone.Commits["./src"]),
		TargetRevision: strings.TrimSpace(clone.Commits["target"]),
		ResultRevision: strings.TrimSpace(commit.Commit),
	}
	if acknowledgement.SourceRevision == "" || acknowledgement.TargetRevision == "" ||
		acknowledgement.ResultRevision == "" {
		return nil
	}

	return acknowledgement
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

func jsonObject(value any) (map[string]any, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode normalized Kargo Stage data: %w", err)
	}
	var object map[string]any
	if err := json.Unmarshal(encoded, &object); err != nil {
		return nil, fmt.Errorf("decode normalized Kargo Stage data: %w", err)
	}

	return object, nil
}

func firstNonempty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}

	return ""
}
