import { Badge, Box, Button, Divider, Group, Popover, Stack, Text } from "@mantine/core";
import { IconActivityHeartbeat } from "@tabler/icons-react";
import { useEffect, useSyncExternalStore } from "react";
import { useLocation } from "react-router";

import {
  formatPerformanceDuration,
  pagePerformanceStore,
  recordPageCommit,
  type BrowserPerformanceMetric,
  type ServerPerformanceMetric,
} from "~/page-performance";

export function PerformanceFooter() {
  const location = useLocation();
  const snapshot = useSyncExternalStore(
    pagePerformanceStore.subscribe,
    pagePerformanceStore.getSnapshot,
    pagePerformanceStore.getServerSnapshot,
  );

  useEffect(() => {
    recordPageCommit(location.key);
  }, [location.key]);

  const serverMetrics = snapshot?.serverMetrics.filter((metric) =>
    ["server", "ssr", "omitted"].includes(metric.name),
  );
  const routeMetrics = snapshot?.serverMetrics.filter((metric) =>
    ["loader", "action"].includes(metric.name),
  );
  const apiMetrics = snapshot?.serverMetrics.filter((metric) => metric.name === "api");

  return (
    <Box className="performance-footer" component="footer">
      <Divider />
      <Group justify="flex-end" py="xs">
        <Popover position="top-end" shadow="md" width="min(420px, calc(100vw - 2rem))" withArrow>
          <Popover.Target>
            <Button
              aria-label="Show page performance details"
              color="gray"
              leftSection={<IconActivityHeartbeat size={15} stroke={1.8} />}
              size="compact-xs"
              variant="subtle"
            >
              {snapshot
                ? `${snapshot.headline} in ${formatPerformanceDuration(snapshot.overallMs)}`
                : "Measuring performance…"}
            </Button>
          </Popover.Target>
          <Popover.Dropdown>
            <Stack gap="md">
              <Box>
                <Text fw={700} size="sm">
                  Page performance
                </Text>
                <Text c="dimmed" size="xs">
                  {snapshot?.contextLabel ?? "Waiting for the page to settle"}
                </Text>
              </Box>

              {snapshot ? (
                <>
                  <MetricGroup label="Browser" metrics={snapshot.browserMetrics} />
                  <ServerMetricGroup label="Server" metrics={serverMetrics ?? []} />
                  <ServerMetricGroup label="Routes" metrics={routeMetrics ?? []} />
                  <ServerMetricGroup label="API calls" metrics={apiMetrics ?? []} />
                  <Text c="dimmed" size="xs">
                    Timings can overlap, so detail rows do not add up to the overall time.
                  </Text>
                </>
              ) : (
                <Text c="dimmed" size="sm">
                  Timing details will appear after hydration and the first browser paint.
                </Text>
              )}
            </Stack>
          </Popover.Dropdown>
        </Popover>
      </Group>
    </Box>
  );
}

function MetricGroup({
  label,
  metrics,
}: Readonly<{ label: string; metrics: BrowserPerformanceMetric[] }>) {
  if (metrics.length === 0) return null;

  return (
    <Stack gap={5}>
      <Text c="dimmed" fw={700} size="xs" tt="uppercase">
        {label}
      </Text>
      {metrics.map((metric, index) => (
        <MetricRow
          durationMs={metric.durationMs}
          key={`${metric.name}-${index}`}
          label={metric.label}
        />
      ))}
    </Stack>
  );
}

function ServerMetricGroup({
  label,
  metrics,
}: Readonly<{ label: string; metrics: ServerPerformanceMetric[] }>) {
  if (metrics.length === 0) return null;

  return (
    <Stack gap={5}>
      <Text c="dimmed" fw={700} size="xs" tt="uppercase">
        {label}
      </Text>
      {metrics.map((metric, index) => (
        <MetricRow
          durationMs={metric.durationMs}
          key={`${metric.name}-${metric.description}-${index}`}
          label={serverMetricLabel(metric)}
          status={metric.status}
        />
      ))}
    </Stack>
  );
}

function MetricRow({
  durationMs,
  label,
  status,
}: Readonly<{ durationMs: number; label: string; status?: number | "error" }>) {
  return (
    <Group align="flex-start" gap="sm" justify="space-between" wrap="nowrap">
      <Group gap={6} miw={0} wrap="nowrap">
        <Text className="performance-metric-label" size="xs" title={label}>
          {label}
        </Text>
        {status === undefined ? null : (
          <Badge color={statusColor(status)} size="xs" variant="light">
            {status}
          </Badge>
        )}
      </Group>
      <Text className="performance-metric-value" fw={600} size="xs">
        {formatPerformanceDuration(durationMs)}
      </Text>
    </Group>
  );
}

function serverMetricLabel(metric: ServerPerformanceMetric) {
  const descriptions: Record<string, string> = {
    action: "Action",
    loader: "Loader",
    server: "Request",
    ssr: "SSR",
  };
  const prefix = descriptions[metric.name];
  const detail = metric.description || metric.name;
  return prefix ? `${prefix} · ${detail}` : detail;
}

function statusColor(status: number | "error") {
  if (status === "error" || status >= 500) return "red";
  if (status >= 400) return "orange";
  if (status >= 300) return "yellow";
  return "green";
}
