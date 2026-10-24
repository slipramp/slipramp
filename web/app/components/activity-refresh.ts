import { useEffect } from "react";
import { useRevalidator } from "react-router";

import { useThemePreset } from "~/theme-preset";

export function useActivityRefresh(active: boolean) {
  const revalidator = useRevalidator();

  useEffect(() => {
    if (!active) return;

    const interval = window.setInterval(() => {
      if (document.visibilityState === "visible" && revalidator.state === "idle") {
        void revalidator.revalidate();
      }
    }, 3000);

    return () => window.clearInterval(interval);
  }, [active, revalidator]);
}

export function useKawaiiActivityRefresh(active: boolean) {
  const { themePreset } = useThemePreset();
  useActivityRefresh(active && themePreset === "kawaii");
}
