import { Anchor, Group, Title, type AnchorProps } from "@mantine/core";
import type { ReactNode } from "react";
import { Link } from "react-router";

import { KargoIconLink } from "~/features/promotions/kargo-link";
import { promotionProjectDetailPath } from "~/features/promotions/promotion-project-inventory-state";

type PromotionProjectLinkValue = {
  cluster: string;
  integration: string;
  kargoUrl?: string;
  project: string;
};

export function PromotionProjectLink({
  c,
  fw = 600,
  heading = false,
  label,
  project,
  showKargoLink = true,
  size = "sm",
  to,
}: Readonly<{
  c?: AnchorProps["c"];
  fw?: number;
  heading?: boolean;
  label?: ReactNode;
  project: PromotionProjectLinkValue;
  showKargoLink?: boolean;
  size?: "lg" | "md" | "sm" | "xl" | "xs";
  to?: string;
}>) {
  const nameLink = (
    <Anchor
      c={c}
      component={Link}
      fw={heading ? undefined : fw}
      inherit={heading || undefined}
      size={heading ? undefined : size}
      to={to ?? promotionProjectDetailPath(project)}
    >
      {label ?? project.project}
    </Anchor>
  );

  return (
    <Group gap={4} wrap="nowrap">
      {heading ? (
        <Title order={2} size="h3">
          {nameLink}
        </Title>
      ) : (
        nameLink
      )}
      {showKargoLink && project.kargoUrl ? (
        <KargoIconLink label={`Open ${project.project} in Kargo`} url={project.kargoUrl} />
      ) : null}
    </Group>
  );
}
