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
import { useMemo, useState, type ReactNode } from "react";
import { Form, Link, useNavigation } from "react-router";

import { proxyArtifactDownload } from "~/api/artifact-download.server";
import { createServerAPIClient, getServerAPIURL } from "~/api/client.server";
import {
  decidePromotion,
  getCurrentPrincipal,
  getPromotion,
  type GitCommit,
  type ProductPrincipal,
  type Promotion,
} from "~/api/generated";
import { RelativeTime, Timestamp } from "~/components/display";
import { useActionSuccessToast } from "~/components/action-notifications";
import { useActivityRefresh } from "~/components/activity-refresh";
import { CommitReference } from "~/components/git-commit";
import { ContextValue } from "~/components/context-value";
import { Identity } from "~/components/identity";
import { KargoButton } from "~/components/kargo-link";
import { KawaiiActivity } from "~/components/kawaii-activity";
import { PageBackLink } from "~/components/page-back-link";
import { PageHeader, PageHeaderFact, PageHeaderFacts } from "~/components/page";
import { formValue, optionalFormValue } from "~/form-data";
import { promotionDetailActivity, promotionDetailNeedsRefresh } from "~/execution-activity";
import { filterArgoDiffResources, parseArgoDiff, type ArgoDiffResource } from "~/promotion-diff";
import { pageTitle } from "~/page-title";
import { runStatusLabel } from "~/run-status";

import { promotionStatusColor, promotionStatusLabel } from "./promotions";
import styles from "./promotion.module.css";
import type { Route } from "./+types/promotion";

export function meta({ loaderData }: Route.MetaArgs) {
  const title = loaderData?.promotion
    ? `Promotion ${loaderData.promotion.externalPromotionId}`
    : "Promotion";
  return [{ title: pageTitle(title) }];
}

