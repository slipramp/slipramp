import assert from "node:assert/strict";
import test from "node:test";
import {
  canManageGatePolicy,
  gateEditorParams,
  gateLockStatusPath,
  gateRefreshInterval,
  lockExpiryHours,
  lockExpiryUTC,
  readGateChange,
  selectedGateEditor,
} from "./gate-policy.ts";

test("lock status reads use the selected scope and owner", () => {
  assert.equal(gateLockStatusPath({ kind: "global" }), "/gates/lock-status");
  assert.equal(
    gateLockStatusPath({ kind: "target", targetId: "a/b" }),
    "/gates/lock-status/target/a%2Fb",
  );
  assert.equal(
    gateLockStatusPath({ kind: "stage", stageId: "a/b" }),
    "/gates/lock-status/stage/a%2Fb",
  );
});

test("administrators manage all scopes and operators manage local policies", () => {
  for (const kind of ["global", "target", "stage"]) {
    assert.equal(canManageGatePolicy("admin", kind), true);
    assert.equal(canManageGatePolicy("operator", kind), kind !== "global");
    assert.equal(canManageGatePolicy("viewer", kind), false);
    assert.equal(canManageGatePolicy(undefined, kind), false);
  }
});

test("Gate editor navigation preserves other page state and normalizes invalid or duplicate selections", () => {
  const current = new URLSearchParams("view=details&editGate=bad&editGate=lock");
  assert.equal(selectedGateEditor(current, true), undefined);
  for (const intent of ["lock", "unlock", "set-window", "remove-window"]) {
    const next = gateEditorParams(current, intent);
    assert.deepEqual(next.getAll("editGate"), [intent]);
    assert.equal(selectedGateEditor(next, true), intent);
    assert.equal(selectedGateEditor(next, false), undefined);
    assert.equal(gateEditorParams(next).toString(), "view=details");
  }
});

function form(action, fields = []) {
  const result = new FormData();
  result.set("gateAction", action);
  for (const [key, value] of fields) result.append(key, value);
  return result;
}

test("locks support explicit UTC expiry and indefinite holds", () => {
  const lock = form("lock", [["reason", " Maintenance "]]);
  assert.deepEqual(readGateChange(lock), { action: "lock", body: { reason: "Maintenance" } });
  lock.set("expiryUTC", "2027-03-14T02:30");
  assert.equal(readGateChange(lock).body.expiresAt, "2027-03-14T02:30:00.000Z");
  lock.set("expiryUTC", "2027-02-30T12:00");
  assert.ok(readGateChange(lock).error);
  for (const fields of [[], [["reason", ""]], [["reason", "  "]]]) {
    assert.deepEqual(readGateChange(form("lock", fields)), { action: "lock", body: {} });
  }
  assert.deepEqual(readGateChange(form("unlock")), { action: "unlock" });
});

test("expiry shortcuts fill an editable UTC date across calendar boundaries", () => {
  const now = Date.parse("2026-12-31T23:30:00Z");
  const expected = {
    "1 hour": "2027-01-01T00:30",
    "1 day": "2027-01-01T23:30",
    "1 week": "2027-01-07T23:30",
  };
  for (const [label, expiry] of Object.entries(expected)) {
    const value = lockExpiryUTC(lockExpiryHours[label], now);
    assert.equal(value, expiry);
    assert.deepEqual(readGateChange(form("lock", [["expiryUTC", value]])), {
      action: "lock",
      body: { expiresAt: expiry + ":00.000Z" },
    });
  }
});

test("window forms preserve multiple weekdays, split overnight intervals, and independent row IDs", () => {
  const window = form("set-window", [
    ["timeZone", "America/Denver"],
    ["interval", "2"],
    ["days-2", "monday"],
    ["days-2", "tuesday"],
    ["start-2", "23:15"],
    ["end-2", "24:00"],
    ["interval", "8"],
    ["days-8", "wednesday"],
    ["start-8", "00:00"],
    ["end-8", "02:00"],
  ]);
  assert.deepEqual(readGateChange(window), {
    action: "set-window",
    body: {
      timeZone: "America/Denver",
      weekly: [
        { days: ["monday", "tuesday"], start: "23:15", end: "24:00" },
        { days: ["wednesday"], start: "00:00", end: "02:00" },
      ],
    },
  });
  window.set("end-2", "00:00");
  assert.ok(readGateChange(window).error);
  window.set("end-2", "24:00");
  window.delete("days-8");
  assert.ok(readGateChange(window).error);
  assert.deepEqual(readGateChange(form("remove-window")), { action: "remove-window" });
  assert.ok(readGateChange(form("unknown")).error);
});

test("scope refresh follows the nearest future lock expiry, transition or observation expiry", () => {
  const checkedAt = "2027-01-01T00:00:00.000Z";
  const global = { checkedAt, lock: { expiresAt: "2027-01-01T00:00:12.000Z" } };
  const local = {
    checkedAt,
    window: {
      observation: {
        validUntil: "2027-01-01T00:00:05.500Z",
        nextTransition: "2027-01-01T01:00:00.000Z",
      },
    },
  };
  assert.equal(gateRefreshInterval(undefined, global, local), 6000);
  assert.equal(gateRefreshInterval(global), 12000);
  assert.equal(gateRefreshInterval({ checkedAt, lock: { expiresAt: checkedAt } }), 30000);
});
