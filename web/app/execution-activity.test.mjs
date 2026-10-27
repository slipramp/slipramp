import assert from "node:assert/strict";
import test from "node:test";

import {
  activityAnimationDelay,
  activityAnimationPose,
  promotionActivity,
  promotionDetailActivity,
  promotionDetailNeedsRefresh,
  runActivity,
  runResultActivity,
} from "./execution-activity.ts";

test("animation offsets are stable and spread out similar Run IDs", () => {
  const ids = [
    "11111111-1111-4111-8111-111111111111",
    "11111111-1111-4111-8111-111111111112",
    "11111111-1111-4111-8111-111111111113",
  ];
  const offsets = ids.map(activityAnimationDelay);

  assert.deepEqual(ids.map(activityAnimationDelay), offsets);
  assert.equal(new Set(offsets).size, ids.length);
  assert.ok(offsets.every((offset) => offset <= 0 && offset > -10000));
});

test("waiting animation variants are stable and spread across IDs", () => {
  const ids = Array.from({ length: 200 }, (_, index) => `run-${index}`);
  const variants = ids.map((id) => activityAnimationPose("waiting", id));

  assert.deepEqual(
    ids.map((id) => activityAnimationPose("waiting", id)),
    variants,
  );
  assert.deepEqual(
    new Set(variants),
    new Set([
      "phone-scroll",
      "book-browse",
      "camera-snap",
      "traffic-light-match",
      "iced-coffee-sip",
      "magazine-load",
      "rifle-clean",
      "popcorn-munch",
      "headphone-headbang",
      "toaster-vigil",
      "suspended-animation",
      "bubblegum-blow",
      "binocular-scan",
      "hourglass-turn",
      "duck-float",
      "lava-lamp-gaze",
      "plasma-globe-touch",
      "progress-bar-stare",
      "snail-coach",
      "beach-ball-roll",
      "cat-petting",
      "grass-touch",
      "pinwheel-spin",
      "skull-consult",
      "candle-vigil",
      "ghost-jar",
      "ramen-slurp",
      "window-seat-dreamer",
      "tray-table-snack",
      "red-eye-passenger",
      "seatbelt-click",
      "turbulence-cup-save",
      "cabin-blanket-tuck",
      "egg-basket-check",
    ]),
  );
});

test("working animation variants are stable and spread across IDs", () => {
  const ids = Array.from({ length: 1000 }, (_, index) => `run-${index}`);
  const variants = ids.map((id) => activityAnimationPose("working", id));

  assert.deepEqual(
    ids.map((id) => activityAnimationPose("working", id)),
    variants,
  );
  assert.deepEqual(
    new Set(variants),
    new Set([
      "laptop-typing",
      "inflight-laptop-typing",
      "runway-marshal",
      "flight-controller",
      "mainframe-reel-watch",
      "cpu-cooler-install",
      "ram-install",
      "blueprint-review",
      "clipboard-check",
      "walkie-talkie",
      "antenna-tune",
      "drill-drive",
      "weld-spark-pipe",
      "grenade-throw",
      "mortar-drop",
      "shoulder-launcher-check",
      "map-coordinate-check",
      "rangefinder-check",
      "field-medic-check",
      "field-radio-operator",
      "belt-feed-check",
      "entrenching-tool-dig",
      "gas-mask-seal-check",
      "drone-reconnaissance",
      "plate-carrier-donning",
      "eod-suit-rise",
      "field-generator-start",
      "jackhammer-ride",
      "bubble-level-check",
      "paint-roller-swipe",
      "pumpkin-autopsy",
      "breaker-bar-bounce",
      "hay-bale-roll",
      "watering-can-pour",
      "wheelbarrow-push",
      "dough-flip",
      "dough-knead",
      "cake-piping",
      "grillmaster-flip",
      "wok-toss",
      "kebab-turn",
    ]),
  );
});

test("celebrating animation variants are stable and spread across IDs", () => {
  const ids = Array.from({ length: 80 }, (_, index) => `run-${index}`);
  const variants = ids.map((id) => activityAnimationPose("celebrating", id));

  assert.deepEqual(
    ids.map((id) => activityAnimationPose("celebrating", id)),
    variants,
  );
  assert.deepEqual(
    new Set(variants),
    new Set([
      "beer-sip",
      "pennant-wave",
      "check-badge",
      "beer-toast",
      "explosion-walkaway",
      "flower-offer",
      "championship-belt",
      "confetti-cannon",
      "jackpot-stars",
      "pistol-salute",
      "flag-salute",
      "trophy-hoist",
      "cowbell-clang",
      "chart-rise",
      "server-hug",
      "cargo-loader-triumph",
      "guitar-power-chord",
      "oven-mitt-triumph",
      "parachute-landing",
    ]),
  );
});

