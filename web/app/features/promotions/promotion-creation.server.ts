import type { RouterContextProvider } from "react-router";
import { redirect } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import { createPromotion } from "~/api/generated";
import { formValue, problemDetail } from "~/shared/form-data";
import {
  parsePromotionReviewPolicySelection,
  promotionReviewPolicyUpdate,
  type PromotionCreationActionResult,
} from "~/features/promotions/promotion-creation";

export async function submitPromotionCreation(
  request: Request,
  context: Pick<RouterContextProvider, "get">,
): Promise<Response | PromotionCreationActionResult> {
  const formData = await request.formData();
  const targetStageId = formValue(formData, "targetStageId");
  const sourceStageId = formValue(formData, "sourceStageId");
  const freightName = formValue(formData, "freightName");
  const mode = formValue(formData, "mode");
  const reviewPolicy = parsePromotionReviewPolicySelection(
    formValue(formData, "reviewPolicy") || "unchanged",
  );
  if (!targetStageId || !freightName) {
    return { error: "Choose exact Freight to promote." };
  }
  if (mode !== "view" && mode !== "background") {
    return { error: "Choose how to open the created Promotion." };
  }
  if (!reviewPolicy) {
    return { error: "Choose a valid Stage review policy." };
  }

  const policyUpdate = promotionReviewPolicyUpdate(reviewPolicy);
  const result = await createPromotion({
    body: {
      freightName,
      ...(sourceStageId ? { sourceStageId } : {}),
      ...(policyUpdate ? { policy: policyUpdate } : {}),
    },
    client: createServerAPIClient(request, context),
    path: { stageId: targetStageId },
  });
  if (!result.data) {
    return {
      error: problemDetail(result.error, "The Promotion could not be created."),
    };
  }
  if (mode === "view") {
    return redirect(`/promotions/${result.data.id}`);
  }

  return { promotionId: result.data.id };
}
