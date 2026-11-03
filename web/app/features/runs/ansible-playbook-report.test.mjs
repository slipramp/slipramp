import assert from "node:assert/strict";
import test from "node:test";

import {
  ansibleSummaryBadges,
  countAnsibleUnchangedResults,
  filterAnsiblePlays,
  paginateAnsiblePlays,
} from "./ansible-playbook-report.ts";

function result(order, host, outcome, overrides = {}) {
  return {
    host,
    ignoredFailure: false,
    loopItem: false,
    order,
    outcome,
    ...overrides,
  };
}

function task(order, name, action, results, overrides = {}) {
  return {
    action,
    id: `task-${order}`,
    name,
    order,
    redacted: false,
    results,
    ...overrides,
  };
}

const plays = [
  {
    id: "play-web",
    name: "Configure web hosts",
    order: 1,
    tasks: [
      task(2, "Install package", "ansible.builtin.package", [result(3, "web-1", "changed")], {
        sourcePath: "roles/web/tasks/main.yml:4",
      }),
      task(4, "Check service", "ansible.builtin.service", [result(5, "web-2", "ok")]),
      task(6, "Wait for endpoint", "ansible.builtin.uri", [
        result(7, "web-1", "failed", { ignoredFailure: true }),
        result(8, "web-2", "skipped"),
      ]),
    ],
  },
  {
    id: "play-db",
    name: "Configure database",
    order: 9,
    tasks: [
      task(10, "Reach database", "ansible.builtin.ping", [result(11, "db-1", "unreachable")]),
      task(12, "Started before cancellation", "ansible.builtin.command", []),
    ],
  },
];

test("builds recap badges from host totals rather than loop and rescued result events", () => {
  const badges = ansibleSummaryBadges({
    complete: true,
    hostTotals: [
      {
        changed: 1,
        failed: 0,
        host: "web-1",
        ignored: 1,
        ok: 2,
        rescued: 1,
        skipped: 1,
        unreachable: 0,
      },
      {
        changed: 0,
        failed: 0,
        host: "db-1",
        ignored: 0,
        ok: 0,
        rescued: 2,
        skipped: 0,
        unreachable: 1,
      },
    ],
    hosts: 2,
    plays: 2,
    results: { changed: 3, failed: 2, ok: 2, skipped: 1, total: 9, unreachable: 1 },
    tasks: 5,
  });

  assert.deepEqual(
    badges.map(({ count, label }) => ({ count, label })),
    [
      { count: 1, label: "changed" },
      { count: 1, label: "unreachable" },
      { count: 1, label: "skipped" },
      { count: 3, label: "rescued" },
      { count: 1, label: "ignored" },
    ],
  );
});

test("filters Ansible detail by play, task, action, host, and outcome", () => {
  const options = { outcomes: [], showUnchanged: true };

  assert.deepEqual(
    filterAnsiblePlays(plays, { ...options, keyword: "configure web" }).map((play) => play.name),
    ["Configure web hosts"],
  );
  assert.equal(
    filterAnsiblePlays(plays, { ...options, keyword: "wait endpoint" })[0].tasks[0].name,
    "Wait for endpoint",
  );
  assert.equal(
    filterAnsiblePlays(plays, { ...options, keyword: "builtin.package" })[0].tasks[0].name,
    "Install package",
  );
  assert.deepEqual(
    filterAnsiblePlays(plays, { ...options, keyword: "web-2" })[0].tasks.flatMap((entry) =>
      entry.results.map((entryResult) => entryResult.outcome),
    ),
    ["ok", "skipped"],
  );
  assert.deepEqual(
    filterAnsiblePlays(plays, {
      keyword: "",
      outcomes: ["failed", "unreachable"],
      showUnchanged: false,
    }).flatMap((play) => play.tasks.map((entry) => entry.name)),
    ["Wait for endpoint", "Reach database"],
  );
  assert.deepEqual(
    filterAnsiblePlays(plays, {
      keyword: "",
      outcomes: ["ok"],
      showUnchanged: false,
    }).flatMap((play) => play.tasks.map((entry) => entry.name)),
    ["Check service"],
  );
});

test("hides unchanged detail by default while retaining partial and redacted tasks", () => {
  const redacted = task(13, "[redacted]", "[redacted]", [result(14, "db-1", "ok")], {
    redacted: true,
  });
  const withRedaction = [{ ...plays[1], tasks: [...plays[1].tasks, redacted] }];
  const filtered = filterAnsiblePlays(withRedaction, {
    keyword: "",
    outcomes: [],
    showUnchanged: false,
  });

  assert.deepEqual(
    filtered[0].tasks.map((entry) => entry.name),
    ["Reach database", "Started before cancellation", "[redacted]"],
  );
  assert.equal(countAnsibleUnchangedResults(withRedaction), 0);
});

test("paginates tasks while preserving play grouping and event order", () => {
  const page = paginateAnsiblePlays(plays, 2, 3);

  assert.equal(page.page, 2);
  assert.equal(page.totalPages, 2);
  assert.equal(page.totalTasks, 5);
  assert.deepEqual(
    page.plays.map((play) => ({
      name: play.name,
      tasks: play.tasks.map((entry) => entry.name),
    })),
    [{ name: "Configure database", tasks: ["Reach database", "Started before cancellation"] }],
  );
});
