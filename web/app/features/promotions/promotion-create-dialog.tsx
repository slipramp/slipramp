import {
  Alert,
  Anchor,
  Badge,
  Button,
  Code,
  Group,
  Menu,
  Modal,
  NativeSelect,
  Paper,
  SimpleGrid,
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
} from "~/features/promotions/promotion-creation";
import {
  freightArtifactVersionLabel,
  freightDisplayLabel,
  freightOriginLabel,
} from "~/features/promotions/promotion-freight";
import type {
  PromotionFreightItem,
  PromotionProjectStage,
  PromotionStageCapability,
  PromotionStagePolicy,
} from "~/api/generated";
import { formatTimestamp } from "~/shared/display";
import { useTimeZone } from "~/shared/time-zone-preference";

type PromotionCreateProps = Readonly<{
  stage: Pick<PromotionProjectStage, "id" | "name" | "policy" | "capabilities">;
  freight: PromotionFreightItem[];
  environment?: string;
  inPlace?: boolean;
  initialFreightName?: string;
  sourceStageId?: string;
  sourceStageName?: string;
}>;

type PromotionCreateMenuProps = Readonly<{
  stage: PromotionCreateProps["stage"];
  eligibleFreight: PromotionFreightItem[];
  currentFreight: PromotionFreightItem[];
  environment?: string;
}>;

