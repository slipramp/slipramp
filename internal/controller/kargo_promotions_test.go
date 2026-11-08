package controller

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/api"
	controllerkargo "github.com/slipramp/slipramp/internal/controller/kargo"
)

type kargoPromotionCreatorFunc func(
	context.Context,
	api.KargoPromotionCreationCommand,
) (controllerkargo.PromotionMaterialization, error)

func (f kargoPromotionCreatorFunc) Ensure(
	ctx context.Context,
	command api.KargoPromotionCreationCommand,
) (controllerkargo.PromotionMaterialization, error) {
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

func TestControllerReportsKargoPromotionCreationResults(t *testing.T) {
	t.Parallel()

	type creationResult struct {
		name       string
		ensure     controllerkargo.PromotionMaterialization
		ensureErr  error
		result     api.KargoPromotionDispatchReportResult
		reportErr  error
		wantHandle error
	}
	tests := []creationResult{
		{
			name:   "created",
			ensure: controllerkargo.PromotionMaterialization{Name: "stage.01test.freight", UID: "promotion-uid", Created: true},
			result: api.KargoPromotionDispatchReportResultCreated,
		},
		{
			name:      "retryable failure",
			ensureErr: errors.New("apiserver unavailable"),
			result:    api.KargoPromotionDispatchReportResultRetryable,
		},
	}
	for _, report := range kargoPromotionReportErrors() {
		result := creationResult{
			name:      report.name,
			ensure:    controllerkargo.PromotionMaterialization{Name: "stage.01test.freight", UID: "promotion-uid", Created: true},
			result:    api.KargoPromotionDispatchReportResultCreated,
			reportErr: report.err,
		}
		if report.permanent {
			result.wantHandle = report.err
		}
		tests = append(tests, result)
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
				) (controllerkargo.PromotionMaterialization, error) {
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

func TestControllerReportsTerminalKargoPromotionReconciliation(t *testing.T) {
	t.Parallel()

	tests := append([]kargoPromotionReportErrorCase{{name: "accepted"}}, kargoPromotionReportErrors()...)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			command := testKargoPromotionReconciliationCommand()
			phase := api.KargoPromotionReconciliationReportPhaseSucceeded
			diagnostic := "Kargo completed the Promotion."
			observation := &controllerkargo.PromotionObservation{
				Name:       command.KargoName,
				UID:        *command.KargoUID,
				Phase:      &phase,
				Diagnostic: &diagnostic,
			}
			var reported api.KargoPromotionReconciliationReport
			app := &Controller{
				logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
				promotionReconciler: kargoPromotionReconcilerFunc(func(
					context.Context,
					api.KargoPromotionReconciliationCommand,
				) (*controllerkargo.PromotionObservation, error) {
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
					return test.err
				}),
			}
			var wantHandle error
			if test.permanent {
				wantHandle = test.err
			}
			if err := app.handleKargoPromotionReconciliation(t.Context(), command); !errors.Is(err, wantHandle) {
				t.Fatalf("handleKargoPromotionReconciliation() error = %v, want %v", err, wantHandle)
			}
			if reported.KargoName != observation.Name || reported.KargoUID != observation.UID ||
				reported.Phase == nil || *reported.Phase != *observation.Phase ||
				reported.Diagnostic == nil || *reported.Diagnostic != *observation.Diagnostic {
				t.Errorf("report = %#v", reported)
			}
		})
	}
}

func TestControllerReportsUnboundKargoPromotionIdentity(t *testing.T) {
	t.Parallel()

	command := testKargoPromotionReconciliationCommand()
	command.Source = api.Kargo
	command.KargoUID = nil
	observation := &controllerkargo.PromotionObservation{Name: command.KargoName, UID: "promotion-uid"}
	var reported api.KargoPromotionReconciliationReport
	app := &Controller{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		promotionReconciler: kargoPromotionReconcilerFunc(func(
			context.Context,
			api.KargoPromotionReconciliationCommand,
		) (*controllerkargo.PromotionObservation, error) {
			return observation, nil
		}),
		promotionReconciliationReports: kargoPromotionReconciliationReporterFunc(func(
			_ context.Context,
			_ uuid.UUID,
			report api.KargoPromotionReconciliationReport,
		) error {
			reported = report
			return nil
		}),
	}
	if err := app.handleKargoPromotionReconciliation(t.Context(), command); err != nil {
		t.Fatalf("handleKargoPromotionReconciliation() error = %v", err)
	}
	if reported.KargoName != command.KargoName || reported.KargoUID != observation.UID || reported.Phase != nil {
		t.Fatalf("identity report = %#v", reported)
	}
}

type kargoPromotionReportErrorCase struct {
	name      string
	err       error
	permanent bool
}

func kargoPromotionReportErrors() []kargoPromotionReportErrorCase {
	return []kargoPromotionReportErrorCase{
		{name: "rejected credential", err: errControllerCredentialRejected, permanent: true},
		{
			name:      "invalid report",
			err:       newServerResponseError("report Kargo Promotion", http.StatusBadRequest),
			permanent: true,
		},
		{
			name:      "malformed response",
			err:       invalidServerResponse("report Kargo Promotion", "response JSON does not match the API contract"),
			permanent: true,
		},
		{
			name:      "missing Promotion",
			err:       newServerResponseError("report Kargo Promotion", http.StatusNotFound),
			permanent: true,
		},
		{
			name:      "identity conflict",
			err:       newServerResponseError("report Kargo Promotion", http.StatusConflict),
			permanent: true,
		},
		{name: "ambiguous report", err: errors.New("connection reset")},
		{name: "request timeout", err: newServerResponseError("report Kargo Promotion", http.StatusRequestTimeout)},
		{name: "rate limited", err: newServerResponseError("report Kargo Promotion", http.StatusTooManyRequests)},
		{name: "Server unavailable", err: newServerResponseError("report Kargo Promotion", http.StatusServiceUnavailable)},
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
				) (controllerkargo.PromotionMaterialization, error) {
					return controllerkargo.PromotionMaterialization{}, waitForDeadline(ctx)
				})
				app.promotionReconciler = kargoPromotionReconcilerFunc(func(
					ctx context.Context,
					_ api.KargoPromotionReconciliationCommand,
				) (*controllerkargo.PromotionObservation, error) {
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
) (*controllerkargo.PromotionObservation, error)

func (f kargoPromotionReconcilerFunc) Reconcile(
	ctx context.Context,
	command api.KargoPromotionReconciliationCommand,
) (*controllerkargo.PromotionObservation, error) {
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
	uid := "promotion-uid"

	return api.KargoPromotionReconciliationCommand{
		ID:          command.ID,
		Source:      api.Slipramp,
		Integration: command.Integration,
		Project:     command.Project,
		Stage:       command.Stage,
		KargoName:   command.ExternalName,
		KargoUID:    &uid,
	}
}

func TestControllerReportsUnconfiguredKargoPromotion(t *testing.T) {
	t.Parallel()

	var reported api.KargoPromotionDispatchReport
	app := &Controller{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		promotionReports: kargoPromotionDispatchReporterFunc(func(
			_ context.Context,
			_ uuid.UUID,
			report api.KargoPromotionDispatchReport,
		) error {
			reported = report
			return nil
		}),
	}
	if err := app.handleKargoPromotionCreation(t.Context(), testKargoPromotionCommand()); err != nil {
		t.Fatalf("handleKargoPromotionCreation() error = %v", err)
	}
	if reported.Result != api.KargoPromotionDispatchReportResultFailed || reported.Diagnostic == nil {
		t.Fatalf("report = %#v", reported)
	}
}
