import { ActionIcon, Popover, Select, Stack, Text } from "@mantine/core";
import { IconWorld } from "@tabler/icons-react";
import { useState } from "react";

import {
  defaultTimeZone,
  resolvedTimeZone,
  timeZoneLabel,
  timeZoneOptions,
} from "~/shared/time-zone";
import { useTimeZone } from "~/shared/time-zone-preference";

export function TimeZoneControl() {
  const { setTimeZone, timeZone } = useTimeZone();
  const [opened, setOpened] = useState(false);
  const options = opened
    ? timeZoneOptions(resolvedTimeZone())
    : [{ label: timeZoneLabel(timeZone), value: timeZone }];

  return (
    <Popover onChange={setOpened} opened={opened} position="bottom-end" shadow="md" width={340}>
      <Popover.Target>
        <ActionIcon
          aria-label={`Choose timezone. Current timezone: ${timeZone}`}
          size="lg"
          title={`Timezone: ${timeZone}`}
          variant="light"
        >
          <IconWorld size={19} stroke={1.8} />
        </ActionIcon>
      </Popover.Target>
      <Popover.Dropdown>
        <Stack gap="sm">
          <Select
            allowDeselect={false}
            data={options}
            label="Timezone"
            maxDropdownHeight={280}
            nothingFoundMessage="No timezone found"
            onChange={(value) => {
              if (value) {
                setTimeZone(value);
                setOpened(false);
              }
            }}
            searchable
            value={timeZone}
          />
          <Text c="dimmed" size="xs">
            All dates and times use this timezone. The default is {defaultTimeZone}, and your
            selection is stored in this browser.
          </Text>
          <Text c="dimmed" size="xs">
            Current selection: {timeZoneLabel(timeZone)}
          </Text>
        </Stack>
      </Popover.Dropdown>
    </Popover>
  );
}
