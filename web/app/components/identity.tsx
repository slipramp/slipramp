import { Avatar, Group, Stack, Text, type TextProps } from "@mantine/core";
import type { ReactNode } from "react";

type IdentityProps = {
  avatarSize?: number | string;
  detail?: ReactNode;
  name: string;
  nameProps?: TextProps;
};

export function Identity({ avatarSize = 26, detail, name, nameProps }: Readonly<IdentityProps>) {
  return (
    <Group gap="xs" wrap="nowrap">
      <Avatar color="initials" name={name} size={avatarSize} variant="light" />
      <Stack gap={0} style={{ minWidth: 0 }}>
        <Text fw={600} lineClamp={1} size="sm" {...nameProps}>
          {name}
        </Text>
        {detail}
      </Stack>
    </Group>
  );
}
