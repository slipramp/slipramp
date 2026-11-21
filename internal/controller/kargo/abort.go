package kargo

import (
	"context"
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

const (
	abortAnnotation        = "kargo.akuity.io/abort"
	cancellationAnnotation = "slipramp.com/cancellation-request"
)

func (c *PromotionManager) requestAbort(ctx context.Context, object *unstructured.Unstructured, id string) error {
	// The Kargo webhook can enrich the original action with its own actor metadata.
	existing := object.GetAnnotations()[abortAnnotation]
	var request struct {
		Action string `json:"action"`
	}
	if existing == "terminate" || (json.Unmarshal([]byte(existing), &request) == nil && request.Action == "terminate") {
		return nil
	}
	patch, err := json.Marshal(map[string]any{
		"metadata": map[string]any{
			"uid":             object.GetUID(),
			"resourceVersion": object.GetResourceVersion(),
			"annotations": map[string]string{
				abortAnnotation:        `{"action":"terminate"}`,
				cancellationAnnotation: id,
			},
		},
	})
	if err != nil {
		return err
	}
	_, err = c.promotions.Namespace(object.GetNamespace()).Patch(
		ctx, object.GetName(), types.MergePatchType, patch, metav1.PatchOptions{},
	)
	if err != nil {
		return fmt.Errorf("request Kargo Promotion abort: %w", err)
	}
	return nil
}
