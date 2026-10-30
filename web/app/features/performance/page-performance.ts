import type { ClientInstrumentation } from "react-router";

export type ServerTimingSource = {
  description: string;
  duration: number;
  name: string;
};

export type ServerPerformanceMetric = {
  description: string;
  durationMs: number;
  name: string;
  status?: number | "error";
};

export type BrowserPerformanceMetric = {
  durationMs: number;
  label: string;
  name: string;
};

export type PagePerformanceSnapshot = {
  browserMetrics: BrowserPerformanceMetric[];
  contextLabel: string;
  headline: string;
  kind: "document" | "navigation";
  overallMs: number;
  serverMetrics: ServerPerformanceMetric[];
};

export type DocumentPerformanceInput = {
  domContentLoadedMs?: number;
  firstContentfulPaintMs?: number;
  hydrationEndedAtMs: number;
  hydrationStartedAtMs: number;
  largestContentfulPaintMs?: number;
  loadEventEndMs?: number;
  responseEndMs?: number;
  responseStartMs?: number;
  serverTiming?: readonly ServerTimingSource[];
};

export type NavigationResourceTiming = {
  durationMs: number;
  serverTiming: readonly ServerTimingSource[];
};

export type NavigationPerformanceInput = {
  paintedAtMs: number;
  pattern?: string;
  resourceTimings?: readonly NavigationResourceTiming[];
  routerEndedAtMs: number;
  startedAtMs: number;
};

type ActiveNavigation = {
  id: number;
  paintedAtMs?: number;
  paintScheduled: boolean;
  pattern?: string;
  routerEndedAtMs?: number;
  startedAtMs: number;
};

let currentSnapshot: PagePerformanceSnapshot | undefined;
const listeners = new Set<() => void>();

export const pagePerformanceStore = {
  getServerSnapshot: () => undefined,
  getSnapshot: () => currentSnapshot,
  subscribe(listener: () => void) {
    listeners.add(listener);
    return () => {
      listeners.delete(listener);
    };
  },
};

let initialized = false;
let hydrationStartedAtMs = 0;
let hydrationEndedAtMs: number | undefined;
let initialCommitScheduled = false;
let initialMeasurementOpen = true;
let initialSnapshotPublished = false;
let initialRefreshScheduled = false;
let largestContentfulPaintMs: number | undefined;
let largestContentfulPaintObserver: PerformanceObserver | undefined;
let activeNavigation: ActiveNavigation | undefined;
let nextNavigationID = 1;

export function serverPerformanceMetrics(
  sources: readonly ServerTimingSource[] = [],
): ServerPerformanceMetric[] {
  return sources
    .filter((source) => Number.isFinite(source.duration) && source.duration >= 0)
    .map((source) => {
      const statusMatch = source.description.match(/ \[(error|\d{3})\]$/);
      const statusValue = statusMatch?.[1];

      return {
        description: statusMatch
          ? source.description.slice(0, statusMatch.index).trim()
          : source.description,
        durationMs: source.duration,
        name: source.name,
        status: statusValue === "error" ? "error" : statusValue ? Number(statusValue) : undefined,
      };
    });
}

export function buildDocumentPerformance(input: DocumentPerformanceInput): PagePerformanceSnapshot {
  const browserMetrics: BrowserPerformanceMetric[] = [];
  const responseStartMs = positiveValue(input.responseStartMs);
  const responseEndMs = positiveValue(input.responseEndMs);
  const domContentLoadedMs = positiveValue(input.domContentLoadedMs);
  const loadEventEndMs = positiveValue(input.loadEventEndMs);
  const firstContentfulPaintMs = positiveValue(input.firstContentfulPaintMs);
  const largestPaintMs = positiveValue(input.largestContentfulPaintMs);

  addBrowserMetric(browserMetrics, "ttfb", "Time to first byte", responseStartMs);
  addBrowserMetric(
    browserMetrics,
    "response",
    "Response transfer",
    responseStartMs === undefined || responseEndMs === undefined
      ? undefined
      : responseEndMs - responseStartMs,
  );
  addBrowserMetric(browserMetrics, "dom-ready", "DOM content loaded", domContentLoadedMs);
  addBrowserMetric(browserMetrics, "load", "Window load", loadEventEndMs);
  addBrowserMetric(browserMetrics, "fcp", "First contentful paint", firstContentfulPaintMs);
  addBrowserMetric(browserMetrics, "lcp", "Largest contentful paint", largestPaintMs);
  addBrowserMetric(
    browserMetrics,
    "hydration",
    "React hydration",
    input.hydrationEndedAtMs - input.hydrationStartedAtMs,
  );

  return {
    browserMetrics,
    contextLabel: "Initial SSR load",
    headline: "Page loaded",
    kind: "document",
    overallMs: Math.max(
      0,
      loadEventEndMs ?? 0,
      domContentLoadedMs ?? 0,
      responseEndMs ?? 0,
      firstContentfulPaintMs ?? 0,
      largestPaintMs ?? 0,
      input.hydrationEndedAtMs,
    ),
    serverMetrics: serverPerformanceMetrics(input.serverTiming),
  };
}

