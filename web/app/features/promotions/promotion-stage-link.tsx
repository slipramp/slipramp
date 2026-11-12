import { Anchor, Group, Title } from "@mantine/core";
import type { ReactNode } from "react";
import { Link } from "react-router";

import { KargoIconLink } from "~/features/promotions/kargo-link";
import { promotionStagePath } from "~/features/promotions/promotion-stage";

type StageLinkValue = {
  id: string;
  kargoUrl?: string;
  name: string;
};

export function PromotionStageLink({
  fw = 600,
  heading = false,
  label,
  showKargoLink = true,
  size = "sm",
  stage,
}: Readonly<{
  fw?: number;
  heading?: boolean;
  label?: ReactNode;
  showKargoLink?: boolean;
  size?: "lg" | "md" | "sm" | "xl" | "xs";
  stage: StageLinkValue;
}>) {
  const nameLink = (
    <Anchor
      component={Link}
      fw={heading ? undefined : fw}
      inherit={heading || undefined}
      size={heading ? undefined : size}
      to={promotionStagePath(stage.id)}
    >
      {label ?? stage.name}
    </Anchor>
  );

  return (
    <Group gap={4} wrap="nowrap">
      {heading ? (
        <Title order={3} size="h4">
          {nameLink}
        </Title>
      ) : (
        nameLink
      )}
      {showKargoLink && stage.kargoUrl ? (
        <KargoIconLink label={`Open ${stage.name} in Kargo`} url={stage.kargoUrl} />
      ) : null}
    </Group>
  );
}
