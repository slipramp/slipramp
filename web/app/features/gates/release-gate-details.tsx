import {
  Alert,
  Anchor,
  Badge,
  Box,
  Button,
  Code,
  Divider,
  Group,
  Modal,
  Stack,
  Table,
  Text,
  Textarea,
  Title,
  VisuallyHidden,
} from "@mantine/core";
import { IconExternalLink } from "@tabler/icons-react";
import { Fragment, useEffect } from "react";
import { Link, useFetcher, useSearchParams } from "react-router";

import type { GateOverride, GateRequirement, GateSummary } from "~/api/generated";
import {
  type GateDetailField,
  gateCanBeOverridden,
  hasMatchingOverride,
  gatePolicyHistoryPath,
  repositoryGateDetailFields,
  gateEvidenceFields,
  gateRequirementBadges,
  gateSummaryBadges,
  releaseGateSearchParams,
  selectedReleaseGate,
} from "~/features/gates/gate-presentation";
import { useActionSuccessToast } from "~/shared/action-notifications";
import { RelativeTime } from "~/shared/display-values";
import { Identity } from "~/shared/identity";
import { DetailSection } from "~/shared/page";

type GateSubject = "Promotion" | "Run";

export function ReleaseGateSection({
  gates,
  title,
}: Readonly<{ gates: GateSummary; title: string }>) {
  if (gates.state === "not-required") return null;
  const released = gates.state === "released";
  const summary = gateSummaryBadges[gates.state];
  return (
    <DetailSection
      key={released ? "released" : "unreleased"}
      defaultExpanded={!released}
      title={
        <Group component="span" gap="xs" justify="space-between" pe="xs" wrap="nowrap">
          {title}
          <Badge color={summary.color} component="span" variant="light">
            {summary.label}
          </Badge>
        </Group>
      }
    >
      <GateRequirementsTable gates={gates} label={title} />
    </DetailSection>
  );
}

function GateRequirementsTable({ gates, label }: Readonly<{ gates: GateSummary; label: string }>) {
  const [params] = useSearchParams();
  if (gates.requirements.length === 0) return null;
  return (
    <Table aria-label={label}>
      <Table.Thead>
        <Table.Tr>
          <Table.Th scope="col">Gate</Table.Th>
          <Table.Th scope="col">Status</Table.Th>
          <Table.Th scope="col">
            <VisuallyHidden>Details</VisuallyHidden>
          </Table.Th>
        </Table.Tr>
      </Table.Thead>
      <Table.Tbody>
        {gates.requirements.map((gate) => {
          const name = gate.title ?? gate.name;
          return (
            <Table.Tr key={gate.key}>
              <Table.Td w="100%" maw={0}>
                <Text inherit truncate title={name}>
                  {name}
                </Text>
              </Table.Td>
              <Table.Td>
                <GateStatusBadge gate={gate} />
              </Table.Td>
              <Table.Td ta="right">
                <Button
                  component={Link}
                  to={{ search: releaseGateSearchParams(params, gate.key).toString() }}
                  preventScrollReset
                  aria-haspopup="dialog"
                  aria-label={`View details for ${name}`}
                  variant="subtle"
                  size="compact-xs"
                >
                  Details
                </Button>
              </Table.Td>
            </Table.Tr>
          );
        })}
      </Table.Tbody>
    </Table>
  );
}

function GateStatusBadge({ gate }: Readonly<{ gate: GateRequirement }>) {
  const badge =
    hasMatchingOverride(gate) && gate.state !== "stopped"
      ? { label: gate.releasedAt ? "Released by override" : "Overridden", color: "orange" }
      : gateRequirementBadges[gate.state];
  return (
    <Badge color={badge.color} size="sm" variant="light" miw="max-content">
      {badge.label}
    </Badge>
  );
}

type GateDialogProps = {
  gates: GateSummary;
  subject: GateSubject;
  subjectId: string;
  localOwnerId: string;
  canOverride: boolean;
};

export function ReleaseGateDialog({ subjectId, ...props }: Readonly<GateDialogProps>) {
  const [params] = useSearchParams();
  const gateKey = params.get("gate");
  return gateKey === null ? null : <GateDialog key={subjectId + "/" + gateKey} {...props} />;
}

