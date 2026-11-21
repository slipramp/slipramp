import { Badge, Tabs } from "@mantine/core";
import { IconShieldCheck } from "@tabler/icons-react";

export function GatesTab({ count }: Readonly<{ count: number }>) {
  return (
    <Tabs.Tab
      leftSection={<IconShieldCheck aria-hidden size={16} />}
      rightSection={
        count > 0 ? (
          <Badge color="gray" size="xs" variant="light">
            {count}
          </Badge>
        ) : undefined
      }
      style={{ flexShrink: 0 }}
      value="gates"
    >
      Gates
    </Tabs.Tab>
  );
}
