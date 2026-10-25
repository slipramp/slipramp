import {
  Badge,
  Box,
  Button,
  Code,
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

import activityStyles from "~/components/kawaii-activity.module.css";
import { PageHeader } from "~/components/page";
import {
  activityAnimationDelay,
  executionAnimationPosePools,
  type ExecutionAnimationPose,
} from "~/execution-activity";

type MascotPose = {
  label: string;
  pose: ExecutionAnimationPose;
};

type MascotGroup = {
  color: string;
  description: string;
  id: string;
  poses: readonly MascotPose[];
  title: string;
};

const mascotPoseLabels = {
  "phone-scroll": "Phone scroll",
  "book-browse": "Book browse",
  "iced-coffee-sip": "Iced coffee sip",
  "magazine-load": "Magazine load",
  "popcorn-munch": "Popcorn munch",
  "headphone-headbang": "Headphone headbang",
  "toaster-vigil": "Toaster vigil",
  "suspended-animation": "Suspended animation",
  "bubblegum-blow": "Bubblegum blow",
  "binocular-scan": "Binocular scan",
  "hourglass-turn": "Hourglass turn",
  "duck-float": "Duck float",
  "lava-lamp-gaze": "Lava-lamp gaze",
  "snail-coach": "Snail coach",
  "beach-ball-roll": "Beach-ball roll",
  "cat-petting": "Cat petting",
  "pinwheel-spin": "Pinwheel spin",
  "laptop-typing": "Laptop typing",
  "blueprint-review": "Blueprint review",
  "clipboard-check": "Clipboard check",
  "walkie-talkie": "Walkie-talkie",
  "antenna-tune": "Antenna tune",
  "drill-drive": "Drill drive",
  "weld-spark-pipe": "Weld spark pipe",
  "grenade-throw": "Grenade throw",
  "jackhammer-ride": "Jackhammer ride",
  "bubble-level-check": "Bubble-level check",
  "paint-roller-swipe": "Paint-roller swipe",
  "beer-sip": "Beer sip",
  "pennant-wave": "Pennant wave",
  "check-badge": "Check badge",
  "beer-toast": "Beer toast",
  "flower-offer": "Flower offer",
  "championship-belt": "Championship belt",
  "confetti-cannon": "Confetti cannon",
  "rifle-salute": "Rifle salute",
  "trophy-hoist": "Trophy hoist",
  "cowbell-clang": "Cowbell clang",
  "laptop-slump": "Laptop slump",
  "wrench-fumble": "Wrench fumble",
  "button-panic": "Button panic",
  "chart-plunge": "Chart plunge",
  "play-dead": "Play dead",
  "trash-can-hide": "Trash-can hide",
  "lever-regret": "Lever regret",
  "anvil-bonk": "Anvil bonk",
  "cable-cocoon": "Cable cocoon",
  "nuclear-vaporize": "Nuclear vaporize",
  "too-many-arms": "Too many arms",
  "trapdoor-deploy": "Trapdoor deploy",
  "steamroller-flat": "Steamroller flat",
  "under-desk-drag": "Under-desk drag",
  "fire-extinguisher-blast": "Fire-extinguisher blast",
  "explosion-walkaway": "Explosion walkaway",
} as const satisfies Record<ExecutionAnimationPose, string>;

function mascotPoses(poses: readonly ExecutionAnimationPose[]): readonly MascotPose[] {
  return poses.map((pose) => ({ label: mascotPoseLabels[pose], pose }));
}

const mascotGroups = [
  {
    color: "violet",
    description: "Runs waiting for a Controller and Promotions waiting for operator review.",
    id: "waiting",
    poses: mascotPoses(executionAnimationPosePools.waiting),
    title: "Waiting",
  },
  {
    color: "blue",
    description: "Runs in progress and Promotions being created or prepared.",
    id: "working",
    poses: mascotPoses(executionAnimationPosePools.working),
    title: "Working",
  },
  {
    color: "green",
    description: "Runs that succeeded and Promotions that were approved.",
    id: "successful",
    poses: mascotPoses(executionAnimationPosePools.celebrating),
    title: "Successful",
  },
  {
    color: "red",
    description: "Runs that failed or timed out and Promotions that were rejected or failed.",
    id: "unsuccessful",
    poses: mascotPoses(executionAnimationPosePools.frustrated),
    title: "Unsuccessful",
  },
  {
    color: "gray",
    description: "Canceled Runs and expired Promotions intentionally keep the neutral phone pose.",
    id: "stopped-or-expired",
    poses: mascotPoses(executionAnimationPosePools.phone),
    title: "Stopped or expired",
  },
] as const satisfies readonly MascotGroup[];

export const mascotPoseCount = Object.keys(mascotPoseLabels).length;

export function MascotGallery() {
  const [hideSmallVariants, setHideSmallVariants] = useState(false);

  return (
    <Stack gap="xl">
      <Stack gap="md">
        <Paper aria-label="Mascot phases" component="nav" p="md" withBorder>
          <Stack gap="sm">
            <Text fw={700} size="sm">
              Jump to a phase
            </Text>
            <Group gap="xs">
              {mascotGroups.map((group) => (
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
        </Paper>

        <PageHeader
          actions={
            <Switch
              checked={hideSmallVariants}
              label="Hide small variants"
              onChange={(event) => setHideSmallVariants(event.currentTarget.checked)}
            />
          }
          description="All production mascot loops grouped by the Run and Promotion states that use them."
          title="Mascot gallery"
        />
      </Stack>

      {mascotGroups.map((group) => (
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
          <SimpleGrid cols={{ base: 1, sm: 2, xl: 3 }}>
            {group.poses.map((pose) => (
              <MascotCard
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
  hideSmallVariants,
  label,
  pose,
}: Readonly<MascotPose & { hideSmallVariants: boolean }>) {
  const { entry, ref } = useIntersection<HTMLDivElement>({ rootMargin: "320px 0px" });
  const loaded = entry?.isIntersecting ?? false;

  return (
    <Paper p="lg" ref={ref} withBorder>
      <Stack align="center" gap="md">
        <SpritePreview label={label} loaded={loaded} pose={pose} size={140} />
        <Stack align="center" gap={2}>
          <Text fw={650}>{label}</Text>
          {hideSmallVariants ? null : <Code>{pose}</Code>}
        </Stack>
        {hideSmallVariants ? null : (
          <Group align="flex-end" gap="xl">
            <PreviewSize label="Mobile" loaded={loaded} pose={pose} size={104} />
            <PreviewSize label="Compact" loaded={loaded} pose={pose} size={48} />
          </Group>
        )}
      </Stack>
    </Paper>
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
