import {
  ActionIcon,
  Box,
  Combobox,
  Modal,
  ScrollArea,
  Text,
  TextInput,
  useCombobox,
  useMantineTheme,
} from "@mantine/core";
import { useClickOutside, useMediaQuery } from "@mantine/hooks";
import { IconSearch } from "@tabler/icons-react";
import { useCallback, useEffect, useRef } from "react";
import { useFetcher, useLocation, useNavigate, useNavigationType } from "react-router";

import classes from "./global-search.module.css";
import { searchGroups } from "./search-results";
import { limitSearchQuery, searchPaletteLocation, searchPaletteParam } from "./search-state";
import type { loader } from "./routes/search";

export function GlobalSearch({ kargoEnabled }: Readonly<{ kargoEnabled: boolean }>) {
  const location = useLocation();
  const navigate = useNavigate();
  const navigationType = useNavigationType();
  const theme = useMantineTheme();
  const desktop = useMediaQuery(`(min-width: ${theme.breakpoints.md})`, true);
  const inputRef = useRef<HTMLInputElement>(null);
  const query = new URLSearchParams(location.search).get(searchPaletteParam);
  const opened = query !== null;
  const term = query?.trim() ?? "";
  const { data, load } = useFetcher<typeof loader>();
  const response = term && data?.query === term ? data : undefined;
  const groups = response?.results ? searchGroups(response.results) : [];

  const change = useCallback(
    (value: string) => {
      // History can advance between keystrokes before the router renders the new location.
      const current = new URL(window.location.href);
      void navigate(searchPaletteLocation(current, value), {
        replace: current.searchParams.has(searchPaletteParam),
        state: opened ? location.state : { ...location.state, paletteOpenedHere: true },
        defaultShouldRevalidate: false,
        preventScrollReset: true,
      });
    },
    [location.state, navigate, opened],
  );

  function close() {
    if (!opened) return;
    if (location.state?.paletteOpenedHere) void navigate(-1);
    else
      void navigate(searchPaletteLocation(location, null), {
        replace: true,
        defaultShouldRevalidate: false,
        preventScrollReset: true,
      });
  }
  const desktopField = useClickOutside<HTMLDivElement>(
    (event) => {
      // Router links handle their click before it reaches the document.
      if (!event.defaultPrevented) close();
    },
    ["click"],
    undefined,
    desktop && opened,
  );
  const store = useCombobox({
    opened,
    onDropdownOpen: () => change(""),
  });

  useEffect(() => {
    function shortcut(event: KeyboardEvent) {
      if (
        !(event.metaKey || event.ctrlKey) ||
        event.altKey ||
        event.shiftKey ||
        event.key.toLowerCase() !== "k"
      )
        return;
      event.preventDefault();
      if (event.repeat) return;
      if (!opened) change("");
      inputRef.current?.focus();
      inputRef.current?.select();
    }
    window.addEventListener("keydown", shortcut);
    return () => window.removeEventListener("keydown", shortcut);
  }, [change, opened]);

  useEffect(() => {
    if (inputRef.current && (!opened || navigationType === "POP")) {
      inputRef.current.value = query ?? "";
    }
  }, [navigationType, opened, query]);

  useEffect(() => {
    if (desktop && opened) inputRef.current?.focus({ preventScroll: true });
  }, [desktop, opened]);

  useEffect(() => {
    if (!term) return;
    const timer = window.setTimeout(() => {
      void load(`/resources/search?${new URLSearchParams({ q: term })}`);
    }, 180);
    return () => window.clearTimeout(timer);
  }, [term, load]);

  const { resetSelectedOption, selectFirstOption } = store;
  useEffect(() => {
    resetSelectedOption();
    if (response?.results) selectFirstOption();
  }, [resetSelectedOption, response, selectFirstOption]);

  const placeholder = kargoEnabled ? "Search targets, stages, and pipelines" : "Search targets";
  let status = "";
  if (!term && desktop)
    status = kargoEnabled
      ? "Search by name, repository, or project."
      : "Search by target or repository name.";
  else if (term && !response) status = "Searching…";
  else if (response && !response.results) status = "Search is unavailable. Try again in a moment.";
  else if (term && groups.length === 0) status = "No matches.";

  const input = (
    <TextInput
      ref={inputRef}
      aria-label={placeholder}
      aria-keyshortcuts="Meta+K Control+K"
      title="Search (⌘K / Ctrl+K)"
      data-autofocus={desktop ? undefined : true}
      leftSection={<IconSearch aria-hidden size={18} stroke={1.8} />}
      onChange={(event) => {
        const field = event.currentTarget;
        const value = limitSearchQuery(field.value);
        if (value !== field.value) field.value = value;
        change(value);
      }}
      onClick={() => {
        if (!opened) change("");
      }}
      onKeyDown={(event) => {
        if (desktop && event.key === "Tab") close();
        if (opened && event.key === "Escape") {
          event.stopPropagation();
          close();
        }
      }}
      placeholder={desktop ? "Search Slipramp" : placeholder}
      defaultValue={query ?? ""}
    />
  );
  const results = (
    <>
      <ScrollArea.Autosize mah="55dvh" type="auto">
        <Combobox.Options aria-label="Search results">
          {groups.map(({ label, items }) => (
            <Combobox.Group key={label} label={label}>
              {items.map((item) => (
                <Combobox.Option className={classes.option} key={item.path} value={item.path}>
                  <Text size="sm" fw={600}>
                    {item.label}
                  </Text>
                  <Text size="xs" className={classes.detail}>
                    {item.detail}
                  </Text>
                </Combobox.Option>
              ))}
            </Combobox.Group>
          ))}
        </Combobox.Options>
      </ScrollArea.Autosize>
      {status && (
        <Text
          component="output"
          display="block"
          c="dimmed"
          size="sm"
          p={desktop ? "xs" : undefined}
          mt={desktop ? 0 : "sm"}
        >
          {status}
        </Text>
      )}
    </>
  );

  return (
    <>
      <ActionIcon
        aria-label="Search"
        aria-keyshortcuts="Meta+K Control+K"
        hiddenFrom="md"
        onClick={() => change("")}
        size="lg"
        title="Search (⌘K / Ctrl+K)"
        variant="light"
      >
        <IconSearch size={19} stroke={1.8} />
      </ActionIcon>
      <Combobox
        onOptionSubmit={(path) => void navigate(path)}
        store={store}
        withinPortal={false}
        keepMounted={false}
        shadow="md"
      >
        {desktop ? (
          <Box ref={desktopField} visibleFrom="md">
            <Combobox.Target withExpandedAttribute>{input}</Combobox.Target>
            <Combobox.Dropdown>{results}</Combobox.Dropdown>
          </Box>
        ) : (
          <Modal
            stackId="global-search"
            opened={opened}
            onClose={close}
            title="Search"
            closeButtonProps={{ "aria-label": "Close search" }}
            size="lg"
            yOffset="12dvh"
          >
            <Combobox.EventsTarget withExpandedAttribute>{input}</Combobox.EventsTarget>
            {results}
          </Modal>
        )}
      </Combobox>
    </>
  );
}
