import {
  Alert,
  Badge,
  Box,
  Button,
  Group,
  Menu,
  Modal,
  Paper,
  Stack,
  Text,
  TextInput,
  Title,
} from "@mantine/core";
import {
  IconAdjustments,
  IconArrowDown,
  IconArrowUp,
  IconCheck,
  IconDownload,
  IconMaximize,
  IconPlayerPause,
  IconPlayerPlay,
  IconSearch,
  IconTextWrap,
} from "@tabler/icons-react";
import {
  memo,
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useReducer,
  useRef,
  useState,
  type KeyboardEvent,
} from "react";

import { ANSI_DEFAULT_BACKGROUND, ANSI_DEFAULT_FOREGROUND, parseAnsi } from "~/features/runs/ansi";
import type { Artifact } from "~/api/generated";
import { formatBinarySize } from "~/shared/display";
import {
  BoundedLogBuffer,
  LOG_TAIL_MAX_LINES,
  LOG_TAIL_WINDOW_BYTES,
} from "~/features/runs/log-buffer";
import {
  initialRunLogStreamState,
  isRunLogStreamActive,
  isRunLogStreamTerminal,
  reduceRunLogStream,
  runLogStreamDiagnostic,
  runLogStreamLabel,
} from "~/features/runs/run-log-stream-state";
import {
  startRunLogStreamSession,
  type RunLogStreamEnvironment,
} from "~/features/runs/run-log-stream";

const estimatedLineHeight = 20;
const virtualLineOverscan = 12;

const browserRunLogStreamEnvironment: RunLogStreamEnvironment = {
  cancelAnimationFrame: (handle) => window.cancelAnimationFrame(handle),
  clearTimeout: (handle) => window.clearTimeout(handle),
  createEventSource: (url) => new EventSource(url),
  requestAnimationFrame: (callback) => window.requestAnimationFrame(callback),
  setTimeout: (callback, delay) => window.setTimeout(callback, delay),
};

