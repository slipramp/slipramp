import { startTransition, StrictMode } from "react";
import { hydrateRoot } from "react-dom/client";
import { HydratedRouter } from "react-router/dom";

import { clientPerformanceInstrumentation, initializeClientPerformance } from "~/page-performance";

initializeClientPerformance();

startTransition(() => {
  hydrateRoot(
    document,
    <StrictMode>
      <HydratedRouter instrumentations={[clientPerformanceInstrumentation]} />
    </StrictMode>,
  );
});
