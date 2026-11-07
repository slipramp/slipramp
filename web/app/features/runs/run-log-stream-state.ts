export type RunLogStreamState =
  | Readonly<{ phase: "waiting" }>
  | Readonly<{ phase: "connecting" }>
  | Readonly<{ phase: "live" }>
  | Readonly<{ phase: "paused" }>
  | Readonly<{ offsetBytes: number; phase: "reconnecting" }>
  | Readonly<{ phase: "retained" }>
  | Readonly<{ diagnostic: string; phase: "incomplete" }>
  | Readonly<{ diagnostic: string; phase: "error" }>;

export type RunLogStreamAction =
  | Readonly<{ artifactAvailable: boolean; type: "artifact-availability-changed" }>
  | Readonly<{ type: "opened" }>
  | Readonly<{
      artifactAvailable: boolean;
      offsetBytes: number;
      type: "connection-lost";
    }>
  | Readonly<{ type: "pause" }>
  | Readonly<{ type: "search-navigation" }>
  | Readonly<{ artifactAvailable: boolean; type: "resume" }>
  | Readonly<{ type: "sealed" }>
  | Readonly<{ type: "incomplete" }>
  | Readonly<{ detail: string; type: "client-error" }>;

export function initialRunLogStreamState(artifactAvailable: boolean): RunLogStreamState {
  return artifactAvailable ? { phase: "connecting" } : { phase: "waiting" };
}

export function reduceRunLogStream(
  state: RunLogStreamState,
  action: RunLogStreamAction,
): RunLogStreamState {
  if (isRunLogStreamTerminal(state)) return state;

  switch (action.type) {
    case "artifact-availability-changed":
      if (state.phase === "waiting" && action.artifactAvailable) {
        return { phase: "connecting" };
      }
      if (
        !action.artifactAvailable &&
        (state.phase === "connecting" || state.phase === "reconnecting")
      ) {
        return { phase: "waiting" };
      }
      return state;
    case "opened":
      return state.phase === "paused" ? state : { phase: "live" };
    case "connection-lost":
      if (state.phase === "paused") return state;
      return action.artifactAvailable
        ? { offsetBytes: action.offsetBytes, phase: "reconnecting" }
        : { phase: "waiting" };
    case "pause":
    case "search-navigation":
      return { phase: "paused" };
    case "resume":
      if (state.phase !== "paused") return state;
      return initialRunLogStreamState(action.artifactAvailable);
    case "sealed":
      return state.phase === "paused" ? state : { phase: "retained" };
    case "incomplete":
      return state.phase === "paused"
        ? state
        : {
            diagnostic:
              "Only this durable prefix could be retained. The incomplete log cannot be downloaded as a complete artifact.",
            phase: "incomplete",
          };
    case "client-error":
      return state.phase === "paused"
        ? state
        : {
            diagnostic: `The durable log stream is invalid. ${action.detail}`,
            phase: "error",
          };
  }
}

export function isRunLogStreamTerminal(state: RunLogStreamState) {
  return state.phase === "retained" || state.phase === "incomplete" || state.phase === "error";
}

export function isRunLogStreamActive(state: RunLogStreamState) {
  return state.phase !== "paused" && !isRunLogStreamTerminal(state);
}

export function runLogStreamDiagnostic(state: RunLogStreamState) {
  if (state.phase === "reconnecting") {
    return `Connection lost; retrying from durable byte ${state.offsetBytes}.`;
  }
  if (state.phase === "incomplete" || state.phase === "error") return state.diagnostic;
  return undefined;
}

export function runLogStreamLabel(state: RunLogStreamState) {
  switch (state.phase) {
    case "waiting":
      return "Waiting for logs";
    case "connecting":
      return "Connecting";
    case "live":
      return "Live";
    case "paused":
      return "Paused";
    case "reconnecting":
      return "Reconnecting";
    case "retained":
      return "Retained";
    case "incomplete":
      return "Incomplete";
    case "error":
      return "Error";
  }
}