export function RunLogViewer({
  artifact,
  runId,
}: Readonly<{ artifact?: Artifact; runId: string }>) {
  const logAvailable = artifact !== undefined;
  const initialOffsetBytes = Math.max(0, (artifact?.sizeBytes ?? 0) - LOG_TAIL_WINDOW_BYTES);
  const [buffer] = useState(() => new BoundedLogBuffer(initialOffsetBytes));

  const [, setBufferVersion] = useState(0);
  const [fullscreenOpened, setFullscreenOpened] = useState(false);
  const [matchPosition, setMatchPosition] = useState(0);
  const [searchQuery, setSearchQuery] = useState("");
  const [streamState, dispatchStream] = useReducer(
    reduceRunLogStream,
    logAvailable,
    initialRunLogStreamState,
  );
  const [wrapLines, setWrapLines] = useState(true);
  const streamActive = isRunLogStreamActive(streamState);
  const streamTerminal = isRunLogStreamTerminal(streamState);
  const paused = streamState.phase === "paused";
  const status = runLogStreamLabel(streamState);
  const diagnostic = runLogStreamDiagnostic(streamState);

  useEffect(() => {
    dispatchStream({ artifactAvailable: logAvailable, type: "artifact-availability-changed" });
  }, [logAvailable]);

  useEffect(() => {
    if (!streamActive) return;

    return startRunLogStreamSession({
      artifactAvailable: logAvailable,
      buffer,
      buildURL: (offsetBytes) => {
        const streamURL = new URL(`/api/v1/runs/${runId}/logs`, window.location.origin);
        streamURL.searchParams.set("offsetBytes", String(offsetBytes));
        return streamURL.toString();
      },
      dispatch: dispatchStream,
      environment: browserRunLogStreamEnvironment,
      onBufferChanged: () => setBufferVersion((current) => current + 1),
    });
  }, [buffer, logAvailable, runId, streamActive]);

  const matchingLines = buffer.findMatchingLines(searchQuery);
  const selectedMatchPosition = Math.min(matchPosition, Math.max(matchingLines.length - 1, 0));

  const selectMatch = (direction: number) => {
    if (matchingLines.length === 0) return;

    dispatchStream({ type: "search-navigation" });
    setMatchPosition(
      (selectedMatchPosition + direction + matchingLines.length) % matchingLines.length,
    );
  };
  const handleSearchKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key !== "Enter") return;

    event.preventDefault();
    selectMatch(event.shiftKey ? -1 : 1);
  };
  const updateSearchQuery = (value: string) => {
    setMatchPosition(0);
    setSearchQuery(value);
  };

  const selectedMatchLine = matchingLines[selectedMatchPosition];
  const activeMatchLine = paused || streamTerminal ? selectedMatchLine : undefined;
  const hasSearchQuery = searchQuery.trim().length > 0;
  const canDownloadRaw =
    artifact !== undefined &&
    artifact.retentionState === "retained" &&
    (artifact.state === "sealed" || streamState.phase === "retained");
  const statusColor =
    status === "Error" || status === "Incomplete"
      ? "red"
      : status === "Reconnecting"
        ? "yellow"
        : status === "Retained"
          ? "green"
          : undefined;
  const windowDescription =
    buffer.windowStartBytes === 0
      ? `${formatBinarySize(buffer.sizeBytes)} visible`
      : `${formatBinarySize(buffer.sizeBytes)} tail from byte ${buffer.windowStartBytes.toLocaleString()}`;
  const controls = (
    <>
      <Group gap="xs" justify="space-between" wrap="wrap">
        <Group gap="xs" wrap="wrap">
          <Badge color={statusColor} variant="light">
            {status}
          </Badge>
          <Text c="dimmed" size="xs">
            {windowDescription} · {buffer.lineCount.toLocaleString()} lines
          </Text>
        </Group>
        <Group gap="xs" wrap="wrap">
          {!streamTerminal ? (
            <Button
              leftSection={
                paused ? (
                  <IconPlayerPlay aria-hidden size={15} />
                ) : (
                  <IconPlayerPause aria-hidden size={15} />
                )
              }
              onClick={() => {
                dispatchStream(
                  paused ? { artifactAvailable: logAvailable, type: "resume" } : { type: "pause" },
                );
              }}
              size="compact-sm"
              variant="light"
            >
              {paused ? "Follow" : "Pause"}
            </Button>
          ) : null}
          <Menu position="bottom-end" withinPortal>
            <Menu.Target>
              <Button
                leftSection={<IconAdjustments aria-hidden size={15} />}
                size="compact-sm"
                variant="default"
              >
                Options
              </Button>
            </Menu.Target>
            <Menu.Dropdown>
              <Menu.Item
                aria-checked={wrapLines}
                closeMenuOnClick={false}
                leftSection={<IconTextWrap aria-hidden size={15} />}
                onClick={() => setWrapLines((current) => !current)}
                rightSection={wrapLines ? <IconCheck aria-hidden size={14} /> : null}
                role="menuitemcheckbox"
              >
                Wrap lines
              </Menu.Item>
              <Menu.Divider />
              {canDownloadRaw && artifact ? (
                <Menu.Item
                  component="a"
                  href={`/api/v1/artifacts/${artifact.id}/download`}
                  leftSection={<IconDownload aria-hidden size={15} />}
                >
                  Download raw log
                </Menu.Item>
              ) : (
                <Menu.Item
                  disabled
                  leftSection={<IconDownload aria-hidden size={15} />}
                  title="Raw download is available after the complete log is retained."
                >
                  Download raw log
                </Menu.Item>
              )}
            </Menu.Dropdown>
          </Menu>
        </Group>
      </Group>
      <Group align="center" gap="xs" wrap="wrap">
        <TextInput
          aria-label="Search visible log tail"
          leftSection={<IconSearch aria-hidden size={15} />}
          onChange={(event) => updateSearchQuery(event.currentTarget.value)}
          onKeyDown={handleSearchKeyDown}
          placeholder="Search visible tail"
          size="sm"
          style={{ flex: "1 1 18rem" }}
          value={searchQuery}
        />
        {hasSearchQuery ? (
          <>
            <Text aria-live="polite" c="dimmed" miw={110} size="xs">
              {matchingLines.length === 0
                ? "No matches"
                : `${selectedMatchPosition + 1} of ${matchingLines.length} matching lines`}
            </Text>
            <Button.Group>
              <Button
                aria-label="Previous log search match"
                disabled={matchingLines.length === 0}
                onClick={() => selectMatch(-1)}
                size="compact-sm"
                variant="default"
              >
                <IconArrowUp aria-hidden size={15} />
              </Button>
              <Button
                aria-label="Next log search match"
                disabled={matchingLines.length === 0}
                onClick={() => selectMatch(1)}
                size="compact-sm"
                variant="default"
              >
                <IconArrowDown aria-hidden size={15} />
              </Button>
            </Button.Group>
          </>
        ) : null}
      </Group>
    </>
  );

  return (
    <>
      <Paper p="md" withBorder>
        <Stack gap="sm">
          <Group justify="space-between">
            <Title order={2}>Logs</Title>
            <Button
              leftSection={<IconMaximize aria-hidden size={15} />}
              onClick={() => setFullscreenOpened(true)}
              size="compact-sm"
              variant="subtle"
            >
              Full screen
            </Button>
          </Group>
          {controls}
          {diagnostic ? (
            <Alert
              color={status === "Incomplete" || status === "Reconnecting" ? "yellow" : "red"}
              title={status}
            >
              {diagnostic}
            </Alert>
          ) : null}
          <VirtualizedLogOutput
            activeMatchLine={activeMatchLine}
            buffer={buffer}
            following={!paused}
            key={`inline-${wrapLines}`}
            searchQuery={searchQuery}
            wrapLines={wrapLines}
          />
          <Text c="dimmed" size="xs">
            The viewer keeps at most {formatBinarySize(LOG_TAIL_WINDOW_BYTES)} and{" "}
            {LOG_TAIL_MAX_LINES.toLocaleString()} lines in memory. Raw download contains the
            complete retained log.
          </Text>
        </Stack>
      </Paper>

      <Modal
        classNames={{ body: "run-log-modal-body", content: "run-log-modal-content" }}
        fullScreen
        onClose={() => setFullscreenOpened(false)}
        opened={fullscreenOpened}
        title="Run logs"
      >
        <Stack className="run-log-fullscreen-layout" gap="sm">
          {controls}
          {diagnostic ? (
            <Alert
              color={status === "Incomplete" || status === "Reconnecting" ? "yellow" : "red"}
              title={status}
            >
              {diagnostic}
            </Alert>
          ) : null}
          <VirtualizedLogOutput
            activeMatchLine={activeMatchLine}
            buffer={buffer}
            following={!paused}
            fullscreen
            key={`fullscreen-${wrapLines}`}
            searchQuery={searchQuery}
            wrapLines={wrapLines}
          />
        </Stack>
      </Modal>
    </>
  );
}

