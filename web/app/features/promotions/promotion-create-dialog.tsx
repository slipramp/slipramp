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
  currentFreightForPromotion,
  eligibleStageFreight,
  initialPromotionFreightName,
  promotionCreationContext,
  promotionCreationURLSelection,
  upstreamPromotionOptions,
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
  repeatLabel?: "Retry" | "Re-promote";
  formAction?: string;
  notice?: string;
  currentFreight?: PromotionFreightItem[];
  inPlace?: boolean;
  initialFreightName?: string;
  sourceStageId?: string;
  sourceStageName?: string;
}>;

function PromotionCreateDialog({
  stage,
  freight,
  currentFreight,
  inPlace,
  repeatLabel,
  formAction,
  notice,
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
  const current = currentFreightForPromotion(currentFreight ?? [], selected);
  const actionLabel = repeatLabel ?? (inPlace ? "Re-promote" : "Promote");
  const submittingMode = fetcher.formData?.get("mode");

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
          <StagePolicyBadge canEdit={!formAction} onEdit={editPolicy} stage={stage} />
        </Group>
      }
    >
      <fetcher.Form action={formAction} method="post">
        <Stack gap="md">
          {notice ? <Text size="sm">{notice}</Text> : null}
          {sourceStageName ? <PromotionFact label="Source Stage" value={sourceStageName} /> : null}

          {fetcher.data?.error ? (
            <Alert color="red" title="Promotion not created">
              {fetcher.data.error}
            </Alert>
          ) : null}

          {currentFreight ? (
            <FreightComparison current={current} selected={selected} />
          ) : (
            <SelectedFreight freight={selected} />
          )}

          {freight.length > 1 || (!currentFreight && !selected) ? (
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

function promotionDialogSearch(searchParams: URLSearchParams, option: PromotionCreateProps) {
  return updatePromotionCreationSearchParams(searchParams, {
    context: promotionCreationContext(option),
    freightName:
      initialPromotionFreightName(option.freight, option.initialFreightName) || undefined,
    sourceStageId: option.sourceStageId,
    stageId: option.stage.id,
  }).toString();
}

function promotionUnavailable(option: PromotionCreateProps, emptyLabel: string) {
  const capability = option.stage.capabilities.promotion;
  if (capability.status !== "ready")
    return capability.reason ?? "Promotion is unavailable for this Stage.";
  return option.freight.length === 0 ? emptyLabel : undefined;
}

function PromotionDialogLink({
  label,
  option,
  size = "compact-xs",
  unavailableLabel,
}: Readonly<{
  label: string;
  option: PromotionCreateProps;
  size?: "compact-xs" | "compact-md";
  unavailableLabel: string;
}>) {
  const [searchParams] = useSearchParams();
  const unavailable = promotionUnavailable(option, unavailableLabel);
  const Icon = option.inPlace || option.repeatLabel ? IconRefresh : IconArrowUp;
  const props = {
    leftSection: <Icon aria-hidden size={size === "compact-md" ? 16 : 14} />,
    size,
    variant: "light",
  };
  if (unavailable) {
    return (
      <Tooltip label={unavailable}>
        <span>
          <Button {...props} disabled>
            {label}
          </Button>
        </span>
      </Tooltip>
    );
  }
  return (
    <Button
      {...props}
      component={Link}
      preventScrollReset
      to={{ search: promotionDialogSearch(searchParams, option) }}
    >
      {label}
    </Button>
  );
}

export function PromotionCreateButton({
  unavailableLabel = "No eligible Freight is currently observed.",
  ...option
}: PromotionCreateProps & { unavailableLabel?: string }) {
  return (
    <>
      <PromotionDialogLink
        label={option.repeatLabel ?? (option.inPlace ? "Re-promote" : "Promote")}
        option={option}
        size={option.repeatLabel ? "compact-md" : "compact-xs"}
        unavailableLabel={unavailableLabel}
      />
      <PromotionDialogFor options={[option]} />
    </>
  );
}

export function FreightPromotionLink({
  stage,
  freight,
  sourceStageId,
}: Readonly<{
  stage: PromotionProjectStage;
  freight: PromotionFreightItem;
  sourceStageId?: string;
}>) {
  return (
    <PromotionDialogLink
      label="Promote"
      option={{ stage, freight: [freight], initialFreightName: freight.name, sourceStageId }}
      unavailableLabel="No eligible Freight is currently observed."
    />
  );
}

function inPlaceOption(stage: PromotionProjectStage) {
  return {
    icon: <IconRefresh aria-hidden size={15} />,
    label: "Re-promote in place",
    props: { freight: stage.freight.items, inPlace: true, stage },
    unavailableLabel: "This Stage has no current Freight to promote in place.",
  };
}

export function InPlacePromotionLink({
  stage,
  label,
}: Readonly<{ stage: PromotionProjectStage; label: "Sync" | "Re-promote" }>) {
  const option = inPlaceOption(stage);
  return (
    <PromotionDialogLink
      label={label}
      option={option.props}
      size={label === "Sync" ? "compact-md" : "compact-xs"}
      unavailableLabel={option.unavailableLabel}
    />
  );
}

function stagePromotionOptions(stage: PromotionProjectStage) {
  const currentFreight = stage.freight.items;
  const eligibleFreight = eligibleStageFreight(stage);
  const upstreamOptions = upstreamPromotionOptions(stage);
  return [
    {
      icon: <IconArrowUp aria-hidden size={15} />,
      label: "Promote eligible Freight",
      props: { freight: eligibleFreight, stage },
      unavailableLabel: "No eligible Freight is currently observed.",
    },
    ...upstreamOptions.map((option) => ({
      icon: <IconArrowUp aria-hidden size={15} />,
      label: `Promote from ${option.sourceStageName}`,
      props: {
        currentFreight,
        freight: option.freight,
        sourceStageId: option.sourceStageId,
        sourceStageName: option.sourceStageName,
        stage,
      },
      unavailableLabel: "No eligible current Freight is observed on this upstream Stage.",
    })),
    inPlaceOption(stage),
  ] satisfies Array<{
    icon: ReactNode;
    label: string;
    props: PromotionCreateProps;
    unavailableLabel: string;
  }>;
}

export function StagePromotionDialog({ stage }: Readonly<{ stage: PromotionProjectStage }>) {
  const [searchParams] = useSearchParams();
  const requested = promotionCreationURLSelection(searchParams);
  if (requested?.stageId !== stage.id) return null;
  if (requested.context === "candidate") {
    const candidate = stage.requestedFreight
      .filter((request) => request.direct)
      .flatMap((request) => request.candidates)
      .find((item) => item.readiness === "ready" && item.freight.name === requested.freightName);
    if (!candidate) return null;
    return (
      <PromotionDialogFor
        options={[
          {
            stage,
            currentFreight: stage.freight.items,
            freight: [candidate.freight],
            initialFreightName: candidate.freight.name,
          },
        ]}
      />
    );
  }
  return (
    <PromotionDialogFor options={stagePromotionOptions(stage).map((option) => option.props)} />
  );
}

export function PromotionCreateMenu({ stage }: Readonly<{ stage: PromotionProjectStage }>) {
  const [searchParams, setSearchParams] = useSearchParams();
  return (
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
        {stagePromotionOptions(stage).map((option) => {
          const unavailable = promotionUnavailable(option.props, option.unavailableLabel);
          return (
            <Menu.Item
              disabled={unavailable !== undefined}
              key={option.label}
              leftSection={option.icon}
              onClick={() =>
                setSearchParams(promotionDialogSearch(searchParams, option.props), {
                  preventScrollReset: true,
                })
              }
              title={unavailable}
            >
              {option.label}
            </Menu.Item>
          );
        })}
      </Menu.Dropdown>
    </Menu>
  );
}

function PromotionDialogFor({ options }: Readonly<{ options: PromotionCreateProps[] }>) {
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

  if (!props || props.stage.capabilities.promotion.status !== "ready") return null;
  return (
    <PromotionCreateDialog
      {...props}
      onClose={close}
      onSelectedNameChange={changeSelectedName}
      selectedName={selectedName}
    />
  );
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

function FreightComparison({
  current,
  selected,
}: Readonly<{
  current?: PromotionFreightItem;
  selected?: PromotionFreightItem;
}>) {
  return (
    <Stack gap="lg">
      <FreightComparisonItem
        emptyMessage="No current Freight is observed for this Warehouse."
        freight={current}
      >
        Current Freight
      </FreightComparisonItem>
      <FreightComparisonItem emptyMessage="Choose the Freight to promote." freight={selected}>
        Selected Freight
      </FreightComparisonItem>
    </Stack>
  );
}

function FreightComparisonItem({
  children,
  emptyMessage,
  freight,
}: Readonly<{
  children: ReactNode;
  emptyMessage: string;
  freight?: PromotionFreightItem;
}>) {
  return (
    <Stack gap="sm">
      <Text fw={600}>{children}</Text>
      {freight ? (
        <FreightSummary freight={freight} />
      ) : (
        <Text c="dimmed" size="sm">
          {emptyMessage}
        </Text>
      )}
    </Stack>
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
