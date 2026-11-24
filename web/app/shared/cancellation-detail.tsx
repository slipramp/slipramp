import { Badge, Button, Group, Paper, Stack, Text } from "@mantine/core";
import { IconBan, IconOctagonFilled } from "@tabler/icons-react";
import { Form, useNavigation } from "react-router";
import type { Cancellation } from "~/api/generated";
import { Timestamp } from "~/shared/display-values";
import { Identity } from "~/shared/identity";
import { LifecycleStep } from "~/shared/lifecycle-step";

const cancellationLabels = {
  requested: "Requested",
  completed: "Completed",
  overtaken: "Overtaken",
} as const;

export function CancelButton({ children }: Readonly<{ children: string }>) {
  const navigation = useNavigation();
  return (
    <Form method="post">
      <input name="intent" type="hidden" value="cancel" />
      <Button
        color="red"
        leftSection={<IconOctagonFilled aria-hidden size={16} />}
        loading={
          navigation.state === "submitting" && navigation.formData?.get("intent") === "cancel"
        }
        size="compact-md"
        type="submit"
        variant="filled"
      >
        {children}
      </Button>
    </Form>
  );
}

export function CancellationStep({
  cancellation,
  selected,
  onSelect,
}: Readonly<{
  cancellation: Cancellation;
  selected: boolean;
  onSelect: () => void;
}>) {
  if (cancellation.state === "not-requested") return null;
  return (
    <LifecycleStep
      icon={IconBan}
      label="Cancellation"
      time={cancellation.requestedAt}
      color={cancellation.state === "requested" ? "yellow" : "gray"}
      status={cancellationLabels[cancellation.state]}
      selected={selected}
      onSelect={onSelect}
      highlighted
    />
  );
}

export function CancellationDetail({
  cancellation,
  subject,
  outcome,
  finishedAt,
}: Readonly<{
  cancellation: Cancellation;
  subject: "Run" | "Promotion";
  outcome?: string;
  finishedAt?: string;
}>) {
  if (cancellation.state === "not-requested") return null;
  const { state, requestedAt, requestedBy } = cancellation;
  return (
    <Paper bg="var(--mantine-color-default-hover)" p="sm" radius="md">
      <Stack gap={5}>
        <Group gap="xs" justify="space-between">
          <Text fw={700} size="sm">
            Cancellation
          </Text>
          <Badge color={state === "requested" ? "yellow" : "gray"} variant="light">
            {cancellationLabels[state]}
          </Badge>
        </Group>
        <Group gap={4}>
          <Text size="xs">Requested</Text>
          {requestedAt && <Timestamp size="xs" value={requestedAt} />}
          {requestedBy && (
            <>
              <Text size="xs">by</Text>
              <Identity avatarSize={18} name={requestedBy.displayName} nameProps={{ size: "xs" }} />
            </>
          )}
        </Group>
        <Text size="xs" c="dimmed">
          {state === "requested"
            ? "Waiting for the Controller to confirm the outcome."
            : state === "completed"
              ? `The ${subject} was canceled.`
              : `The ${subject} finished before cancellation${outcome ? ` (${outcome})` : ""}.`}
        </Text>
        {state !== "requested" && finishedAt && finishedAt !== requestedAt && (
          <Group gap={4}>
            <Text c="dimmed" size="xs">
              Finished
            </Text>
            <Timestamp c="dimmed" size="xs" value={finishedAt} />
          </Group>
        )}
      </Stack>
    </Paper>
  );
}
