import {
  Alert,
  Anchor,
  Badge,
  Button,
  Divider,
  Group,
  Stack,
  Table,
  Text,
  Title,
} from "@mantine/core";
import { useEffect, type ReactNode } from "react";
import { Link, useSearchParams } from "react-router";
import type { ServerGateScope, WeeklyWindow } from "~/api/generated";
import { RelativeTime, Timestamp } from "~/shared/display-values";
import { GatePolicyEditor } from "./gate-policy-editor";
import { gateEditorParams, selectedGateEditor, type GateChange } from "./gate-policy";
import type { GatePolicyRead } from "./gate-policy.server";

export function GatePolicyPanel({
  policy,
  canManage,
  subjectName,
}: Readonly<{ policy: GatePolicyRead; canManage: boolean; subjectName: string }>) {
  const [params, setParams] = useSearchParams();
  const editor = selectedGateEditor(params, canManage);
  function select(action?: GateChange["action"]) {
    setParams(gateEditorParams(params, action), { replace: !action, preventScrollReset: true });
  }
  const canonical = gateEditorParams(params, editor).toString();
  const current = params.toString();
  useEffect(() => {
    if (canonical !== current) setParams(canonical, { replace: true, preventScrollReset: true });
  }, [canonical, current, setParams]);

  const scope = policy.scope;
  if (!scope)
    return (
      <Alert color="red" title="Gate policy unavailable">
        {policy.error}
      </Alert>
    );
  const lock = scope.lock;
  const window = scope.window;
  return (
    <Stack gap="md">
      <PolicyHeading
        title="Lock"
        controls={
          canManage && (
            <Button
              size="compact-sm"
              variant="light"
              onClick={() => select(lock ? "unlock" : "lock")}
            >
              {lock ? "Release lock" : "Create lock"}
            </Button>
          )
        }
      />
      {lock ? (
        <Alert color="yellow" title="Active lock">
          <Stack gap={2}>
            <Text size="sm">{lock.reason}</Text>
            <Text size="sm">
              {lock.actor.displayName} · <Timestamp value={lock.createdAt} /> ·{" "}
              {lock.expiresAt ? (
                <>
                  Expires <Timestamp value={lock.expiresAt} />
                </>
              ) : (
                "Until released"
              )}
            </Text>
          </Stack>
        </Alert>
      ) : (
        <Text size="sm" c="dimmed">
          No active lock.
        </Text>
      )}
      <Divider />
      <PolicyHeading
        title="Weekly window"
        controls={
          canManage && (
            <Group gap="xs">
              <Button size="compact-sm" variant="light" onClick={() => select("set-window")}>
                {window ? "Edit weekly window" : "Set weekly window"}
              </Button>
              {window && (
                <Button size="compact-sm" variant="default" onClick={() => select("remove-window")}>
                  Remove window
                </Button>
              )}
            </Group>
          )
        }
      />
      {window ? (
        <Stack gap="xs">
          <WindowStatus window={window} />
          <Schedule definition={window.definition} />
          {window.state === "unavailable" && (
            <Text size="sm">
              Release waits for a fresh window observation or a matching waiver.
            </Text>
          )}
          <Text c="dimmed" size="sm">
            Observed <Timestamp value={window.observation.evaluatedAt} /> · Valid until{" "}
            <Timestamp value={window.observation.validUntil} />
          </Text>
        </Stack>
      ) : (
        <Text size="sm" c="dimmed">
          No weekly window configured.
        </Text>
      )}
      <Divider />
      <Title order={3}>Policy history</Title>
      <PolicyHistory scope={scope} />
      {editor && (
        <GatePolicyEditor
          key={scope.id + editor}
          action={editor}
          onClose={() => select()}
          scope={scope}
          subjectName={subjectName}
        />
      )}
    </Stack>
  );
}

function PolicyHeading({ title, controls }: Readonly<{ title: string; controls: ReactNode }>) {
  return (
    <Group align="center" justify="space-between">
      <Title order={3}>{title}</Title>
      {controls}
    </Group>
  );
}

