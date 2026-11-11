import type { LogEvent } from "../../api/generated/index.ts";

import type { BoundedLogBuffer } from "./log-buffer.ts";
import type { RunLogStreamAction } from "./run-log-stream-state.ts";

export const RUN_LOG_RECONNECT_DELAY_MS = 1000;

type LogEventSource = Pick<EventSource, "addEventListener" | "close">;

export type RunLogStreamEnvironment = Readonly<{
  cancelAnimationFrame: (handle: number) => void;
  clearTimeout: (handle: number) => void;
  createEventSource: (url: string) => LogEventSource;
  queueMicrotask: (callback: () => void) => void;
  requestAnimationFrame: (callback: FrameRequestCallback) => number;
  setTimeout: (callback: () => void, delay: number) => number;
}>;

type RunLogStreamSessionOptions = Readonly<{
  buffer: BoundedLogBuffer;
  buildURL: (offsetBytes: number) => string;
  dispatch: (action: RunLogStreamAction) => void;
  environment: RunLogStreamEnvironment;
  isArtifactAvailable: () => boolean;
  onBufferChanged: () => void;
}>;

export function startRunLogStreamSession({
  buffer,
  buildURL,
  dispatch,
  environment,
  isArtifactAvailable,
  onBufferChanged,
}: RunLogStreamSessionOptions) {
  let activeConnection = 0;
  let activeSource: LogEventSource | undefined;
  let reconnectTimer: number | undefined;
  let renderFrame: number | undefined;
  let stopped = false;

  const scheduleRender = () => {
    if (renderFrame !== undefined) return;

    renderFrame = environment.requestAnimationFrame(() => {
      renderFrame = undefined;
      if (!stopped) onBufferChanged();
    });
  };
  const finishRender = () => {
    if (renderFrame === undefined) return;

    environment.cancelAnimationFrame(renderFrame);
    renderFrame = undefined;
    onBufferChanged();
  };
  const closeActiveSource = () => {
    activeConnection += 1;
    const source = activeSource;
    activeSource = undefined;
    source?.close();
  };
  const finish = (action: RunLogStreamAction) => {
    if (stopped) return;

    stopped = true;
    closeActiveSource();
    if (reconnectTimer !== undefined) {
      environment.clearTimeout(reconnectTimer);
      reconnectTimer = undefined;
    }
    finishRender();
    dispatch(action);
  };
  const connect = () => {
    if (stopped) return;

    const connection = activeConnection + 1;
    activeConnection = connection;

    let source: LogEventSource;
    try {
      source = environment.createEventSource(buildURL(buffer.cursorBytes));
    } catch (error) {
      const detail = error instanceof Error ? error.message : "The connection could not be opened.";
      finish({ detail, type: "client-error" });
      return;
    }
    activeSource = source;

    const isCurrent = () => !stopped && connection === activeConnection && source === activeSource;
    source.addEventListener("open", () => {
      if (!isCurrent()) return;
      dispatch({ type: "opened" });
    });
    source.addEventListener("chunk", (event) => {
      if (!isCurrent()) return;

      try {
        const payload = JSON.parse((event as MessageEvent<string>).data) as LogEvent;
        const contents = decodeBase64(payload.contentsBase64);
        if (
          buffer.append({
            contents,
            nextOffsetBytes: payload.nextOffsetBytes,
            offsetBytes: payload.offsetBytes,
          })
        ) {
          scheduleRender();
        }
      } catch (error) {
        const detail = error instanceof Error ? error.message : "The event could not be decoded.";
        finish({ detail, type: "client-error" });
      }
    });
    source.addEventListener("sealed", () => {
      if (!isCurrent()) return;
      finish({ type: "sealed" });
    });
    source.addEventListener("failed", () => {
      if (!isCurrent()) return;
      finish({ type: "incomplete" });
    });
    source.addEventListener("error", () => {
      if (!isCurrent() || reconnectTimer !== undefined) return;

      closeActiveSource();
      dispatch({
        artifactAvailable: isArtifactAvailable(),
        offsetBytes: buffer.cursorBytes,
        type: "connection-lost",
      });
      reconnectTimer = environment.setTimeout(() => {
        reconnectTimer = undefined;
        connect();
      }, RUN_LOG_RECONNECT_DELAY_MS);
    });
  };

  // React Strict Mode immediately cleans up and restarts effects once during
  // development. Let that probe finish before opening a network connection so
  // it cannot abort an otherwise valid EventSource request.
  environment.queueMicrotask(connect);

  return () => {
    if (stopped) return;

    stopped = true;
    closeActiveSource();
    if (reconnectTimer !== undefined) environment.clearTimeout(reconnectTimer);
    if (renderFrame !== undefined) environment.cancelAnimationFrame(renderFrame);
  };
}

function decodeBase64(value: string) {
  const binary = atob(value);
  const bytes = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index += 1) {
    bytes[index] = binary.charCodeAt(index);
  }
  return bytes;
}