function GateDialog({
  gates,
  subject,
  localOwnerId,
  canOverride,
}: Readonly<Omit<GateDialogProps, "subjectId">>) {
  const [params, setParams] = useSearchParams();
  const fetcher = useFetcher<{ error?: string; message?: string }>();
  const busy = fetcher.state !== "idle";
  const result = busy ? undefined : fetcher.data;
  useActionSuccessToast(result, `${subject} updated`, result?.message);

  const gate = selectedReleaseGate(params, gates);
  const current = params.toString();
  const canonical = releaseGateSearchParams(
    params,
    result?.message ? undefined : gate?.key,
  ).toString();
  useEffect(() => {
    if (!busy && canonical !== current) {
      setParams(canonical, { replace: true, preventScrollReset: true });
    }
  }, [busy, canonical, current, setParams]);

  if (!gate) return null;
  return (
    <Modal.Root
      opened
      centered
      size="lg"
      onClose={() => {
        if (!busy)
          setParams(releaseGateSearchParams(params), { replace: true, preventScrollReset: true });
      }}
      closeOnEscape={!busy}
      closeOnClickOutside={!busy}
    >
      <Modal.Overlay />
      <Modal.Content>
        <Modal.Header>
          <Modal.Title miw={0} me="sm" style={{ overflowWrap: "anywhere" }}>
            {gate.title ?? gate.name}
          </Modal.Title>
          <Group gap="xs" wrap="nowrap" style={{ flexShrink: 0 }}>
            <GateStatusBadge gate={gate} />
            <Modal.CloseButton aria-label="Close gate details" disabled={busy} />
          </Group>
        </Modal.Header>
        <Modal.Body>
          <Stack gap="md" style={{ overflowWrap: "anywhere" }}>
            {gates.state === "stopped" && (
              <Text size="sm" c="dimmed">
                Cancellation ended the gate wait before this {subject} was released.
              </Text>
            )}
            {gate.origin === "repository" && (
              <GateDetailFields fields={repositoryGateDetailFields(gate)} />
            )}
            {gate.state !== "stopped" && (
              <>
                {gate.releasedAt && (
                  <Text size="sm" fw={600}>
                    Evidence at release
                  </Text>
                )}
                {!gate.evidence && !gate.evaluatedAt && (
                  <Text size="sm" c="dimmed">
                    {gate.releasedAt && hasMatchingOverride(gate)
                      ? "Released with an operator override."
                      : "No observation available."}
                  </Text>
                )}
                <GateDetailFields fields={gateEvidenceFields(gate.evidence, gate.evaluatedAt)} />
              </>
            )}
            <PolicyVersionLink gate={gate} localOwnerId={localOwnerId} version={gate.version} />
            {gate.state !== "stopped" && (
              <>
                {gate.releasedAt && (
                  <Text size="sm">
                    Released <RelativeTime value={gate.releasedAt} inherit showSeconds />
                  </Text>
                )}
                {gate.diagnostic && <Text size="sm">{gate.diagnostic}</Text>}
                {gate.queryUrl && (
                  <Anchor
                    href={gate.queryUrl}
                    rel="noreferrer"
                    size="sm"
                    target="_blank"
                    aria-label={`Open in Prometheus for ${gate.title ?? gate.name}`}
                  >
                    <Group component="span" gap={4}>
                      Open in Prometheus <IconExternalLink aria-hidden size={13} />
                    </Group>
                  </Anchor>
                )}
              </>
            )}

            {Boolean(gate.overrides?.length) && (
              <Stack gap="sm">
                <Divider />
                <Title order={3} size="h5">
                  Override history
                </Title>
                {gate.overrides?.map((decision) => (
                  <OverrideDecision
                    key={decision.recordedAt + decision.version}
                    decision={decision}
                    gate={gate}
                    localOwnerId={localOwnerId}
                  />
                ))}
              </Stack>
            )}
            {fetcher.data?.error && (
              <Alert color="red" title="Override could not be recorded">
                {fetcher.data.error}
              </Alert>
            )}
            {canOverride && gates.state === "waiting" && gateCanBeOverridden(gate) && (
              <fetcher.Form key={gate.version} method="post">
                <Stack gap="xs">
                  <Divider />
                  <Title order={3} size="h5">
                    Override gate
                  </Title>
                  <input name="intent" type="hidden" value="override-gate" />
                  <input name="gateKey" type="hidden" value={gate.key} />
                  <input name="version" type="hidden" value={gate.version} />
                  <Textarea maxLength={1024} name="reason" label="Reason for override (optional)" />
                  <Button color="orange" type="submit" size="sm" ml="auto" loading={busy}>
                    Override Gate
                  </Button>
                </Stack>
              </fetcher.Form>
            )}
          </Stack>
        </Modal.Body>
      </Modal.Content>
    </Modal.Root>
  );
}

function renderGateDetailValue(field: GateDetailField) {
  const { value } = field;
  switch (field.format) {
    case "actor":
      return (
        <Group gap="sm" wrap="nowrap">
          <Identity avatarSize={24} name={value} />
          <RelativeTime c="dimmed" size="sm" value={field.createdAt} showSeconds />
        </Group>
      );
    case "time":
      return <RelativeTime value={value} inherit showSeconds />;
    case "code":
      return <Code>{value}</Code>;
    default:
      return value;
  }
}

function GateDetailFields({ fields }: Readonly<{ fields: GateDetailField[] }>) {
  if (!fields.length) return null;
  return (
    <Box
      component="dl"
      m={0}
      style={{
        display: "grid",
        gridTemplateColumns: "min(8rem, 40%) minmax(0, 1fr)",
        columnGap: "var(--mantine-spacing-md)",
        rowGap: 2,
      }}
    >
      {fields.map((field) => (
        <Fragment key={field.label}>
          <Text component="dt" size="sm" c="dimmed">
            {field.label}
          </Text>
          <Text component="dd" m={0} size="sm">
            {renderGateDetailValue(field)}
          </Text>
        </Fragment>
      ))}
    </Box>
  );
}

function PolicyVersionLink({
  gate,
  localOwnerId,
  version,
}: Readonly<{ gate: GateRequirement; localOwnerId: string; version: GateRequirement["version"] }>) {
  const to = gatePolicyHistoryPath(gate, localOwnerId, version);
  if (!to) return null;
  const earlierVersion = gate.version && version !== gate.version;
  return (
    <Anchor component={Link} size="sm" to={to} w="fit-content">
      {earlierVersion ? "View earlier policy version" : "View policy version"} {version}
    </Anchor>
  );
}

function OverrideDecision({
  decision,
  gate,
  localOwnerId,
}: Readonly<{ decision: GateOverride; gate: GateRequirement; localOwnerId: string }>) {
  return (
    <Stack gap={2}>
      <Group gap="sm">
        <Identity avatarSize={24} name={decision.actor.displayName} />
        <RelativeTime c="dimmed" size="sm" value={decision.recordedAt} />
      </Group>
      {decision.reason && <Text size="sm">{decision.reason}</Text>}
      <PolicyVersionLink gate={gate} localOwnerId={localOwnerId} version={decision.version} />
    </Stack>
  );
}
