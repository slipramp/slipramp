import {
  Alert,
  Anchor,
  Badge,
  Button,
  Checkbox,
  Code,
  Group,
  Paper,
  SimpleGrid,
  Stack,
  Tabs,
  Text,
  Textarea,
  TextInput,
  Title,
} from "@mantine/core";
import { useMemo, useState } from "react";
import { Form, Link, useNavigation } from "react-router";

import { proxyArtifactDownload } from "~/api/artifact-download.server";
import { createServerAPIClient, getServerAPIURL } from "~/api/client.server";
import {
  decidePromotion,
  getCurrentPrincipal,
  getPromotion,
  type ProductPrincipal,
  type Promotion,
} from "~/api/generated";
import { filterArgoDiffResources, parseArgoDiff, type ArgoDiffResource } from "~/promotion-diff";
import { pageTitle } from "~/page-title";

import { promotionStatusColor, promotionStatusLabel, snapshotString } from "./promotions";
import type { Route } from "./+types/promotion";

export function meta({ loaderData }: Route.MetaArgs) {
  const title = loaderData?.promotion
    ? `Promotion ${loaderData.promotion.externalPromotionId}`
    : "Promotion";
  return [{ title: pageTitle(title) }];
}

export async function loader({ params, request }: Route.LoaderArgs) {
  const client = createServerAPIClient(request);
  const [promotionResult, principalResult] = await Promise.all([
    getPromotion({ client, path: { promotionId: params.promotionId } }),
    getCurrentPrincipal({ client }),
  ]);
  const promotion = promotionResult.data;
  const principal = principalResult.data;

  let gitDiff: ArtifactText = {};
  let argoDiff: ArtifactText = {};
  let argoResources: ArgoDiffResource[] | undefined;
  if (promotion && canReadSensitiveArtifacts(principal)) {
    [gitDiff, argoDiff] = await Promise.all([
      loadArtifactText(promotion.gitDiffArtifactId, request),
      loadArtifactText(promotion.argoDiffArtifactId, request),
    ]);
    if (argoDiff.contents !== undefined) {
      try {
        argoResources = parseArgoDiff(argoDiff.contents).resources;
      } catch (error) {
        argoDiff = {
          error: error instanceof Error ? error.message : "The Argo CD diff artifact is malformed.",
        };
      }
    }
  } else if (promotion?.gitDiffArtifactId || promotion?.argoDiffArtifactId) {
    const error = "Operator permission is required to read sensitive Promotion diffs.";
    gitDiff = { error };
    argoDiff = { error };
  }

  return {
    argoDiff,
    argoResources,
    error: promotionResult.error?.detail ?? promotionResult.error?.title,
    gitDiff,
    principal,
    promotion,
  };
}

export async function action({ params, request }: Route.ActionArgs) {
  const formData = await request.formData();
  const decision = formValue(formData, "decision");
  if (decision !== "approved" && decision !== "rejected") {
    return { error: "Choose approve or reject." };
  }
  const comment = optionalValue(formValue(formData, "comment"));
  const result = await decidePromotion({
    body: { comment, decision },
    client: createServerAPIClient(request),
    path: { promotionId: params.promotionId },
  });

  return result.data
    ? { message: `The Promotion was ${decision}.` }
    : {
        error:
          result.error?.detail ?? result.error?.title ?? "The Promotion decision was not recorded.",
      };
}

