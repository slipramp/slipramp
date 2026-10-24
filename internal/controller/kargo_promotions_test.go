package controller

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubetesting "k8s.io/client-go/testing"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/promotion"
)

type kargoPromotionCreatorFunc func(
	context.Context,
	api.KargoPromotionCreationCommand,
) (string, bool, error)

func (f kargoPromotionCreatorFunc) Ensure(
	ctx context.Context,
	command api.KargoPromotionCreationCommand,
) (string, bool, error) {
	return f(ctx, command)
}

type kargoPromotionDispatchReporterFunc func(
	context.Context,
	uuid.UUID,
	api.KargoPromotionDispatchReport,
) error

func (f kargoPromotionDispatchReporterFunc) ReportKargoPromotionDispatch(
	ctx context.Context,
	promotionID uuid.UUID,
	report api.KargoPromotionDispatchReport,
) error {
	return f(ctx, promotionID, report)
}

func TestKargoPromotionCreatorCreatesAndRecoversMatchingObject(t *testing.T) {
	t.Parallel()

	stage := testKargoStage("stage-uid")
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), stage)
	client.PrependReactor("create", "promotions", func(action kubetesting.Action) (bool, runtime.Object, error) {
		create := action.(kubetesting.CreateAction)
		object := create.GetObject().(*unstructured.Unstructured).DeepCopy()
		object.SetUID(types.UID("promotion-uid"))
		err := client.Tracker().Create(kargoPromotionResource, object, create.GetNamespace())
		return true, object, err
	})
	creator := newDynamicKargoPromotionCreator(
		[]promotion.ObserverConfig{{Name: "prod", StageNamespaces: []string{"kargo-core"}}},
		client,
	)
	command := testKargoPromotionCommand()

	uid, created, err := creator.Ensure(t.Context(), command)
	if err != nil {
		t.Fatalf("first Ensure() error = %v", err)
	}
	if uid != "promotion-uid" || !created {
		t.Errorf("first Ensure() = (%q, %t)", uid, created)
	}

	uid, created, err = creator.Ensure(t.Context(), command)
	if err != nil {
		t.Fatalf("repeated Ensure() error = %v", err)
	}
	if uid != "promotion-uid" || created {
		t.Errorf("repeated Ensure() = (%q, %t)", uid, created)
	}
	stored, err := client.Resource(kargoPromotionResource).Namespace("kargo-core").Get(
		t.Context(),
		command.ExternalName,
		metav1.GetOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if stored.GetAnnotations()[promotionIDAnnotation] != command.ID.String() {
		t.Errorf("Promotion annotations = %#v", stored.GetAnnotations())
	}
}

func TestKargoPromotionCreatorRejectsIdentityAndStageConflicts(t *testing.T) {
	t.Parallel()

	command := testKargoPromotionCommand()
	existing := testKargoPromotion(command, "different-freight")
	existing.SetUID(types.UID("promotion-uid"))
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), testKargoStage("stage-uid"), existing)
	creator := newDynamicKargoPromotionCreator(
		[]promotion.ObserverConfig{{Name: "prod", StageNamespaces: []string{"kargo-core"}}},
		client,
	)
	if _, _, err := creator.Ensure(t.Context(), command); !isPermanentKargoPromotionError(err) {
		t.Fatalf("collision error = %v, want permanent", err)
	}

	client = dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), testKargoStage("new-stage-uid"))
	creator = newDynamicKargoPromotionCreator(
		[]promotion.ObserverConfig{{Name: "prod", StageNamespaces: []string{"kargo-core"}}},
		client,
	)
	if _, _, err := creator.Ensure(t.Context(), command); !isPermanentKargoPromotionError(err) {
		t.Fatalf("Stage UID error = %v, want permanent", err)
	}
}

