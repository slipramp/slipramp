import { ActionIcon, Tooltip } from "@mantine/core";
import { IconPlayerPause, IconRefresh } from "@tabler/icons-react";
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useId,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { useRevalidator } from "react-router";

const defaultRefreshIntervalMs = 3000;

type RefreshRegistration = {
  active: boolean;
  intervalMs: number;
};

type ActivityRefreshContextValue = {
  activeIntervalMs?: number;
  enabled: boolean;
  register: (id: string, registration?: RefreshRegistration) => void;
  setEnabled: (enabled: boolean) => void;
};

const ActivityRefreshContext = createContext<ActivityRefreshContextValue | undefined>(undefined);

export function ActivityRefreshProvider({ children }: Readonly<{ children: ReactNode }>) {
  const [enabled, setEnabledState] = useState(true);
  const [registrations, setRegistrations] = useState<Map<string, RefreshRegistration>>(new Map());

  const register = useCallback((id: string, registration?: RefreshRegistration) => {
    setRegistrations((current) => {
      const next = new Map(current);
      if (registration) next.set(id, registration);
      else next.delete(id);
      return next;
    });
  }, []);

  const setEnabled = useCallback((nextEnabled: boolean) => {
    setEnabledState(nextEnabled);
  }, []);

  const activeIntervalMs = [...registrations.values()]
    .filter((registration) => registration.active)
    .reduce<number | undefined>(
      (shortest, registration) =>
        shortest === undefined
          ? registration.intervalMs
          : Math.min(shortest, registration.intervalMs),
      undefined,
    );
  const value = useMemo(
    () => ({ activeIntervalMs, enabled, register, setEnabled }),
    [activeIntervalMs, enabled, register, setEnabled],
  );

  return (
    <ActivityRefreshContext.Provider value={value}>{children}</ActivityRefreshContext.Provider>
  );
}

export function useActivityRefresh(active: boolean, intervalMs = defaultRefreshIntervalMs) {
  const id = useId();
  const refresh = useContext(ActivityRefreshContext);
  const revalidator = useRevalidator();

  if (!refresh) throw new Error("useActivityRefresh requires ActivityRefreshProvider");
  const { enabled, register } = refresh;

  useEffect(() => {
    register(id, { active, intervalMs });
    return () => register(id);
  }, [active, id, intervalMs, register]);

  useEffect(() => {
    if (!active || !enabled) return;

    const interval = window.setInterval(() => {
      if (document.visibilityState === "visible" && revalidator.state === "idle") {
        void revalidator.revalidate();
      }
    }, intervalMs);

    return () => window.clearInterval(interval);
  }, [active, enabled, intervalMs, revalidator]);
}

export function ActivityRefreshControl() {
  const refresh = useContext(ActivityRefreshContext);
  if (!refresh) throw new Error("ActivityRefreshControl requires ActivityRefreshProvider");

  const active = refresh.activeIntervalMs !== undefined;
  const interval =
    refresh.activeIntervalMs === undefined
      ? undefined
      : formatRefreshInterval(refresh.activeIntervalMs);
  const description = !active
    ? "This page does not refresh automatically."
    : refresh.enabled
      ? `This page refreshes every ${interval} while visible. Select to pause.`
      : `Auto-refresh is paused. This page normally refreshes every ${interval}. Select to resume.`;

  return (
    <Tooltip
      label={description}
      position="bottom-end"
      events={{ hover: true, focus: true, touch: false }}
      withArrow
    >
      <ActionIcon
        aria-label="Auto-refresh"
        aria-description={description}
        aria-pressed={active && refresh.enabled}
        disabled={!active}
        onClick={() => refresh.setEnabled(!refresh.enabled)}
        size="lg"
        variant="light"
      >
        {active && refresh.enabled ? (
          <IconPlayerPause aria-hidden size={19} stroke={1.8} />
        ) : (
          <IconRefresh aria-hidden size={19} stroke={1.8} />
        )}
      </ActionIcon>
    </Tooltip>
  );
}

function formatRefreshInterval(intervalMs: number) {
  if (intervalMs % 1000 === 0) return `${intervalMs / 1000}s`;
  return `${intervalMs}ms`;
}
