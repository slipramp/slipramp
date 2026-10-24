import { createTheme, type MantineColorsTuple } from "@mantine/core";

const lilac: MantineColorsTuple = [
  "#f8f3ff",
  "#eee5ff",
  "#dccaff",
  "#c7abff",
  "#b28dfb",
  "#a174f6",
  "#955fef",
  "#804bd5",
  "#6d3eb8",
  "#5b3597",
];

const mint: MantineColorsTuple = [
  "#effcf8",
  "#dcf8ef",
  "#baf0df",
  "#8be4ca",
  "#60d8b6",
  "#42cda8",
  "#2ebc98",
  "#24977b",
  "#227862",
  "#1f6252",
];

const sakura: MantineColorsTuple = [
  "#fff0f6",
  "#ffe3ed",
  "#ffc9dd",
  "#f7a8c7",
  "#ef83ad",
  "#e9679b",
  "#e4518c",
  "#cc3d78",
  "#b53268",
  "#9f2a59",
];

const sun: MantineColorsTuple = [
  "#fff9db",
  "#fff3bf",
  "#ffe88a",
  "#ffdc5c",
  "#ffd43b",
  "#fcc419",
  "#fab005",
  "#f59f00",
  "#e08a00",
  "#c77700",
];

export const sliprampTheme = createTheme({
  colors: { lilac, mint, sakura, sun },
  cursorType: "pointer",
  defaultRadius: "md",
  fontFamily: '"Manrope Variable", "Avenir Next", "Segoe UI", sans-serif',
  fontFamilyMonospace: '"SFMono-Regular", Consolas, "Liberation Mono", monospace',
  headings: {
    fontFamily: '"Manrope Variable", "Avenir Next", "Segoe UI", sans-serif',
    fontWeight: "700",
  },
  primaryColor: "lilac",
  primaryShade: { dark: 4, light: 6 },
  components: {
    Anchor: { defaultProps: { size: "sm" } },
    Badge: { defaultProps: { radius: "sm" } },
    Button: { defaultProps: { radius: "md", size: "sm" } },
    Paper: { defaultProps: { radius: "lg" } },
  },
});