export function buildNavigationPerformance(
  input: NavigationPerformanceInput,
): PagePerformanceSnapshot {
  const browserMetrics: BrowserPerformanceMetric[] = [];
  const resources = input.resourceTimings ?? [];

  addBrowserMetric(
    browserMetrics,
    "router",
    "Router navigation",
    input.routerEndedAtMs - input.startedAtMs,
  );
  addBrowserMetric(
    browserMetrics,
    "render",
    "Render to next frame",
    input.paintedAtMs - input.routerEndedAtMs,
  );

  for (const [index, resource] of resources.entries()) {
    addBrowserMetric(
      browserMetrics,
      "route-data",
      resources.length === 1 ? "Route data request" : `Route data request ${index + 1}`,
      resource.durationMs,
    );
  }

  return {
    browserMetrics,
    contextLabel: input.pattern ? `Route ${input.pattern}` : "Client navigation",
    headline: "Page updated",
    kind: "navigation",
    overallMs: Math.max(0, input.paintedAtMs - input.startedAtMs),
    serverMetrics: resources.flatMap((resource) => serverPerformanceMetrics(resource.serverTiming)),
  };
}

export function formatPerformanceDuration(durationMs: number) {
  if (!Number.isFinite(durationMs) || durationMs < 0) return "—";
  if (durationMs < 10) return `${durationMs.toFixed(1)} ms`;
  if (durationMs < 1_000) return `${Math.round(durationMs)} ms`;
  if (durationMs < 10_000) return `${(durationMs / 1_000).toFixed(2)} s`;
  return `${(durationMs / 1_000).toFixed(1)} s`;
}

export function initializeClientPerformance() {
  if (initialized || typeof window === "undefined") return;

  initialized = true;
  hydrationStartedAtMs = performance.now();
  observeLargestContentfulPaint();
}

export function recordPageCommit(_locationKey: string) {
  if (typeof window === "undefined") return;

  if (activeNavigation) {
    scheduleNavigationPaint(activeNavigation);
    return;
  }

  if (initialSnapshotPublished || initialCommitScheduled) return;

  initialCommitScheduled = true;
  hydrationEndedAtMs = performance.now();
  requestAnimationFrame(() => {
    publishInitialSnapshot();
    scheduleInitialRefresh();
  });
}

export const clientPerformanceInstrumentation: ClientInstrumentation = {
  router(router) {
    router.instrument({
      navigate: async (callNavigate) => {
        initialMeasurementOpen = false;
        largestContentfulPaintObserver?.disconnect();

        const navigation: ActiveNavigation = {
          id: nextNavigationID,
          paintScheduled: false,
          startedAtMs: performance.now(),
        };
        nextNavigationID += 1;
        activeNavigation = navigation;

        const result = await callNavigate();
        if (activeNavigation?.id !== navigation.id) return;

        navigation.pattern = result.meta?.pattern;
        navigation.routerEndedAtMs = performance.now();
        if (
          navigation.paintedAtMs !== undefined &&
          navigation.paintedAtMs < navigation.routerEndedAtMs
        ) {
          navigation.paintedAtMs = undefined;
          navigation.paintScheduled = false;
        }
        finishNavigationIfReady(navigation);

        requestAnimationFrame(() => {
          requestAnimationFrame(() => scheduleNavigationPaint(navigation));
        });
      },
    });
  },
};

