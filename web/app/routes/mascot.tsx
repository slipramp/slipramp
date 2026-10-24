import { Badge, Box, Code, Group, Paper, SimpleGrid, Stack, Text, Title } from "@mantine/core";

import activityStyles from "~/components/kawaii-activity.module.css";
import { PageHeader } from "~/components/page";
import {
  activityAnimationDelay,
  executionAnimationPosePools,
  type ExecutionAnimationPose,
} from "~/execution-activity";
import { pageTitle } from "~/page-title";

type MascotPose = {
  label: string;
  pose: ExecutionAnimationPose;
};

type MascotGroup = {
  color: string;
  description: string;
  poses: readonly MascotPose[];
  title: string;
};

const mascotPoseLabels = {
  "phone-scroll": "Phone scroll",
  "book-browse": "Book browse",
  "iced-coffee-sip": "Iced coffee sip",
  "popcorn-munch": "Popcorn munch",
  "headphone-headbang": "Headphone headbang",
  "laptop-typing": "Laptop typing",
  "blueprint-review": "Blueprint review",
  "clipboard-check": "Clipboard check",
  "walkie-talkie": "Walkie-talkie",
  "beer-sip": "Beer sip",
  "pennant-wave": "Pennant wave",
  "check-badge": "Check badge",
  "beer-toast": "Beer toast",
  "laptop-slump": "Laptop slump",
  "wrench-fumble": "Wrench fumble",
  "button-panic": "Button panic",
  "play-dead": "Play dead",
  "trash-can-hide": "Trash-can hide",
  "lever-regret": "Lever regret",
  "anvil-bonk": "Anvil bonk",
} as const satisfies Record<ExecutionAnimationPose, string>;

function mascotPoses(poses: readonly ExecutionAnimationPose[]): readonly MascotPose[] {
  return poses.map((pose) => ({ label: mascotPoseLabels[pose], pose }));
}

const mascotGroups = [
  {
    color: "violet",
    description: "Runs waiting to be picked up by a Controller.",
    poses: mascotPoses(executionAnimationPosePools.waiting),
    title: "Queued",
  },
  {
    color: "blue",
    description: "Runs that are assigned, preparing, or running.",
    poses: mascotPoses(executionAnimationPosePools.working),
    title: "Working",
  },
  {
    color: "green",
    description: "Runs that finished successfully.",
    poses: mascotPoses(executionAnimationPosePools.celebrating),
    title: "Succeeded",
  },
  {
    color: "red",
    description: "Runs that failed or timed out.",
    poses: mascotPoses(executionAnimationPosePools.frustrated),
    title: "Failed or timed out",
  },
  {
    color: "gray",
    description: "Canceled Runs intentionally keep the neutral phone pose.",
    poses: mascotPoses(executionAnimationPosePools.phone),
    title: "Canceled",
  },
] as const satisfies readonly MascotGroup[];

export function meta() {
  return [{ title: pageTitle("Mascot gallery") }];
}

export default function MascotGallery() {
  return (
    <Stack gap="xl">
      <PageHeader
        description="All production mascot loops grouped by the Run state that uses them."
        title="Mascot gallery"
      />

      {mascotGroups.map((group) => (
        <Stack gap="md" key={group.title}>
          <Group gap="sm">
            <Title order={2}>{group.title}</Title>
            <Badge color={group.color} variant="light">
              {group.poses.length} {group.poses.length === 1 ? "pose" : "poses"}
            </Badge>
          </Group>
          <Text c="dimmed">{group.description}</Text>
          <SimpleGrid cols={{ base: 1, sm: 2, xl: 3 }}>
            {group.poses.map((pose) => (
              <MascotCard key={`${group.title}:${pose.pose}`} {...pose} />
            ))}
          </SimpleGrid>
        </Stack>
      ))}
    </Stack>
  );
}

function MascotCard({ label, pose }: Readonly<MascotPose>) {
  return (
    <Paper p="lg" withBorder>
      <Stack align="center" gap="md">
        <SpritePreview label={label} pose={pose} size={140} />
        <Stack align="center" gap={2}>
          <Text fw={650}>{label}</Text>
          <Code>{pose}</Code>
        </Stack>
        <Group align="flex-end" gap="xl">
          <PreviewSize label="Mobile" pose={pose} size={104} />
          <PreviewSize label="Compact" pose={pose} size={48} />
        </Group>
      </Stack>
    </Paper>
  );
}

function PreviewSize({
  label,
  pose,
  size,
}: Readonly<{ label: string; pose: ExecutionAnimationPose; size: number }>) {
  return (
    <Stack align="center" gap={4}>
      <SpritePreview pose={pose} size={size} />
      <Text c="dimmed" size="xs">
        {label} · {size}px
      </Text>
    </Stack>
  );
}

function SpritePreview({
  label,
  pose,
  size,
}: Readonly<{
  label?: string;
  pose: ExecutionAnimationPose;
  size: number;
}>) {
  return (
    <Box
      aria-label={label ? `${label} animation` : undefined}
      role={label ? "img" : undefined}
      style={{ height: size, position: "relative", width: size }}
    >
      <Box
        aria-hidden="true"
        className={activityStyles.sprite}
        data-pose={pose}
        style={{
          animationDelay: `${activityAnimationDelay(`mascot-gallery:${pose}`)}ms`,
          width: size,
        }}
      />
    </Box>
  );
}
