import {
  Alert,
  Badge,
  Box,
  Button,
  Code,
  Divider,
  Group,
  Paper,
  SimpleGrid,
  Stack,
  Switch,
  Text,
  Title,
} from "@mantine/core";
import { useIntersection } from "@mantine/hooks";
import { useState } from "react";
import { Link, useSearchParams } from "react-router";

import galleryStyles from "~/components/mascot-gallery.module.css";
import activityStyles from "~/components/kawaii-activity.module.css";
import { PageHeader } from "~/components/page";
import { PageBackLink } from "~/components/page-back-link";
import { activityAnimationDelay, type ExecutionAnimationPose } from "~/execution-activity";
import {
  findMascotPose,
  mascotGalleryURL,
  mascotGroupsForTag,
  mascotPoseCatalog,
  mascotPoseCount,
  mascotPoseGroups,
  mascotTagFromSearchParams,
  mascotTagLabels,
  mascotTags,
  type MascotPose,
} from "~/mascot-poses";

export { mascotPoseCount } from "~/mascot-poses";

export function MascotGallery({
  detailPathPrefix = "/mascot",
  galleryPath = "/mascot",
}: Readonly<{ detailPathPrefix?: string; galleryPath?: string }>) {
  const [compactView, setCompactView] = useState(false);
  const [hideSmallVariants, setHideSmallVariants] = useState(false);
  const [searchParams] = useSearchParams();
  const selectedTag = mascotTagFromSearchParams(searchParams);
  const visibleGroups = mascotGroupsForTag(selectedTag);
  const visiblePoseCount = mascotPoseCatalog.filter(
    (pose) => !selectedTag || pose.tags.includes(selectedTag),
  ).length;

  return (
    <Stack gap="xl">
      <Stack gap="md">
        <Paper aria-label="Mascot gallery filters" component="nav" p="md" withBorder>
          <Stack gap="md">
            <Stack gap="sm">
              <Group justify="space-between">
                <Text fw={700} size="sm">
                  Filter by tag
                </Text>
                {selectedTag ? (
                  <Button component={Link} size="compact-sm" to={galleryPath} variant="subtle">
                    Clear filters
                  </Button>
                ) : null}
              </Group>
              <Group gap="xs">
                {mascotTags.map(({ label, tag }) => {
                  const selected = selectedTag === tag;

                  return (
                    <Button
                      aria-pressed={selected}
                      color="pink"
                      component={Link}
                      key={tag}
                      radius="xl"
                      size="compact-sm"
                      to={mascotGalleryURL(galleryPath, selected ? undefined : tag)}
                      variant={selected ? "filled" : "light"}
                    >
                      {label}
                    </Button>
                  );
                })}
              </Group>
            </Stack>

            <Divider />

            <Stack gap="sm">
              <Text fw={700} size="sm">
                Jump to a phase
              </Text>
              <Group gap="xs">
                {visibleGroups.map((group) => (
                  <Button
                    color={group.color}
                    component="a"
                    href={`#${group.id}`}
                    key={group.id}
                    radius="xl"
                    size="compact-sm"
                    variant="light"
                  >
                    {group.title}
                  </Button>
                ))}
              </Group>
            </Stack>
          </Stack>
        </Paper>

        <PageHeader
          actions={
            <Group gap="md">
              <Switch
                checked={hideSmallVariants}
                disabled={compactView}
                label="Hide small variants"
                onChange={(event) => setHideSmallVariants(event.currentTarget.checked)}
              />
              <Switch
                checked={compactView}
                label="Compact view"
                onChange={(event) => setCompactView(event.currentTarget.checked)}
              />
            </Group>
          }
          description={
            !selectedTag
              ? "All production mascot loops grouped by the Run and Promotion states that use them."
              : `Showing ${visiblePoseCount} of ${mascotPoseCount} poses tagged ${mascotTagLabels[selectedTag].toLocaleLowerCase()}.`
          }
          title="Mascot gallery"
        />
      </Stack>

      {visibleGroups.map((group) => (
        <Stack
          component="section"
          gap="md"
          id={group.id}
          key={group.id}
          style={{ scrollMarginTop: "5.5rem" }}
        >
          <Group gap="sm">
            <Title order={2}>{group.title}</Title>
            <Badge color={group.color} variant="light">
              {group.poses.length} {group.poses.length === 1 ? "pose" : "poses"}
            </Badge>
          </Group>
          <Text c="dimmed">{group.description}</Text>
          <SimpleGrid
            cols={compactView ? { base: 2, sm: 3, md: 4, xl: 6 } : { base: 1, sm: 2, xl: 3 }}
            spacing={compactView ? "sm" : "md"}
            verticalSpacing={compactView ? "sm" : "md"}
          >
            {group.poses.map((pose) => (
              <MascotCard
                compact={compactView}
                detailPathPrefix={detailPathPrefix}
                hideSmallVariants={hideSmallVariants}
                key={`${group.title}:${pose.pose}`}
                {...pose}
              />
            ))}
          </SimpleGrid>
        </Stack>
      ))}
    </Stack>
  );
}

