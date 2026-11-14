import { SimpleGrid, Stack } from "@mantine/core";
import { useOutletContext } from "react-router";

import { SectionHeader } from "~/shared/page";
import { CodeList, ResourceRequirementsTable, TechnicalValue } from "~/shared/technical-definition";

import type { PlacementOutletContext } from "./placement";

export default function PlacementOverview() {
  const { detail } = useOutletContext<PlacementOutletContext>();
  const { placement } = detail;

  return (
    <Stack gap="lg">
      <SectionHeader
        description="Defaults applied when a Target Action does not provide an override."
        title="Execution configuration"
      />
      <SimpleGrid cols={{ base: 1, sm: 2 }}>
        <TechnicalValue label="Service account" value={placement.serviceAccount} />
        <TechnicalValue label="Default timeout" value={placement.timeout ?? "No default"} />
      </SimpleGrid>
      <ResourceRequirementsTable
        empty="No resource defaults"
        label="Resource defaults"
        limits={placement.resources.limits}
        requests={placement.resources.requests}
      />
      <CodeList
        empty="No logical credential sources"
        label="Credential names"
        values={placement.credentialNames}
      />
    </Stack>
  );
}
