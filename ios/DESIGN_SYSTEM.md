# Slipramp iOS design system

Slipramp uses an **operational kawaii** visual language: calm, friendly, and a
little playful, while remaining recognizably native to iOS. The product is an
operator tool, so hierarchy, state, accessibility, and safe decisions always
win over decoration.

The system is implemented in `Slipramp/DesignSystem`. This document is the
decision record for extending it.

## Principles

1. **Native underneath.** Keep `NavigationStack`, `TabView`, sheets, alerts,
   confirmation dialogs, SF Symbols, Dynamic Type, semantic labels, and native
   control behavior. Custom styling must not make an iOS control behave like a
   web control.
2. **Status before sparkle.** Operational state must be readable from a label
   and symbol, never color alone. Semantic status colors are consistent across
   Inbox, Runs, Promotions, and logs.
3. **Kawaii is punctuation.** Soft geometry, lavender and blush accents, and the
   axolotl give the app warmth. The mascot belongs in onboarding, success,
   waiting, and empty states—not in dense data rows or destructive decisions.
4. **Calm by default.** Screens use one quiet canvas, white or charcoal cards,
   generous spacing, and one strong accent. Gradients belong to brand artwork,
   not ordinary controls or data surfaces.
5. **Operator density is intentional.** Overview screens use cards and strong
   hierarchy. Detail, diff, log, and decision screens stay denser and closer to
   native lists and forms.

## Foundations

### Color

| Role | Light reference | Use |
| --- | --- | --- |
| Ramp violet | `#6D4AEF` | Primary action, selection, active work |
| Soft violet | `#A38AF5` | Decorative accent only |
| Axolotl blush | `#F78FB3` | Decorative warmth only |
| Hard-hat gold | `#F4BD45` | Brand art and small highlights |
| Canvas | `#F7F5FC` | Screen background |
| Surface | `#FFFFFF` | Cards and grouped content |
| Muted surface | `#F0EBFA` | Metrics and input wells |
| Border | `#DBD4E8` | Quiet surface separation |

Dark mode uses the same semantic roles with an ink canvas, charcoal-violet
surfaces, and system-adaptive text. Settings lets the operator follow the
system appearance or explicitly select light or dark; the preference applies
app-wide and persists on the device. Do not branch on color scheme in feature
views; add or adjust a token in `SliprampTheme`.

Status colors are semantic:

- accent: running or selected work;
- info: assigned, preparing, or connecting;
- success: completed successfully or all clear;
- warning: queued, pending, or needs attention;
- danger: failed, rejected, unavailable, or destructive;
- neutral: canceled, expired, or inactive.

Status components always pair color with a written label and, where space
allows, an SF Symbol.

### Type

Use Apple text styles so Dynamic Type works without a parallel type scale.

- large title: top-level product or screen identity;
- title 2/3: overview metrics and section headings;
- headline: row and card titles;
- body/subheadline: primary supporting content;
- caption: metadata, counts, hashes, and helper text;
- monospaced design: commits, logs, identifiers, and code only.

Rounded system type is reserved for short expressive display moments. Body
copy and operational data use the standard system design.

### Space and shape

The spacing scale is 4, 8, 12, 16, 24, and 32 points. Feature views should use
these tokens instead of introducing near-duplicates.

- controls: 12-point corner radius and at least 44-point hit targets;
- cards: 18-point corner radius;
- branded hero surfaces: 26-point corner radius;
- pills: capsules, only for compact state or category labels.

Cards use a one-point quiet border and restrained shadow. Avoid nesting cards
or turning every row into a pill.

## Components

| Component | Purpose |
| --- | --- |
| `SliprampScreenBackground` | Adaptive screen canvas; decoration is opt-in for onboarding |
| `SliprampCard` | Overview grouping and tappable summary surface |
| `SliprampSectionHeader` | Overview section title and optional count |
| `SliprampStatusPill` | Compact semantic state with label and optional symbol |
| `SliprampMetricCard` | One numeric activity measure |
| `SliprampCallout` | Inline error, warning, or informational message |
| `SliprampEmptyState` | Empty, success, and retry states; mascot is opt-in |
| `SliprampLoadingState` | Branded but native progress state |
| `SliprampField` | Labeled onboarding field with native text input behavior |
| `SliprampPrimaryButtonStyle` | One prominent action per surface |
| `SliprampMark` / `SliprampMascot` | Static brand artwork with resilient symbol fallback |
| `SliprampAnimatedMascot` | Four-frame production loop with native resource caching and Reduce Motion behavior |
| `SliprampMascotActivityView` | One expressive Run or Promotion status moment for detail screens |

Prefer composing these primitives over adding feature-specific variants. Add a
new primitive only after the same visual and behavioral pattern appears in at
least two features.

## Screen patterns

- **Onboarding:** decorative canvas, mascot, one card, one primary action.
- **Overview:** large native title, activity summary, titled card groups,
  pull-to-refresh.
- **Detail:** native inset list with branded status pill and semantic callouts.
- **Decision:** native sheet or confirmation dialog; state the permanence and
  use system destructive roles.
- **Logs and diffs:** content-first, monospaced, horizontally scrollable when
  necessary, with chrome limited to status and filtering.
- **Settings:** calm grouped cards and native buttons; no mascot is required.

## Mascot rules

Use one mascot moment on a screen at most. It may greet, reassure, wait, or
celebrate, but it must not obscure operational facts or make failure and
destructive confirmation feel unserious. Mascot art is decorative and hidden
from accessibility; nearby copy carries the meaning.

Do not randomly select a pose on every render. When stateful poses are added,
choose them deterministically from durable product identity, matching the web
mascot behavior.

Run and Promotion detail screens use the generated production catalog and the
same FNV-1a selection, frame duration, and reviewed frame holds as the web.
Keep the optimized sprites in the app bundle so mascot presentation remains
instant and offline. Decode only the visible pose; do not preload the catalog.

## Accessibility and motion

- Support Dynamic Type without clipped status or action labels.
- Maintain 44-point minimum interactive targets.
- Never encode state only with hue, animation, or mascot pose.
- Keep decorative images hidden from VoiceOver.
- Respect Reduce Motion for any future animation. Functional transitions must
  work with animation disabled.
- Use system contrast and semantic foreground styles wherever a custom brand
  color is not necessary.

## Adding a feature

Start with the closest screen pattern and existing components. Map every new
state to a `SliprampTone`, verify light and dark mode, test at a large Dynamic
Type size, and check VoiceOver labels for compact status UI. If a feature needs
a new visual rule, update this document and the design-system implementation in
the same change.
