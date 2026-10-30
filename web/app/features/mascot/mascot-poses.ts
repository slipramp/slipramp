import { executionAnimationPosePools, type ExecutionAnimationPose } from "./execution-activity.ts";

export const mascotTagLabels = {
  agriculture: "Agriculture",
  animals: "Animals",
  aviation: "Aviation",
  computers: "Computers",
  construction: "Construction",
  "food-and-drink": "Food & drink",
  leisure: "Leisure",
  macabre: "Macabre",
  military: "Military",
  "science-fiction": "Science fiction",
  slapstick: "Slapstick",
} as const;

export type MascotTag = keyof typeof mascotTagLabels;

export type MascotPose = {
  label: string;
  pose: ExecutionAnimationPose;
  tags: readonly MascotTag[];
};

export type MascotGroup = {
  color: string;
  description: string;
  id: string;
  poses: readonly MascotPose[];
  title: string;
};

const mascotPoseMetadata = {
  "phone-scroll": { label: "Phone scroll", tags: ["leisure"] },
  "book-browse": { label: "Book browse", tags: ["leisure"] },
  "camera-snap": { label: "Camera snap", tags: ["leisure"] },
  "traffic-light-match": { label: "Traffic-light match", tags: ["construction"] },
  "iced-coffee-sip": { label: "Iced coffee sip", tags: ["food-and-drink", "leisure"] },
  "magazine-load": { label: "Magazine load", tags: ["military"] },
  "rifle-clean": { label: "Rifle clean", tags: ["military"] },
  "popcorn-munch": { label: "Popcorn munch", tags: ["food-and-drink", "leisure"] },
  "headphone-headbang": { label: "Headphone headbang", tags: ["leisure"] },
  "toaster-vigil": { label: "Toaster vigil", tags: ["food-and-drink", "leisure"] },
  "suspended-animation": {
    label: "Suspended animation",
    tags: ["science-fiction", "leisure"],
  },
  "bubblegum-blow": { label: "Bubblegum blow", tags: ["food-and-drink", "leisure"] },
  "binocular-scan": { label: "Binocular scan", tags: ["military", "leisure"] },
  "hourglass-turn": { label: "Hourglass turn", tags: ["leisure"] },
  "duck-float": { label: "Duck float", tags: ["animals", "leisure"] },
  "lava-lamp-gaze": { label: "Lava-lamp gaze", tags: ["leisure", "science-fiction"] },
  "plasma-globe-touch": {
    label: "Plasma-globe touch",
    tags: ["leisure", "science-fiction"],
  },
  "progress-bar-stare": { label: "Progress-bar stare", tags: ["computers"] },
  "snail-coach": { label: "Snail coach", tags: ["animals", "leisure"] },
  "beach-ball-roll": { label: "Beach-ball roll", tags: ["leisure"] },
  "cat-petting": { label: "Cat petting", tags: ["animals", "leisure"] },
  "grass-touch": { label: "Grass touch", tags: ["leisure"] },
  "pinwheel-spin": { label: "Pinwheel spin", tags: ["leisure"] },
  "skull-consult": { label: "Skull consult", tags: ["macabre", "leisure"] },
  "candle-vigil": { label: "Candle vigil", tags: ["macabre", "leisure"] },
  "ghost-jar": { label: "Ghost jar", tags: ["macabre"] },
  "ramen-slurp": { label: "Ramen slurp", tags: ["food-and-drink", "leisure"] },
  "window-seat-dreamer": {
    label: "Window-seat dreamer",
    tags: ["aviation", "leisure"],
  },
  "tray-table-snack": {
    label: "Tray-table snack",
    tags: ["aviation", "food-and-drink"],
  },
  "red-eye-passenger": {
    label: "Red-eye passenger",
    tags: ["aviation", "leisure"],
  },
  "seatbelt-click": { label: "Seatbelt click", tags: ["aviation"] },
  "turbulence-cup-save": { label: "Turbulence cup save", tags: ["aviation"] },
  "cabin-blanket-tuck": {
    label: "Cabin blanket tuck",
    tags: ["aviation", "leisure"],
  },
  "egg-basket-check": { label: "Egg-basket check", tags: ["agriculture"] },
  "mainframe-reel-watch": { label: "Mainframe-reel watch", tags: ["computers"] },
  "cpu-cooler-install": { label: "CPU-cooler install", tags: ["computers"] },
  "ram-install": { label: "RAM install", tags: ["computers"] },
  "laptop-typing": { label: "Laptop typing", tags: ["computers"] },
  "inflight-laptop-typing": {
    label: "Inflight laptop typing",
    tags: ["aviation", "computers"],
  },
  "runway-marshal": { label: "Runway marshal", tags: ["aviation"] },
  "flight-controller": { label: "Flight controller", tags: ["aviation"] },
  "blueprint-review": { label: "Blueprint review", tags: ["construction"] },
  "clipboard-check": { label: "Clipboard check", tags: ["construction"] },
  "walkie-talkie": { label: "Walkie-talkie", tags: ["military"] },
  "antenna-tune": { label: "Antenna tune", tags: ["military"] },
  "drill-drive": { label: "Drill drive", tags: ["construction"] },
  "weld-spark-pipe": { label: "Weld spark pipe", tags: ["construction"] },
  "grenade-throw": { label: "Grenade throw", tags: ["military"] },
  "mortar-drop": { label: "Mortar drop", tags: ["military"] },
  "shoulder-launcher-check": { label: "Shoulder-launcher check", tags: ["military"] },
  "map-coordinate-check": { label: "Map coordinate check", tags: ["military"] },
  "rangefinder-check": { label: "Rangefinder check", tags: ["military"] },
  "field-medic-check": { label: "Field medic check", tags: ["military"] },
  "field-radio-operator": { label: "Field-radio operator", tags: ["military"] },
  "belt-feed-check": { label: "Belt-feed check", tags: ["military"] },
  "entrenching-tool-dig": {
    label: "Entrenching-tool dig",
    tags: ["construction", "military"],
  },
  "gas-mask-seal-check": { label: "Gas-mask seal check", tags: ["military"] },
  "drone-reconnaissance": {
    label: "Drone reconnaissance",
    tags: ["aviation", "military"],
  },
  "plate-carrier-donning": { label: "Plate-carrier donning", tags: ["military"] },
  "eod-suit-rise": { label: "EOD-suit rise", tags: ["military"] },
  "field-generator-start": {
    label: "Field-generator start",
    tags: ["construction", "military"],
  },
  "jackhammer-ride": { label: "Jackhammer ride", tags: ["construction", "slapstick"] },
  "bubble-level-check": { label: "Bubble-level check", tags: ["construction"] },
  "paint-roller-swipe": { label: "Paint-roller swipe", tags: ["construction"] },
  "pumpkin-autopsy": { label: "Pumpkin autopsy", tags: ["construction", "macabre"] },
  "breaker-bar-bounce": { label: "Breaker-bar bounce", tags: ["construction", "slapstick"] },
  "hay-bale-roll": { label: "Hay-bale roll", tags: ["agriculture"] },
  "watering-can-pour": { label: "Watering-can pour", tags: ["agriculture"] },
  "wheelbarrow-push": { label: "Wheelbarrow push", tags: ["agriculture"] },
  "dough-flip": { label: "Dough flip", tags: ["food-and-drink"] },
  "dough-knead": { label: "Dough knead", tags: ["food-and-drink"] },
  "cake-piping": { label: "Cake piping", tags: ["food-and-drink"] },
  "grillmaster-flip": { label: "Grillmaster flip", tags: ["food-and-drink"] },
  "wok-toss": { label: "Wok toss", tags: ["food-and-drink"] },
  "kebab-turn": { label: "Kebab turn", tags: ["food-and-drink"] },
  "beer-sip": { label: "Beer sip", tags: ["food-and-drink"] },
  "pennant-wave": { label: "Pennant wave", tags: ["leisure"] },
  "check-badge": { label: "Check badge", tags: ["construction"] },
  "beer-toast": { label: "Beer toast", tags: ["food-and-drink"] },
  "flower-offer": { label: "Flower offer", tags: ["leisure"] },
  "championship-belt": { label: "Championship belt", tags: ["leisure"] },
  "confetti-cannon": { label: "Confetti cannon", tags: ["leisure"] },
  "jackpot-stars": { label: "Jackpot stars", tags: ["leisure", "slapstick"] },
  "pistol-salute": { label: "Pistol salute", tags: ["military"] },
  "flag-salute": { label: "Flag salute", tags: ["military"] },
  "trophy-hoist": { label: "Trophy hoist", tags: ["leisure"] },
  "cowbell-clang": { label: "Cowbell clang", tags: ["leisure"] },
  "chart-rise": { label: "Chart rise", tags: ["slapstick"] },
  "server-hug": { label: "Server hug", tags: ["computers"] },
  "floppy-disk-insert": { label: "Floppy-disk insert", tags: ["computers"] },
  "hard-drive-spinup": { label: "Hard-drive spinup", tags: ["computers"] },
  "pixel-heart-display": { label: "Pixel-heart display", tags: ["computers"] },
  "cargo-loader-triumph": {
    label: "Cargo-loader triumph",
    tags: ["construction", "science-fiction"],
  },
  "guitar-power-chord": { label: "Guitar power chord", tags: ["leisure"] },
  "oven-mitt-triumph": { label: "Oven-mitt triumph", tags: ["food-and-drink"] },
  "parachute-landing": { label: "Parachute landing", tags: ["aviation", "military"] },
  "laptop-slump": { label: "Laptop slump", tags: ["computers", "slapstick"] },
  "server-bite": { label: "Server bite", tags: ["computers", "slapstick"] },
  "rocket-boot-misfire": { label: "Rocket-boot misfire", tags: ["science-fiction", "slapstick"] },
  "kaiju-server-stomp": {
    label: "Kaiju server stomp",
    tags: ["computers", "science-fiction", "slapstick"],
  },
  "wrench-fumble": { label: "Wrench fumble", tags: ["construction", "slapstick"] },
  "button-panic": { label: "Button panic", tags: ["slapstick"] },
  "chart-plunge": { label: "Chart plunge", tags: ["slapstick"] },
  "play-dead": { label: "Play dead", tags: ["macabre", "slapstick"] },
  "tombstone-peek": { label: "Tombstone peek", tags: ["macabre", "slapstick"] },
  "trash-can-hide": { label: "Trash-can hide", tags: ["slapstick"] },
  "lever-regret": { label: "Lever regret", tags: ["slapstick"] },
  "anvil-bonk": { label: "Anvil bonk", tags: ["construction", "slapstick"] },
  "cable-cocoon": { label: "Cable cocoon", tags: ["computers", "slapstick"] },
  "nuclear-vaporize": {
    label: "Nuclear vaporize",
    tags: ["science-fiction", "slapstick"],
  },
  "lightning-vaporize": {
    label: "Lightning vaporize",
    tags: ["science-fiction", "slapstick"],
  },
  "too-many-arms": {
    label: "Too many arms",
    tags: ["macabre", "science-fiction", "slapstick"],
  },
  "trapdoor-deploy": { label: "Trapdoor deploy", tags: ["slapstick"] },
  "steamroller-flat": { label: "Steamroller flat", tags: ["construction", "slapstick"] },
  "under-desk-drag": { label: "Under-desk drag", tags: ["macabre", "slapstick"] },
  "fire-extinguisher-blast": {
    label: "Fire-extinguisher blast",
    tags: ["construction", "slapstick"],
  },
  "parachute-collapse": {
    label: "Parachute collapse",
    tags: ["aviation", "military", "slapstick"],
  },
  "explosion-walkaway": {
    label: "Explosion walkaway",
    tags: ["military", "slapstick"],
  },
} as const satisfies Record<ExecutionAnimationPose, { label: string; tags: readonly MascotTag[] }>;

