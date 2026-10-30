import { Text, Tooltip, type TextProps } from "@mantine/core";

import { formatDatestamp, formatRelativeTime, formatTimestamp } from "~/shared/display";
import { useTimeZone } from "~/shared/time-zone-preference";

type TimeDisplayProps = TextProps & {
  value: string;
};

export function Timestamp({ value, ...props }: Readonly<TimeDisplayProps>) {
  const { timeZone } = useTimeZone();
  const formatted = formatTimestamp(value, timeZone);

  return (
    <Tooltip label={formatted} openDelay={350}>
      <Text component="time" dateTime={value} suppressHydrationWarning {...props}>
        {formatted}
      </Text>
    </Tooltip>
  );
}

export function Datestamp({ value, ...props }: Readonly<TimeDisplayProps>) {
  const { timeZone } = useTimeZone();
  const formatted = formatDatestamp(value, timeZone);

  return (
    <Tooltip label={formatTimestamp(value, timeZone)} openDelay={350}>
      <Text component="time" dateTime={value} suppressHydrationWarning {...props}>
        {formatted}
      </Text>
    </Tooltip>
  );
}

export function RelativeTime({ value, ...props }: Readonly<TimeDisplayProps>) {
  const { timeZone } = useTimeZone();

  return (
    <Tooltip label={formatTimestamp(value, timeZone)} openDelay={350}>
      <Text component="time" dateTime={value} suppressHydrationWarning {...props}>
        {formatRelativeTime(value)}
      </Text>
    </Tooltip>
  );
}