export async function loader({ context, params, request }: Route.LoaderArgs) {
  const client = createServerAPIClient(request, context);
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

export async function action({ context, params, request }: Route.ActionArgs) {
  const formData = await request.formData();
  const decision = formValue(formData, "decision");
  if (decision !== "approved" && decision !== "rejected") {
    return { error: "Choose approve or reject." };
  }
  const comment = optionalFormValue(formData, "comment");
  const result = await decidePromotion({
    body: { comment, decision },
    client: createServerAPIClient(request, context),
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
  useActionSuccessToast(actionData, "Decision recorded", actionData?.message);

  const canReadDiffs = canReadSensitiveArtifacts(loaderData.principal);
  const preparedDiffsLoaded =
    !canReadDiffs ||
    (loaderData.gitDiff.contents !== undefined && loaderData.argoResources !== undefined);
  useActivityRefresh(
    loaderData.promotion !== undefined &&
      promotionDetailNeedsRefresh(loaderData.promotion, preparedDiffsLoaded),
  );

  if (!loaderData.promotion) {
    return (
      <Stack gap="md">
        <PageBackLink label="Back to promotions" to="/promotions" />
        <Alert color="red" title="Promotion not found">
          {loaderData.error ?? "The Promotion could not be loaded."}
        </Alert>
      </Stack>
    );
  }

  const promotion = loaderData.promotion;
  const canWrite = canWritePromotions(loaderData.principal);
  const detailActivity = promotionDetailActivity(promotion);

  return (
    <Stack gap="lg">
      <PageHeader
        actions={promotion.kargoUrl ? <KargoButton url={promotion.kargoUrl} /> : undefined}
        back={<PageBackLink label="Back to promotions" to="/promotions" />}
        badges={
          <Badge color={promotionStatusColor(promotion.status)} variant="light">
            {promotionStatusLabel(promotion.status)}
          </Badge>
        }
        details={
          <PageHeaderFacts>
            <PageHeaderFact label="Promotion">
              <Code>{promotion.id.slice(0, 8)}</Code>
            </PageHeaderFact>
            <PageHeaderFact label="Created">
              <Group gap="xs" wrap="wrap">
                <Timestamp value={promotion.createdAt} />
                <Text c="dimmed">·</Text>
                <RelativeTime c="dimmed" value={promotion.createdAt} />
              </Group>
            </PageHeaderFact>
            <PageHeaderFact label="Source">
              {promotion.source === "kargo" ? "Kargo" : "Slipramp"}
            </PageHeaderFact>
            <PageHeaderFact label="Requested by">
              <Identity
                avatarSize={22}
                name={promotion.requestedBy.displayName}
                nameProps={{ fw: 500, size: "sm" }}
              />
            </PageHeaderFact>
          </PageHeaderFacts>
        }
        title={promotion.externalPromotionId}
      />

      <KawaiiActivity activity={detailActivity} animationKey={promotion.id} />

      {actionData?.error ? (
        <Alert color="red" title="Decision failed">
          {actionData.error}
        </Alert>
      ) : null}
      {promotion.message ? (
        <Alert
          className={detailActivity ? styles.resultNotice : undefined}
          color={promotion.status === "expired" ? "gray" : "red"}
          title="Promotion result"
        >
          {promotion.message}
        </Alert>
      ) : null}
      {promotion.status === "creating" ? (
        <Alert className={styles.creationNotice} color="cyan" title="Creating Promotion in Kargo">
          Slipramp is delivering this request or waiting for Kargo to submit its promotion data.
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
        <SimpleGrid cols={{ base: 1, sm: 2, lg: 3 }} spacing="lg">
          <Fact
            label="Integration"
            value={
              <Anchor
                component={Link}
                to={`/promotions?integration=${encodeURIComponent(promotion.integration)}`}
              >
                {promotion.integration}
              </Anchor>
            }
          />
          <Fact
            label="Stage"
            value={
              <Anchor
                component={Link}
                to={`/promotions?stageId=${encodeURIComponent(promotion.stageId)}`}
              >
                {stage.project}/{stage.name}
              </Anchor>
            }
          />
          <Fact
            label="Application"
            value={
              <Anchor
                component={Link}
                to={`/promotions?application=${encodeURIComponent(stage.application)}`}
              >
                {stage.application}
              </Anchor>
            }
          />
          <Fact label="Environment" value={stage.environment} />
          <Fact
            label="Repository"
            value={
              <Anchor component={Link} to={`/repositories/${promotion.repositoryId}`}>
                {repository.name}
              </Anchor>
            }
          />
          <Fact label="Policy snapshot" value={policy.effectiveMode} />
          <Fact label="Target branch" value={promotion.targetBranch} />
          <Fact label="Candidate branch" value={promotion.candidateBranch ?? "Waiting for Kargo"} />
          {promotion.expiresAt ? (
            <Fact label="Review expires" value={<Timestamp value={promotion.expiresAt} />} />
          ) : null}
        </SimpleGrid>
        <SimpleGrid cols={{ base: 1, sm: 2, lg: 3 }} spacing="lg">
          <Revision
            commit={promotion.baseCommit}
            label="Base revision"
            repositoryId={promotion.repositoryId}
          />
          <Revision
            commit={promotion.candidateCommit}
            label="Candidate revision"
            repositoryId={promotion.repositoryId}
          />
        </SimpleGrid>
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
              <Badge variant="light">{runStatusLabel(run)}</Badge>
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
        {promotion.gitDiffArtifact?.description || promotion.argoDiffArtifact?.description ? (
          <Stack gap={2}>
            {promotion.gitDiffArtifact?.description ? (
              <Text c="dimmed" size="sm">
                <Code>git-diff</Code> — {promotion.gitDiffArtifact.description}
              </Text>
            ) : null}
            {promotion.argoDiffArtifact?.description ? (
              <Text c="dimmed" size="sm">
                <Code>argo-diff</Code> — {promotion.argoDiffArtifact.description}
              </Text>
            ) : null}
          </Stack>
        ) : null}
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
              <Identity avatarSize={26} name={promotion.decision.decidedBy.displayName} />
              <Text c="dimmed">·</Text>
              <Timestamp value={promotion.decision.decidedAt} />
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

function Fact({ label, value }: Readonly<{ label: string; value: ReactNode }>) {
  return (
    <Stack gap={0}>
      <Text c="dimmed" size="xs">
        {label}
      </Text>
      <ContextValue>{value}</ContextValue>
    </Stack>
  );
}

function Revision({
  commit,
  label,
  repositoryId,
}: Readonly<{ commit?: GitCommit; label: string; repositoryId: string }>) {
  return (
    <Stack gap={2}>
      <Text c="dimmed" size="xs">
        {label}
      </Text>
      <ContextValue>
        {commit ? (
          <CommitReference commit={commit} repositoryId={repositoryId} />
        ) : (
          "Waiting for Kargo"
        )}
      </ContextValue>
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
