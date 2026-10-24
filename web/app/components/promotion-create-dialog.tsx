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

import {
  promotionChoiceKey,
  type PromotionChoice,
  type PromotionCreationActionResult,
} from "~/promotion-creation";

export function PromotionCreateDialog({
  choices,
  onClose,
}: Readonly<{
  choices: PromotionChoice[];
  onClose: () => void;
}>) {
  const fetcher = useFetcher<PromotionCreationActionResult>();
  const [selectedKey, setSelectedKey] = useState(
    choices.length === 1 && choices[0] ? promotionChoiceKey(choices[0]) : "",
  );
  const handledPromotionID = useRef<string | undefined>(undefined);
  const selected = choices.find((choice) => promotionChoiceKey(choice) === selectedKey);
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
    <Modal
      centered
      onClose={onClose}
      opened
      size="lg"
      title={`Promote to ${choices[0]?.targetStageName ?? "Stage"}`}
    >
      <fetcher.Form method="post">
        <Stack gap="md">
          <SimpleGrid cols={{ base: 1, sm: 2 }}>
            <PromotionFact label="Target Stage" value={choices[0]?.targetStageName ?? "Unknown"} />
            <PromotionFact label="Environment" value={choices[0]?.targetEnvironment ?? "Unknown"} />
          </SimpleGrid>

          {choices.length > 1 ? (
            <NativeSelect
              data={[
                { label: "Choose exact Freight", value: "" },
                ...choices.map((choice) => ({
                  label: `${choice.sourceEnvironment} · ${choice.sourceStageName} · ${choice.freight.name}`,
                  value: promotionChoiceKey(choice),
                })),
              ]}
              label="Source Freight"
              onChange={(event) => setSelectedKey(event.currentTarget.value)}
              required
              value={selectedKey}
            />
          ) : null}

          {fetcher.data?.error ? (
            <Alert color="red" title="Promotion not created">
              {fetcher.data.error}
            </Alert>
          ) : null}

          {selected ? <PromotionChoiceDetails choice={selected} /> : null}

          {selected?.sourceStageId === selected?.targetStageId ? (
            <Alert color="yellow" title="In-place Promotion">
              This reruns the Stage&apos;s Kargo promotion template with the same Freight.
            </Alert>
          ) : null}

          <input name="targetStageId" type="hidden" value={selected?.targetStageId ?? ""} />
          <input name="sourceStageId" type="hidden" value={selected?.sourceStageId ?? ""} />
          <input name="freightName" type="hidden" value={selected?.freight.name ?? ""} />

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

export function PromotionCreateButton({
  choices,
  onClick,
  unavailableLabel = "No eligible Freight is currently observed on a declared upstream Stage.",
}: Readonly<{ choices: PromotionChoice[]; onClick: () => void; unavailableLabel?: string }>) {
  if (choices.length === 0) {
    return (
      <Tooltip label={unavailableLabel}>
        <span>
          <Button disabled leftSection={<IconArrowUp aria-hidden size={14} />} size="compact-xs">
            Promote
          </Button>
        </span>
      </Tooltip>
    );
  }

  return (
    <Button
      leftSection={<IconArrowUp aria-hidden size={14} />}
      onClick={onClick}
      size="compact-xs"
      variant="light"
    >
      Promote
    </Button>
  );
}

function PromotionChoiceDetails({ choice }: Readonly<{ choice: PromotionChoice }>) {
  return (
    <Paper p="md" withBorder>
      <Stack gap="sm">
        <SimpleGrid cols={{ base: 1, sm: 2 }}>
          <PromotionFact label="Source Stage" value={choice.sourceStageName} />
          <PromotionFact label="Source environment" value={choice.sourceEnvironment} />
          <PromotionFact label="Origin" value={choice.freight.origin} />
        </SimpleGrid>
        {choice.freight.artifacts.length === 0 ? (
          <Text c="dimmed" size="sm">
            This Freight does not report versioned artifacts.
          </Text>
        ) : (
          <Stack gap="xs">
            <Text c="dimmed" size="xs">
              Versions
            </Text>
            {choice.freight.artifacts.map((artifact) => (
              <Group
                key={`${artifact.kind}:${artifact.repository ?? ""}:${artifact.version}`}
                gap="xs"
                wrap="wrap"
              >
                <Badge size="sm" variant="light">
                  {artifact.kind}
                </Badge>
                {artifact.repository ? (
                  <Text c="dimmed" size="sm">
                    {artifact.repository}
                  </Text>
                ) : null}
                <Code>{artifact.version}</Code>
              </Group>
            ))}
          </Stack>
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
