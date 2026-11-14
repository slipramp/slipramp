import {
  Alert,
  Anchor,
  Button,
  Grid,
  Group,
  Menu,
  Modal,
  Paper,
  Radio,
  ScrollArea,
  Stack,
  Text,
  Tooltip,
} from "@mantine/core";
import { notifications } from "@mantine/notifications";
import { IconArrowUp, IconChevronDown, IconExternalLink, IconRefresh } from "@tabler/icons-react";
import { useEffect, useRef, type ReactNode } from "react";
import { Link, useFetcher, useSearchParams } from "react-router";

import type { PromotionCreationActionResult } from "~/features/promotions/promotion-creation";
import {
  initialPromotionFreightName,
  promotionCreationContext,
  promotionCreationURLSelection,
  updatePromotionCreationSearchParams,
  visiblePromotionFreight,
} from "~/features/promotions/promotion-creation";
import { FreightContents, FreightIdentifier } from "~/features/promotions/freight-display";
import { freightOriginLabel } from "~/features/promotions/promotion-freight";
import { StagePolicyBadge } from "~/features/promotions/promotion-policy-badge";
import type { PromotionFreightItem, PromotionProjectStage } from "~/api/generated";

type PromotionCreateProps = Readonly<{
  stage: Pick<PromotionProjectStage, "id" | "name" | "policy" | "capabilities">;
  freight: PromotionFreightItem[];
  inPlace?: boolean;
  initialFreightName?: string;
  sourceStageId?: string;
  sourceStageName?: string;
}>;

type PromotionCreateMenuProps = Readonly<{
  stage: PromotionCreateProps["stage"];
  eligibleFreight: PromotionFreightItem[];
  currentFreight: PromotionFreightItem[];
}>;

function PromotionCreateDialog({
  stage,
  freight,
  inPlace,
  sourceStageId,
  sourceStageName,
  onClose,
  onSelectedNameChange,
  selectedName,
}: PromotionCreateProps & {
  onClose: () => void;
  onSelectedNameChange: (name: string) => void;
  selectedName: string;
}) {
  const fetcher = useFetcher<PromotionCreationActionResult>();
  const [searchParams, setSearchParams] = useSearchParams();
  const handledPromotionID = useRef<string | undefined>(undefined);
  const selected = freight.find((item) => item.name === selectedName);
  const submittingMode = fetcher.formData?.get("mode");
  const actionLabel = inPlace ? "Re-promote" : "Promote";

  function editPolicy() {
    const next = new URLSearchParams(searchParams);
    next.set("policy", stage.id);
    setSearchParams(next, { preventScrollReset: true });
  }

  useEffect(() => {
    const promotionID = fetcher.data?.promotionId;
    if (!promotionID || handledPromotionID.current === promotionID) return;

    handledPromotionID.current = promotionID;
    notifications.show({
      color: "green",
      message: (
        <Text size="sm">
          Slipramp is creating it in Kargo.{" "}
          <Anchor component={Link} to={`/promotions/${promotionID}`}>
            View Promotion
          </Anchor>
        </Text>
      ),
      title: "Promotion started",
    });
    onClose();
  }, [fetcher.data, onClose]);

  return (
    <Modal
      centered
      onClose={onClose}
      opened
      size="min(72rem, calc(100vw - 2rem))"
      stackId="promotion-create"
      styles={{ title: { flex: 1 } }}
      title={
        <Group gap="md" justify="space-between" wrap="nowrap" w="100%">
          <Text fw={600}>{`${actionLabel} ${stage.name}`}</Text>
          <StagePolicyBadge canEdit onEdit={editPolicy} stage={stage} />
        </Group>
      }
    >
      <fetcher.Form method="post">
        <Stack gap="md">
          {sourceStageName ? <PromotionFact label="Source Stage" value={sourceStageName} /> : null}

          {fetcher.data?.error ? (
            <Alert color="red" title="Promotion not created">
              {fetcher.data.error}
            </Alert>
          ) : null}

          <SelectedFreight freight={selected} />

          {freight.length > 1 || !selected ? (
            <AvailableFreight
              freight={freight}
              onChange={onSelectedNameChange}
              selectedName={selected?.name ?? ""}
            />
          ) : null}

          {inPlace ? (
            <Alert color="yellow" title="In-place Promotion">
              This reruns the Stage&apos;s Kargo promotion template with the same Freight.
            </Alert>
          ) : null}

          <input name="targetStageId" type="hidden" value={stage.id} />
          {sourceStageId || inPlace ? (
            <input name="sourceStageId" type="hidden" value={sourceStageId ?? stage.id} />
          ) : null}
          <input name="freightName" type="hidden" value={selected?.name ?? ""} />

          <Group justify="space-between" wrap="wrap">
            <Button onClick={onClose} type="button" variant="default">
              Cancel
            </Button>
            <Group gap="sm">
              <Button
                disabled={!selected}
                loading={fetcher.state !== "idle" && submittingMode === "background"}
                name="mode"
                type="submit"
                value="background"
                variant="light"
              >
                {actionLabel} in background
              </Button>
              <Button
                disabled={!selected}
                leftSection={<IconExternalLink aria-hidden size={15} />}
                loading={fetcher.state !== "idle" && submittingMode === "view"}
                name="mode"
                type="submit"
                value="view"
              >
                {actionLabel} and view
              </Button>
            </Group>
          </Group>
        </Stack>
      </fetcher.Form>
    </Modal>
  );
}

