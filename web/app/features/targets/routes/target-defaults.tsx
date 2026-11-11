import { Anchor, Paper, SimpleGrid, Stack, Text, Title } from "@mantine/core";
import { Link, useOutletContext } from "react-router";

import { sortedEntries } from "~/features/runs/action-run";
import { targetFilterPath } from "~/features/targets/target-filter";
import {
  CodeList,
  MappingTable,
  ResourceRequirementsTable,
  TechnicalValue,
} from "~/shared/technical-definition";

import type { TargetOutletContext } from "./target";

export default function TargetDefaults() {
  const { target: record } = useOutletContext<TargetOutletContext>();
  const target = record.definition;
  const credentials = sortedEntries(target.spec.credentials);

  return (
    <Paper p="lg" withBorder>
      <Stack gap="lg">
        <Stack gap={2}>
          <Title order={2}>Target defaults</Title>
          <Text c="dimmed" size="sm">
            Actions inherit these values unless they provide an override.
          </Text>
        </Stack>

        <TechnicalValue label="Path" value={target.spec.path ?? "Repository root"} />
        <SimpleGrid cols={{ base: 1, sm: 3 }}>
          <TechnicalValue
            label="Runtime"
            value={
              target.spec.runtime ? (
                <Anchor component={Link} to={targetFilterPath("runtime", target.spec.runtime)}>
                  {target.spec.runtime}
                </Anchor>
              ) : (
                "Set by action"
              )
            }
          />
          <TechnicalValue
            label="Placement"
            value={
              target.spec.placement ? (
                <Anchor component={Link} to={targetFilterPath("placement", target.spec.placement)}>
                  {target.spec.placement}
                </Anchor>
              ) : (
                "Set by action"
              )
            }
          />
          <TechnicalValue label="Timeout" value={target.spec.timeout ?? "Placement default"} />
        </SimpleGrid>

        <CodeList empty="No watch paths" label="Watch paths" values={target.spec.watch ?? []} />
        <MappingTable
          empty="No target credential bindings"
          entries={credentials}
          label="Credential bindings"
        />
        <ResourceRequirementsTable
          limits={target.spec.resources?.limits}
          requests={target.spec.resources?.requests}
        />
      </Stack>
    </Paper>
  );
}
