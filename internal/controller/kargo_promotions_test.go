package controller

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"testing/synctest"
	"time"

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
) (kargoPromotionMaterialization, error)

func (f kargoPromotionCreatorFunc) Ensure(
	ctx context.Context,
	command api.KargoPromotionCreationCommand,
) (kargoPromotionMaterialization, error) {
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

type kargoPromotionReconciliationReporterFunc func(
	context.Context,
	uuid.UUID,
	api.KargoPromotionReconciliationReport,
) error

func (f kargoPromotionReconciliationReporterFunc) ReportKargoPromotionReconciliation(
	ctx context.Context,
	promotionID uuid.UUID,
	report api.KargoPromotionReconciliationReport,
) error {
	return f(ctx, promotionID, report)
}

func TestKargoPromotionCreatorCreatesAndRecoversMatchingObject(t *testing.T) {
	t.Parallel()

	stage := testKargoStage("stage-uid")
	client := newTestKargoDynamicClient(stage)
	client.PrependReactor("create", "promotions", func(action kubetesting.Action) (bool, runtime.Object, error) {
		create := action.(kubetesting.CreateAction)
		object := create.GetObject().(*unstructured.Unstructured).DeepCopy()
		object.SetName("external-dns-prod.01test.freight")
		object.SetUID(types.UID("promotion-uid"))
		err := client.Tracker().Create(kargoPromotionResource, object, create.GetNamespace())
		return true, object, err
	})
	creator := newDynamicKargoPromotionCreator(
		[]promotion.ObserverConfig{{Name: "prod", StageNamespaces: []string{"kargo-core"}}},
		client,
	)
	command := testKargoPromotionCommand()

	result, err := creator.Ensure(t.Context(), command)
	if err != nil {
		t.Fatalf("first Ensure() error = %v", err)
	}
	if result.Name != "external-dns-prod.01test.freight" || result.UID != "promotion-uid" || !result.Created {
		t.Errorf("first Ensure() = %#v", result)
	}

	result, err = creator.Ensure(t.Context(), command)
	if err != nil {
		t.Fatalf("repeated Ensure() error = %v", err)
	}
	if result.Name != "external-dns-prod.01test.freight" || result.UID != "promotion-uid" || result.Created {
		t.Errorf("repeated Ensure() = %#v", result)
	}
	stored, err := client.Resource(kargoPromotionResource).Namespace("kargo-core").Get(
		t.Context(),
		result.Name,
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
	client := newTestKargoDynamicClient(testKargoStage("stage-uid"), existing)
	creator := newDynamicKargoPromotionCreator(
		[]promotion.ObserverConfig{{Name: "prod", StageNamespaces: []string{"kargo-core"}}},
		client,
	)
	if _, err := creator.Ensure(t.Context(), command); !isPermanentKargoPromotionError(err) {
		t.Fatalf("collision error = %v, want permanent", err)
	}

	client = newTestKargoDynamicClient(testKargoStage("new-stage-uid"))
	creator = newDynamicKargoPromotionCreator(
		[]promotion.ObserverConfig{{Name: "prod", StageNamespaces: []string{"kargo-core"}}},
		client,
	)
	if _, err := creator.Ensure(t.Context(), command); !isPermanentKargoPromotionError(err) {
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

			client := newTestKargoDynamicClient(testKargoStage("stage-uid"))
			client.PrependReactor("create", "promotions", func(kubetesting.Action) (bool, runtime.Object, error) {
				return true, nil, test.err
			})
			creator := newDynamicKargoPromotionCreator(
				[]promotion.ObserverConfig{{Name: "prod", StageNamespaces: []string{"kargo-core"}}},
				client,
			)
			_, err := creator.Ensure(t.Context(), testKargoPromotionCommand())
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
		ensure     kargoPromotionMaterialization
		ensureErr  error
		result     api.KargoPromotionDispatchReportResult
		reportErr  error
		wantHandle error
	}{
		{
			name:   "created",
			ensure: kargoPromotionMaterialization{Name: "stage.01test.freight", UID: "promotion-uid", Created: true},
			result: api.KargoPromotionDispatchReportResultCreated,
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
			ensure:    kargoPromotionMaterialization{Name: "stage.01test.freight", UID: "promotion-uid", Created: true},
			result:    api.KargoPromotionDispatchReportResultCreated,
			reportErr: errors.New("connection reset"),
		},
		{
			name:       "rejected credential",
			ensure:     kargoPromotionMaterialization{Name: "stage.01test.freight", UID: "promotion-uid", Created: true},
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
				) (kargoPromotionMaterialization, error) {
					return test.ensure, test.ensureErr
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
			if test.ensureErr == nil && (reported.KargoName == nil || *reported.KargoName != test.ensure.Name ||
				reported.KargoUID == nil || *reported.KargoUID != test.ensure.UID) {
				t.Errorf("created report = %#v", reported)
			}
			if test.ensureErr != nil && reported.Diagnostic == nil {
				t.Errorf("failed report = %#v", reported)
			}
		})
	}
}

func TestKargoPromotionReconcilerReportsOnlyCanonicalTerminalObjects(t *testing.T) {
	t.Parallel()

	for _, phase := range []api.KargoPromotionReconciliationReportPhase{
		api.KargoPromotionReconciliationReportPhaseFailed,
		api.KargoPromotionReconciliationReportPhaseErrored,
		api.KargoPromotionReconciliationReportPhaseAborted,
		api.KargoPromotionReconciliationReportPhaseSucceeded,
	} {
		t.Run(string(phase), func(t *testing.T) {
			t.Parallel()

			command := testKargoPromotionCommand()
			object := testKargoPromotion(command, command.Freight)
			object.SetUID(types.UID("promotion-uid"))
			if err := unstructured.SetNestedField(object.Object, string(phase), "status", "phase"); err != nil {
				t.Fatal(err)
			}
			if err := unstructured.SetNestedField(object.Object, "Kargo terminal\ndetail\x00", "status", "message"); err != nil {
				t.Fatal(err)
			}
			client := newTestKargoDynamicClient(object)
			reconciler := newDynamicKargoPromotionCreator(
				[]promotion.ObserverConfig{{Name: "prod", StageNamespaces: []string{"kargo-core"}}},
				client,
			)

			observed, err := reconciler.Reconcile(t.Context(), testKargoPromotionReconciliationCommand())
			if err != nil {
				t.Fatalf("Reconcile() error = %v", err)
			}
			if observed == nil || observed.Name != command.ExternalName || observed.UID != "promotion-uid" ||
				observed.Phase != phase || !strings.Contains(observed.Diagnostic, "Kargo terminal detail") {
				t.Errorf("observation = %#v", observed)
			}
		})
	}
}

func TestKargoPromotionReconcilerIgnoresNonterminalPhaseAndRejectsUIDReplacement(t *testing.T) {
	t.Parallel()

	command := testKargoPromotionCommand()
	object := testKargoPromotion(command, command.Freight)
	object.SetUID(types.UID("promotion-uid"))
	if err := unstructured.SetNestedField(object.Object, "Running", "status", "phase"); err != nil {
		t.Fatal(err)
	}
	client := newTestKargoDynamicClient(object)
	reconciler := newDynamicKargoPromotionCreator(
		[]promotion.ObserverConfig{{Name: "prod", StageNamespaces: []string{"kargo-core"}}},
		client,
	)

	observed, err := reconciler.Reconcile(t.Context(), testKargoPromotionReconciliationCommand())
	if err != nil || observed != nil {
		t.Errorf("nonterminal Reconcile() = %#v, %v", observed, err)
	}
	replacement := testKargoPromotionReconciliationCommand()
	replacement.KargoUID = "replacement-uid"
	if _, err := reconciler.Reconcile(t.Context(), replacement); err == nil {
		t.Fatal("replacement UID was accepted")
	}
}

func TestControllerReportsTerminalKargoPromotionReconciliation(t *testing.T) {
	t.Parallel()

	command := testKargoPromotionReconciliationCommand()
	observation := &kargoPromotionTerminalObservation{
		Name:       command.KargoName,
		UID:        command.KargoUID,
		Phase:      api.KargoPromotionReconciliationReportPhaseSucceeded,
		Diagnostic: "terminal before callback",
	}
	var reported api.KargoPromotionReconciliationReport
	app := &Controller{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		promotionReconciler: kargoPromotionReconcilerFunc(func(
			context.Context,
			api.KargoPromotionReconciliationCommand,
		) (*kargoPromotionTerminalObservation, error) {
			return observation, nil
		}),
		promotionReconciliationReports: kargoPromotionReconciliationReporterFunc(func(
			ctx context.Context,
			promotionID uuid.UUID,
			report api.KargoPromotionReconciliationReport,
		) error {
			if err := ctx.Err(); err != nil {
				t.Errorf("report context was canceled with the Kubernetes attempt: %v", err)
			}
			if promotionID != command.ID {
				t.Errorf("reported Promotion ID = %s", promotionID)
			}
			reported = report
			return nil
		}),
	}
	if err := app.handleKargoPromotionReconciliation(t.Context(), command); err != nil {
		t.Fatalf("handleKargoPromotionReconciliation() error = %v", err)
	}
	if reported.KargoName != observation.Name || reported.KargoUID != observation.UID ||
		reported.Phase != observation.Phase || reported.Diagnostic != observation.Diagnostic {
		t.Errorf("report = %#v", reported)
	}
}

func TestControllerResumesPollingAfterKargoOperationDeadline(t *testing.T) {
	t.Parallel()

	creation := testKargoPromotionCommand()
	reconciliation := testKargoPromotionReconciliationCommand()
	tests := []struct {
		name    string
		command api.ControllerCommand
	}{
		{
			name: "creation",
			command: api.ControllerCommand{
				Type: api.ControllerCommandTypeCreateKargoPromotion, Promotion: &creation,
			},
		},
		{
			name: "reconciliation",
			command: api.ControllerCommand{
				Type: api.ControllerCommandTypeReconcileKargoPromotion, PromotionReconciliation: &reconciliation,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				ctx, stop := context.WithCancel(t.Context())
				defer stop()
				waitForDeadline := func(attemptContext context.Context) error {
					<-attemptContext.Done()
					if !errors.Is(attemptContext.Err(), context.DeadlineExceeded) {
						t.Errorf("Kubernetes attempt error = %v, want deadline exceeded", attemptContext.Err())
					}

					return attemptContext.Err()
				}
				app := newTestController(&stubRunJobs{}, &memoryLogReporter{})
				app.promotions = kargoPromotionCreatorFunc(func(
					ctx context.Context,
					_ api.KargoPromotionCreationCommand,
				) (kargoPromotionMaterialization, error) {
					return kargoPromotionMaterialization{}, waitForDeadline(ctx)
				})
				app.promotionReconciler = kargoPromotionReconcilerFunc(func(
					ctx context.Context,
					_ api.KargoPromotionReconciliationCommand,
				) (*kargoPromotionTerminalObservation, error) {
					return nil, waitForDeadline(ctx)
				})
				var reported bool
				app.promotionReports = kargoPromotionDispatchReporterFunc(func(
					ctx context.Context,
					_ uuid.UUID,
					report api.KargoPromotionDispatchReport,
				) error {
					reported = true
					if ctx.Err() != nil || report.Result != api.KargoPromotionDispatchReportResultRetryable {
						t.Errorf("timed-out creation report = %#v, context error = %v", report, ctx.Err())
					}

					return nil
				})
				polls := 0
				app.server = commandPollerFunc(func(context.Context) (*api.ControllerCommand, error) {
					polls++
					if polls == 1 {
						return &test.command, nil
					}
					stop()

					return nil, nil
				})

				startedAt := time.Now()
				if err := app.Run(ctx); err != nil {
					t.Fatalf("Run() error = %v", err)
				}
				if polls != 2 || time.Since(startedAt) != kargoPromotionOperationTimeout+app.retryDelay {
					t.Errorf("polls = %d after %s, want polling to resume after the bounded attempt", polls, time.Since(startedAt))
				}
				if reported != (test.command.Promotion != nil) {
					t.Errorf("creation retry report sent = %t", reported)
				}
			})
		})
	}
}

