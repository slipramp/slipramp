package controller

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

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
	Ensure(context.Context, api.KargoPromotionCreationCommand) (string, bool, error)
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
) (string, bool, error) {
	integration := string(command.Integration)
	namespace := string(command.Project)
	if !slices.Contains(c.allowedNamespaces[integration], namespace) {
		return "", false, permanentKargoPromotionFailure(
			"Kargo integration %q does not observe project namespace %q",
			integration,
			namespace,
		)
	}

	stage, err := c.stages.Namespace(namespace).Get(ctx, command.Stage, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return "", false, permanentKargoPromotionFailure(
				"target Kargo Stage %s/%s no longer exists",
				namespace,
				command.Stage,
			)
		}
		return "", false, classifyKargoPromotionAPIError("get target Kargo Stage", err)
	}
	if string(stage.GetUID()) != command.StageUID {
		return "", false, permanentKargoPromotionFailure(
			"target Kargo Stage %s/%s UID changed from %q to %q",
			namespace,
			command.Stage,
			command.StageUID,
			stage.GetUID(),
		)
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
		uid := string(created.GetUID())
		if uid == "" {
			return "", false, errors.New("created Kargo Promotion has no Kubernetes UID")
		}

		return uid, true, nil
	}
	if !apierrors.IsAlreadyExists(err) {
		return "", false, classifyKargoPromotionAPIError("create Kargo Promotion", err)
	}

	existing, err := c.promotions.Namespace(namespace).Get(ctx, command.ExternalName, metav1.GetOptions{})
	if err != nil {
		return "", false, classifyKargoPromotionAPIError("get existing Kargo Promotion", err)
	}
	if err := validateExistingKargoPromotion(existing, command); err != nil {
		return "", false, err
	}
	uid := string(existing.GetUID())
	if uid == "" {
		return "", false, permanentKargoPromotionFailure("existing Kargo Promotion has no Kubernetes UID")
	}

	return uid, false, nil
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
	var uid string
	var created bool
	var materializeErr error
	if c.promotions == nil {
		materializeErr = permanentKargoPromotionFailure(
			"Kargo integration %q is not configured on this Controller",
			command.Integration,
		)
	} else {
		uid, created, materializeErr = c.promotions.Ensure(ctx, command)
	}

	report := api.KargoPromotionDispatchReport{Attempt: command.Attempt}
	if materializeErr == nil {
		report.Result = api.KargoPromotionDispatchReportResultCreated
		report.KargoUID = &uid
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
		"kargo_uid", uid,
		"created", created,
	)

	return nil
}

func boundedKargoPromotionDiagnostic(value string) string {
	const maximumRunes = 4096
	value = strings.TrimSpace(value)
	if value == "" {
		value = "Kargo Promotion creation failed without a diagnostic."
	}
	runes := []rune(value)
	if len(runes) <= maximumRunes {
		return value
	}

	return string(runes[:maximumRunes])
}
