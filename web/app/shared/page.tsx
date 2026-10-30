import { Group, Paper, Stack, Text, Title } from "@mantine/core";
import type { ReactNode } from "react";

type PageHeaderProps = {
  actions?: ReactNode;
  badges?: ReactNode;
  back?: ReactNode;
  description?: ReactNode;
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
    <Stack className="page-header" gap="xs">
      {back}
      <Group align="flex-end" justify="space-between" wrap="wrap">
        <Group className="page-header-heading" gap="md" wrap="nowrap">
          <Stack className="page-header-copy" gap={2}>
            <Group gap="sm" wrap="wrap">
              <Title order={1}>{title}</Title>
              {badges}
            </Group>
            {typeof description === "string" ? <Text c="dimmed">{description}</Text> : description}
            {details}
          </Stack>
        </Group>
        {actions ? (
          <Group gap="sm" wrap="wrap">
            {actions}
          </Group>
        ) : null}
      </Group>
    </Stack>
  );
}

export function PageHeaderFacts({ children }: Readonly<{ children: ReactNode }>) {
  return (
    <Group align="flex-start" gap="xl" mt="xs" wrap="wrap">
      {children}
    </Group>
  );
}

export function PageHeaderFact({
  children,
  label,
}: Readonly<{ children: ReactNode; label: string }>) {
  return (
    <Stack gap={0}>
      <Text c="dimmed" size="xs" tt="uppercase">
        {label}
      </Text>
      <Text component="div" size="sm">
        {children}
      </Text>
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
