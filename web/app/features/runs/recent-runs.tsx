import { Alert, Button, Stack, Text } from "@mantine/core";
import { IconListDetails } from "@tabler/icons-react";
import { Link } from "react-router";

import type { Run } from "~/api/generated";
import { RunHistoryTable } from "~/features/runs/run-history-table";
import { useActivityRefresh } from "~/features/shell/activity-refresh";
import { SectionHeader } from "~/shared/page";

export const recentRunLimit = 10;

export function RecentRuns({
  available,
  description,
  emptyMessage,
  hasActiveRuns,
  plainEmpty = false,
  runs,
  showSource = false,
  showTarget = true,
  title = "Recent runs",
  viewAllTo,
}: Readonly<{
  available: boolean;
  description?: string;
  emptyMessage: string;
  hasActiveRuns?: boolean;
  plainEmpty?: boolean;
  runs: Run[];
  showSource?: boolean;
  showTarget?: boolean;
  title?: string;
  viewAllTo: string;
}>) {
  useActivityRefresh(hasActiveRuns ?? runs.some((run) => run.phase !== "finished"));

  return (
    <Stack gap="md">
      <SectionHeader
        actions={
          <Button
            component={Link}
            leftSection={<IconListDetails size={16} />}
            size="compact-md"
            to={viewAllTo}
            variant="light"
          >
            View all runs
          </Button>
        }
        description={description}
        title={title}
      />
      {!available ? (
        <Alert color="red" title="Run history unavailable">
          Run history could not be loaded.
        </Alert>
      ) : plainEmpty && runs.length === 0 ? (
        <Text c="dimmed">{emptyMessage}</Text>
      ) : (
        <RunHistoryTable
          emptyMessage={emptyMessage}
          runs={runs}
          showSource={showSource}
          showTarget={showTarget}
        />
      )}
    </Stack>
  );
}
