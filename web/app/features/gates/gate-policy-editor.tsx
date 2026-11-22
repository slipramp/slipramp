import {
  Alert,
  Button,
  Checkbox,
  Divider,
  Fieldset,
  Group,
  Modal,
  Stack,
  Text,
  TextInput,
  Textarea,
} from "@mantine/core";
import { useRef, useState } from "react";
import { useFetcher } from "react-router";

import type { ServerGateScope, WeeklyWindow } from "~/api/generated";
import { weekdays, type GateChange } from "./gate-policy";
import type { submitGatePolicy } from "./gate-policy.server";

const actionLabels = {
  lock: "Create lock",
  unlock: "Release lock",
  "set-window": "Save weekly window",
  "remove-window": "Remove weekly window",
};

export function GatePolicyEditor({
  scope,
  subjectName,
  action,
  onClose,
}: Readonly<{
  scope: ServerGateScope;
  subjectName: string;
  action: GateChange["action"];
  onClose: () => void;
}>) {
  const fetcher = useFetcher<typeof submitGatePolicy>();
  const busy = fetcher.state !== "idle";
  const label = actionLabels[action];
  return (
    <Modal
      opened
      centered
      title={label + " · " + subjectName}
      onClose={() => {
        if (!busy) onClose();
      }}
      size={action === "set-window" ? "lg" : "md"}
      closeOnEscape={!busy}
      closeOnClickOutside={!busy}
    >
      <fetcher.Form method="post">
        <Stack gap="md">
          <input type="hidden" name="gateAction" value={action} />
          {fetcher.data && "error" in fetcher.data && (
            <Alert color="red" title="Change could not be saved">
              {fetcher.data.error}
            </Alert>
          )}
          {action === "lock" && (
            <>
              <Textarea
                label="Reason"
                name="reason"
                required
                maxLength={1024}
                autosize
                minRows={2}
              />
              <TextInput
                label="Expiry in UTC"
                description="Leave empty to hold until released."
                name="expiryUTC"
                type="datetime-local"
              />
            </>
          )}
          {action === "unlock" && (
            <Text>
              Release the current lock{scope.lock ? " for “" + scope.lock.reason + "”" : ""}?
              Waiting work can proceed when its other Gates pass.
            </Text>
          )}
          {action === "set-window" && <WindowFields definition={scope.window?.definition} />}
          {action === "remove-window" && (
            <Text>
              Remove the weekly schedule from this scope? Waiting work can proceed whenever its
              other Gates pass.
            </Text>
          )}
          <Group justify="flex-end">
            <Button variant="default" disabled={busy} onClick={onClose}>
              Cancel
            </Button>
            <Button loading={busy} type="submit">
              {label}
            </Button>
          </Group>
        </Stack>
      </fetcher.Form>
    </Modal>
  );
}

function WindowFields({ definition }: Readonly<{ definition?: WeeklyWindow }>) {
  const [rows, setRows] = useState(() =>
    (definition?.weekly ?? [{ days: [], start: "09:00", end: "17:00" }]).map((interval, id) => ({
      id,
      interval,
    })),
  );
  const nextId = useRef(rows.length);
  function addRow() {
    const row = { id: nextId.current++, interval: { days: [], start: "09:00", end: "17:00" } };
    setRows((current) => [...current, row]);
  }
  return (
    <>
      <TextInput
        label="IANA time zone"
        description="For example, America/Denver or UTC."
        name="timeZone"
        defaultValue={definition?.timeZone ?? "UTC"}
        required
        maxLength={255}
      />
      <Text size="sm">
        Use local 24-hour times. Split overnight hours at midnight, ending the first interval at
        24:00.
      </Text>
      {rows.map(({ id, interval }, index) => (
        <Fieldset key={id} variant="unstyled" aria-label={"Interval " + (index + 1)}>
          <Stack gap="xs">
            <Divider />
            <Group justify="space-between">
              <Text fw={600} size="sm">
                Interval {index + 1}
              </Text>
              {rows.length > 1 && (
                <Button
                  variant="subtle"
                  size="compact-sm"
                  aria-label={"Remove interval " + (index + 1)}
                  onClick={() => setRows((current) => current.filter((row) => row.id !== id))}
                >
                  Remove
                </Button>
              )}
            </Group>
            <input type="hidden" name="interval" value={id} />
            <Group gap="sm">
              {weekdays.map((day) => (
                <Checkbox
                  key={day}
                  name={"days-" + id}
                  value={day}
                  label={day.slice(0, 1).toUpperCase() + day.slice(1, 3)}
                  aria-label={day}
                  defaultChecked={interval.days.some((d) => d === day)}
                />
              ))}
            </Group>
            <Group grow>
              <TextInput
                label="Start"
                name={"start-" + id}
                defaultValue={interval.start}
                placeholder="09:00"
                maxLength={5}
                required
              />
              <TextInput
                label="End"
                name={"end-" + id}
                defaultValue={interval.end}
                placeholder="17:00"
                maxLength={5}
                required
              />
            </Group>
          </Stack>
        </Fieldset>
      ))}
      <Button
        variant="light"
        size="compact-sm"
        w="fit-content"
        onClick={addRow}
        disabled={rows.length >= 128}
      >
        Add interval
      </Button>
      {definition && (
        <Text size="sm" c="dimmed">
          Saving creates a new window version. Existing waivers for this window will expire.
        </Text>
      )}
    </>
  );
}
