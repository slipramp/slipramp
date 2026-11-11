import {
  Alert,
  Anchor,
  Badge,
  Button,
  Code,
  Group,
  Modal,
  NativeSelect,
  Paper,
  SimpleGrid,
  Stack,
  Text,
  Tooltip,
} from "@mantine/core";
import { notifications } from "@mantine/notifications";
import { IconArrowUp, IconExternalLink } from "@tabler/icons-react";
import { useEffect, useRef, useState } from "react";
import { Link, useFetcher } from "react-router";

import type { PromotionCreationActionResult } from "~/features/promotions/promotion-creation";
import { initialPromotionFreightName } from "~/features/promotions/promotion-creation";
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

function PromotionCreateDialog({
  stage,
  freight,
  environment,
  inPlace,
  initialFreightName,
  sourceStageId,
  sourceStageName,
  onClose,
}: PromotionCreateProps & { onClose: () => void }) {
  const fetcher = useFetcher<PromotionCreationActionResult>();
  const [selectedName, setSelectedName] = useState(() =>
    initialPromotionFreightName(freight, initialFreightName),
  );
  const handledPromotionID = useRef<string | undefined>(undefined);
  const selected = freight.find((item) => item.name === selectedName);
  const submittingMode = fetcher.formData?.get("mode");

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
    <Modal centered onClose={onClose} opened size="lg" title={`Promote to ${stage.name}`}>
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
              onChange={(event) => setSelectedName(event.currentTarget.value)}
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
                Promote in background
              </Button>
              <Button
                disabled={!selected}
                leftSection={<IconExternalLink aria-hidden size={15} />}
                loading={fetcher.state !== "idle" && submittingMode === "view"}
                name="mode"
                type="submit"
                value="view"
              >
                Promote and view
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
  const [opened, setOpened] = useState(false);
  const capability = props.stage.capabilities.promotion;
  if (capability.status !== "ready" || (props.freight.length === 0 && !opened)) {
    return (
      <Tooltip label={capability.reason ?? unavailableLabel}>
        <span>
          <Button disabled leftSection={<IconArrowUp aria-hidden size={14} />} size="compact-xs">
            Promote
          </Button>
        </span>
      </Tooltip>
    );
  }

  return (
    <>
      <Button
        leftSection={<IconArrowUp aria-hidden size={14} />}
        onClick={() => setOpened(true)}
        size="compact-xs"
        variant="light"
      >
        Promote
      </Button>
      {opened ? <PromotionCreateDialog {...props} onClose={() => setOpened(false)} /> : null}
    </>
  );
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