function mascotPoses(poses: readonly ExecutionAnimationPose[]): readonly MascotPose[] {
  return poses
    .map((pose) => ({ pose, ...mascotPoseMetadata[pose] }))
    .toSorted((left, right) => left.label.localeCompare(right.label));
}

export const mascotGroups = [
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

const catalogPoseNames = [
  ...executionAnimationPosePools.waiting,
  ...executionAnimationPosePools.working,
  ...executionAnimationPosePools.celebrating,
  ...executionAnimationPosePools.frustrated,
] as const;

export const mascotPoseCatalog = mascotPoses(catalogPoseNames);
export const mascotPoseCount = mascotPoseCatalog.length;

export const mascotTags = Object.entries(mascotTagLabels).map(([tag, label]) => ({
  label,
  tag: tag as MascotTag,
}));

export function findMascotPose(pose: string | undefined): MascotPose | undefined {
  return mascotPoseCatalog.find((candidate) => candidate.pose === pose);
}

export function mascotGroupsForTag(tag: MascotTag | undefined): readonly MascotGroup[] {
  if (!tag) return mascotGroups;

  return mascotGroups
    .map((group) => ({
      ...group,
      poses: group.poses.filter((pose) => pose.tags.includes(tag)),
    }))
    .filter((group) => group.poses.length > 0);
}

export function mascotTagFromSearchParams(searchParams: URLSearchParams): MascotTag | undefined {
  const tag = searchParams.get("tag");

  return tag && tag in mascotTagLabels ? (tag as MascotTag) : undefined;
}

export function mascotGalleryURL(path: string, tag: MascotTag | undefined): string {
  if (!tag) return path;

  const searchParams = new URLSearchParams();
  searchParams.set("tag", tag);

  return `${path}?${searchParams.toString()}`;
}

export function mascotPoseGroups(pose: ExecutionAnimationPose): readonly MascotGroup[] {
  return mascotGroups.filter((group) => group.poses.some((candidate) => candidate.pose === pose));
}
