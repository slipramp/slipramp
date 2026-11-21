import { SimpleGrid, Stack, Text, type TextProps } from "@mantine/core";
import type { ReactNode } from "react";

export function ContextValue({
  children,
  size,
}: Readonly<{ children: ReactNode; size?: TextProps["size"] }>) {
  return (
    <Text className="context-value" component="div" size={size}>
      {children}
    </Text>
  );
}

export function DetailGrid({ children }: Readonly<{ children: ReactNode }>) {
  return (
    <SimpleGrid cols={{ base: 1, sm: 2 }} spacing="lg">
      {children}
    </SimpleGrid>
  );
}

export function DetailFact({ children, label }: Readonly<{ children: ReactNode; label: string }>) {
  return (
    <Stack gap={3} style={{ minWidth: 0 }}>
      <Text c="dimmed" fw={700} size="xs" tt="uppercase">
        {label}
      </Text>
      <ContextValue size="sm">{children}</ContextValue>
    </Stack>
  );
}