func TestKargoPromotionCreatorClassifiesAdmissionFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		err       error
		permanent bool
	}{
		{
			name: "forbidden",
			err: apierrors.NewForbidden(
				schema.GroupResource{Group: "kargo.akuity.io", Resource: "promotions"},
				"slipramp-test",
				errors.New("admission denied"),
			),
			permanent: true,
		},
		{
			name: "unavailable",
			err:  apierrors.NewServiceUnavailable("apiserver unavailable"),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), testKargoStage("stage-uid"))
			client.PrependReactor("create", "promotions", func(kubetesting.Action) (bool, runtime.Object, error) {
				return true, nil, test.err
			})
			creator := newDynamicKargoPromotionCreator(
				[]promotion.ObserverConfig{{Name: "prod", StageNamespaces: []string{"kargo-core"}}},
				client,
			)
			_, _, err := creator.Ensure(t.Context(), testKargoPromotionCommand())
			if isPermanentKargoPromotionError(err) != test.permanent {
				t.Errorf("Ensure() error = %v, permanent = %t", err, isPermanentKargoPromotionError(err))
			}
		})
	}
}

func TestControllerReportsKargoPromotionCreationResults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		ensureUID  string
		ensureErr  error
		result     api.KargoPromotionDispatchReportResult
		reportErr  error
		wantHandle error
	}{
		{
			name:      "created",
			ensureUID: "promotion-uid",
			result:    api.KargoPromotionDispatchReportResultCreated,
		},
		{
			name:      "terminal failure",
			ensureErr: permanentKargoPromotionFailure("admission denied"),
			result:    api.KargoPromotionDispatchReportResultFailed,
		},
		{
			name:      "retryable failure",
			ensureErr: errors.New("apiserver unavailable"),
			result:    api.KargoPromotionDispatchReportResultRetryable,
		},
		{
			name:      "ambiguous report",
			ensureUID: "promotion-uid",
			result:    api.KargoPromotionDispatchReportResultCreated,
			reportErr: errors.New("connection reset"),
		},
		{
			name:       "rejected credential",
			ensureUID:  "promotion-uid",
			result:     api.KargoPromotionDispatchReportResultCreated,
			reportErr:  errControllerCredentialRejected,
			wantHandle: errControllerCredentialRejected,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var reported api.KargoPromotionDispatchReport
			app := &Controller{
				logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
				promotions: kargoPromotionCreatorFunc(func(
					context.Context,
					api.KargoPromotionCreationCommand,
				) (string, bool, error) {
					return test.ensureUID, true, test.ensureErr
				}),
				promotionReports: kargoPromotionDispatchReporterFunc(func(
					_ context.Context,
					_ uuid.UUID,
					report api.KargoPromotionDispatchReport,
				) error {
					reported = report
					return test.reportErr
				}),
			}
			err := app.handleKargoPromotionCreation(t.Context(), testKargoPromotionCommand())
			if !errors.Is(err, test.wantHandle) {
				t.Fatalf("handleKargoPromotionCreation() error = %v, want %v", err, test.wantHandle)
			}
			if reported.Result != test.result {
				t.Errorf("report = %#v", reported)
			}
			if test.ensureErr == nil && (reported.KargoUID == nil || *reported.KargoUID != test.ensureUID) {
				t.Errorf("created report = %#v", reported)
			}
			if test.ensureErr != nil && reported.Diagnostic == nil {
				t.Errorf("failed report = %#v", reported)
			}
		})
	}
}

func testKargoPromotionCommand() api.KargoPromotionCreationCommand {
	promotionID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1950")
	return api.KargoPromotionCreationCommand{
		ID:           promotionID,
		Attempt:      1,
		ExternalName: "slipramp-" + promotionID.String(),
		Integration:  "prod",
		Project:      "kargo-core",
		Stage:        "external-dns-prod",
		StageUID:     "stage-uid",
		Freight:      "freight-42",
	}
}

func testKargoStage(uid string) *unstructured.Unstructured {
	stage := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "kargo.akuity.io/v1alpha1",
		"kind":       "Stage",
		"metadata": map[string]any{
			"name":      "external-dns-prod",
			"namespace": "kargo-core",
		},
	}}
	stage.SetUID(types.UID(uid))
	return stage
}

func testKargoPromotion(
	command api.KargoPromotionCreationCommand,
	freight string,
) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "kargo.akuity.io/v1alpha1",
		"kind":       "Promotion",
		"metadata": map[string]any{
			"name":      command.ExternalName,
			"namespace": string(command.Project),
			"annotations": map[string]any{
				promotionIDAnnotation: command.ID.String(),
			},
		},
		"spec": map[string]any{
			"stage":   command.Stage,
			"freight": freight,
		},
	}}
}