function addBrowserMetric(
  metrics: BrowserPerformanceMetric[],
  name: string,
  label: string,
  durationMs: number | undefined,
) {
  if (durationMs === undefined || !Number.isFinite(durationMs) || durationMs < 0) return;
  metrics.push({ durationMs, label, name });
}

function positiveValue(value: number | undefined) {
  return value !== undefined && Number.isFinite(value) && value > 0 ? value : undefined;
}

function publishSnapshot(snapshot: PagePerformanceSnapshot) {
  currentSnapshot = snapshot;
  for (const listener of listeners) listener();
}

function publishInitialSnapshot() {
  if (
    typeof window === "undefined" ||
    hydrationEndedAtMs === undefined ||
    !initialMeasurementOpen
  ) {
    return;
  }

  const navigation = performance.getEntriesByType("navigation")[0] as
    | PerformanceNavigationTiming
    | undefined;
  const firstContentfulPaint = performance.getEntriesByName("first-contentful-paint", "paint")[0];

  publishSnapshot(
    buildDocumentPerformance({
      domContentLoadedMs: navigation?.domContentLoadedEventEnd,
      firstContentfulPaintMs: firstContentfulPaint?.startTime,
      hydrationEndedAtMs,
      hydrationStartedAtMs,
      largestContentfulPaintMs,
      loadEventEndMs: navigation?.loadEventEnd,
      responseEndMs: navigation?.responseEnd,
      responseStartMs: navigation?.responseStart,
      serverTiming: navigation?.serverTiming,
    }),
  );
  initialSnapshotPublished = true;
}

function scheduleInitialRefresh() {
  if (initialRefreshScheduled || typeof window === "undefined") return;
  initialRefreshScheduled = true;

  if (document.readyState === "complete") {
    refreshInitialSnapshot();
  } else {
    window.addEventListener("load", refreshInitialSnapshot, { once: true });
  }
}

function refreshInitialSnapshot() {
  requestAnimationFrame(publishInitialSnapshot);
}

function observeLargestContentfulPaint() {
  if (typeof PerformanceObserver === "undefined") return;

  try {
    largestContentfulPaintObserver = new PerformanceObserver((list) => {
      for (const entry of list.getEntries()) {
        largestContentfulPaintMs = Math.max(largestContentfulPaintMs ?? 0, entry.startTime);
      }
      if (initialMeasurementOpen && initialSnapshotPublished) publishInitialSnapshot();
    });
    largestContentfulPaintObserver.observe({ buffered: true, type: "largest-contentful-paint" });
  } catch {
    largestContentfulPaintObserver = undefined;
  }
}

function scheduleNavigationPaint(navigation: ActiveNavigation) {
  if (
    navigation.paintScheduled ||
    navigation.paintedAtMs !== undefined ||
    activeNavigation?.id !== navigation.id
  ) {
    return;
  }

  navigation.paintScheduled = true;
  requestAnimationFrame(() => {
    if (activeNavigation?.id !== navigation.id) return;
    navigation.paintedAtMs = performance.now();
    finishNavigationIfReady(navigation);
  });
}

function finishNavigationIfReady(navigation: ActiveNavigation) {
  if (
    navigation.routerEndedAtMs === undefined ||
    navigation.paintedAtMs === undefined ||
    activeNavigation?.id !== navigation.id
  ) {
    return;
  }

  const resourceTimings = (performance.getEntriesByType("resource") as PerformanceResourceTiming[])
    .filter((entry) => entry.startTime >= navigation.startedAtMs && entry.serverTiming.length > 0)
    .map((entry) => ({
      durationMs: entry.duration,
      serverTiming: entry.serverTiming,
    }));

  publishSnapshot(
    buildNavigationPerformance({
      paintedAtMs: navigation.paintedAtMs,
      pattern: navigation.pattern,
      resourceTimings,
      routerEndedAtMs: navigation.routerEndedAtMs,
      startedAtMs: navigation.startedAtMs,
    }),
  );
  activeNavigation = undefined;
}
