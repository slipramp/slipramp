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
import { IconExternalLink } from "@tabler/icons-react";
import { useMemo, type ReactNode } from "react";
import { Form, Link, useNavigation, useSearchParams } from "react-router";

import { proxyArtifactDownload } from "~/api/artifact-download.server";
import { createServerAPIClient, getServerAPIURL } from "~/api/client.server";
import {
  decidePromotion,
  getPromotion,
  type GitCommit,
  type ProductPrincipal,
  type Promotion,
} from "~/api/generated";
import { getProductPrincipal } from "~/features/auth/product-authentication.server";
import { RelativeTime, Timestamp } from "~/shared/display-values";
import { useActionSuccessToast } from "~/shared/action-notifications";
import { useActivityRefresh } from "~/features/shell/activity-refresh";
import { CommitReference } from "~/features/commits/git-commit";
import { ContextValue } from "~/shared/context-value";
import { Identity } from "~/shared/identity";
import { KargoButton } from "~/features/promotions/kargo-link";
import {
  promotionDeliveryStatus,
  promotionExecutionStatus,
  promotionOverallStatus,
  promotionReviewStatus,
  type PromotionStatusPresentation,
} from "~/features/promotions/promotion-status";
import { PromotionStatusBadge } from "~/features/promotions/promotion-status-badge";
import { KawaiiActivity } from "~/features/mascot/kawaii-activity";
import { PageBackLink } from "~/shared/page-back-link";
import { PageHeader, PageHeaderFact, PageHeaderFacts } from "~/shared/page";
import { formValue, optionalFormValue } from "~/shared/form-data";
import {
  promotionDetailActivity,
  promotionDetailNeedsRefresh,
} from "~/features/mascot/execution-activity";
import {
  filterArgoDiffResources,
  parseArgoDiff,
  type ArgoDiffResource,
} from "~/features/promotions/promotion-diff";
import { pageTitle } from "~/shared/page-title";
import { PromotionStageLink } from "~/features/promotions/promotion-stage-link";
import { runStatusLabel } from "~/features/runs/run-status";
import { skipPresentationSearchParamRevalidation } from "~/shared/search-params";

import styles from "./promotion.module.css";
import type { Route } from "./+types/promotion";

export function meta({ loaderData }: Route.MetaArgs) {
  const title = loaderData?.promotion
    ? `Promotion ${loaderData.promotion.externalPromotionId}`
    : "Promotion";
  return [{ title: pageTitle(title) }];
}

export const shouldRevalidate = skipPresentationSearchParamRevalidation;

export async function loader({ context, params, request }: Route.LoaderArgs) {
  const client = createServerAPIClient(request, context);
  const [promotionResult, principal] = await Promise.all([
    getPromotion({ client, path: { promotionId: params.promotionId } }),
    getProductPrincipal(context),
  ]);
  const promotion = promotionResult.data;

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
  const overallStatus = promotionOverallStatus(promotion);

  return (
    <Stack gap="lg">
      <PageHeader
        actions={
          promotion.kargoUrl || promotion.argoCdUrl ? (
            <Group gap="sm">
              {promotion.kargoUrl ? <KargoButton url={promotion.kargoUrl} /> : null}
              {promotion.argoCdUrl ? (
                <Button
                  component="a"
                  href={promotion.argoCdUrl}
                  leftSection={<IconExternalLink aria-hidden size={16} />}
                  rel="noreferrer"
                  size="compact-md"
                  target="_blank"
                  variant="light"
                >
                  Open in Argo CD
                </Button>
              ) : null}
            </Group>
          ) : undefined
        }
        back={<PageBackLink label="Back to promotions" to="/promotions" />}
        badges={<PromotionStatusBadge status={overallStatus} />}
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
      <PromotionStatusDetails promotion={promotion} />

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
        <Alert className={styles.creationNotice} color="cyan" title="Waiting for review context">
          Kargo has not submitted the Git context required to prepare this review yet.
        </Alert>
      ) : null}
      <CreationDispatchNotice promotion={promotion} />
      <KargoOutcomeNotice promotion={promotion} />

      <Preparation promotion={promotion} />
      <Diffs
        argoDiff={loaderData.argoDiff}
        argoResources={loaderData.argoResources}
        gitDiff={loaderData.gitDiff}
        promotion={promotion}
      />
      <DecisionPanel canWrite={canWrite} promotion={promotion} />
      <PromotionContext promotion={promotion} />
    </Stack>
  );
}

function CreationDispatchNotice({ promotion }: Readonly<{ promotion: Promotion }>) {
  const dispatch = promotion.creationDispatch;
  if (dispatch.phase !== "failed") return null;

  return (
    <Alert color="red" title="Kargo object delivery failed">
      {dispatch.diagnostic ?? "Slipramp could not materialize the Kargo Promotion."}
    </Alert>
  );
}

function KargoOutcomeNotice({ promotion }: Readonly<{ promotion: Promotion }>) {
  if (!promotion.kargoOutcome || promotion.kargoOutcome.phase === "Succeeded") return null;

  return (
    <Alert color="red" title={`Kargo Promotion ${promotion.kargoOutcome.phase.toLowerCase()}`}>
      <Stack gap="xs">
        <Text>
          {promotion.kargoOutcome.diagnostic ??
            "Kargo reached an unsuccessful terminal phase without a diagnostic."}
        </Text>
        <Text c="dimmed" size="sm">
          Observed <Timestamp value={promotion.kargoOutcome.observedAt} />
        </Text>
      </Stack>
    </Alert>
  );
}

