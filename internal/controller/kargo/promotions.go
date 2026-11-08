package kargo

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/slipramp/slipramp/internal/api"
)

const promotionIDAnnotation = "slipramp.com/promotion-id"

var kargoPromotionResource = schema.GroupVersionResource{
	Group:    "kargo.akuity.io",
	Version:  "v1alpha1",
	Resource: "promotions",
}

type permanentKargoPromotionError struct {
	err error
}

func (e *permanentKargoPromotionError) Error() string { return e.err.Error() }
func (e *permanentKargoPromotionError) Unwrap() error { return e.err }

func permanentKargoPromotionFailure(format string, values ...any) error {
	return &permanentKargoPromotionError{err: fmt.Errorf(format, values...)}
}

func IsPermanentPromotionError(err error) bool {
	var permanent *permanentKargoPromotionError
	return errors.As(err, &permanent)
}

type PromotionMaterialization struct {
	Name    string
	UID     string
	Created bool
}

type PromotionObservation struct {
	Name       string
	UID        string
	Phase      *api.KargoPromotionReconciliationReportPhase
	Diagnostic *string
}

type PromotionManager struct {
	stages            dynamic.NamespaceableResourceInterface
	promotions        dynamic.NamespaceableResourceInterface
	allowedNamespaces map[string][]string
}

func NewPromotionManager(
	config Config,
	client dynamic.Interface,
) *PromotionManager {
	allowed := make(map[string][]string, len(config.Integrations))
	for _, integration := range config.Integrations {
		allowed[integration.Name] = slices.Clone(integration.StageNamespaces)
	}

	return &PromotionManager{
		stages:            client.Resource(kargoStageResource),
		promotions:        client.Resource(kargoPromotionResource),
		allowedNamespaces: allowed,
	}
}

func (c *PromotionManager) Ensure(
	ctx context.Context,
	command api.KargoPromotionCreationCommand,
) (PromotionMaterialization, error) {
	integration := string(command.Integration)
	namespace := string(command.Project)
	if !slices.Contains(c.allowedNamespaces[integration], namespace) {
		return PromotionMaterialization{}, permanentKargoPromotionFailure(
			"Kargo integration %q does not observe project namespace %q",
			integration,
			namespace,
		)
	}

	stage, err := c.stages.Namespace(namespace).Get(ctx, command.Stage, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return PromotionMaterialization{}, permanentKargoPromotionFailure(
				"target Kargo Stage %s/%s no longer exists",
				namespace,
				command.Stage,
			)
		}
		return PromotionMaterialization{}, classifyKargoPromotionAPIError("get target Kargo Stage", err)
	}
	if string(stage.GetUID()) != command.StageUID {
		return PromotionMaterialization{}, permanentKargoPromotionFailure(
			"target Kargo Stage %s/%s UID changed from %q to %q",
			namespace,
			command.Stage,
			command.StageUID,
			stage.GetUID(),
		)
	}

	existing, err := c.findKargoPromotion(ctx, namespace, command)
	if err != nil {
		return PromotionMaterialization{}, err
	}
	if existing != nil {
		return kargoPromotionIdentity(existing, false)
	}

	desired := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "kargo.akuity.io/v1alpha1",
		"kind":       "Promotion",
		"metadata": map[string]any{
			"name":      command.ExternalName,
			"namespace": namespace,
			"annotations": map[string]any{
				promotionIDAnnotation: command.ID.String(),
			},
		},
		"spec": map[string]any{
			"stage":   command.Stage,
			"freight": command.Freight,
		},
	}}
	created, err := c.promotions.Namespace(namespace).Create(ctx, desired, metav1.CreateOptions{})
	if err == nil {
		if err := validateExistingKargoPromotion(created, command); err != nil {
			return PromotionMaterialization{}, err
		}
		return kargoPromotionIdentity(created, true)
	}
	if !apierrors.IsAlreadyExists(err) {
		return PromotionMaterialization{}, classifyKargoPromotionAPIError("create Kargo Promotion", err)
	}

	existing, err = c.promotions.Namespace(namespace).Get(ctx, command.ExternalName, metav1.GetOptions{})
	if err != nil {
		return PromotionMaterialization{}, classifyKargoPromotionAPIError("get existing Kargo Promotion", err)
	}
	if err := validateExistingKargoPromotion(existing, command); err != nil {
		return PromotionMaterialization{}, err
	}

	return kargoPromotionIdentity(existing, false)
}