type kargoPromotionReconcilerFunc func(
	context.Context,
	api.KargoPromotionReconciliationCommand,
) (*kargoPromotionTerminalObservation, error)

func (f kargoPromotionReconcilerFunc) Reconcile(
	ctx context.Context,
	command api.KargoPromotionReconciliationCommand,
) (*kargoPromotionTerminalObservation, error) {
	return f(ctx, command)
}

func testKargoPromotionCommand() api.KargoPromotionCreationCommand {
	promotionID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1950")
	return api.KargoPromotionCreationCommand{
		ID:           promotionID,
		Attempt:      1,
		ExternalName: "external-dns-prod-slipramp-" + promotionID.String(),
		Integration:  "prod",
		Project:      "kargo-core",
		Stage:        "external-dns-prod",
		StageUID:     "stage-uid",
		Freight:      "freight-42",
	}
}

func testKargoPromotionReconciliationCommand() api.KargoPromotionReconciliationCommand {
	command := testKargoPromotionCommand()

	return api.KargoPromotionReconciliationCommand{
		ID:          command.ID,
		Integration: command.Integration,
		Project:     command.Project,
		KargoName:   command.ExternalName,
		KargoUID:    "promotion-uid",
	}
}

func newTestKargoDynamicClient(objects ...runtime.Object) *dynamicfake.FakeDynamicClient {
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{
			kargoStageResource:     "StageList",
			kargoPromotionResource: "PromotionList",
		},
		objects...,
	)
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