function VirtualizedLogOutput({
  activeMatchLine,
  buffer,
  following,
  fullscreen = false,
  searchQuery,
  wrapLines,
}: Readonly<{
  activeMatchLine?: number;
  buffer: BoundedLogBuffer;
  following: boolean;
  fullscreen?: boolean;
  searchQuery: string;
  wrapLines: boolean;
}>) {
  const [measurements, setMeasurements] = useState<ReadonlyMap<string, number>>(new Map());
  const outputReference = useRef<HTMLDivElement>(null);
  const [viewport, setViewport] = useState({ height: 480, scrollTop: 0, width: 0 });

  useEffect(() => {
    const output = outputReference.current;
    if (output === null) return;

    const updateSize = () => {
      setViewport((current) => ({
        height: output.clientHeight,
        scrollTop: current.scrollTop,
        width: output.clientWidth,
      }));
    };
    const observer = new ResizeObserver(updateSize);
    observer.observe(output);
    return () => observer.disconnect();
  }, []);

  const offsets = Array.from({ length: buffer.lineCount + 1 }, () => 0);
  for (let index = 0; index < buffer.lineCount; index += 1) {
    const key = buffer.getLineKey(index);
    offsets[index + 1] = (offsets[index] ?? 0) + (measurements.get(key) ?? estimatedLineHeight);
  }
  const totalHeight = offsets.at(-1) ?? 0;

  useLayoutEffect(() => {
    const output = outputReference.current;
    if (output === null || !following) return;

    output.scrollTop = totalHeight;
  }, [following, totalHeight]);

  const activeMatchTop =
    activeMatchLine === undefined
      ? undefined
      : ((offsets[activeMatchLine] ?? 0) + (offsets[activeMatchLine + 1] ?? estimatedLineHeight)) /
        2;

  useLayoutEffect(() => {
    const output = outputReference.current;
    if (output === null || activeMatchTop === undefined) return;

    const centeredTop = activeMatchTop - output.clientHeight / 2;
    output.scrollTop = Math.max(0, centeredTop);
  }, [activeMatchTop]);

  const measureLine = useCallback((key: string, height: number) => {
    if (height <= 0) return;

    setMeasurements((current) => {
      if (Math.abs((current.get(key) ?? 0) - height) < 0.5) return current;

      const next = new Map(current);
      next.set(key, height);
      if (next.size > LOG_TAIL_MAX_LINES) {
        const oldestKey = next.keys().next().value;
        if (oldestKey !== undefined) next.delete(oldestKey);
      }
      return next;
    });
  }, []);
  const visibleRange = {
    start: Math.max(0, findLineAtOffset(offsets, viewport.scrollTop) - virtualLineOverscan),
    end: Math.min(
      buffer.lineCount,
      findLineAtOffset(offsets, viewport.scrollTop + viewport.height) + virtualLineOverscan + 1,
    ),
  };
  const visibleLines = [];
  for (let index = visibleRange.start; index < visibleRange.end; index += 1) {
    visibleLines.push({ index, line: buffer.getLine(index), top: offsets[index] ?? 0 });
  }

  return (
    <Box
      aria-label="Run log tail"
      className={fullscreen ? "run-log run-log-fullscreen" : "run-log"}
      onScroll={(event) => {
        const output = event.currentTarget;
        setViewport((current) => ({ ...current, scrollTop: output.scrollTop }));
      }}
      ref={outputReference}
      role="log"
      style={{
        backgroundColor: ANSI_DEFAULT_BACKGROUND,
        color: ANSI_DEFAULT_FOREGROUND,
        height: fullscreen ? "100%" : 480,
        overflow: "auto",
        position: "relative",
        tabSize: 4,
      }}
      tabIndex={0}
    >
      {buffer.lineCount === 0 ? (
        <Text c="dimmed" ff="monospace" p="sm" size="sm">
          Waiting for durable log output…
        </Text>
      ) : (
        <Box
          style={{
            height: totalHeight,
            minWidth: wrapLines ? undefined : "max-content",
            position: "relative",
          }}
        >
          {visibleLines.map(({ index, line, top }) => (
            <VirtualLogLine
              active={index === activeMatchLine}
              key={line.key}
              lineKey={line.key}
              measure={measureLine}
              searchQuery={searchQuery}
              text={line.text}
              top={top}
              wrapLines={wrapLines}
            />
          ))}
        </Box>
      )}
    </Box>
  );
}