function WindowStatus({ window }: Readonly<{ window: NonNullable<ServerGateScope["window"]> }>) {
  const labels = { passing: "Open", blocking: "Closed", unavailable: "Unavailable" };
  const observation = window.observation;
  return (
    <Group gap="xs">
      <Badge color={window.state === "passing" ? "green" : "yellow"}>{labels[window.state]}</Badge>
      <Text size="sm">{window.definition.timeZone}</Text>
      {window.state === "passing" && observation.intervalStart && (
        <Text size="sm">
          Open since <Timestamp value={observation.intervalStart} />
        </Text>
      )}
      {window.state !== "unavailable" && observation.nextTransition && (
        <Text size="sm">
          {window.state === "passing" ? "Closes" : "Opens"}{" "}
          <RelativeTime value={observation.nextTransition} />
        </Text>
      )}
    </Group>
  );
}

function Schedule({ definition }: Readonly<{ definition: WeeklyWindow }>) {
  return (
    <Stack gap={0}>
      {definition.weekly.map((interval) => (
        <Text key={interval.days.join() + interval.start + interval.end} size="sm">
          {interval.days.map((day) => day.slice(0, 1).toUpperCase() + day.slice(1, 3)).join(", ")} ·{" "}
          {interval.start}–{interval.end}
        </Text>
      ))}
    </Stack>
  );
}

function PolicyHistory({ scope }: Readonly<{ scope: ServerGateScope }>) {
  const events = [...scope.events, ...scope.windowEvents].toSorted(
    (a, b) => Date.parse(b.recordedAt) - Date.parse(a.recordedAt),
  );
  if (!events.length)
    return (
      <Text size="sm" c="dimmed">
        No policy changes recorded.
      </Text>
    );
  const labels = {
    lock: "Lock created",
    unlock: "Lock released",
    set: "Window set",
    remove: "Window removed",
  };
  return (
    <Table.ScrollContainer minWidth={520}>
      <Table>
        <Table.Thead>
          <Table.Tr>
            <Table.Th>Change</Table.Th>
            <Table.Th>Actor</Table.Th>
            <Table.Th>Recorded</Table.Th>
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {events.map((event) => (
            <Table.Tr key={event.action + event.version}>
              <Table.Td>
                <Text size="sm" fw={600}>
                  {labels[event.action]} · v{event.version}
                </Text>
                {"reason" in event && <Text size="sm">{event.reason}</Text>}
                {"expiresAt" in event && event.expiresAt && (
                  <Text size="sm">
                    Expiry <Timestamp value={event.expiresAt} />
                  </Text>
                )}
                {"definition" in event && event.definition && (
                  <>
                    <Text size="sm">{event.definition.timeZone}</Text>
                    <Schedule definition={event.definition} />
                  </>
                )}
              </Table.Td>
              <Table.Td>{event.actor.displayName}</Table.Td>
              <Table.Td>
                <Timestamp value={event.recordedAt} />
              </Table.Td>
            </Table.Tr>
          ))}
        </Table.Tbody>
      </Table>
    </Table.ScrollContainer>
  );
}

export function GlobalGateSummary({
  policy,
}: Readonly<{
  policy: { error?: string; scope?: Pick<ServerGateScope, "lock" | "window"> };
}>) {
  const scope = policy.scope;
  return (
    <Stack gap="xs">
      <Group justify="space-between">
        <Title order={3}>Global policy</Title>
        <Anchor component={Link} to="/gates" size="sm">
          View global Gates
        </Anchor>
      </Group>
      {!scope ? (
        <Alert color="red" title="Global policy unavailable">
          {policy.error}
        </Alert>
      ) : (
        <>
          {scope.lock && <Text size="sm">Global lock active</Text>}
          {scope.window && <WindowStatus window={scope.window} />}
          {!scope.lock && !scope.window && (
            <Text size="sm" c="dimmed">
              No global lock or weekly window.
            </Text>
          )}
        </>
      )}
    </Stack>
  );
}
