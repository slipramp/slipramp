import { Group, Paper, Stack, Text, Title } from "@mantine/core";
import type { ReactNode } from "react";

type PageHeaderProps = {
  actions?: ReactNode;
  badges?: ReactNode;
  back?: ReactNode;
  description: ReactNode;
  details?: ReactNode;
  title: ReactNode;
};

export function PageHeader({
  actions,
  back,
  badges,
  description,
  details,
  title,
}: Readonly<PageHeaderProps>) {
  return (
    <Stack gap="xs">
      {back}
      <Group align="flex-end" justify="space-between" wrap="wrap">
        <Stack gap={2} style={{ minWidth: 0 }}>
          <Group gap="sm" wrap="wrap">
            <Title order={1}>{title}</Title>
            {badges}
          </Group>
          {typeof description === "string" ? <Text c="dimmed">{description}</Text> : description}
          {details}
        </Stack>
        {actions ? (
          <Group gap="sm" wrap="wrap">
            {actions}
          </Group>
        ) : null}
      </Group>
    </Stack>
  );
}

export function SectionHeader({
  actions,
  description,
  title,
}: Readonly<{ actions?: ReactNode; description?: ReactNode; title: ReactNode }>) {
  return (
    <Group align="flex-end" justify="space-between" wrap="wrap">
      <Stack gap={2}>
        <Title order={2}>{title}</Title>
        {description ? <Text c="dimmed">{description}</Text> : null}
      </Stack>
      {actions}
    </Group>
  );
}

export function EmptyState({
  action,
  description,
  title,
}: Readonly<{ action?: ReactNode; description: ReactNode; title?: ReactNode }>) {
  return (
    <Paper p="lg" withBorder>
      <Stack align="flex-start" gap="xs">
        {title ? <Text fw={650}>{title}</Text> : null}
        <Text c="dimmed" size="sm">
          {description}
        </Text>
        {action}
      </Stack>
    </Paper>
  );
}
