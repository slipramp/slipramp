import type { Run, TargetDefinition } from "~/api/generated";
import { runStatusColor, runStatusLabel } from "../runs/run-status.ts";

export type ScheduledActionDefinition = {
  actionName: string;
  actionTitle?: string;
  cron: string;
  triggerName: string;
};

export type ScheduledActionRun =
  | {
      definition: ScheduledActionDefinition;
      state: "available";
      run?: Run;
    }
  | {
      definition: ScheduledActionDefinition;
      state: "unavailable";
    };

export function scheduledActionDefinitions(target: TargetDefinition) {
  return Object.entries(target.spec.actions)
    .toSorted(([left], [right]) => left.localeCompare(right))
    .flatMap(([actionName, action]) =>
      (action.triggers ?? [])
        .filter(
          (trigger): trigger is typeof trigger & { schedule: { cron: string } } =>
            trigger.schedule !== undefined,
        )
        .map((trigger) => ({
          actionName,
          actionTitle: action.title,
          cron: trigger.schedule.cron,
          triggerName: trigger.name,
        })),
    );
}

export async function loadLatestScheduledRuns(
  definitions: ScheduledActionDefinition[],
  loadLatest: (definition: ScheduledActionDefinition) => Promise<Run | undefined>,
): Promise<ScheduledActionRun[]> {
  return Promise.all(
    definitions.map(async (definition): Promise<ScheduledActionRun> => {
      try {
        return {
          definition,
          run: await loadLatest(definition),
          state: "available",
        };
      } catch {
        return { definition, state: "unavailable" };
      }
    }),
  );
}

export function scheduledRunPresentation(record: ScheduledActionRun) {
  if (record.state === "unavailable") {
    return { color: "yellow", label: "Unavailable" };
  }
  if (!record.run) {
    return { color: "gray", label: "Not run yet" };
  }

  return {
    color: runStatusColor(record.run),
    label: runStatusLabel(record.run),
  };
}

export function scheduledRunHistoryPath(targetId: string, definition: ScheduledActionDefinition) {
  const search = new URLSearchParams({
    targetId,
    action: definition.actionName,
    source: "schedule",
    triggerName: definition.triggerName,
  });

  return `/runs?${search}`;
}

export function scheduledActionsDescription(archived: boolean) {
  return archived
    ? "Schedules are inactive while this Target is archived. Historical latest Runs remain visible."
    : "Latest immutable Run created by each UTC schedule in the active Target definition.";
}
