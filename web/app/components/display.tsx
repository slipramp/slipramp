import { Text, Tooltip, type TextProps } from "@mantine/core";

import { formatDatestamp, formatRelativeTime, formatTimestamp } from "~/display";

type TimeDisplayProps = TextProps & {
  value: string;
};

export function Timestamp({ value, ...props }: Readonly<TimeDisplayProps>) {
  const formatted = formatTimestamp(value);

  return (
    <Tooltip label={formatted} openDelay={350}>
      <Text component="time" dateTime={value} suppressHydrationWarning {...props}>
        {formatted}
      </Text>
    </Tooltip>
  );
}

export function Datestamp({ value, ...props }: Readonly<TimeDisplayProps>) {
  const formatted = formatDatestamp(value);

  return (
    <Tooltip label={formatTimestamp(value)} openDelay={350}>
      <Text component="time" dateTime={value} suppressHydrationWarning {...props}>
        {formatted}
      </Text>
    </Tooltip>
  );
}

export function RelativeTime({ value, ...props }: Readonly<TimeDisplayProps>) {
  return (
    <Tooltip label={formatTimestamp(value)} openDelay={350}>
      <Text component="time" dateTime={value} suppressHydrationWarning {...props}>
        {formatRelativeTime(value)}
      </Text>
    </Tooltip>
  );
}
