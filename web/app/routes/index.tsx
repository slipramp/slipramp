import { Badge, Group, Stack, Text, Title } from "@mantine/core";

import { createServerAPIClient } from "~/api/client.server";
import { getHealth } from "~/api/generated";

import type { Route } from "./+types/index";

export async function loader() {
  const result = await getHealth({
    client: createServerAPIClient(),
  });

  return {
    apiStatus: result.data?.status ?? "unavailable",
    rendering: "Server-rendered",
  } as const;
}

export default function Index({ loaderData }: Route.ComponentProps) {
  const apiAvailable = loaderData.apiStatus === "ok";

  return (
    <Stack gap="xs">
      <Title order={1}>Slipramp</Title>
      <Text>The web process checks the Slipramp Server through the generated API client.</Text>
      <Group gap="xs">
        <Badge variant="light">{loaderData.rendering}</Badge>
        <Badge color={apiAvailable ? "green" : "red"} variant="light">
          {apiAvailable ? "API connected" : "API unavailable"}
        </Badge>
      </Group>
    </Stack>
  );
}
