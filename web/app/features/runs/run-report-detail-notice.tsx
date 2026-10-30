import { Alert } from "@mantine/core";

import type { ReportLoad } from "~/features/runs/run-reports";

export function ReportDetailNotice({ state }: Readonly<{ state: ReportLoad["state"] }>) {
  switch (state) {
    case "available":
      return null;
    case "restricted":
      return (
        <Alert color="yellow" title="Report detail restricted">
          The aggregate summary is visible, but an operator or administrator is required to read
          this sensitive report or download its source Artifact.
        </Alert>
      );
    case "expired":
      return (
        <Alert color="orange" title="Report detail expired">
          The durable summary remains available, but the normalized detail and source Artifact have
          expired.
        </Alert>
      );
    case "deleted":
      return (
        <Alert color="orange" title="Report content deleted">
          The durable summary remains available, but the normalized detail and source Artifact have
          been deleted.
        </Alert>
      );
    case "unavailable":
      return (
        <Alert color="orange" title="Report detail unavailable">
          The report projection is no longer available. Its durable aggregate summary is shown.
        </Alert>
      );
    case "error":
      return (
        <Alert color="red" title="Report detail could not be loaded">
          Slipramp could not read the normalized report projection. Try refreshing this page.
        </Alert>
      );
  }
}
