package controller

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
	"github.com/slipramp/slipramp/internal/promotion"
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

func isPermanentKargoPromotionError(err error) bool {
	var permanent *permanentKargoPromotionError
	return errors.As(err, &permanent)
}

type kargoPromotionCreator interface {
	Ensure(context.Context, api.KargoPromotionCreationCommand) (kargoPromotionMaterialization, error)
}

type kargoPromotionReconciler interface {
	Reconcile(
		context.Context,
		api.KargoPromotionReconciliationCommand,
	) (*kargoPromotionTerminalObservation, error)
}

type kargoPromotionMaterialization struct {
	Name    string
	UID     string
	Created bool
}

type kargoPromotionTerminalObservation struct {
	Name       string
	UID        string
	Phase      api.KargoPromotionReconciliationReportPhase
	Diagnostic string
}

type dynamicKargoPromotionCreator struct {
	stages            dynamic.NamespaceableResourceInterface
	promotions        dynamic.NamespaceableResourceInterface
	allowedNamespaces map[string][]string
}

func newDynamicKargoPromotionCreator(
	configs []promotion.ObserverConfig,
	client dynamic.Interface,
) *dynamicKargoPromotionCreator {
	allowed := make(map[string][]string, len(configs))
	for _, config := range configs {
		allowed[config.Name] = slices.Clone(config.StageNamespaces)
	}

	return &dynamicKargoPromotionCreator{
		stages:            client.Resource(kargoStageResource),
		promotions:        client.Resource(kargoPromotionResource),
		allowedNamespaces: allowed,
	}
}

func (c *dynamicKargoPromotionCreator) Ensure(
	ctx context.Context,
	command api.KargoPromotionCreationCommand,
) (kargoPromotionMaterialization, error) {
	integration := string(command.Integration)
	namespace := string(command.Project)
	if !slices.Contains(c.allowedNamespaces[integration], namespace) {
		return kargoPromotionMaterialization{}, permanentKargoPromotionFailure(
			"Kargo integration %q does not observe project namespace %q",
			integration,
			namespace,
		)
	}

	stage, err := c.stages.Namespace(namespace).Get(ctx, command.Stage, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return kargoPromotionMaterialization{}, permanentKargoPromotionFailure(
				"target Kargo Stage %s/%s no longer exists",
				namespace,
				command.Stage,
			)
		}
		return kargoPromotionMaterialization{}, classifyKargoPromotionAPIError("get target Kargo Stage", err)
	}
	if string(stage.GetUID()) != command.StageUID {
		return kargoPromotionMaterialization{}, permanentKargoPromotionFailure(
			"target Kargo Stage %s/%s UID changed from %q to %q",
			namespace,
			command.Stage,
			command.StageUID,
			stage.GetUID(),
		)
	}

	existing, err := c.findKargoPromotion(ctx, namespace, command)
	if err != nil {
		return kargoPromotionMaterialization{}, err
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
			return kargoPromotionMaterialization{}, err
		}
		return kargoPromotionIdentity(created, true)
	}
	if !apierrors.IsAlreadyExists(err) {
		return kargoPromotionMaterialization{}, classifyKargoPromotionAPIError("create Kargo Promotion", err)
	}

	existing, err = c.promotions.Namespace(namespace).Get(ctx, command.ExternalName, metav1.GetOptions{})
	if err != nil {
		return kargoPromotionMaterialization{}, classifyKargoPromotionAPIError("get existing Kargo Promotion", err)
	}
	if err := validateExistingKargoPromotion(existing, command); err != nil {
		return kargoPromotionMaterialization{}, err
	}

	return kargoPromotionIdentity(existing, false)
}

func (c *dynamicKargoPromotionCreator) Reconcile(
	ctx context.Context,
	command api.KargoPromotionReconciliationCommand,
) (*kargoPromotionTerminalObservation, error) {
	integration := string(command.Integration)
	namespace := string(command.Project)
	if !slices.Contains(c.allowedNamespaces[integration], namespace) {
		return nil, fmt.Errorf(
			"kargo integration %q does not observe project namespace %q",
			integration,
			namespace,
		)
	}

	items, err := c.promotions.Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, classifyKargoPromotionAPIError("list Kargo Promotions for reconciliation", err)
	}

	var found *unstructured.Unstructured
	for index := range items.Items {
		candidate := &items.Items[index]
		if candidate.GetAnnotations()[promotionIDAnnotation] != command.ID.String() {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("multiple Kargo Promotions claim Slipramp Promotion %s", command.ID)
		}
		found = candidate
	}
	if found == nil {
		return nil, fmt.Errorf("kargo Promotion for Slipramp Promotion %s was not found", command.ID)
	}
	if found.GetName() != command.KargoName || string(found.GetUID()) != command.KargoUID {
		return nil, fmt.Errorf(
			"kargo Promotion %s/%s identity does not match canonical name %q and UID %q",
			namespace,
			found.GetName(),
			command.KargoName,
			command.KargoUID,
		)
	}

	phase, foundPhase, err := unstructured.NestedString(found.Object, "status", "phase")
	if err != nil {
		return nil, fmt.Errorf("read Kargo Promotion %s/%s phase: %w", namespace, found.GetName(), err)
	}
	if !foundPhase || !terminalKargoPromotionPhase(phase) {
		return nil, nil
	}
	message, _, err := unstructured.NestedString(found.Object, "status", "message")
	if err != nil {
		return nil, fmt.Errorf("read Kargo Promotion %s/%s message: %w", namespace, found.GetName(), err)
	}
	diagnostic := fmt.Sprintf(
		"Kargo Promotion %s/%s reached terminal phase %s before Slipramp received its callback.",
		namespace,
		found.GetName(),
		phase,
	)
	if strings.TrimSpace(message) != "" {
		diagnostic += " " + strings.TrimSpace(message)
	}

	return &kargoPromotionTerminalObservation{
		Name:       found.GetName(),
		UID:        string(found.GetUID()),
		Phase:      api.KargoPromotionReconciliationReportPhase(phase),
		Diagnostic: boundedKargoPromotionDiagnostic(diagnostic),
	}, nil
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

