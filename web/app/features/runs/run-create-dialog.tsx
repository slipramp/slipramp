import {
  Alert,
  Anchor,
  Button,
  Center,
  Group,
  Loader,
  Menu,
  Modal,
  Stack,
  Text,
} from "@mantine/core";
import { notifications } from "@mantine/notifications";
import { IconChevronDown, IconExternalLink, IconPlayerPlay } from "@tabler/icons-react";
import { useEffect, useRef, type CSSProperties } from "react";
import { Link, useFetcher, useSearchParams } from "react-router";

import type { Action, Target, TargetDefinition } from "~/api/generated";
import {
  actionRunDialogDataPath,
  actionRunPath,
  runCreationURLSelection,
  updateRunCreationSearchParams,
  type RunCreationActionResult,
} from "~/features/runs/action-run";
import { RunParameterFields } from "~/features/runs/run-parameter-fields";

type RunCreateOption = Readonly<{
  actionName: string;
  actionTitle?: string;
  targetId: string;
}>;

type RunCreateDialogLoad = {
  actionDefinition?: Action;
  error?: string;
  record?: Target;
  target?: TargetDefinition;
};

function RunCreateDialog({
  actionName,
  actionTitle,
  onClose,
  targetId,
}: RunCreateOption & { onClose: () => void }) {
  const definitionFetcher = useFetcher<RunCreateDialogLoad>();
  const creationFetcher = useFetcher<RunCreationActionResult>();
  const loadDefinition = definitionFetcher.load;
  const handledRunID = useRef<string | undefined>(undefined);
  const fullViewPath = actionRunPath(targetId, actionName);
  const definition = definitionFetcher.data;
  const loadedAction = definition?.actionDefinition;
  const unavailable =
    definition !== undefined &&
    (!definition.record || definition.record.archivedAt || !definition.target || !loadedAction);
  const inputs = Object.keys(loadedAction?.inputs ?? {});
  const submittingMode = creationFetcher.formData?.get("mode");

  useEffect(() => {
    void loadDefinition(actionRunDialogDataPath(targetId, actionName));
  }, [actionName, loadDefinition, targetId]);

  useEffect(() => {
    const runID = creationFetcher.data?.runId;
    if (!runID || handledRunID.current === runID) return;

    handledRunID.current = runID;
    notifications.show({
      color: "green",
      message: (
        <Text size="sm">
          Slipramp created the Run.{" "}
          <Anchor component={Link} to={`/runs/${runID}`}>
            View Run
          </Anchor>
        </Text>
      ),
      title: "Run created",
    });
    onClose();
  }, [creationFetcher.data, onClose]);

  return (
    <Modal
      centered
      onClose={onClose}
      opened
      size="xl"
      styles={{ title: { flex: 1 } }}
      title={
        <Group gap="sm" justify="space-between" pr="sm" w="100%" wrap="wrap">
          <Text component="span" inherit>
            Run {loadedAction?.title ?? actionTitle ?? actionName}
          </Text>
          <Anchor component={Link} to={fullViewPath}>
            Full view
          </Anchor>
        </Group>
      }
    >
      {definition === undefined || definitionFetcher.state === "loading" ? (
        <Center mih={180}>
          <Loader aria-label="Loading action parameters" size="sm" />
        </Center>
      ) : unavailable ? (
        <Stack gap="md">
          <Alert color="red" title="Action unavailable">
            {definition.error ?? "This action is no longer available."}
          </Alert>
          <DialogCancelButton onClose={onClose} />
        </Stack>
      ) : inputs.length > 0 ? (
        <Stack gap="md">
          <Alert color="blue" title="Artifact input required">
            Start this action from a compatible artifact on its producing Run.
          </Alert>
          <DialogCancelButton onClose={onClose} />
        </Stack>
      ) : (
        <creationFetcher.Form action={fullViewPath} method="post">
          <Stack gap="lg">
            {creationFetcher.data?.error ? (
              <Alert color="red" title="Run not created">
                {creationFetcher.data.error}
              </Alert>
            ) : null}

            {loadedAction?.description || loadedAction?.documentationUrl ? (
              <Stack gap="xs">
                {loadedAction.description ? (
                  <Text c="dimmed">{loadedAction.description}</Text>
                ) : null}
                {loadedAction.documentationUrl ? (
                  <Anchor href={loadedAction.documentationUrl} rel="noreferrer" target="_blank">
                    <Group gap={4} wrap="nowrap">
                      <span>Documentation</span>
                      <IconExternalLink aria-hidden size={14} />
                    </Group>
                  </Anchor>
                ) : null}
              </Stack>
            ) : null}

            <RunParameterFields declarations={loadedAction?.parameters} />

            <Group justify="space-between" wrap="wrap">
              <Button onClick={onClose} type="button" variant="default">
                Cancel
              </Button>
              <Group gap="sm">
                <Button
                  loading={creationFetcher.state !== "idle" && submittingMode === "background"}
                  name="mode"
                  type="submit"
                  value="background"
                  variant="light"
                >
                  Create in background
                </Button>
                <Button
                  leftSection={<IconExternalLink aria-hidden size={15} />}
                  loading={creationFetcher.state !== "idle" && submittingMode === "view"}
                  name="mode"
                  type="submit"
                  value="view"
                >
                  Create and view
                </Button>
              </Group>
            </Group>
          </Stack>
        </creationFetcher.Form>
      )}
    </Modal>
  );
}