test("frustrated animation variants are stable and spread across IDs", () => {
  const ids = Array.from({ length: 120 }, (_, index) => `run-${index}`);
  const variants = ids.map((id) => activityAnimationPose("frustrated", id));

  assert.deepEqual(
    ids.map((id) => activityAnimationPose("frustrated", id)),
    variants,
  );
  assert.deepEqual(
    new Set(variants),
    new Set([
      "laptop-slump",
      "server-bite",
      "rocket-boot-misfire",
      "kaiju-server-stomp",
      "wrench-fumble",
      "button-panic",
      "chart-plunge",
      "play-dead",
      "tombstone-peek",
      "trash-can-hide",
      "lever-regret",
      "anvil-bonk",
      "cable-cocoon",
      "nuclear-vaporize",
      "lightning-vaporize",
      "too-many-arms",
      "trapdoor-deploy",
      "steamroller-flat",
      "under-desk-drag",
      "fire-extinguisher-blast",
      "parachute-collapse",
    ]),
  );
});

test("the phone animation maps to its descriptive pose", () => {
  assert.equal(activityAnimationPose("phone", "run-1"), "phone-scroll");
});

test("queued Runs use a waiting pose and executing Runs use a working pose", () => {
  for (const phase of ["queued", "assigned", "preparing", "running"]) {
    assert.equal(
      runActivity({ observationState: "current", phase }).animation,
      phase === "queued" ? "waiting" : "working",
      phase,
    );
  }

  assert.equal(runActivity({ phase: "finished" }), undefined);
});

test("terminal Runs distinguish successful, failed, timed-out, and canceled outcomes", () => {
  const outcomes = [
    {
      outcome: "succeeded",
      animation: "celebrating",
      tone: "success",
      title: "Run succeeded",
    },
    {
      outcome: "failed",
      animation: "frustrated",
      tone: "error",
      title: "Run failed",
      description: "The action failed. Details are in the logs below.",
    },
    {
      outcome: "timed-out",
      animation: "frustrated",
      tone: "error",
      title: "Run timed out",
      description: "The action exceeded its time limit.",
    },
    {
      outcome: "canceled",
      animation: "phone",
      tone: "neutral",
      title: "Run canceled",
      description: "Your run was canceled.",
    },
  ];

  for (const { outcome, ...expected } of outcomes) {
    const { result, ...activity } = runResultActivity({ phase: "finished", outcome });
    assert.deepEqual(activity, expected, outcome);
    assert.deepEqual(
      result,
      outcome === "succeeded" ? undefined : { reason: undefined, exitCode: undefined },
      outcome,
    );
  }
});

test("successful Runs only show a success label", () => {
  assert.deepEqual(
    runResultActivity({
      phase: "finished",
      outcome: "succeeded",
      outcomeReason: "ProcessCompleted",
      outcomeMessage: "The process completed with detailed output.",
      exitCode: 0,
    }),
    { animation: "celebrating", tone: "success", title: "Run succeeded" },
  );
});

test("unsuccessful terminal outcomes preserve their original diagnostics and zero exit codes", () => {
  const outcomeReason = "  ExactReason  ";
  const outcomeMessage = "Original diagnostic.\n  Preserve its formatting.";

  for (const outcome of ["failed", "timed-out", "canceled"]) {
    const activity = runResultActivity({
      phase: "finished",
      outcome,
      outcomeReason,
      outcomeMessage,
      exitCode: 0,
    });

    assert.equal(activity.description, outcomeMessage, outcome);
    assert.deepEqual(activity.result, { reason: outcomeReason, exitCode: 0 }, outcome);
  }
});

test("terminal diagnostics retain nonzero exit codes and explicit empty messages", () => {
  const activity = runResultActivity({
    phase: "finished",
    outcome: "failed",
    outcomeMessage: "",
    exitCode: 137,
  });

  assert.equal(activity.description, "");
  assert.deepEqual(activity.result, { reason: undefined, exitCode: 137 });
});

test("recorded terminal outcomes take precedence over stale observations and cancellation intent", () => {
  for (const outcome of ["succeeded", "failed", "timed-out", "canceled"]) {
    assert.deepEqual(
      runResultActivity({
        phase: "finished",
        outcome,
        observationState: "unknown",
        cancellationRequestedAt: "2026-10-24T00:00:00Z",
      }),
      runResultActivity({ phase: "finished", outcome }),
      outcome,
    );
  }
});