func (c *dynamicKargoPromotionCreator) findKargoPromotion(
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
) (kargoPromotionMaterialization, error) {
	name := object.GetName()
	uid := string(object.GetUID())
	if name == "" || uid == "" {
		return kargoPromotionMaterialization{}, errors.New("kargo promotion has no admitted name or Kubernetes UID")
	}

	return kargoPromotionMaterialization{Name: name, UID: uid, Created: created}, nil
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

func (c *Controller) handleKargoPromotionCreation(
	ctx context.Context,
	command api.KargoPromotionCreationCommand,
) error {
	var materialization kargoPromotionMaterialization
	var materializeErr error
	if c.promotions == nil {
		materializeErr = permanentKargoPromotionFailure(
			"Kargo integration %q is not configured on this Controller",
			command.Integration,
		)
	} else {
		materialization, materializeErr = c.promotions.Ensure(ctx, command)
	}

	report := api.KargoPromotionDispatchReport{Attempt: command.Attempt}
	if materializeErr == nil {
		report.Result = api.KargoPromotionDispatchReportResultCreated
		report.KargoName = &materialization.Name
		report.KargoUID = &materialization.UID
	} else {
		diagnostic := boundedKargoPromotionDiagnostic(materializeErr.Error())
		report.Diagnostic = &diagnostic
		if isPermanentKargoPromotionError(materializeErr) {
			report.Result = api.KargoPromotionDispatchReportResultFailed
		} else {
			report.Result = api.KargoPromotionDispatchReportResultRetryable
		}
	}

	reportErr := c.promotionReports.ReportKargoPromotionDispatch(ctx, command.ID, report)
	if errors.Is(reportErr, errControllerCredentialRejected) {
		return reportErr
	}
	if reportErr != nil {
		c.logger.Warn(
			"report Kargo Promotion creation",
			"promotion_id", command.ID,
			"attempt", command.Attempt,
			"error", reportErr,
		)

		return nil
	}
	if materializeErr != nil {
		c.logger.Warn(
			"materialize Kargo Promotion",
			"promotion_id", command.ID,
			"attempt", command.Attempt,
			"terminal", isPermanentKargoPromotionError(materializeErr),
			"error", materializeErr,
		)

		return nil
	}

	c.logger.Info(
		"Kargo Promotion materialized",
		"promotion_id", command.ID,
		"kargo_name", materialization.Name,
		"kargo_uid", materialization.UID,
		"created", materialization.Created,
	)

	return nil
}

func (c *Controller) handleKargoPromotionReconciliation(
	ctx context.Context,
	command api.KargoPromotionReconciliationCommand,
) error {
	if c.promotionReconciler == nil {
		c.logger.Warn(
			"reconcile Kargo Promotion without a configured integration",
			"promotion_id", command.ID,
			"integration", command.Integration,
		)

		return nil
	}

	observation, err := c.promotionReconciler.Reconcile(ctx, command)
	if err != nil {
		c.logger.Warn(
			"reconcile Kargo Promotion",
			"promotion_id", command.ID,
			"kargo_name", command.KargoName,
			"kargo_uid", command.KargoUID,
			"error", err,
		)

		return nil
	}
	if observation == nil {
		return nil
	}

	reportErr := c.promotionReconciliationReports.ReportKargoPromotionReconciliation(
		ctx,
		command.ID,
		api.KargoPromotionReconciliationReport{
			KargoName:  observation.Name,
			KargoUID:   observation.UID,
			Phase:      observation.Phase,
			Diagnostic: observation.Diagnostic,
		},
	)
	if errors.Is(reportErr, errControllerCredentialRejected) {
		return reportErr
	}
	if reportErr != nil {
		c.logger.Warn(
			"report Kargo Promotion reconciliation",
			"promotion_id", command.ID,
			"phase", observation.Phase,
			"error", reportErr,
		)

		return nil
	}

	c.logger.Info(
		"Kargo Promotion terminal phase reconciled",
		"promotion_id", command.ID,
		"kargo_name", observation.Name,
		"kargo_uid", observation.UID,
		"phase", observation.Phase,
	)

	return nil
}

func boundedKargoPromotionDiagnostic(value string) string {
	const maximumRunes = 4096
	value = strings.Map(func(character rune) rune {
		if unicode.IsControl(character) {
			return ' '
		}

		return character
	}, value)
	value = strings.Join(strings.Fields(value), " ")
	if value == "" {
		value = "Kargo Promotion creation failed without a diagnostic."
	}
	runes := []rune(value)
	if len(runes) <= maximumRunes {
		return value
	}

	return string(runes[:maximumRunes])
}
