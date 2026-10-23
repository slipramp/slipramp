import { Anchor, Code, Group, HoverCard, Paper, Stack, Text, Title } from "@mantine/core";
import { IconExternalLink, IconGitCommit } from "@tabler/icons-react";

import type { GitCommit } from "~/api/generated";
import { RelativeTime, Timestamp } from "~/components/display";
import { Identity } from "~/components/identity";
import { shortIdentifier } from "~/display";

type CommitReferenceProps = {
  commit: GitCommit;
  length?: number;
};

export function CommitReference({ commit, length = 12 }: Readonly<CommitReferenceProps>) {
  const hasDetails = Boolean(commit.title || commit.author || commit.authoredAt);
  const reference = commit.url ? (
    <Anchor
      aria-label={`Open commit ${commit.sha}`}
      href={commit.url}
      rel="noreferrer"
      target="_blank"
    >
      <Code>{shortIdentifier(commit.sha, length)}</Code>
    </Anchor>
  ) : (
    <Code aria-label={`Commit ${commit.sha}`} tabIndex={hasDetails ? 0 : undefined}>
      {shortIdentifier(commit.sha, length)}
    </Code>
  );

  if (!hasDetails) return reference;

  return (
    <HoverCard openDelay={250} position="bottom-start" shadow="md" width={360} withinPortal>
      <HoverCard.Target>{reference}</HoverCard.Target>
      <HoverCard.Dropdown>
        <Stack gap="xs">
          <Text
            fw={650}
            lineClamp={2}
            size="sm"
            style={{ overflowWrap: "anywhere" }}
            title={commit.title}
          >
            {commit.title ?? "Untitled commit"}
          </Text>
          <Code style={{ overflowWrap: "anywhere" }}>{commit.sha}</Code>
          <Group gap="xs" wrap="wrap">
            {commit.author ? (
              <Identity avatarSize={22} name={commit.author} nameProps={{ fw: 500, size: "xs" }} />
            ) : null}
            {commit.author && commit.authoredAt ? (
              <Text c="dimmed" size="xs">
                ·
              </Text>
            ) : null}
            {commit.authoredAt ? (
              <RelativeTime c="dimmed" size="xs" value={commit.authoredAt} />
            ) : null}
          </Group>
        </Stack>
      </HoverCard.Dropdown>
    </HoverCard>
  );
}

export function CommitDetails({ commit }: Readonly<{ commit: GitCommit }>) {
  const title = commit.title ?? `Commit ${shortIdentifier(commit.sha, 12)}`;

  return (
    <Paper p="lg" withBorder>
      <Group align="flex-start" justify="space-between" wrap="wrap">
        <Stack gap="xs" style={{ flex: "1 1 32rem", minWidth: 0 }}>
          <Group gap="xs">
            <IconGitCommit aria-hidden size={18} />
            <Text c="dimmed" fw={650} size="xs" tt="uppercase">
              Git commit
            </Text>
          </Group>
          <Title lineClamp={2} order={2} style={{ overflowWrap: "anywhere" }} title={title}>
            {commit.url ? (
              <Anchor
                c="inherit"
                fw="inherit"
                fz="inherit"
                href={commit.url}
                rel="noreferrer"
                target="_blank"
                underline="hover"
              >
                {title}
              </Anchor>
            ) : (
              title
            )}
          </Title>
          <Group gap="xs" wrap="wrap">
            {commit.author ? <Identity avatarSize={28} name={commit.author} /> : null}
            {commit.author && commit.authoredAt ? (
              <Text c="dimmed" size="sm">
                ·
              </Text>
            ) : null}
            {commit.authoredAt ? (
              <Timestamp c="dimmed" size="sm" value={commit.authoredAt} />
            ) : null}
          </Group>
          <Code style={{ overflowWrap: "anywhere", width: "fit-content" }}>{commit.sha}</Code>
        </Stack>
        {commit.url ? (
          <Anchor href={commit.url} rel="noreferrer" style={{ flexShrink: 0 }} target="_blank">
            <Group gap={6} wrap="nowrap">
              View commit
              <IconExternalLink aria-hidden size={15} />
            </Group>
          </Anchor>
        ) : null}
      </Group>
    </Paper>
  );
}
