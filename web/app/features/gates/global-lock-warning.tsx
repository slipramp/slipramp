import { Alert, Anchor, Text } from "@mantine/core";
import { IconLock } from "@tabler/icons-react";
import { useEffect } from "react";
import { Link, useFetcher } from "react-router";

import { Timestamp } from "~/shared/display-values";
import { gateRefreshInterval } from "./gate-policy";
import type { loadGlobalLockStatus } from "./gate-policy.server";
import type { loader } from "./routes/global-lock-status";

export function GlobalLockWarning({
  initial,
}: Readonly<{ initial: Awaited<ReturnType<typeof loadGlobalLockStatus>> }>) {
  const { data, load, state } = useFetcher<typeof loader>();
  const status = data && data.readAt > initial.readAt ? data : initial;
  useEffect(() => {
    if (state !== "idle") return;
    const delay = gateRefreshInterval(
      status.checkedAt ? { checkedAt: status.checkedAt, lock: status.lock } : undefined,
    );
    const refresh = () => {
      if (document.visibilityState === "visible") void load("/gates/lock-status");
    };
    const timer = window.setTimeout(refresh, delay);
    document.addEventListener("visibilitychange", refresh);
    return () => {
      window.clearTimeout(timer);
      document.removeEventListener("visibilitychange", refresh);
    };
  }, [load, state, status]);

  if (status.unavailable) {
    return <Alert mb="md" color="yellow" title="Global lock status unavailable" />;
  }
  const lock = status.lock;
  if (!lock) return null;
  return (
    <Alert
      mb="md"
      color="yellow"
      icon={<IconLock aria-hidden />}
      title="Global release lock active"
    >
      <Text size="sm" style={{ overflowWrap: "anywhere" }}>
        {lock.reason}
      </Text>
      <Text size="sm">
        Locked by {lock.actor.displayName} ·{" "}
        {lock.expiresAt ? (
          <>
            Expires <Timestamp value={lock.expiresAt} showSeconds />
          </>
        ) : (
          "Until released"
        )}
      </Text>
      <Anchor component={Link} to="/gates" size="sm">
        View global Gates
      </Anchor>
    </Alert>
  );
}
