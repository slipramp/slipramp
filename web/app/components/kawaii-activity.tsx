import { activityAnimationDelay, type ExecutionActivity } from "~/execution-activity";

import styles from "./kawaii-activity.module.css";

export function KawaiiActivity({
  activity,
  animationKey,
  compact = false,
}: Readonly<{ activity?: ExecutionActivity; animationKey: string; compact?: boolean }>) {
  if (!activity) return null;

  return (
    <div
      aria-hidden={compact || undefined}
      className={styles.activity}
      data-compact={compact || undefined}
      data-kawaii-activity
    >
      <div aria-hidden="true" className={styles.scene}>
        <div
          className={styles.sprite}
          data-animation={activity.animation}
          style={{ animationDelay: `${activityAnimationDelay(animationKey)}ms` }}
        />
      </div>
      {!compact ? (
        <div className={styles.copy}>
          <p className={styles.title}>{activity.title}</p>
          <p className={styles.description}>{activity.description}</p>
        </div>
      ) : null}
    </div>
  );
}
