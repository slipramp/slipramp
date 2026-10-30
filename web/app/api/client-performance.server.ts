import type { ServerPerformanceTrace } from "~/features/performance/performance.server";
import type { Client } from "./generated/client";

const instrumentedMethods = new Set([
  "connect",
  "delete",
  "get",
  "head",
  "options",
  "patch",
  "post",
  "put",
  "trace",
]);

export function instrumentAPIClient(client: Client, trace: ServerPerformanceTrace) {
  return new Proxy(client, {
    get(target, property, receiver) {
      const value = Reflect.get(target, property, receiver);
      if (
        typeof property !== "string" ||
        !instrumentedMethods.has(property) ||
        typeof value !== "function"
      ) {
        return value;
      }

      return async (...arguments_: unknown[]) => {
        const options = requestOptions(arguments_[0]);
        const description = `${property.toUpperCase()} ${requestDescription(options)}`;
        const span = trace.start("api", description);

        try {
          const result: unknown = await Reflect.apply(value, target, arguments_);
          span.finish({ status: responseStatus(result) });
          return result;
        } catch (error) {
          span.finish({ status: "error" });
          throw error;
        }
      };
    },
  });
}

function requestOptions(value: unknown) {
  if (typeof value !== "object" || value === null) return undefined;
  return value as { query?: Record<string, unknown>; url?: string };
}

function requestDescription(options: ReturnType<typeof requestOptions>) {
  const path = options?.url ?? "unknown";
  const query = options?.query;
  if (!query) return path;

  const search = new URLSearchParams();
  for (const key of Object.keys(query).toSorted()) {
    const value = query[key];
    if (value === undefined || value === null || value === "") continue;
    search.set(key, sensitiveQueryParameter(key) ? "redacted" : String(value));
  }

  const serialized = search.toString();
  return serialized ? `${path}?${serialized}` : path;
}

function sensitiveQueryParameter(name: string) {
  return /(authorization|credential|key|password|secret|token)/i.test(name);
}

function responseStatus(result: unknown) {
  if (typeof result !== "object" || result === null || !("response" in result)) return undefined;
  const response = result.response;
  return response instanceof Response ? response.status : undefined;
}