export default function PromotionDetail({ actionData, loaderData }: Route.ComponentProps) {
  if (!loaderData.promotion) {
    return (
      <Alert color="red" title="Promotion not found">
        {loaderData.error ?? "The Promotion could not be loaded."}
      </Alert>
    );
  }

  const promotion = loaderData.promotion;
  const canWrite = canWritePromotions(loaderData.principal);

  return (
    <Stack gap="lg">
      <Group align="flex-start" justify="space-between">
        <Stack gap={2}>
          <Anchor component={Link} size="sm" to="/promotions">
            Promotions
          </Anchor>
          <Title order={1}>{promotion.externalPromotionId}</Title>
          <Text c="dimmed">Promotion {promotion.id}</Text>
        </Stack>
        <Badge color={promotionStatusColor(promotion.status)} size="lg" variant="light">
          {promotionStatusLabel(promotion.status)}
        </Badge>
      </Group>

      {actionData?.error ? (
        <Alert color="red" title="Decision failed">
          {actionData.error}
        </Alert>
      ) : null}
      {actionData?.message ? (
        <Alert color="green" title="Decision recorded">
          {actionData.message}
        </Alert>
      ) : null}
      {promotion.message ? (
        <Alert color={promotion.status === "expired" ? "gray" : "red"} title="Promotion result">
          {promotion.message}
        </Alert>
      ) : null}

      <PromotionContext promotion={promotion} />
      <Preparation promotion={promotion} />
      <Diffs
        argoDiff={loaderData.argoDiff}
        argoResources={loaderData.argoResources}
        gitDiff={loaderData.gitDiff}
        promotion={promotion}
      />
      <DecisionPanel canWrite={canWrite} promotion={promotion} />
    </Stack>
  );
}

function PromotionContext({ promotion }: Readonly<{ promotion: Promotion }>) {
  const stage = promotion.stageSnapshot;
  const repository = promotion.repositorySnapshot;
  const policy = promotion.policySnapshot;

  return (
    <Paper p="md" withBorder>
      <Stack gap="md">
        <Title order={2}>Promotion context</Title>
        <SimpleGrid cols={{ base: 1, sm: 2, lg: 4 }}>
          <Fact label="Integration" value={promotion.integration} />
          <Fact
            label="Stage"
            value={`${snapshotString(stage, "project") ?? "unknown"}/${snapshotString(stage, "name") ?? "unknown"}`}
          />
          <Fact label="Application" value={snapshotString(stage, "application") ?? "Unknown"} />
          <Fact label="Environment" value={snapshotString(stage, "environment") ?? "Unknown"} />
          <Fact label="Repository" value={snapshotString(repository, "name") ?? "Unknown"} />
          <Fact label="Target branch" value={promotion.targetBranch} />
          <Fact label="Candidate branch" value={promotion.candidateBranch} />
          <Fact
            label="Policy snapshot"
            value={snapshotString(policy, "effectiveMode") ?? "Unknown"}
          />
        </SimpleGrid>
        <SimpleGrid cols={{ base: 1, md: 2 }}>
          <Revision label="Base revision" value={promotion.baseRevision} />
          <Revision label="Candidate revision" value={promotion.candidateRevision} />
        </SimpleGrid>
        <Group gap="xl">
          <Text size="sm">
            Requested by <strong>{promotion.requestedBy.displayName}</strong>
          </Text>
          <Text c="dimmed" component="time" dateTime={promotion.createdAt} size="sm">
            {formatTimestamp(promotion.createdAt)}
          </Text>
          {promotion.expiresAt ? (
            <Text c="dimmed" component="time" dateTime={promotion.expiresAt} size="sm">
              Review expires {formatTimestamp(promotion.expiresAt)}
            </Text>
          ) : null}
        </Group>
      </Stack>
    </Paper>
  );
}

function Preparation({ promotion }: Readonly<{ promotion: Promotion }>) {
  const run = promotion.preparationRun;

  return (
    <Paper p="md" withBorder>
      <Stack gap="sm">
        <Title order={2}>Preparation Run</Title>
        {!run ? (
          <Text c="dimmed">
            {promotion.reviewRequired
              ? "The preparation Run has not been attached yet."
              : "This Promotion was approved automatically and did not require preparation."}
          </Text>
        ) : (
          <Group justify="space-between">
            <Stack gap={0}>
              <Anchor component={Link} fw={600} to={`/runs/${run.id}`}>
                Run {run.id.slice(0, 8)}
              </Anchor>
              <Text c="dimmed" size="sm">
                {run.target} · {run.action}
              </Text>
            </Stack>
            <Group>
              <Anchor component={Link} to={`/runs/${run.id}`}>
                View status and logs
              </Anchor>
              <Badge variant="light">{run.outcome ?? run.phase}</Badge>
            </Group>
          </Group>
        )}
      </Stack>
    </Paper>
  );
}

