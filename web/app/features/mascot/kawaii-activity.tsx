import {
  activityAnimationDelay,
  activityAnimationPose,
  type ExecutionActivity,
} from "~/features/mascot/execution-activity";

import styles from "./kawaii-activity.module.css";

export function KawaiiActivity({
  activity,
  animationKey,
  compact = false,
}: Readonly<{ activity?: ExecutionActivity; animationKey: string; compact?: boolean }>) {
  if (!activity) return null;

  const pose = activityAnimationPose(activity.animation, animationKey);

  return (
    <div
      aria-hidden={compact || undefined}
      aria-atomic={compact ? undefined : true}
      className={styles.activity}
      data-compact={compact || undefined}
      data-kawaii-activity
      data-tone={activity.tone}
      role={compact ? undefined : "status"}
    >
      <div aria-hidden="true" className={styles.scene}>
        <div
          className={styles.sprite}
          data-pose={pose}
          style={{ animationDelay: `${activityAnimationDelay(animationKey)}ms` }}
        />
      </div>
      {!compact ? (
        <div className={styles.copy}>
          <p className={styles.title}>{activity.title}</p>
          {activity.description ? (
            <p className={styles.description}>{activity.description}</p>
          ) : null}
          {activity.result && (activity.result.reason || activity.result.exitCode !== undefined) ? (
            <div className={styles.result}>
              {activity.result.reason ? <code>{activity.result.reason}</code> : null}
              {activity.result.exitCode !== undefined ? (
                <p>
                  Process exit code <code>{activity.result.exitCode}</code>
                </p>
              ) : null}
            </div>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}
