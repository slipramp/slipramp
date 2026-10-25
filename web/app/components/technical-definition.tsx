import { Code, Paper, Stack, Table, Text } from "@mantine/core";
import type { ReactNode } from "react";

import { ContextValue } from "~/components/context-value";

type Entries = ReadonlyArray<readonly [string, string]>;

export function CommandBlock({ command }: Readonly<{ command: string[] }>) {
  return (
    <TechnicalBlock label="Argument array">
      <Paper className="technical-data-table" withBorder>
        <Table horizontalSpacing="sm" verticalSpacing="xs">
          <Table.Tbody>
            {command.map((argument, index) => (
              <Table.Tr key={index}>
                <Table.Td w="1%">
                  <Text c="dimmed" ff="monospace" size="xs" style={{ whiteSpace: "nowrap" }}>
                    argv[{index}]
                  </Text>
                </Table.Td>
                <Table.Td>
                  <Code className="technical-inline-value">{argument}</Code>
                </Table.Td>
              </Table.Tr>
            ))}
          </Table.Tbody>
        </Table>
      </Paper>
    </TechnicalBlock>
  );
}

export function CodeList({
  empty,
  label,
  values,
}: Readonly<{ empty: string; label: string; values: string[] }>) {
  return (
    <TechnicalBlock label={label}>
      {values.length === 0 ? (
        <Text c="dimmed" size="sm">
          {empty}
        </Text>
      ) : (
        <Code block className="technical-code-block technical-code-list">
          {values.join("\n")}
        </Code>
      )}
    </TechnicalBlock>
  );
}

export function MappingTable({
  empty,
  entries,
  label,
}: Readonly<{ empty: string; entries: Entries; label: string }>) {
  return (
    <TechnicalBlock label={label}>
      {entries.length === 0 ? (
        <Text c="dimmed" size="sm">
          {empty}
        </Text>
      ) : (
        <Paper className="technical-data-table" withBorder>
          <Table horizontalSpacing="sm" verticalSpacing="xs">
            <Table.Tbody>
              {entries.map(([name, value]) => (
                <Table.Tr key={name}>
                  <Table.Td>
                    <Code>{name}</Code>
                  </Table.Td>
                  <Table.Td className="technical-mapping-arrow" c="dimmed">
                    →
                  </Table.Td>
                  <Table.Td>
                    <Code>{value}</Code>
                  </Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        </Paper>
      )}
    </TechnicalBlock>
  );
}

export function ResourceRequirementsTable({
  empty = "No resource values",
  label = "Resources",
  limits,
  requests,
}: Readonly<{
  empty?: string;
  label?: string;
  limits?: Record<string, string>;
  requests?: Record<string, string>;
}>) {
  const names = [
    ...new Set([...Object.keys(requests ?? {}), ...Object.keys(limits ?? {})]),
  ].toSorted();

  return (
    <TechnicalBlock label={label}>
      {names.length === 0 ? (
        <Text c="dimmed" size="sm">
          {empty}
        </Text>
      ) : (
        <Paper className="technical-data-table" withBorder>
          <Table horizontalSpacing="sm" verticalSpacing="xs">
            <Table.Thead>
              <Table.Tr>
                <Table.Th>Resource</Table.Th>
                <Table.Th>Request</Table.Th>
                <Table.Th>Limit</Table.Th>
              </Table.Tr>
            </Table.Thead>
            <Table.Tbody>
              {names.map((name) => (
                <Table.Tr key={name}>
                  <Table.Td>
                    <Code>{name}</Code>
                  </Table.Td>
                  <Table.Td>
                    {requests?.[name] ? <Code>{requests[name]}</Code> : <Text c="dimmed">—</Text>}
                  </Table.Td>
                  <Table.Td>
                    {limits?.[name] ? <Code>{limits[name]}</Code> : <Text c="dimmed">—</Text>}
                  </Table.Td>
                </Table.Tr>
              ))}
            </Table.Tbody>
          </Table>
        </Paper>
      )}
    </TechnicalBlock>
  );
}

export function TechnicalValue({
  description,
  label,
  value,
}: Readonly<{ description?: string; label: string; value: ReactNode }>) {
  return (
    <Stack gap={4}>
      <TechnicalLabel>{label}</TechnicalLabel>
      <ContextValue size="sm">
        <Code className="technical-inline-value">{value}</Code>
      </ContextValue>
      {description ? (
        <Text c="dimmed" size="xs">
          {description}
        </Text>
      ) : null}
    </Stack>
  );
}

function TechnicalBlock({ children, label }: Readonly<{ children: ReactNode; label: string }>) {
  return (
    <Stack gap="xs">
      <TechnicalLabel>{label}</TechnicalLabel>
      {children}
    </Stack>
  );
}

function TechnicalLabel({ children }: Readonly<{ children: ReactNode }>) {
  return (
    <Text c="dimmed" fw={700} size="xs" tt="uppercase">
      {children}
    </Text>
  );
}