function Diffs({
  argoDiff,
  argoResources,
  gitDiff,
  promotion,
}: Readonly<{
  argoDiff: ArtifactText;
  argoResources?: ArgoDiffResource[];
  gitDiff: ArtifactText;
  promotion: Promotion;
}>) {
  if (!promotion.gitDiffArtifactId && !promotion.argoDiffArtifactId) {
    return null;
  }

  return (
    <Paper p="md" withBorder>
      <Stack gap="md">
        <Group justify="space-between">
          <Title order={2}>Prepared diffs</Title>
          <Group gap="md">
            {promotion.gitDiffArtifactId ? (
              <Anchor href={`/api/v1/artifacts/${promotion.gitDiffArtifactId}/download`}>
                Download Git diff
              </Anchor>
            ) : null}
            {promotion.argoDiffArtifactId ? (
              <Anchor href={`/api/v1/artifacts/${promotion.argoDiffArtifactId}/download`}>
                Download Argo CD diff
              </Anchor>
            ) : null}
          </Group>
        </Group>
        <Tabs defaultValue="git">
          <Tabs.List>
            <Tabs.Tab value="git">Git</Tabs.Tab>
            <Tabs.Tab value="argo">Argo CD resources</Tabs.Tab>
          </Tabs.List>
          <Tabs.Panel pt="md" value="git">
            <ArtifactError error={gitDiff.error} />
            {gitDiff.contents !== undefined ? (
              gitDiff.contents.length === 0 ? (
                <Text c="dimmed">No Git changes.</Text>
              ) : (
                <Code block style={{ maxHeight: 640, overflow: "auto", whiteSpace: "pre" }}>
                  {gitDiff.contents}
                </Code>
              )
            ) : null}
          </Tabs.Panel>
          <Tabs.Panel pt="md" value="argo">
            <ArtifactError error={argoDiff.error} />
            {argoResources ? <ArgoDiff resources={argoResources} /> : null}
          </Tabs.Panel>
        </Tabs>
      </Stack>
    </Paper>
  );
}

function ArgoDiff({ resources }: Readonly<{ resources: ArgoDiffResource[] }>) {
  const [query, setQuery] = useState("");
  const [showVersionMetadata, setShowVersionMetadata] = useState(false);
  const filtered = useMemo(
    () => filterArgoDiffResources(resources, query, showVersionMetadata),
    [query, resources, showVersionMetadata],
  );
  const hiddenVersionCount = resources.filter((resource) => resource.versionMetadataOnly).length;

  return (
    <Stack gap="md">
      <Group align="flex-end">
        <TextInput
          label="Filter by kind or name"
          onChange={(event) => setQuery(event.currentTarget.value)}
          placeholder="Deployment or external-dns"
          value={query}
        />
        <Checkbox
          checked={showVersionMetadata}
          label={`Show version-metadata-only changes (${hiddenVersionCount})`}
          onChange={(event) => setShowVersionMetadata(event.currentTarget.checked)}
        />
      </Group>
      {filtered.length === 0 ? (
        <Text c="dimmed">
          {resources.length === 0
            ? "No Argo CD resource changes."
            : "No resources match the filter."}
        </Text>
      ) : (
        filtered.map((resource) => (
          <Paper key={resourceKey(resource)} p="sm" withBorder>
            <Stack gap="xs">
              <Group gap="xs">
                <Badge variant="light">{resource.kind}</Badge>
                <Text fw={600}>{resource.name}</Text>
                {resource.namespace ? (
                  <Text c="dimmed" size="sm">
                    {resource.namespace}
                  </Text>
                ) : null}
                {resource.versionMetadataOnly ? (
                  <Badge color="gray" variant="outline">
                    Version metadata only
                  </Badge>
                ) : null}
              </Group>
              <Code block style={{ maxHeight: 480, overflow: "auto", whiteSpace: "pre" }}>
                {resource.diff || "No textual diff."}
              </Code>
            </Stack>
          </Paper>
        ))
      )}
    </Stack>
  );
}

