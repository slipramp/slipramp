import { Badge } from "@mantine/core";

import type { PromotionProjectStage } from "~/api/generated";
import {
  promotionStagePolicyColor,
  promotionStagePolicyPresentation,
} from "~/features/promotions/promotion-policy";

type StagePolicyBadgeProps = Readonly<{
  canEdit?: boolean;
  onEdit?: () => void;
  stage: Pick<PromotionProjectStage, "name" | "policy" | "capabilities">;
}>;

export function StagePolicyBadge({ canEdit = false, onEdit, stage }: StagePolicyBadgeProps) {
  const presentation = promotionStagePolicyPresentation(stage);
  const color = presentation.editable
    ? promotionStagePolicyColor(stage.policy.effectiveMode)
    : "gray";
  const editable = canEdit && presentation.editable && onEdit;

  if (!editable) {
    return (
      <Badge color={color} variant="light">
        {presentation.label}
      </Badge>
    );
  }

  return (
    <Badge
      aria-label={`Edit ${stage.name} policy`}
      color={color}
      component="button"
      onClick={onEdit}
      style={{ cursor: "pointer" }}
      type="button"
      variant="light"
    >
      {presentation.label}
    </Badge>
  );
}