function PromotionCreateDialog({
  stage,
  freight,
  environment,
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
  const handledPromotionID = useRef<string | undefined>(undefined);
  const selected = freight.find((item) => item.name === selectedName);
  const submittingMode = fetcher.formData?.get("mode");
  const actionLabel = inPlace ? "Re-promote" : "Promote";

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
    <Modal centered onClose={onClose} opened size="lg" title={`${actionLabel} ${stage.name}`}>
      <fetcher.Form method="post">
        <Stack gap="md">
          {environment ? <PromotionFact label="Environment" value={environment} /> : null}
          {sourceStageName ? <PromotionFact label="Source Stage" value={sourceStageName} /> : null}

          {freight.length > 1 || !selected ? (
            <NativeSelect
              data={[
                { label: "Choose exact Freight", value: "" },
                ...freight.map((item) => ({
                  label: `${freightDisplayLabel(item)} · ${freightOriginLabel(item.origin)}`,
                  value: item.name,
                })),
              ]}
              label="Source Freight"
              onChange={(event) => onSelectedNameChange(event.currentTarget.value)}
              required
              value={selected?.name ?? ""}
            />
          ) : null}

          {fetcher.data?.error ? (
            <Alert color="red" title="Promotion not created">
              {fetcher.data.error}
            </Alert>
          ) : null}

          {selected ? <FreightDetails freight={selected} /> : null}

          {inPlace ? (
            <Alert color="yellow" title="In-place Promotion">
              This reruns the Stage&apos;s Kargo promotion template with the same Freight.
            </Alert>
          ) : null}

          <StagePolicyControl policy={stage.policy} reviewCapability={stage.capabilities.review} />
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

function StagePolicyControl({
  policy,
  reviewCapability,
}: Readonly<{
  policy: PromotionStagePolicy;
  reviewCapability: PromotionStageCapability;
}>) {
  const reviewRequired = policy.effectiveMode === "review-required";
  const reviewReady = reviewCapability.status === "ready";
  const { timeZone } = useTimeZone();

  return (
    <Paper p="md" withBorder>
      <Stack gap="sm">
        <Group justify="space-between" wrap="wrap">
          <Text fw={600}>Current Stage policy</Text>
          <Badge color={reviewRequired ? "yellow" : "green"} variant="light">
            {reviewRequired ? "Review required" : "Automatic"}
          </Badge>
        </Group>
        <Text c="dimmed" size="sm">
          {stagePolicyDescription(policy, timeZone)}
        </Text>
        {policy.reason ? (
          <Text size="sm">
            <Text c="dimmed" component="span">
              Reason:{" "}
            </Text>
            {policy.reason}
          </Text>
        ) : null}
        <NativeSelect
          data={[
            { label: "Keep current policy", value: "unchanged" },
            { label: "Use automatic policy", value: "automatic" },
            { disabled: !reviewReady, label: "Require review permanently", value: "permanent" },
            { disabled: !reviewReady, label: "Require review for 30 minutes", value: "30m" },
            { disabled: !reviewReady, label: "Require review for 1 hour", value: "1h" },
            { disabled: !reviewReady, label: "Require review for 24 hours", value: "24h" },
          ]}
          defaultValue="unchanged"
          description="A change is applied to the Stage before this Promotion is created."
          label="Review policy"
          name="reviewPolicy"
        />
        {!reviewReady ? (
          <Text c="dimmed" size="sm">
            {reviewCapability.reason} Review-required cannot be selected until this capability is
            ready. If it is already configured, select automatic deliberately before promoting.
          </Text>
        ) : null}
      </Stack>
    </Paper>
  );
}

function stagePolicyDescription(policy: PromotionStagePolicy, timeZone: string) {
  if (policy.effectiveMode === "review-required") {
    return policy.expiresAt
      ? `New Promotions require review until ${formatTimestamp(policy.expiresAt, timeZone)}.`
      : "New Promotions require review until the Stage policy is changed.";
  }
  if (policy.configuredMode === "review-required" && policy.expiresAt) {
    return `The review window expired ${formatTimestamp(policy.expiresAt, timeZone)}. New Promotions are automatic.`;
  }

  return "New Promotions proceed automatically without operator review.";
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
  environment,
}: PromotionCreateMenuProps) {
  const capability = stage.capabilities.promotion;
  const options = [
    {
      description: "Start a Promotion with Freight currently eligible for this Stage.",
      icon: <IconArrowUp aria-hidden size={15} />,
      label: "Promote eligible Freight",
      props: { environment, freight: eligibleFreight, stage },
      unavailableLabel: "No eligible Freight is currently observed.",
    },
    {
      description: "Rerun the Stage's promotion template with its active Freight.",
      icon: <IconRefresh aria-hidden size={15} />,
      label: "Re-promote in place",
      props: { environment, freight: currentFreight, inPlace: true, stage },
      unavailableLabel: "This Stage has no current Freight to promote in place.",
    },
  ] satisfies Array<{
    description: string;
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
          <Menu.Label>Promotion options</Menu.Label>
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
                <Stack gap={0}>
                  <Text size="sm">{option.label}</Text>
                  <Text c="dimmed" size="xs">
                    {unavailable ?? option.description}
                  </Text>
                </Stack>
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

function FreightDetails({ freight }: Readonly<{ freight: PromotionFreightItem }>) {
  return (
    <Paper p="md" withBorder>
      <Stack gap="sm">
        <Group justify="space-between" wrap="wrap">
          <Text fw={600}>Selected Freight</Text>
          <Text c="dimmed" size="xs">
            Kargo ID <Code>{freight.name}</Code>
          </Text>
        </Group>
        {freight.artifacts.length === 0 ? (
          <PromotionFact label="Source" value={freightOriginLabel(freight.origin)} />
        ) : (
          <SimpleGrid cols={{ base: 1, sm: 2 }}>
            {freight.artifacts.map((artifact) => (
              <Paper
                key={`${artifact.kind}:${artifact.repository ?? ""}:${artifact.version}`}
                p="sm"
                withBorder
              >
                <Stack gap={6}>
                  <Group justify="space-between" wrap="wrap">
                    <Text c="dimmed" size="xs">
                      Source
                    </Text>
                    <Badge size="xs" variant="light">
                      {artifact.kind}
                    </Badge>
                  </Group>
                  <Text fw={600} size="sm">
                    {artifact.repository ?? freightOriginLabel(freight.origin)}
                  </Text>
                  <Stack gap={2}>
                    <Text c="dimmed" size="xs">
                      {freightArtifactVersionLabel(artifact.kind)}
                    </Text>
                    <Code>{artifact.version}</Code>
                  </Stack>
                </Stack>
              </Paper>
            ))}
          </SimpleGrid>
        )}
      </Stack>
    </Paper>
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