const VirtualLogLine = memo(function VirtualLogLine({
  active,
  lineKey,
  measure,
  searchQuery,
  text,
  top,
  wrapLines,
}: Readonly<{
  active: boolean;
  lineKey: string;
  measure: (key: string, height: number) => void;
  searchQuery: string;
  text: string;
  top: number;
  wrapLines: boolean;
}>) {
  const lineReference = useRef<HTMLDivElement>(null);
  const segments = useMemo(() => parseAnsi(text), [text]);
  const renderedText = useMemo(() => segments.map((segment) => segment.text).join(""), [segments]);
  const matchRanges = useMemo(
    () => findMatchRanges(renderedText, searchQuery),
    [renderedText, searchQuery],
  );

  useLayoutEffect(() => {
    const line = lineReference.current;
    if (line === null) return;

    const recordHeight = () => measure(lineKey, line.getBoundingClientRect().height);
    const observer = new ResizeObserver(recordHeight);
    observer.observe(line);
    return () => observer.disconnect();
  }, [lineKey, measure]);

  let segmentOffset = 0;
  return (
    <Box
      data-active-match={active || undefined}
      ff="monospace"
      ref={lineReference}
      style={{
        fontSize: "var(--mantine-font-size-xs)",
        left: 0,
        lineHeight: `${estimatedLineHeight}px`,
        minHeight: estimatedLineHeight,
        overflowWrap: wrapLines ? "anywhere" : "normal",
        paddingInline: "var(--mantine-spacing-xs)",
        position: "absolute",
        top,
        whiteSpace: wrapLines ? "pre-wrap" : "pre",
        width: wrapLines ? "100%" : "max-content",
      }}
    >
      {segments.length === 0
        ? " "
        : segments.map((segment, segmentIndex) => {
            const start = segmentOffset;
            segmentOffset += segment.text.length;
            return (
              <span key={`${segmentIndex}:${start}`} style={segment.style}>
                {renderHighlightedText(segment.text, start, matchRanges, active)}
              </span>
            );
          })}
    </Box>
  );
});

