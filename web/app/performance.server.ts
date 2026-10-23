import { performance } from "node:perf_hooks";

import {
  createContext,
  type MiddlewareFunction,
  type RouterContextProvider,
  type ServerInstrumentation,
} from "react-router";

const maximumServerTimingMetrics = 32;
const maximumServerTimingDescriptionLength = 120;

export type ServerPerformanceMetricName = "action" | "api" | "loader" | "server" | "ssr";

type ServerPerformanceMetric = {
  description: string;
  durationMs: number;
  name: ServerPerformanceMetricName;
  order: number;
  status?: number | "error";
};

type FinishedSpan = {
  finish: (result?: { status?: number | "error" }) => void;
};

export class ServerPerformanceTrace {
  readonly #metrics: ServerPerformanceMetric[] = [];
  #nextOrder = 0;

  start(name: ServerPerformanceMetricName, description: string): FinishedSpan {
    const order = this.#nextOrder;
    const startedAt = performance.now();
    let finished = false;
    this.#nextOrder += 1;

    return {
      finish: (result) => {
        if (finished) return;
        finished = true;

        this.#metrics.push({
          description,
          durationMs: performance.now() - startedAt,
          name,
          order,
          status: result?.status,
        });
      },
    };
  }

  serverTimingHeader(): string {
    const metrics = this.#metrics.toSorted((left, right) => left.order - right.order);
    const included = metrics.slice(0, maximumServerTimingMetrics);
    const values = included.map(formatServerTimingMetric);

    if (metrics.length > included.length) {
      values.push(`omitted;desc="${metrics.length - included.length} metrics"`);
    }

    return values.join(", ");
  }
}

export const serverPerformanceTraceContext = createContext<ServerPerformanceTrace | undefined>(
  undefined,
);

export function getServerPerformanceTrace(context: Pick<RouterContextProvider, "get">) {
  return context.get(serverPerformanceTraceContext);
}

export const serverPerformanceMiddleware: MiddlewareFunction<Response> = async (
  { context },
  next,
) => {
  const trace = new ServerPerformanceTrace();
  const serverSpan = trace.start("server", "request");
  context.set(serverPerformanceTraceContext, trace);

  const response = await next();
  serverSpan.finish({ status: response.status });

  const serverTiming = trace.serverTimingHeader();
  if (serverTiming) {
    response.headers.set("Server-Timing", serverTiming);
  }

  return response;
};

export const serverPerformanceInstrumentation: ServerInstrumentation = {
  route(route) {
    route.instrument({
      action: async (callAction, { context }) => {
        const span = getServerPerformanceTrace(context)?.start("action", route.id);
        const result = await callAction();
        span?.finish({ status: result.status === "error" ? "error" : undefined });
      },
      loader: async (callLoader, { context }) => {
        const span = getServerPerformanceTrace(context)?.start("loader", route.id);
        const result = await callLoader();
        span?.finish({ status: result.status === "error" ? "error" : undefined });
      },
    });
  },
};

function formatServerTimingMetric(metric: ServerPerformanceMetric) {
  const status = metric.status === undefined ? "" : ` [${metric.status}]`;
  const description = sanitizeServerTimingDescription(`${metric.description}${status}`);
  return `${metric.name};dur=${metric.durationMs.toFixed(1)};desc="${description}"`;
}

function sanitizeServerTimingDescription(value: string) {
  return value
    .replaceAll("\\", "\\\\")
    .replaceAll('"', '\\"')
    .replaceAll(/[\r\n]/g, " ")
    .slice(0, maximumServerTimingDescriptionLength);
}
