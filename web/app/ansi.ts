export const ANSI_DEFAULT_BACKGROUND = "#101113";
export const ANSI_DEFAULT_FOREGROUND = "#f1f3f5";

export type AnsiTextStyle = Readonly<{
  backgroundColor?: string;
  color?: string;
  fontStyle?: "italic";
  fontWeight?: 700;
  opacity?: number;
  textDecoration?: string;
  visibility?: "hidden";
}>;

export type AnsiTextSegment = Readonly<{
  style: AnsiTextStyle;
  text: string;
}>;

type AnsiColor = Readonly<
  { kind: "palette"; value: number } | { blue: number; green: number; kind: "rgb"; red: number }
>;

type AnsiState = {
  background?: AnsiColor;
  bold: boolean;
  dim: boolean;
  foreground?: AnsiColor;
  hidden: boolean;
  inverse: boolean;
  italic: boolean;
  strikethrough: boolean;
  underline: boolean;
};

const palette = [
  "#666666",
  "#cd3131",
  "#0dbc79",
  "#e5e510",
  "#2472c8",
  "#bc3fbc",
  "#11a8cd",
  "#e5e5e5",
  "#858585",
  "#f14c4c",
  "#23d18b",
  "#f5f543",
  "#3b8eea",
  "#d670d6",
  "#29b8db",
  "#ffffff",
] as const;

const colorCubeValues = [0, 95, 135, 175, 215, 255] as const;

export function parseAnsi(input: string): AnsiTextSegment[] {
  const segments: AnsiTextSegment[] = [];
  const state = defaultState();
  let position = 0;
  let textStart = 0;

  const pushText = (text: string) => {
    if (text.length === 0) return;

    const style = styleForState(state);
    const previous = segments.at(-1);
    if (previous !== undefined && stylesMatch(previous.style, style)) {
      segments[segments.length - 1] = { style, text: previous.text + text };
      return;
    }

    segments.push({ style, text });
  };

  while (position < input.length) {
    const characterCode = input.charCodeAt(position);
    const isEscape = characterCode === 0x1b;
    const isControl =
      (characterCode < 0x20 &&
        characterCode !== 0x09 &&
        characterCode !== 0x0a &&
        characterCode !== 0x0d) ||
      characterCode === 0x7f;

    if (!isEscape && !isControl && characterCode !== 0x9b) {
      position += 1;
      continue;
    }

    pushText(input.slice(textStart, position));

    if (characterCode === 0x9b) {
      const sequence = readControlSequence(input, position + 1);
      if (sequence !== undefined && sequence.final === "m") {
        applyGraphicsRendition(state, sequence.parameters);
      }
      position = sequence?.end ?? input.length;
      textStart = position;
      continue;
    }

    if (!isEscape) {
      position += 1;
      textStart = position;
      continue;
    }

    const introducer = input[position + 1];
    if (introducer === "[") {
      const sequence = readControlSequence(input, position + 2);
      if (sequence !== undefined && sequence.final === "m") {
        applyGraphicsRendition(state, sequence.parameters);
      }
      position = sequence?.end ?? input.length;
    } else if ("]PX^_".includes(introducer ?? "")) {
      position = readControlStringEnd(input, position + 2) ?? input.length;
    } else {
      position = Math.min(position + 2, input.length);
    }
    textStart = position;
  }

  pushText(input.slice(textStart));
  return segments;
}

function defaultState(): AnsiState {
  return {
    bold: false,
    dim: false,
    hidden: false,
    inverse: false,
    italic: false,
    strikethrough: false,
    underline: false,
  };
}

function readControlSequence(input: string, start: number) {
  for (let position = start; position < input.length; position += 1) {
    const characterCode = input.charCodeAt(position);
    if (characterCode >= 0x40 && characterCode <= 0x7e) {
      return {
        end: position + 1,
        final: input[position],
        parameters: input.slice(start, position),
      };
    }
  }

  return undefined;
}

function readControlStringEnd(input: string, start: number) {
  for (let position = start; position < input.length; position += 1) {
    if (input.charCodeAt(position) === 0x07) return position + 1;
    if (input.charCodeAt(position) === 0x1b && input[position + 1] === "\\") {
      return position + 2;
    }
  }

  return undefined;
}

