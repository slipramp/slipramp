import { Alert, Anchor, Group, Text } from "@mantine/core";
import { IconArrowRight, IconLock } from "@tabler/icons-react";
import { useEffect } from "react";
import { Link, useFetcher } from "react-router";

import { RelativeTime } from "~/shared/display-values";
import { gateLockStatusPath, gateRefreshInterval, type GateOwner } from "./gate-policy";
import type { loadLockStatus } from "./gate-policy.server";
import { lockAlertColor } from "./gate-presentation";
import type { loader } from "./routes/lock-status";
import classes from "./lock-warning.module.css";

export function LockWarning({
  initial,
  owner,
  gatesPath,
}: Readonly<{
  initial: Awaited<ReturnType<typeof loadLockStatus>>;
  owner: GateOwner;
  gatesPath: string;
}>) {
  const { data, load, state } = useFetcher<typeof loader>();
  const statusPath = gateLockStatusPath(owner);
  const status = data && data.readAt > initial.readAt ? data : initial;
  useEffect(() => {
    if (state !== "idle") return;
    const delay = gateRefreshInterval(
      status.checkedAt ? { checkedAt: status.checkedAt, lock: status.lock } : undefined,
    );
    const refresh = () => {
      if (document.visibilityState === "visible") void load(statusPath);
    };
    const timer = window.setTimeout(refresh, delay);
    document.addEventListener("visibilitychange", refresh);
    return () => {
      window.clearTimeout(timer);
      document.removeEventListener("visibilitychange", refresh);
    };
  }, [load, state, status, statusPath]);

  const label = { global: "Global", target: "Target", stage: "Stage" }[owner.kind];
  const color = lockAlertColor(owner.kind);
  if (status.unavailable) {
    return (
      <Alert
        mb={owner.kind === "global" ? "md" : 0}
        color={color}
        title={`${label} lock status unavailable`}
      />
    );
  }
  const lock = status.lock;
  if (!lock) return null;
  const detail = [lock.reason, `Locked by ${lock.actor.displayName}`].filter(Boolean).join(" · ");
  return (
    <Alert
      color={color}
      mb={owner.kind === "global" ? "xs" : 0}
      px="sm"
      py={6}
      classNames={{ message: classes.message }}
    >
      <div className={classes.content}>
        <Group gap={6} wrap="nowrap" className={classes.heading}>
          <IconLock aria-hidden size={15} />
          <Text fw={700} size="sm">
            {label} lock active
          </Text>
        </Group>
        <Text className={classes.detail} size="xs" truncate title={detail}>
          {detail}
        </Text>
        <Text className={classes.expiry} size="xs">
          {lock.expiresAt ? (
            <>
              Expires <RelativeTime value={lock.expiresAt} inherit />
            </>
          ) : (
            "Until released"
          )}
        </Text>
        <Anchor
          className={classes.link}
          component={Link}
          to={gatesPath}
          aria-label={`View ${owner.kind} Gates`}
          size="xs"
          fw={600}
          c="inherit"
        >
          Gates <IconArrowRight aria-hidden size={13} />
        </Anchor>
      </div>
    </Alert>
  );
}