export function PromotionCreateButton({
  unavailableLabel = "No eligible Freight is currently observed.",
  ...props
}: PromotionCreateProps & { unavailableLabel?: string }) {
  const capability = props.stage.capabilities.promotion;
  const actionLabel = props.inPlace ? "Re-promote" : "Promote";
  const dialog = usePromotionCreateDialogState([props]);

  if (capability.status !== "ready" || (props.freight.length === 0 && !dialog.props)) {
    return (
      <Tooltip label={capability.reason ?? unavailableLabel}>
        <span>
          <Button disabled leftSection={<IconArrowUp aria-hidden size={14} />} size="compact-xs">
            {actionLabel}
          </Button>
        </span>
      </Tooltip>
    );
  }

  return (
    <>
      <Button
        leftSection={<IconArrowUp aria-hidden size={14} />}
        onClick={() => dialog.open(props)}
        size="compact-xs"
        variant="light"
      >
        {actionLabel}
      </Button>
      {dialog.props ? (
        <PromotionCreateDialog
          {...dialog.props}
          onClose={dialog.close}
          onSelectedNameChange={dialog.changeSelectedName}
          selectedName={dialog.selectedName}
        />
      ) : null}
    </>
  );
}

export function PromotionCreateMenu({
  stage,
  eligibleFreight,
  currentFreight,
}: PromotionCreateMenuProps) {
  const capability = stage.capabilities.promotion;
  const options = [
    {
      icon: <IconArrowUp aria-hidden size={15} />,
      label: "Promote eligible Freight",
      props: { freight: eligibleFreight, stage },
      unavailableLabel: "No eligible Freight is currently observed.",
    },
    {
      icon: <IconRefresh aria-hidden size={15} />,
      label: "Re-promote in place",
      props: { freight: currentFreight, inPlace: true, stage },
      unavailableLabel: "This Stage has no current Freight to promote in place.",
    },
  ] satisfies Array<{
    icon: ReactNode;
    label: string;
    props: PromotionCreateProps;
    unavailableLabel: string;
  }>;
  const dialog = usePromotionCreateDialogState(options.map((option) => option.props));

  return (
    <>
      <Menu position="bottom-end" shadow="md" width={280}>
        <Menu.Target>
          <Button
            leftSection={<IconArrowUp aria-hidden size={14} />}
            rightSection={<IconChevronDown aria-hidden size={14} />}
            size="compact-xs"
            variant="light"
          >
            Promote
          </Button>
        </Menu.Target>
        <Menu.Dropdown>
          {options.map((option) => {
            const unavailable =
              capability.status !== "ready"
                ? (capability.reason ?? "Promotion is unavailable for this Stage.")
                : option.props.freight.length === 0
                  ? option.unavailableLabel
                  : undefined;

            return (
              <Menu.Item
                disabled={unavailable !== undefined}
                key={option.label}
                leftSection={option.icon}
                onClick={() => dialog.open(option.props)}
                title={unavailable}
              >
                {option.label}
              </Menu.Item>
            );
          })}
        </Menu.Dropdown>
      </Menu>
      {dialog.props ? (
        <PromotionCreateDialog
          {...dialog.props}
          onClose={dialog.close}
          onSelectedNameChange={dialog.changeSelectedName}
          selectedName={dialog.selectedName}
        />
      ) : null}
    </>
  );
}

