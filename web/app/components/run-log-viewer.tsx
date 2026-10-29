import {
  Alert,
  Badge,
  Box,
  Button,
  Group,
  Modal,
  Paper,
  Stack,
  Switch,
  Text,
  TextInput,
  Title,
} from "@mantine/core";
import {
  IconArrowDown,
  IconArrowUp,
  IconDownload,
  IconMaximize,
  IconPlayerPause,
  IconPlayerPlay,
  IconSearch,
} from "@tabler/icons-react";
import {
  memo,
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent,
} from "react";

import { ANSI_DEFAULT_BACKGROUND, ANSI_DEFAULT_FOREGROUND, parseAnsi } from "~/ansi";
import type { Artifact, LogEvent } from "~/api/generated";
import { formatBinarySize } from "~/display";
import { BoundedLogBuffer, LOG_TAIL_MAX_LINES, LOG_TAIL_WINDOW_BYTES } from "~/log-buffer";

type LogStreamStatus =
  | "Connecting"
  | "Error"
  | "Incomplete"
  | "Live"
  | "Paused"
  | "Reconnecting"
  | "Retained";

const reconnectDelayMs = 1000;
const estimatedLineHeight = 20;
const virtualLineOverscan = 12;

export function RunLogViewer({
  artifact,
  runId,
}: Readonly<{ artifact?: Artifact; runId: string }>) {
  const initialOffsetBytes = Math.max(0, (artifact?.sizeBytes ?? 0) - LOG_TAIL_WINDOW_BYTES);
  const [buffer] = useState(() => new BoundedLogBuffer(initialOffsetBytes));

  const [, setBufferVersion] = useState(0);
  const [diagnostic, setDiagnostic] = useState<string>();
  const [fullscreenOpened, setFullscreenOpened] = useState(false);
  const [matchPosition, setMatchPosition] = useState(0);
  const [paused, setPaused] = useState(false);
  const [searchQuery, setSearchQuery] = useState("");
  const [status, setStatus] = useState<LogStreamStatus>("Connecting");
  const [streamFinished, setStreamFinished] = useState(false);
  const [wrapLines, setWrapLines] = useState(true);

  useEffect(() => {
    if (streamFinished || paused) return;

    let renderFrame: number | undefined;
    let reconnectTimer: number | undefined;
    let source: EventSource | undefined;
    let stopped = false;

    const scheduleRender = () => {
      if (renderFrame !== undefined) return;

      renderFrame = window.requestAnimationFrame(() => {
        renderFrame = undefined;
        setBufferVersion((current) => current + 1);
      });
    };
    const finishRender = () => {
      if (renderFrame === undefined) return;

      window.cancelAnimationFrame(renderFrame);
      renderFrame = undefined;
      setBufferVersion((current) => current + 1);
    };
    const stopWithError = (message: string) => {
      stopped = true;
      source?.close();
      setDiagnostic(message);
      setStatus("Error");
    };
    const connect = () => {
      setStatus("Connecting");
      const streamURL = new URL(`/api/v1/runs/${runId}/logs`, window.location.origin);
      streamURL.searchParams.set("offsetBytes", String(buffer.cursorBytes));
      source = new EventSource(streamURL);
      source.addEventListener("open", () => {
        setDiagnostic(undefined);
        setStatus("Live");
      });
      source.addEventListener("chunk", (event) => {
        try {
          const payload = JSON.parse((event as MessageEvent<string>).data) as LogEvent;
          const contents = decodeBase64(payload.contentsBase64);
          if (
            buffer.append({
              contents,
              nextOffsetBytes: payload.nextOffsetBytes,
              offsetBytes: payload.offsetBytes,
            })
          ) {
            scheduleRender();
          }
        } catch (error) {
          const detail = error instanceof Error ? error.message : "The event could not be decoded.";
          stopWithError(`The durable log stream is invalid. ${detail}`);
        }
      });
      source.addEventListener("sealed", () => {
        stopped = true;
        finishRender();
        setDiagnostic(undefined);
        setStatus("Retained");
        setStreamFinished(true);
        source?.close();
      });
      source.addEventListener("failed", () => {
        stopped = true;
        finishRender();
        setDiagnostic(
          "Only this durable prefix could be retained. The incomplete log cannot be downloaded as a complete artifact.",
        );
        setStatus("Incomplete");
        setStreamFinished(true);
        source?.close();
      });
      source.addEventListener("error", () => {
        source?.close();
        if (stopped || reconnectTimer !== undefined) return;

        setDiagnostic(`Connection lost; retrying from durable byte ${buffer.cursorBytes}.`);
        setStatus("Reconnecting");
        reconnectTimer = window.setTimeout(() => {
          reconnectTimer = undefined;
          connect();
        }, reconnectDelayMs);
      });
    };

    connect();

    return () => {
      stopped = true;
      source?.close();
      if (reconnectTimer !== undefined) window.clearTimeout(reconnectTimer);
      if (renderFrame !== undefined) window.cancelAnimationFrame(renderFrame);
    };
  }, [buffer, paused, runId, streamFinished]);

  const matchingLines = buffer.findMatchingLines(searchQuery);
  const selectedMatchPosition = Math.min(matchPosition, Math.max(matchingLines.length - 1, 0));

  const selectMatch = (direction: number) => {
    if (matchingLines.length === 0) return;

    if (!streamFinished) {
      setPaused(true);
      setStatus("Paused");
    }
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
  const activeMatchLine = paused || streamFinished ? selectedMatchLine : undefined;
  const canDownloadRaw =
    artifact !== undefined &&
    artifact.retentionState === "retained" &&
    (artifact.state === "sealed" || status === "Retained");
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
      <Group gap="xs" wrap="wrap">
        <Text c="dimmed" size="xs">
          {windowDescription} · {buffer.lineCount.toLocaleString()} lines
        </Text>
        <Badge color={statusColor} variant="light">
          {status}
        </Badge>
        {!streamFinished ? (
          <Button
            leftSection={
              paused ? (
                <IconPlayerPlay aria-hidden size={15} />
              ) : (
                <IconPlayerPause aria-hidden size={15} />
              )
            }
            onClick={() => {
              setPaused(!paused);
              setStatus(paused ? "Connecting" : "Paused");
            }}
            size="compact-sm"
            variant="light"
          >
            {paused ? "Follow" : "Pause"}
          </Button>
        ) : null}
        {canDownloadRaw && artifact ? (
          <Button
            component="a"
            href={`/api/v1/artifacts/${artifact.id}/download`}
            leftSection={<IconDownload aria-hidden size={15} />}
            size="compact-sm"
            variant="subtle"
          >
            Download raw log
          </Button>
        ) : (
          <Button
            disabled
            leftSection={<IconDownload aria-hidden size={15} />}
            size="compact-sm"
            title="Raw download is available after the complete log is retained."
            variant="subtle"
          >
            Download raw log
          </Button>
        )}
      </Group>
      <Group align="flex-end" gap="xs" wrap="wrap">
        <TextInput
          aria-label="Search visible log tail"
          leftSection={<IconSearch aria-hidden size={15} />}
          onChange={(event) => updateSearchQuery(event.currentTarget.value)}
          onKeyDown={handleSearchKeyDown}
          placeholder="Search visible tail"
          size="sm"
          value={searchQuery}
        />
        <Text aria-live="polite" c="dimmed" miw={110} size="xs">
          {searchQuery.trim().length === 0
            ? "Search is local"
            : matchingLines.length === 0
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
        <Switch
          checked={wrapLines}
          label="Wrap lines"
          onChange={(event) => setWrapLines(event.currentTarget.checked)}
          size="sm"
        />
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
            <Alert color={status === "Incomplete" ? "yellow" : "red"} title={status}>
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
            <Alert color={status === "Incomplete" ? "yellow" : "red"} title={status}>
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

function decodeBase64(value: string) {
  const binary = atob(value);
  const bytes = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index += 1) {
    bytes[index] = binary.charCodeAt(index);
  }
  return bytes;
}

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