test("finished Runs without an outcome do not imply success or failure", () => {
  assert.equal(
    runResultActivity({
      phase: "finished",
      outcomeReason: "UnknownResult",
      outcomeMessage: "The terminal outcome is unavailable.",
      exitCode: 1,
    }),
    undefined,
  );
});

test("terminal outcomes never add activity mascots to Run lists", () => {
  for (const outcome of ["succeeded", "failed", "timed-out", "canceled"]) {
    assert.equal(runActivity({ phase: "finished", outcome }), undefined, outcome);
  }
});

test("unfinished Runs never show a terminal mascot even with outcome data", () => {
  for (const phase of ["queued", "assigned", "preparing", "running"]) {
    assert.equal(
      runResultActivity({
        phase,
        outcome: "succeeded",
        outcomeReason: "UnexpectedData",
        exitCode: 0,
      }),
      undefined,
      phase,
    );
  }
});

test("queued activity distinguishes assignment from review waits", () => {
  const assignment = runActivity({ phase: "queued", review: { state: "not-required" } });
  const review = runActivity({ phase: "queued", review: { state: "pending" } });

  assert.match(assignment.title, /waiting/i);
  assert.match(assignment.description, /queued and waiting/i);
  assert.deepEqual(review, {
    animation: "waiting",
    title: "Waiting for approval",
    description:
      "This Git-triggered Run needs an operator's approval before it can be assigned to a Controller.",
  });
});

test("unknown observations never claim current execution activity", () => {
  for (const phase of ["queued", "assigned", "preparing", "running", "finished"]) {
    assert.equal(runActivity({ observationState: "unknown", phase }), undefined, phase);
  }
});

test("cancellation intent shows stopping until the Run finishes", () => {
  const cancellationRequestedAt = "2026-10-24T00:00:00Z";

  for (const phase of ["queued", "assigned", "preparing", "running"]) {
    const activity = runActivity({ cancellationRequestedAt, phase });
    assert.equal(activity.title, "Stopping your run", phase);
    assert.equal(activity.animation, phase === "queued" ? "waiting" : "working", phase);
  }
  assert.equal(runActivity({ cancellationRequestedAt, phase: "finished" }), undefined);
});

test("only creating and preparing Promotions show activity", () => {
  assert.equal(promotionActivity({ status: "creating" }).animation, "waiting");
  assert.equal(promotionActivity({ status: "preparing" }).animation, "working");

  for (const status of ["pending", "approved", "rejected", "expired", "error"]) {
    assert.equal(promotionActivity({ status }), undefined, status);
  }
});

test("Promotion details refresh until pending diffs are loaded", () => {
  for (const status of ["creating", "preparing"]) {
    assert.equal(promotionDetailNeedsRefresh({ status }, false), true, status);
    assert.equal(promotionDetailNeedsRefresh({ status }, true), true, status);
  }

  assert.equal(promotionDetailNeedsRefresh({ status: "pending" }, false), true);
  assert.equal(promotionDetailNeedsRefresh({ status: "pending" }, true), false);

  for (const status of ["approved", "rejected", "expired", "error"]) {
    assert.equal(promotionDetailNeedsRefresh({ status }, false), false, status);
  }
});

test("Promotion details give every lifecycle status a fitting mascot", () => {
  const expected = {
    creating: { animation: "waiting", title: "Making a little momentum" },
    preparing: { animation: "working", title: "Good things are in motion" },
    pending: { animation: "waiting", title: "Ready when you are" },
    approved: { animation: "celebrating", tone: "success", title: "Promotion approved" },
    rejected: { animation: "frustrated", tone: "error", title: "Promotion rejected" },
    expired: { animation: "phone", tone: "neutral", title: "Review window expired" },
    error: { animation: "frustrated", tone: "error", title: "Promotion failed" },
  };

  for (const [status, activity] of Object.entries(expected)) {
    assert.deepEqual(
      {
        animation: promotionDetailActivity({ status }).animation,
        tone: promotionDetailActivity({ status }).tone,
        title: promotionDetailActivity({ status }).title,
      },
      { tone: undefined, ...activity },
      status,
    );
  }
});

test("Promotion failure and expiry mascots preserve terminal diagnostics", () => {
  for (const status of ["expired", "error"]) {
    assert.equal(
      promotionDetailActivity({ message: "Original Promotion diagnostic.", status }).description,
      "Original Promotion diagnostic.",
      status,
    );
  }
});
