import type {
  ProductPrincipal,
  ServerGateLockRequest,
  ServerGateScope,
  WeeklyWindow,
} from "~/api/generated";
import { formValue } from "../../shared/form-data.ts";

export type GateOwner =
  | { kind: "global" }
  | { kind: "target"; targetId: string }
  | { kind: "stage"; stageId: string };

export function gateLockStatusPath(owner: GateOwner) {
  const base = "/gates/lock-status";
  switch (owner.kind) {
    case "global":
      return base;
    case "target":
      return `${base}/target/${encodeURIComponent(owner.targetId)}`;
    case "stage":
      return `${base}/stage/${encodeURIComponent(owner.stageId)}`;
  }
}

export function canManageGatePolicy(
  role: ProductPrincipal["role"] | undefined,
  kind: GateOwner["kind"],
) {
  return role === "admin" || (role === "operator" && kind !== "global");
}

export type GateChange =
  | { action: "lock"; body: ServerGateLockRequest }
  | { action: "unlock" }
  | { action: "set-window"; body: WeeklyWindow }
  | { action: "remove-window" };

export function selectedGateEditor(
  params: URLSearchParams,
  canManage: boolean,
): GateChange["action"] | undefined {
  if (!canManage) return undefined;
  const action = params.get("editGate");
  if (
    action === "lock" ||
    action === "unlock" ||
    action === "set-window" ||
    action === "remove-window"
  )
    return action;
  return undefined;
}

export function gateEditorParams(current: URLSearchParams, action?: GateChange["action"]) {
  const next = new URLSearchParams(current);
  if (action) next.set("editGate", action);
  else next.delete("editGate");
  return next;
}

export const weekdays = [
  "monday",
  "tuesday",
  "wednesday",
  "thursday",
  "friday",
  "saturday",
  "sunday",
] as const;

export const lockExpiryHours = { "1 hour": 1, "1 day": 24, "1 week": 7 * 24 };

export function lockExpiryUTC(hours: number, now = Date.now()) {
  return new Date(now + hours * 3_600_000).toISOString().slice(0, 16);
}

export function readGateChange(form: FormData): GateChange | { error: string } {
  const action = form.get("gateAction");
  if (action === "unlock" || action === "remove-window") return { action };
  if (action === "lock") {
    const reason = formValue(form, "reason");
    const expiry = form.get("expiryUTC");
    const body: ServerGateLockRequest = {};
    if (reason) body.reason = reason;
    if (expiry) {
      if (typeof expiry !== "string" || !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/.test(expiry)) {
        return { error: "Enter a valid expiry in UTC." };
      }
      const date = new Date(expiry + ":00Z");
      if (!Number.isFinite(date.getTime()) || date.toISOString().slice(0, 16) !== expiry) {
        return { error: "Enter a valid expiry in UTC." };
      }
      body.expiresAt = date.toISOString();
    }
    return { action, body };
  }
  if (action !== "set-window") return { error: "Unknown Gate policy change." };
  const zone = form.get("timeZone");
  if (typeof zone !== "string" || !zone.trim()) return { error: "Enter an IANA time zone." };
  const intervals = form.getAll("interval");
  if (intervals.length === 0 || intervals.length > 128)
    return { error: "Enter between 1 and 128 weekly intervals." };
  const weekly: WeeklyWindow["weekly"] = [];
  for (const id of intervals) {
    const days = form.getAll("days-" + id);
    const start = form.get("start-" + id);
    const end = form.get("end-" + id);
    if (
      !days.length ||
      !days.every((day): day is (typeof weekdays)[number] => weekdays.some((d) => d === day))
    ) {
      return { error: "Select weekdays for every interval." };
    }
    const clock = /^([01]\d|2[0-3]):[0-5]\d$/;
    if (
      typeof start !== "string" ||
      typeof end !== "string" ||
      !clock.test(start) ||
      !(clock.test(end) || end === "24:00") ||
      start >= end
    ) {
      return { error: "Use increasing HH:MM times. Split overnight intervals at midnight." };
    }
    weekly.push({ days, start, end });
  }
  return { action, body: { timeZone: zone.trim(), weekly } };
}

type GateTiming = Pick<ServerGateScope, "checkedAt" | "lock" | "window">;

export function gateRefreshInterval(...policies: (GateTiming | undefined)[]) {
  let delay = 30_000;
  for (const policy of policies) {
    if (!policy) continue;
    const at = Date.parse(policy.checkedAt);
    for (const bound of [
      policy.lock?.expiresAt,
      policy.window?.observation.nextTransition,
      policy.window?.observation.validUntil,
    ]) {
      if (!bound) continue;
      const remaining = Date.parse(bound) - at;
      if (remaining > 0) delay = Math.min(delay, remaining);
    }
  }
  return Math.max(1_000, Math.ceil(delay / 1_000) * 1_000);
}
