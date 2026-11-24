import {
  Accordion,
  Code,
  CopyButton,
  Group,
  Paper,
  Stack,
  Text,
  Title,
  Tooltip,
  UnstyledButton,
} from "@mantine/core";
import { IconCheck, IconCopy } from "@tabler/icons-react";
import type { ReactNode } from "react";
import { shortIdentifier } from "~/shared/display";

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
      <Stack gap={2}>
        <Group gap="sm" wrap="wrap">
          <Title order={1}>{title}</Title>
          {badges}
        </Group>
        {typeof description === "string" ? <Text c="dimmed">{description}</Text> : description}
      </Stack>
      {details || actions ? (
        <Group align="center" gap="md" wrap="wrap">
          {details}
          {actions ? (
            <Group gap="sm" justify="flex-end" ml="auto" wrap="wrap">
              {actions}
            </Group>
          ) : null}
        </Group>
      ) : null}
    </Stack>
  );
}

export function PageHeaderFacts({ children }: Readonly<{ children: ReactNode }>) {
  return (
    <Group align="flex-start" className="page-header-facts" gap="xl" wrap="wrap">
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

export function CopyableIdFact({ label, value }: Readonly<{ label: string; value: string }>) {
  return (
    <CopyButton key={value} timeout={2_000} value={value}>
      {({ copy, copied }) => (
        <Tooltip label={copied ? "Copied" : `Copy full ${label}`} withArrow>
          <UnstyledButton
            aria-label={copied ? `${label} copied` : `Copy full ${label}`}
            onClick={copy}
            type="button"
          >
            <Text c="dimmed" component="span" display="block" size="xs" tt="uppercase">
              {label}
            </Text>
            <Group component="span" gap={4} wrap="nowrap">
              <Code>{shortIdentifier(value)}</Code>
              {copied ? <IconCheck aria-hidden size={14} /> : <IconCopy aria-hidden size={14} />}
            </Group>
          </UnstyledButton>
        </Tooltip>
      )}
    </CopyButton>
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

export function DetailSection({
  children,
  defaultExpanded = false,
  title,
}: Readonly<{ children: ReactNode; defaultExpanded?: boolean; title: ReactNode }>) {
  return (
    <Accordion
      classNames={{ item: "detail-section-item" }}
      defaultValue={defaultExpanded ? "section" : null}
      order={2}
      styles={{ label: { fontWeight: 700 } }}
    >
      <Accordion.Item value="section">
        <Accordion.Control>{title}</Accordion.Control>
        <Accordion.Panel>{children}</Accordion.Panel>
      </Accordion.Item>
    </Accordion>
  );
}