function PromotionStatusDetails({ promotion }: Readonly<{ promotion: Promotion }>) {
  return (
    <Paper p="md" withBorder>
      <Stack gap="md">
        <Title order={2}>Status details</Title>
        <SimpleGrid cols={{ base: 1, sm: 3 }} spacing="lg">
          <PromotionStatusFact label="Review" status={promotionReviewStatus(promotion)} />
          <PromotionStatusFact label="Kargo delivery" status={promotionDeliveryStatus(promotion)} />
          <PromotionStatusFact
            label="Kargo execution"
            status={promotionExecutionStatus(promotion)}
          />
        </SimpleGrid>
      </Stack>
    </Paper>
  );
}

function PromotionStatusFact({
  label,
  status,
}: Readonly<{ label: string; status: PromotionStatusPresentation }>) {
  return (
    <Stack gap={4}>
      <Text c="dimmed" size="xs">
        {label}
      </Text>
      <Group gap="xs" wrap="wrap">
        <PromotionStatusBadge status={status} />
        {status.recordedAt ? <Timestamp c="dimmed" size="xs" value={status.recordedAt} /> : null}
      </Group>
    </Stack>
  );
}

function PromotionContext({ promotion }: Readonly<{ promotion: Promotion }>) {
  const stage = promotion.stageSnapshot;
  const enrichment = stage.enrichment;
  const review = promotion.reviewContext;
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
              <PromotionStageLink
                label={`${stage.project}/${stage.name}`}
                stage={{ id: promotion.stageId, name: stage.name }}
              />
            }
          />
          <Fact label="Policy snapshot" value={policy.effectiveMode} />
          {promotion.expiresAt ? (
            <Fact label="Review expires" value={<Timestamp value={promotion.expiresAt} />} />
          ) : null}
        </SimpleGrid>
        {enrichment ? (
          <SimpleGrid cols={{ base: 1, sm: 2, lg: 3 }} spacing="lg">
            {enrichment.application ? (
              <Fact
                label="Application"
                value={
                  <Anchor
                    component={Link}
                    to={`/promotions?application=${encodeURIComponent(enrichment.application)}`}
                  >
                    {enrichment.application}
                  </Anchor>
                }
              />
            ) : null}
            {enrichment.environment ? (
              <Fact label="Environment" value={enrichment.environment} />
            ) : null}
          </SimpleGrid>
        ) : null}
        {review ? (
          <>
            <SimpleGrid cols={{ base: 1, sm: 2, lg: 3 }} spacing="lg">
              <Fact
                label="Repository"
                value={
                  <Anchor component={Link} to={`/repositories/${review.repositoryId}`}>
                    {review.repositorySnapshot.name}
                  </Anchor>
                }
              />
              <Fact label="Target branch" value={review.targetBranch} />
              <Fact
                label="Candidate branch"
                value={review.git?.candidateBranch ?? "Waiting for Kargo"}
              />
            </SimpleGrid>
            {review.git ? (
              <SimpleGrid cols={{ base: 1, sm: 2, lg: 3 }} spacing="lg">
                <Revision
                  commit={review.git.baseCommit}
                  label="Base revision"
                  repositoryId={review.repositoryId}
                />
                <Revision
                  commit={review.git.candidateCommit}
                  label="Candidate revision"
                  repositoryId={review.repositoryId}
                />
              </SimpleGrid>
            ) : null}
          </>
        ) : (
          <Alert color="blue" title="Kargo-native Promotion">
            This Promotion has no Slipramp Repository review context. Kargo received only the exact
            Stage and Freight identity.
          </Alert>
        )}
      </Stack>
    </Paper>
  );
}

function Preparation({ promotion }: Readonly<{ promotion: Promotion }>) {
  const run = promotion.preparationRun;
  if (!run) return null;

  return (
    <Paper p="md" withBorder>
      <Stack gap="sm">
        <Title order={2}>Preparation Run</Title>
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
  const [searchParams, setSearchParams] = useSearchParams();
  if (!promotion.gitDiffArtifactId && !promotion.argoDiffArtifactId) {
    return null;
  }

  const activeDiff = searchParams.get("diff") === "argo" ? "argo" : "git";

  function changeDiff(value: string | null) {
    if (!value) return;

    const next = new URLSearchParams(searchParams);
    if (value === "git") next.delete("diff");
    else next.set("diff", value);
    setSearchParams(next, { preventScrollReset: true });
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
        <Tabs keepMounted={false} onChange={changeDiff} value={activeDiff}>
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
  const [searchParams, setSearchParams] = useSearchParams();
  const query = searchParams.get("resourceQuery") ?? "";
  const showVersionMetadata = searchParams.get("showVersionMetadata") === "true";
  const filtered = useMemo(
    () => filterArgoDiffResources(resources, query, showVersionMetadata),
    [query, resources, showVersionMetadata],
  );
  const hiddenVersionCount = resources.filter((resource) => resource.versionMetadataOnly).length;

  function updateFilter(name: "resourceQuery" | "showVersionMetadata", value?: string) {
    const next = new URLSearchParams(searchParams);
    if (value) next.set(name, value);
    else next.delete(name);
    setSearchParams(next, { preventScrollReset: true, replace: true });
  }

  return (
    <Stack gap="md">
      <Group align="flex-end">
        <TextInput
          label="Filter by kind or name"
          onChange={(event) => updateFilter("resourceQuery", event.currentTarget.value)}
          placeholder="Deployment or external-dns"
          value={query}
        />
        <Checkbox
          checked={showVersionMetadata}
          label={`Show version-metadata-only changes (${hiddenVersionCount})`}
          onChange={(event) =>
            updateFilter("showVersionMetadata", event.currentTarget.checked ? "true" : undefined)
          }
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
  if (!promotion.decision && promotion.status !== "pending") return null;

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