function usePromotionCreateDialogState(options: PromotionCreateProps[]) {
  const [searchParams, setSearchParams] = useSearchParams();
  const requested = promotionCreationURLSelection(searchParams);
  const props = options.find(
    (option) =>
      requested?.stageId === option.stage.id &&
      requested.context === promotionCreationContext(option) &&
      requested.sourceStageId === option.sourceStageId,
  );
  const selectedName = props
    ? initialPromotionFreightName(props.freight, requested?.freightName ?? props.initialFreightName)
    : "";

  function open(option: PromotionCreateProps) {
    const initialName = initialPromotionFreightName(option.freight, option.initialFreightName);
    setSearchParams(
      updatePromotionCreationSearchParams(searchParams, {
        context: promotionCreationContext(option),
        freightName: initialName || undefined,
        sourceStageId: option.sourceStageId,
        stageId: option.stage.id,
      }),
      { preventScrollReset: true },
    );
  }

  function close() {
    setSearchParams(updatePromotionCreationSearchParams(searchParams), {
      preventScrollReset: true,
      replace: true,
    });
  }

  function changeSelectedName(name: string) {
    if (!requested) return;
    setSearchParams(
      updatePromotionCreationSearchParams(searchParams, {
        ...requested,
        freightName: name || undefined,
      }),
      { preventScrollReset: true, replace: true },
    );
  }

  return { changeSelectedName, close, open, props, selectedName };
}

function SelectedFreight({ freight }: Readonly<{ freight?: PromotionFreightItem }>) {
  return (
    <Paper p="md" withBorder>
      <Stack gap="sm">
        <Text fw={600}>Selected Freight</Text>
        {freight ? (
          <FreightSummary freight={freight} />
        ) : (
          <Text c="dimmed" size="sm">
            Choose one of the available Freight entries below.
          </Text>
        )}
      </Stack>
    </Paper>
  );
}

function AvailableFreight({
  freight,
  onChange,
  selectedName,
}: Readonly<{
  freight: PromotionFreightItem[];
  onChange: (name: string) => void;
  selectedName: string;
}>) {
  const visibleFreight = visiblePromotionFreight(freight, selectedName);

  return (
    <Stack gap="xs">
      <Group justify="space-between" wrap="wrap">
        <Text fw={600}>Available Freight</Text>
        <Text c="dimmed" size="xs">
          Select the exact Freight to promote
        </Text>
      </Group>
      <ScrollArea.Autosize mah={360} offsetScrollbars type="auto">
        <Radio.Group aria-label="Available Freight" onChange={onChange} value={selectedName}>
          <Stack gap="xs">
            {visibleFreight.map((item) => (
              <Radio.Card
                aria-label={`Select Freight ${item.name}`}
                key={`${item.origin}:${item.name}`}
                p="md"
                radius="md"
                value={item.name}
              >
                <Group align="center" gap="md" wrap="nowrap">
                  <Radio.Indicator />
                  <FreightSummary freight={item} />
                </Group>
              </Radio.Card>
            ))}
          </Stack>
        </Radio.Group>
      </ScrollArea.Autosize>
    </Stack>
  );
}

function FreightSummary({ freight }: Readonly<{ freight: PromotionFreightItem }>) {
  return (
    <Grid gap="md" style={{ flex: 1, minWidth: 0 }}>
      <Grid.Col span={{ base: 12, sm: 2 }}>
        <FreightField label="Freight ID">
          <FreightIdentifier freight={freight} />
        </FreightField>
      </Grid.Col>
      <Grid.Col span={{ base: 12, sm: 2 }}>
        <FreightField label="Warehouse">
          <Text size="sm">{freightOriginLabel(freight.origin)}</Text>
        </FreightField>
      </Grid.Col>
      <Grid.Col span={{ base: 12, sm: 8 }}>
        <FreightField label="Contents">
          <FreightContents freight={freight} />
        </FreightField>
      </Grid.Col>
    </Grid>
  );
}

function FreightField({ children, label }: Readonly<{ children: ReactNode; label: string }>) {
  return (
    <Stack gap={4}>
      <Text c="dimmed" size="xs">
        {label}
      </Text>
      {children}
    </Stack>
  );
}

function PromotionFact({ label, value }: Readonly<{ label: string; value: string }>) {
  return (
    <Stack gap={0}>
      <Text c="dimmed" size="xs">
        {label}
      </Text>
      <Text>{value}</Text>
    </Stack>
  );
}
