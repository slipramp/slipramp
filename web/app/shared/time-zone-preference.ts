import { useCallback, useSyncExternalStore } from "react";

import {
  defaultTimeZone,
  loadTimeZonePreference,
  normalizeTimeZone,
  storeTimeZonePreference,
  timeZoneStorageKey,
} from "~/shared/time-zone";

const timeZoneChangeEvent = "slipramp-time-zone-change";

let inMemoryTimeZone = defaultTimeZone;
let timeZoneLoaded = false;

function getTimeZoneSnapshot() {
  if (!timeZoneLoaded) {
    timeZoneLoaded = true;
    try {
      inMemoryTimeZone = loadTimeZonePreference(window.localStorage);
    } catch {
      // Retain the page-local preference when browser storage is unavailable.
    }
  }
  return inMemoryTimeZone;
}

function subscribeToTimeZone(onStoreChange: () => void) {
  function handleStorage(event: StorageEvent) {
    if (event.key === timeZoneStorageKey || event.key === null) {
      inMemoryTimeZone = normalizeTimeZone(event.newValue);
      timeZoneLoaded = true;
      onStoreChange();
    }
  }

  window.addEventListener(timeZoneChangeEvent, onStoreChange);
  window.addEventListener("storage", handleStorage);
  return () => {
    window.removeEventListener(timeZoneChangeEvent, onStoreChange);
    window.removeEventListener("storage", handleStorage);
  };
}

export function useTimeZone() {
  const timeZone = useSyncExternalStore(
    subscribeToTimeZone,
    getTimeZoneSnapshot,
    () => defaultTimeZone,
  );
  const setTimeZone = useCallback((value: string) => {
    inMemoryTimeZone = normalizeTimeZone(value);
    timeZoneLoaded = true;
    try {
      storeTimeZonePreference(window.localStorage, inMemoryTimeZone);
    } catch {
      // The selected time zone still applies for this page when storage is unavailable.
    }
    window.dispatchEvent(new Event(timeZoneChangeEvent));
  }, []);

  return { setTimeZone, timeZone };
}
