import { Text, type TextProps } from "@mantine/core";
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
