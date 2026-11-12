import type { RouterContextProvider } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import { updatePromotionStagePolicy, type PromotionPolicyMode } from "~/api/generated";
import { submitPromotionCreation } from "~/features/promotions/promotion-creation.server";
import { formValue, optionalFormValue, problemDetail } from "~/shared/form-data";

export async function submitPromotionProjectAction(
  request: Request,
  context: Pick<RouterContextProvider, "get">,
) {
  const formData = await request.clone().formData();
  if (formData.has("mode")) return submitPromotionCreation(request, context);

  const stageId = formValue(formData, "stageId");
  const mode = formValue(formData, "policyMode") as PromotionPolicyMode;
  const reason = optionalFormValue(formData, "reason");
  const expiresAt = optionalFormValue(formData, "expiresAt");
  const result = await updatePromotionStagePolicy({
    body: { expiresAt: mode === "review-required" ? expiresAt : undefined, mode, reason },
    client: createServerAPIClient(request, context),
    path: { stageId },
  });

  return result.data
    ? { message: "The Stage policy was updated.", stageId }
    : { error: problemDetail(result.error, "The Stage policy could not be updated."), stageId };
}
