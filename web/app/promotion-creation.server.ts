import type { RouterContextProvider } from "react-router";
import { redirect } from "react-router";

import { createServerAPIClient } from "~/api/client.server";
import { createPromotion } from "~/api/generated";
import { formValue, problemDetail } from "~/form-data";
import type { PromotionCreationActionResult } from "~/promotion-creation";

export async function submitPromotionCreation(
  request: Request,
  context: Pick<RouterContextProvider, "get">,
): Promise<Response | PromotionCreationActionResult> {
  const formData = await request.formData();
  const targetStageId = formValue(formData, "targetStageId");
  const sourceStageId = formValue(formData, "sourceStageId");
  const freightName = formValue(formData, "freightName");
  const mode = formValue(formData, "mode");
  if (!targetStageId || !sourceStageId || !freightName) {
    return { error: "Choose exact Freight to promote." };
  }
  if (mode !== "view" && mode !== "background") {
    return { error: "Choose how to open the created Promotion." };
  }

  const result = await createPromotion({
    body: { freightName, sourceStageId },
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
