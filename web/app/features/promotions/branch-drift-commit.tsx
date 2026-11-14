import { Group, Stack, Text } from "@mantine/core";

import type { GitCommit } from "~/api/generated";
import { CommitReference } from "~/features/commits/git-commit";
import { RelativeTime } from "~/shared/display-values";

export function BranchDriftCommit({
  branch,
  commit,
  repositoryId,
}: Readonly<{
  branch?: string;
  commit?: GitCommit;
  repositoryId?: string;
}>) {
  return (
    <Stack gap={2}>
      <Text fw={600} size="xs">
        {branch ?? "Unknown branch"}
      </Text>
      <Group gap="xs" wrap="wrap">
        {commit ? (
          <CommitReference commit={commit} repositoryId={repositoryId} />
        ) : (
          <Text size="xs">Unknown</Text>
        )}
        {commit?.authoredAt ? (
          <RelativeTime c="dimmed" size="xs" value={commit.authoredAt} />
        ) : null}
      </Group>
    </Stack>
  );
}
