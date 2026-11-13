import { Group, Stack, Text, Tooltip } from "@mantine/core";
import { IconClock } from "@tabler/icons-react";

import { kargoObservationIsLate } from "~/features/promotions/kargo-observation";
import { formatRelativeTime, formatTimestamp } from "~/shared/display";
import { useTimeZone } from "~/shared/time-zone-preference";

export function KargoObservationTime({ value }: Readonly<{ value: string }>) {
  const { timeZone } = useTimeZone();
  const relative = formatRelativeTime(value);
  const absolute = formatTimestamp(value, timeZone);
  const late = kargoObservationIsLate(value);

  return (
    <Tooltip
      label={
        <Stack gap={0}>
          <Text size="xs">{relative}</Text>
          <Text size="xs">{absolute}</Text>
        </Stack>
      }
      openDelay={350}
      withArrow
    >
      <Group
        aria-label={`Last observed ${relative}, ${absolute}`}
        c={late ? "orange" : "dimmed"}
        component="span"
        display="inline-flex"
        gap={0}
        suppressHydrationWarning
        tabIndex={0}
      >
        <IconClock aria-hidden size={17} stroke={1.8} />
      </Group>
    </Tooltip>
  );
}
