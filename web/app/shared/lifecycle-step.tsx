import { Badge, Box, Stack, Text, UnstyledButton } from "@mantine/core";
import type { TablerIcon } from "@tabler/icons-react";
import { Timestamp } from "~/shared/display-values";
import styles from "./lifecycle-step.module.css";

export function LifecycleStep({
  icon: Icon,
  label,
  time,
  status,
  color,
  selected,
  complete,
  highlighted,
  onSelect,
  missingTime = "Not reached",
}: Readonly<{
  icon: TablerIcon;
  label: string;
  time?: string;
  status: string;
  color: string;
  selected: boolean;
  complete?: boolean;
  highlighted?: boolean;
  onSelect: () => void;
  missingTime?: string;
}>) {
  return (
    <UnstyledButton
      aria-current={selected ? "step" : undefined}
      className={styles.lifecycleStep}
      data-selected={selected || undefined}
      onClick={onSelect}
    >
      <Box className={styles.lifecycleIcon} data-complete={complete || undefined}>
        <Icon aria-hidden size={15} stroke={2.2} />
      </Box>
      <Stack gap={0} style={{ flex: 1 }}>
        <Text fw={650} size="sm">
          {label}
        </Text>
        <Text c="dimmed" size="xs">
          {time ? <Timestamp value={time} /> : missingTime}
        </Text>
      </Stack>
      <Badge color={color} size="xs" variant={highlighted ? "light" : "outline"}>
        {status}
      </Badge>
    </UnstyledButton>
  );
}
