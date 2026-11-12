import { Anchor, Group } from "@mantine/core";
import type { ReactNode } from "react";
import { Link } from "react-router";

import { KargoIconLink } from "~/features/promotions/kargo-link";

type PromotionLinkValue = {
  externalPromotionId: string;
  id: string;
  kargoUrl?: string;
};

export function PromotionLink({
  fw = 600,
  label,
  promotion,
  showKargoLink = true,
  size = "sm",
  title,
}: Readonly<{
  fw?: number;
  label?: ReactNode;
  promotion: PromotionLinkValue;
  showKargoLink?: boolean;
  size?: "lg" | "md" | "sm" | "xl" | "xs";
  title?: string;
}>) {
  return (
    <Group gap={4} wrap="nowrap">
      <Anchor
        component={Link}
        fw={fw}
        size={size}
        title={title}
        to={`/promotions/${encodeURIComponent(promotion.id)}`}
      >
        {label ?? promotion.externalPromotionId}
      </Anchor>
      {showKargoLink && promotion.kargoUrl ? (
        <KargoIconLink
          label={`Open ${promotion.externalPromotionId} in Kargo`}
          url={promotion.kargoUrl}
        />
      ) : null}
    </Group>
  );
}