function findLineAtOffset(offsets: number[], offset: number) {
  let lower = 0;
  let upper = Math.max(0, offsets.length - 1);
  while (lower < upper) {
    const middle = Math.floor((lower + upper + 1) / 2);
    if ((offsets[middle] ?? 0) <= offset) lower = middle;
    else upper = middle - 1;
  }
  return Math.min(lower, Math.max(0, offsets.length - 2));
}

function findMatchRanges(text: string, query: string) {
  const normalizedQuery = query.trim().toLocaleLowerCase();
  if (normalizedQuery.length === 0) return [];

  const normalizedText = text.toLocaleLowerCase();
  const ranges: Array<Readonly<{ end: number; start: number }>> = [];
  let start = normalizedText.indexOf(normalizedQuery);
  while (start >= 0) {
    ranges.push({ end: start + normalizedQuery.length, start });
    start = normalizedText.indexOf(normalizedQuery, start + normalizedQuery.length);
  }
  return ranges;
}

function renderHighlightedText(
  text: string,
  textOffset: number,
  ranges: Array<Readonly<{ end: number; start: number }>>,
  active: boolean,
) {
  const boundaries = new Set([0, text.length]);
  for (const range of ranges) {
    const start = Math.max(0, range.start - textOffset);
    const end = Math.min(text.length, range.end - textOffset);
    if (start < end) {
      boundaries.add(start);
      boundaries.add(end);
    }
  }
  const orderedBoundaries = [...boundaries].toSorted((left, right) => left - right);
  return orderedBoundaries.slice(0, -1).map((start, index) => {
    const end = orderedBoundaries[index + 1] ?? text.length;
    const highlighted = ranges.some(
      (range) => range.start < textOffset + end && range.end > textOffset + start,
    );
    const contents = text.slice(start, end);
    return highlighted ? (
      <mark
        key={`${start}:${end}`}
        style={{ backgroundColor: active ? "#ff922b" : "#ffe066", color: "#101113" }}
      >
        {contents}
      </mark>
    ) : (
      contents
    );
  });
}
