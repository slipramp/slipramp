import type { ExecutionActivity } from "~/execution-activity";

import styles from "./kawaii-activity.module.css";

export function KawaiiActivity({
  activity,
  compact = false,
}: Readonly<{ activity?: ExecutionActivity; compact?: boolean }>) {
  if (!activity) return null;

  return (
    <div
      aria-hidden={compact || undefined}
      className={styles.activity}
      data-compact={compact || undefined}
      data-kawaii-activity
    >
      <div aria-hidden="true" className={styles.worker} />
      {!compact ? (
        <div className={styles.copy}>
          <span className={styles.eyebrow}>Axolotl on the job</span>
          <p className={styles.title}>{activity.title}</p>
          <p className={styles.description}>{activity.description}</p>
        </div>
      ) : null}
    </div>
  );
}
