import { ActionIcon, Anchor, Button, Group, Tooltip, type MantineSize } from "@mantine/core";
import { IconExternalLink } from "@tabler/icons-react";
import type { ReactNode } from "react";

const externalLinkProps = {
  rel: "noreferrer",
  target: "_blank",
} as const;

export function KargoLink({
  children,
  fw,
  size = "sm",
  url,
}: Readonly<{ children: ReactNode; fw?: number; size?: MantineSize; url: string }>) {
  return (
    <Anchor fw={fw} href={url} size={size} {...externalLinkProps}>
      <Group component="span" gap={4} wrap="nowrap">
        {children}
        <IconExternalLink aria-hidden size={13} />
      </Group>
    </Anchor>
  );
}

export function KargoButton({ url }: Readonly<{ url: string }>) {
  return (
    <Button
      component="a"
      href={url}
      leftSection={<IconExternalLink aria-hidden size={16} />}
      size="compact-md"
      variant="light"
      {...externalLinkProps}
    >
      Open in Kargo
    </Button>
  );
}

export function KargoIconLink({
  label,
  url,
}: Readonly<{
  label: string;
  url: string;
}>) {
  return (
    <Tooltip label={label}>
      <ActionIcon
        aria-label={label}
        color="gray"
        component="a"
        href={url}
        size="xs"
        variant="subtle"
        {...externalLinkProps}
      >
        <IconExternalLink aria-hidden size={13} />
      </ActionIcon>
    </Tooltip>
  );
}
