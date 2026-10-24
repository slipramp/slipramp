import { useCallback, useSyncExternalStore } from "react";

export type ThemePreset = "standard" | "kawaii";

const themePresetStorageKey = "slipramp-theme";
const themePresetChangeEvent = "slipramp-theme-change";

const themePresetScript = `
try {
  var storedTheme = window.localStorage.getItem("${themePresetStorageKey}");
  document.documentElement.dataset.sliprampTheme = storedTheme === "kawaii" ? "kawaii" : "standard";
} catch (error) {
  document.documentElement.dataset.sliprampTheme = "standard";
}
`;

export function ThemePresetScript() {
  return <script dangerouslySetInnerHTML={{ __html: themePresetScript }} />;
}

function parseThemePreset(value: string | null | undefined): ThemePreset {
  return value === "kawaii" ? "kawaii" : "standard";
}

function getThemePresetSnapshot(): ThemePreset {
  return parseThemePreset(document.documentElement.dataset.sliprampTheme);
}

function subscribeToThemePreset(onStoreChange: () => void) {
  function handleStorage(event: StorageEvent) {
    if (event.key === themePresetStorageKey) {
      document.documentElement.dataset.sliprampTheme = parseThemePreset(event.newValue);
      onStoreChange();
    }
  }

  window.addEventListener(themePresetChangeEvent, onStoreChange);
  window.addEventListener("storage", handleStorage);
  return () => {
    window.removeEventListener(themePresetChangeEvent, onStoreChange);
    window.removeEventListener("storage", handleStorage);
  };
}

export function useThemePreset() {
  const themePreset = useSyncExternalStore(
    subscribeToThemePreset,
    getThemePresetSnapshot,
    () => "standard",
  );
  const setThemePreset = useCallback((preset: ThemePreset) => {
    document.documentElement.dataset.sliprampTheme = preset;
    try {
      window.localStorage.setItem(themePresetStorageKey, preset);
    } catch {
      // The selected theme still applies for this page when storage is unavailable.
    }
    window.dispatchEvent(new Event(themePresetChangeEvent));
  }, []);

  return { setThemePreset, themePreset };
}