function DialogCancelButton({ onClose }: Readonly<{ onClose: () => void }>) {
  return (
    <Button onClick={onClose} type="button" variant="default">
      Cancel
    </Button>
  );
}

export function RunCreateButton({
  actionName,
  actionTitle,
  className,
  style,
  targetId,
}: RunCreateOption & { className?: string; style?: CSSProperties }) {
  const option = { actionName, actionTitle, targetId };
  const dialog = useRunCreateDialogState([option]);

  return (
    <>
      <Button
        className={className}
        leftSection={<IconPlayerPlay aria-hidden size={16} />}
        onClick={() => dialog.open(option)}
        style={style}
      >
        Run action
      </Button>
      {dialog.option ? (
        <RunCreateDialog key={dialog.key} {...dialog.option} onClose={dialog.close} />
      ) : null}
    </>
  );
}

export function RunCreateMenu({
  actions,
  buttonIcon = false,
  label = "Run action",
  position = "bottom-start",
  size = "compact-sm",
  targetId,
  width = 220,
}: Readonly<{
  actions: ReadonlyArray<{ name: string; title?: string }>;
  buttonIcon?: boolean;
  label?: string;
  position?: "bottom-end" | "bottom-start";
  size?: "compact-md" | "compact-sm";
  targetId: string;
  width?: number;
}>) {
  const options = actions.map((action) => ({
    actionName: action.name,
    actionTitle: action.title,
    targetId,
  }));
  const dialog = useRunCreateDialogState(options);

  return (
    <>
      <Menu position={position} shadow="md" width={width}>
        <Menu.Target>
          <Button
            leftSection={buttonIcon ? <IconPlayerPlay aria-hidden size={16} /> : undefined}
            rightSection={<IconChevronDown aria-hidden size={14} />}
            size={size}
            variant="light"
          >
            {label}
          </Button>
        </Menu.Target>
        <Menu.Dropdown>
          <Menu.Label>Choose an action</Menu.Label>
          {options.map((option) => (
            <Menu.Item
              key={option.actionName}
              leftSection={<IconPlayerPlay aria-hidden size={14} />}
              onClick={() => dialog.open(option)}
            >
              {option.actionTitle ? (
                <Stack gap={0}>
                  <Text size="sm">{option.actionTitle}</Text>
                  <Text c="dimmed" ff="monospace" size="xs">
                    {option.actionName}
                  </Text>
                </Stack>
              ) : (
                option.actionName
              )}
            </Menu.Item>
          ))}
        </Menu.Dropdown>
      </Menu>
      {dialog.option ? (
        <RunCreateDialog key={dialog.key} {...dialog.option} onClose={dialog.close} />
      ) : null}
    </>
  );
}

function useRunCreateDialogState(options: RunCreateOption[]) {
  const [searchParams, setSearchParams] = useSearchParams();
  const requested = runCreationURLSelection(searchParams);
  const option = options.find(
    (candidate) =>
      candidate.targetId === requested?.targetId && candidate.actionName === requested.actionName,
  );

  function open(next: RunCreateOption) {
    setSearchParams(
      updateRunCreationSearchParams(searchParams, {
        actionName: next.actionName,
        targetId: next.targetId,
      }),
      { preventScrollReset: true },
    );
  }

  function close() {
    setSearchParams(updateRunCreationSearchParams(searchParams), {
      preventScrollReset: true,
      replace: true,
    });
  }

  return {
    close,
    key: option ? `${option.targetId}:${option.actionName}` : undefined,
    open,
    option,
  };
}
