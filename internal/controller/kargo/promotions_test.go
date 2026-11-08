package kargo

import (
	"errors"
	"strings"
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
	creator := newTestPromotionManager(
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
	creator := newTestPromotionManager(
		[]promotion.ObserverConfig{{Name: "prod", StageNamespaces: []string{"kargo-core"}}},
		client,
	)
	if _, err := creator.Ensure(t.Context(), command); !IsPermanentPromotionError(err) {
		t.Fatalf("collision error = %v, want permanent", err)
	}

	client = newTestKargoDynamicClient(testKargoStage("new-stage-uid"))
	creator = newTestPromotionManager(
		[]promotion.ObserverConfig{{Name: "prod", StageNamespaces: []string{"kargo-core"}}},
		client,
	)
	if _, err := creator.Ensure(t.Context(), command); !IsPermanentPromotionError(err) {
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
			creator := newTestPromotionManager(
				[]promotion.ObserverConfig{{Name: "prod", StageNamespaces: []string{"kargo-core"}}},
				client,
			)
			_, err := creator.Ensure(t.Context(), testKargoPromotionCommand())
			if IsPermanentPromotionError(err) != test.permanent {
				t.Errorf("Ensure() error = %v, permanent = %t", err, IsPermanentPromotionError(err))
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
			reconciler := newTestPromotionManager(
				[]promotion.ObserverConfig{{Name: "prod", StageNamespaces: []string{"kargo-core"}}},
				client,
			)

			observed, err := reconciler.Reconcile(t.Context(), testKargoPromotionReconciliationCommand())
			if err != nil {
				t.Fatalf("Reconcile() error = %v", err)
			}
			if observed == nil || observed.Name != command.ExternalName || observed.UID != "promotion-uid" ||
				observed.Phase == nil || *observed.Phase != phase || observed.Diagnostic == nil ||
				!strings.Contains(*observed.Diagnostic, "Kargo terminal detail") {
				t.Errorf("observation = %#v", observed)
			}
		})
	}
}

func TestBoundedKargoPromotionDiagnosticSanitizesAndBoundsText(t *testing.T) {
	t.Parallel()

	if diagnostic := BoundedPromotionDiagnostic("\x00\n\t"); diagnostic != "" {
		t.Errorf("control-only diagnostic = %q, want empty", diagnostic)
	}

	diagnostic := BoundedPromotionDiagnostic("  detail\n" + strings.Repeat("é", 5000) + "  ")
	if !strings.HasPrefix(diagnostic, "detail ") || len([]rune(diagnostic)) != 4096 ||
		strings.ContainsAny(diagnostic, "\x00\n\t") {
		t.Errorf("sanitized diagnostic length = %d, value prefix = %q", len([]rune(diagnostic)), diagnostic[:8])
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
	reconciler := newTestPromotionManager(
		[]promotion.ObserverConfig{{Name: "prod", StageNamespaces: []string{"kargo-core"}}},
		client,
	)

	observed, err := reconciler.Reconcile(t.Context(), testKargoPromotionReconciliationCommand())
	if err != nil || observed == nil || observed.Phase != nil || observed.UID != "promotion-uid" {
		t.Errorf("nonterminal Reconcile() = %#v, %v", observed, err)
	}
	replacement := testKargoPromotionReconciliationCommand()
	replacementUID := "replacement-uid"
	replacement.KargoUID = &replacementUID
	if _, err := reconciler.Reconcile(t.Context(), replacement); err == nil {
		t.Fatal("replacement UID was accepted")
	}
}

func TestKargoPromotionReconcilerBindsKargoOriginatedIdentityAndRejectsCoordinateConflicts(t *testing.T) {
	t.Parallel()

	creation := testKargoPromotionCommand()
	object := testKargoPromotion(creation, creation.Freight)
	object.SetUID(types.UID("promotion-uid"))
	object.SetAnnotations(nil)
	reconciler := newTestPromotionManager(
		[]promotion.ObserverConfig{{Name: "prod", StageNamespaces: []string{"kargo-core"}}},
		newTestKargoDynamicClient(object),
	)
	command := testKargoPromotionReconciliationCommand()
	command.Source = api.Kargo
	command.KargoUID = nil

	observed, err := reconciler.Reconcile(t.Context(), command)
	if err != nil || observed == nil || observed.UID != "promotion-uid" || observed.Phase != nil {
		t.Fatalf("unbound Kargo-originated Reconcile() = %#v, %v", observed, err)
	}

	conflicts := map[string]func(*api.KargoPromotionReconciliationCommand){
		"name": func(value *api.KargoPromotionReconciliationCommand) {
			value.KargoName = "another-promotion"
		},
		"namespace": func(value *api.KargoPromotionReconciliationCommand) {
			value.Project = "another-project"
		},
		"Stage": func(value *api.KargoPromotionReconciliationCommand) {
			value.Stage = "another-stage"
		},
	}
	for name, mutate := range conflicts {
		t.Run(name, func(t *testing.T) {
			conflict := command
			mutate(&conflict)
			if _, err := reconciler.Reconcile(t.Context(), conflict); err == nil {
				t.Fatalf("%s conflict was accepted", name)
			}
		})
	}

	object.SetAnnotations(map[string]string{promotionIDAnnotation: uuid.NewString()})
	conflictingReconciler := newTestPromotionManager(
		[]promotion.ObserverConfig{{Name: "prod", StageNamespaces: []string{"kargo-core"}}},
		newTestKargoDynamicClient(object),
	)
	if _, err := conflictingReconciler.Reconcile(t.Context(), command); err == nil {
		t.Fatal("conflicting Slipramp annotation was accepted")
	}
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

func newTestPromotionManager(
	integrations []promotion.ObserverConfig,
	client *dynamicfake.FakeDynamicClient,
) *PromotionManager {
	return NewPromotionManager(Config{Integrations: integrations}, client)
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