function MascotCard({
  compact,
  detailPathPrefix,
  hideSmallVariants,
  label,
  pose,
  tags,
}: Readonly<
  MascotPose & { compact: boolean; detailPathPrefix: string; hideSmallVariants: boolean }
>) {
  const { entry, ref } = useIntersection<HTMLDivElement>({ rootMargin: "320px 0px" });
  const loaded = entry?.isIntersecting ?? false;

  return (
    <Box ref={ref}>
      <Paper
        aria-label={`View ${label}`}
        className={`${galleryStyles.card} ${compact ? galleryStyles.compactCard : ""}`}
        component={Link}
        p={compact ? "sm" : "lg"}
        to={`${detailPathPrefix}/${encodeURIComponent(pose)}`}
        withBorder
      >
        <Stack align="center" gap={compact ? "xs" : "md"}>
          <SpritePreview label={label} loaded={loaded} pose={pose} size={140} />
          <Stack align="center" gap="xs">
            <Stack align="center" gap={2}>
              <Text
                className={compact ? galleryStyles.compactLabel : undefined}
                fw={650}
                size={compact ? "sm" : undefined}
              >
                {label}
              </Text>
              {compact || hideSmallVariants ? null : <Code>{pose}</Code>}
            </Stack>
            {compact ? null : (
              <Group gap={5} justify="center">
                {tags.map((tag) => (
                  <Badge color="pink" key={tag} size="sm" variant="light">
                    {mascotTagLabels[tag]}
                  </Badge>
                ))}
              </Group>
            )}
          </Stack>
          {compact || hideSmallVariants ? null : (
            <Group align="flex-end" gap="xl">
              <PreviewSize label="Mobile" loaded={loaded} pose={pose} size={104} />
              <PreviewSize label="Compact" loaded={loaded} pose={pose} size={48} />
            </Group>
          )}
        </Stack>
      </Paper>
    </Box>
  );
}

export function MascotPoseDetail({
  galleryPath = "/mascot",
  pose: poseName,
}: Readonly<{ galleryPath?: string; pose: string | undefined }>) {
  const pose = findMascotPose(poseName);
  if (!pose) {
    return (
      <Stack gap="md">
        <PageBackLink label="Back to mascot gallery" to={galleryPath} />
        <Alert color="red" title="Mascot pose not found">
          No production mascot pose matches this URL.
        </Alert>
      </Stack>
    );
  }

  const groups = mascotPoseGroups(pose.pose);

  return (
    <Stack gap="xl">
      <PageHeader
        back={<PageBackLink label="Back to mascot gallery" to={galleryPath} />}
        badges={groups.map((group) => (
          <Badge color={group.color} key={group.id} variant="light">
            {group.title}
          </Badge>
        ))}
        description="A production Slipramp mascot loop with a large showcase and every supported interface size."
        title={pose.label}
      />

      <Paper className={galleryStyles.detail} p={{ base: "lg", sm: "xl" }} withBorder>
        <SimpleGrid cols={{ base: 1, md: 2 }} spacing="xl" verticalSpacing="xl">
          <Stack align="center" gap="sm" justify="center">
            <SpritePreview label={pose.label} loaded pose={pose.pose} size={240} />
            <Text c="dimmed" size="sm">
              Showcase · 240px
            </Text>
          </Stack>

          <Stack gap="lg" justify="center">
            <Stack gap={4}>
              <Text c="dimmed" fw={700} size="xs" tt="uppercase">
                Pose slug
              </Text>
              <Code>{pose.pose}</Code>
            </Stack>

            <Stack gap="xs">
              <Text c="dimmed" fw={700} size="xs" tt="uppercase">
                Tags
              </Text>
              <Group gap="xs">
                {pose.tags.map((tag) => (
                  <Button
                    color="pink"
                    component={Link}
                    key={tag}
                    radius="xl"
                    size="compact-sm"
                    to={mascotGalleryURL(galleryPath, tag)}
                    variant="light"
                  >
                    {mascotTagLabels[tag]}
                  </Button>
                ))}
              </Group>
            </Stack>

            <Stack gap="xs">
              <Text c="dimmed" fw={700} size="xs" tt="uppercase">
                Interface sizes
              </Text>
              <Group align="flex-end" gap="xl">
                <PreviewSize label="Detail" loaded pose={pose.pose} size={140} />
                <PreviewSize label="Mobile" loaded pose={pose.pose} size={104} />
                <PreviewSize label="Compact" loaded pose={pose.pose} size={48} />
              </Group>
            </Stack>
          </Stack>
        </SimpleGrid>
      </Paper>
    </Stack>
  );
}

function PreviewSize({
  label,
  loaded,
  pose,
  size,
}: Readonly<{
  label: string;
  loaded: boolean;
  pose: ExecutionAnimationPose;
  size: number;
}>) {
  return (
    <Stack align="center" gap={4}>
      <SpritePreview loaded={loaded} pose={pose} size={size} />
      <Text c="dimmed" size="xs">
        {label} · {size}px
      </Text>
    </Stack>
  );
}

function SpritePreview({
  label,
  loaded,
  pose,
  size,
}: Readonly<{
  label?: string;
  loaded: boolean;
  pose: ExecutionAnimationPose;
  size: number;
}>) {
  return (
    <Box
      aria-label={label ? `${label} animation` : undefined}
      role={label ? "img" : undefined}
      style={{ height: size, position: "relative", width: size }}
    >
      {loaded ? (
        <Box
          aria-hidden="true"
          className={activityStyles.sprite}
          data-pose={pose}
          style={{
            animationDelay: `${activityAnimationDelay(`mascot-gallery:${pose}`)}ms`,
            width: size,
          }}
        />
      ) : null}
    </Box>
  );
}