function DecisionPanel({
  canWrite,
  promotion,
}: Readonly<{ canWrite: boolean; promotion: Promotion }>) {
  const navigation = useNavigation();

  return (
    <Paper p="md" withBorder>
      <Stack gap="md">
        <Title order={2}>Decision</Title>
        {promotion.decision ? (
          <Stack gap="xs">
            <Group gap="xs">
              <Badge color={promotion.decision.decision === "approved" ? "green" : "red"}>
                {promotion.decision.decision}
              </Badge>
              <Text>
                {promotion.decision.decidedBy.displayName} ·{" "}
                {formatTimestamp(promotion.decision.decidedAt)}
              </Text>
            </Group>
            {promotion.decision.comment ? <Text>{promotion.decision.comment}</Text> : null}
            <Text c="dimmed" size="sm">
              This attribution is immutable.
            </Text>
          </Stack>
        ) : promotion.status !== "pending" ? (
          <Text c="dimmed">
            {promotion.status === "expired"
              ? "The review window expired before a decision was recorded."
              : "This Promotion is not awaiting an operator decision."}
          </Text>
        ) : canWrite ? (
          <Form method="post">
            <Stack gap="sm">
              <Textarea
                label="Comment"
                maxLength={4096}
                minRows={3}
                name="comment"
                placeholder="Optional context for the immutable decision"
              />
              <Group justify="flex-end">
                <Button
                  color="red"
                  loading={
                    navigation.state === "submitting" &&
                    navigation.formData?.get("decision") === "rejected"
                  }
                  name="decision"
                  type="submit"
                  value="rejected"
                  variant="light"
                >
                  Reject
                </Button>
                <Button
                  color="green"
                  loading={
                    navigation.state === "submitting" &&
                    navigation.formData?.get("decision") === "approved"
                  }
                  name="decision"
                  type="submit"
                  value="approved"
                >
                  Approve
                </Button>
              </Group>
            </Stack>
          </Form>
        ) : (
          <Text c="dimmed">Operator permission is required to decide this Promotion.</Text>
        )}
      </Stack>
    </Paper>
  );
}

function Fact({ label, value }: Readonly<{ label: string; value: string }>) {
  return (
    <Stack gap={0}>
      <Text c="dimmed" size="xs">
        {label}
      </Text>
      <Text>{value}</Text>
    </Stack>
  );
}

function Revision({ label, value }: Readonly<{ label: string; value: string }>) {
  return (
    <Stack gap={2}>
      <Text c="dimmed" size="xs">
        {label}
      </Text>
      <Code>{value}</Code>
    </Stack>
  );
}

function ArtifactError({ error }: Readonly<{ error?: string }>) {
  return error ? (
    <Alert color="yellow" title="Diff unavailable">
      {error}
    </Alert>
  ) : null;
}

type ArtifactText = { contents?: string; error?: string };

async function loadArtifactText(
  artifactId: string | undefined,
  request: Request,
): Promise<ArtifactText> {
  if (!artifactId) return {};

  const response = await proxyArtifactDownload(artifactId, request, getServerAPIURL());
  if (response.ok) {
    return { contents: await response.text() };
  }

  try {
    const problem = (await response.json()) as { detail?: string; title?: string };
    return { error: problem.detail ?? problem.title ?? "The diff artifact could not be read." };
  } catch {
    return { error: "The diff artifact could not be read." };
  }
}

function canReadSensitiveArtifacts(principal?: ProductPrincipal) {
  return principal?.role === "operator" || principal?.role === "admin";
}

function canWritePromotions(principal?: ProductPrincipal) {
  return principal?.role === "operator" || principal?.role === "admin";
}

function resourceKey(resource: ArgoDiffResource) {
  return [resource.group, resource.kind, resource.namespace, resource.name].join("/");
}

function optionalValue(value: string) {
  return value.trim() || undefined;
}

function formValue(formData: FormData, name: string) {
  const value = formData.get(name);
  return typeof value === "string" ? value : "";
}

function formatTimestamp(value: string) {
  return new Intl.DateTimeFormat("en", { dateStyle: "medium", timeStyle: "short" }).format(
    new Date(value),
  );
}
