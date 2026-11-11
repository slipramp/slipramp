import { Badge, Tooltip } from "@mantine/core";

import type { PromotionStatusPresentation } from "~/features/promotions/promotion-status";

export function PromotionStatusBadge({
  status,
}: Readonly<{ status: PromotionStatusPresentation }>) {
  return (
    <Tooltip label={status.description} withArrow>
      <Badge
        aria-label={`${status.label}: ${status.description}`}
        color={status.color}
        variant="light"
      >
        {status.label}
      </Badge>
    </Tooltip>
  );
}