func (c *PromotionManager) Reconcile(
	ctx context.Context,
	command api.KargoPromotionReconciliationCommand,
) (*PromotionObservation, error) {
	integration := string(command.Integration)
	namespace := string(command.Project)
	if !slices.Contains(c.allowedNamespaces[integration], namespace) {
		return nil, fmt.Errorf(
			"kargo integration %q does not observe project namespace %q",
			integration,
			namespace,
		)
	}

	object, err := c.promotions.Namespace(namespace).Get(ctx, command.KargoName, metav1.GetOptions{})
	if err != nil {
		return nil, classifyKargoPromotionAPIError("get Kargo Promotion for reconciliation", err)
	}
	if object.GetName() != command.KargoName || (object.GetNamespace() != "" && object.GetNamespace() != namespace) {
		return nil, fmt.Errorf("kargo Promotion identity does not match %s/%s", namespace, command.KargoName)
	}
	stage, stageFound, err := unstructured.NestedString(object.Object, "spec", "stage")
	if err != nil || !stageFound || stage != command.Stage {
		return nil, fmt.Errorf(
			"kargo Promotion %s/%s Stage does not match canonical Stage %q",
			namespace,
			command.KargoName,
			command.Stage,
		)
	}
	annotation := object.GetAnnotations()[promotionIDAnnotation]
	if (command.Source == api.Slipramp && annotation != command.ID.String()) ||
		(command.Source == api.Kargo && annotation != "" && annotation != command.ID.String()) {
		return nil, fmt.Errorf(
			"kargo Promotion %s/%s Slipramp identity annotation conflicts with Promotion %s",
			namespace,
			command.KargoName,
			command.ID,
		)
	}
	uid := string(object.GetUID())
	if uid == "" {
		return nil, fmt.Errorf("kargo Promotion %s/%s has no Kubernetes UID", namespace, command.KargoName)
	}
	if command.KargoUID != nil && uid != *command.KargoUID {
		return nil, fmt.Errorf(
			"kargo Promotion %s/%s UID %q does not match canonical UID %q",
			namespace,
			command.KargoName,
			uid,
			*command.KargoUID,
		)
	}

	observation := &PromotionObservation{Name: command.KargoName, UID: uid}
	phase, foundPhase, err := unstructured.NestedString(object.Object, "status", "phase")
	if err != nil {
		return nil, fmt.Errorf("read Kargo Promotion %s/%s phase: %w", namespace, command.KargoName, err)
	}
	if !foundPhase || !terminalKargoPromotionPhase(phase) {
		return observation, nil
	}
	terminalPhase := api.KargoPromotionReconciliationReportPhase(phase)
	observation.Phase = &terminalPhase
	message, _, err := unstructured.NestedString(object.Object, "status", "message")
	if err != nil {
		return nil, fmt.Errorf("read Kargo Promotion %s/%s message: %w", namespace, command.KargoName, err)
	}
	if strings.TrimSpace(message) != "" {
		diagnostic := BoundedPromotionDiagnostic(message)
		if diagnostic != "" {
			observation.Diagnostic = &diagnostic
		}
	}

	return observation, nil
}

func terminalKargoPromotionPhase(phase string) bool {
	switch api.KargoPromotionReconciliationReportPhase(phase) {
	case api.KargoPromotionReconciliationReportPhaseFailed,
		api.KargoPromotionReconciliationReportPhaseErrored,
		api.KargoPromotionReconciliationReportPhaseAborted,
		api.KargoPromotionReconciliationReportPhaseSucceeded:
		return true
	default:
		return false
	}
}

func (c *PromotionManager) findKargoPromotion(
	ctx context.Context,
	namespace string,
	command api.KargoPromotionCreationCommand,
) (*unstructured.Unstructured, error) {
	items, err := c.promotions.Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, classifyKargoPromotionAPIError("list Kargo Promotions", err)
	}

	var found *unstructured.Unstructured
	for index := range items.Items {
		candidate := &items.Items[index]
		if candidate.GetAnnotations()[promotionIDAnnotation] != command.ID.String() {
			continue
		}
		if found != nil {
			return nil, permanentKargoPromotionFailure(
				"multiple Kargo Promotions claim Slipramp Promotion %s",
				command.ID,
			)
		}
		found = candidate
	}
	if found == nil {
		return nil, nil
	}
	if err := validateExistingKargoPromotion(found, command); err != nil {
		return nil, err
	}

	return found, nil
}

func kargoPromotionIdentity(
	object *unstructured.Unstructured,
	created bool,
) (PromotionMaterialization, error) {
	name := object.GetName()
	uid := string(object.GetUID())
	if name == "" || uid == "" {
		return PromotionMaterialization{}, errors.New("kargo promotion has no admitted name or Kubernetes UID")
	}

	return PromotionMaterialization{Name: name, UID: uid, Created: created}, nil
}

func validateExistingKargoPromotion(
	existing *unstructured.Unstructured,
	command api.KargoPromotionCreationCommand,
) error {
	annotation := existing.GetAnnotations()[promotionIDAnnotation]
	stage, stageFound, stageErr := unstructured.NestedString(existing.Object, "spec", "stage")
	freight, freightFound, freightErr := unstructured.NestedString(existing.Object, "spec", "freight")
	if stageErr != nil || freightErr != nil || !stageFound || !freightFound ||
		annotation != command.ID.String() || stage != command.Stage || freight != command.Freight {
		return permanentKargoPromotionFailure(
			"existing Kargo Promotion %s/%s does not match Slipramp Promotion %s",
			existing.GetNamespace(),
			existing.GetName(),
			command.ID,
		)
	}

	return nil
}

func classifyKargoPromotionAPIError(operation string, err error) error {
	wrapped := fmt.Errorf("%s: %w", operation, err)
	if apierrors.IsBadRequest(err) || apierrors.IsForbidden(err) || apierrors.IsInvalid(err) ||
		apierrors.IsUnauthorized(err) || apierrors.IsMethodNotSupported(err) {
		return &permanentKargoPromotionError{err: wrapped}
	}

	return wrapped
}

func BoundedPromotionDiagnostic(value string) string {
	const maximumRunes = 4096
	value = strings.Map(func(character rune) rune {
		if unicode.IsControl(character) {
			return ' '
		}

		return character
	}, value)
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) <= maximumRunes {
		return value
	}

	return string(runes[:maximumRunes])
}