function applyGraphicsRendition(state: AnsiState, value: string) {
  const parameters = (value.length === 0 ? ["0"] : value.split(";")).map((parameter) =>
    parameter.length === 0 ? 0 : Number(parameter),
  );

  if (parameters.some((parameter) => !Number.isInteger(parameter))) return;

  for (let index = 0; index < parameters.length; index += 1) {
    const parameter = parameters[index];
    if (parameter === undefined) continue;

    if (parameter === 0) {
      Object.assign(state, defaultState());
      delete state.background;
      delete state.foreground;
    } else if (parameter === 1) {
      state.bold = true;
    } else if (parameter === 2) {
      state.dim = true;
    } else if (parameter === 3) {
      state.italic = true;
    } else if (parameter === 4) {
      state.underline = true;
    } else if (parameter === 7) {
      state.inverse = true;
    } else if (parameter === 8) {
      state.hidden = true;
    } else if (parameter === 9) {
      state.strikethrough = true;
    } else if (parameter === 21) {
      state.bold = false;
    } else if (parameter === 22) {
      state.bold = false;
      state.dim = false;
    } else if (parameter === 23) {
      state.italic = false;
    } else if (parameter === 24) {
      state.underline = false;
    } else if (parameter === 27) {
      state.inverse = false;
    } else if (parameter === 28) {
      state.hidden = false;
    } else if (parameter === 29) {
      state.strikethrough = false;
    } else if (parameter >= 30 && parameter <= 37) {
      state.foreground = { kind: "palette", value: parameter - 30 };
    } else if (parameter === 38 || parameter === 48) {
      const parsed = readExtendedColor(parameters, index + 1);
      if (parsed !== undefined) {
        if (parameter === 38) state.foreground = parsed.color;
        else state.background = parsed.color;
        index = parsed.end;
      }
    } else if (parameter === 39) {
      delete state.foreground;
    } else if (parameter >= 40 && parameter <= 47) {
      state.background = { kind: "palette", value: parameter - 40 };
    } else if (parameter === 49) {
      delete state.background;
    } else if (parameter >= 90 && parameter <= 97) {
      state.foreground = { kind: "palette", value: parameter - 90 + 8 };
    } else if (parameter >= 100 && parameter <= 107) {
      state.background = { kind: "palette", value: parameter - 100 + 8 };
    }
  }
}

function readExtendedColor(parameters: number[], start: number) {
  const mode = parameters[start];
  if (mode === 5) {
    const paletteIndex = parameters[start + 1];
    if (paletteIndex === undefined || paletteIndex < 0 || paletteIndex > 255) return undefined;

    return {
      color: { kind: "palette", value: paletteIndex } satisfies AnsiColor,
      end: start + 1,
    };
  }

  if (mode !== 2) return undefined;

  const red = parameters[start + 1];
  const green = parameters[start + 2];
  const blue = parameters[start + 3];
  if (red === undefined || green === undefined || blue === undefined) return undefined;
  if ([red, green, blue].some((channel) => channel < 0 || channel > 255)) return undefined;

  return {
    color: { blue, green, kind: "rgb", red } satisfies AnsiColor,
    end: start + 3,
  };
}

function styleForState(state: AnsiState): AnsiTextStyle {
  let foreground = resolveColor(state.foreground, state.bold);
  let background = resolveColor(state.background, false);

  if (state.inverse) {
    [foreground, background] = [
      background ?? ANSI_DEFAULT_BACKGROUND,
      foreground ?? ANSI_DEFAULT_FOREGROUND,
    ];
  }

  const decorations = [
    state.underline ? "underline" : "",
    state.strikethrough ? "line-through" : "",
  ]
    .filter(Boolean)
    .join(" ");

  return {
    ...(background === undefined ? {} : { backgroundColor: background }),
    ...(foreground === undefined ? {} : { color: foreground }),
    ...(state.italic ? { fontStyle: "italic" as const } : {}),
    ...(state.bold ? { fontWeight: 700 as const } : {}),
    ...(state.dim ? { opacity: 0.65 } : {}),
    ...(decorations.length === 0 ? {} : { textDecoration: decorations }),
    ...(state.hidden ? { visibility: "hidden" as const } : {}),
  };
}

function resolveColor(color: AnsiColor | undefined, bold: boolean) {
  if (color === undefined) return undefined;
  if (color.kind === "rgb") return `rgb(${color.red} ${color.green} ${color.blue})`;

  let colorIndex = color.value;
  if (bold && colorIndex < 8) colorIndex += 8;
  if (colorIndex < palette.length) return palette[colorIndex];

  if (colorIndex < 232) {
    const cubeIndex = colorIndex - 16;
    const red = colorCubeValues[Math.floor(cubeIndex / 36)];
    const green = colorCubeValues[Math.floor((cubeIndex % 36) / 6)];
    const blue = colorCubeValues[cubeIndex % 6];
    return `rgb(${red} ${green} ${blue})`;
  }

  const channel = 8 + (colorIndex - 232) * 10;
  return `rgb(${channel} ${channel} ${channel})`;
}

function stylesMatch(left: AnsiTextStyle, right: AnsiTextStyle) {
  return JSON.stringify(left) === JSON.stringify(right);
}
