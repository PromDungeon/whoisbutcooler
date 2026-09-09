# Country borders — Design

**Date:** 2026-09-09
**Status:** Approved
**Builds on:** [2026-09-08-whoisbutcooler-design.md](2026-09-08-whoisbutcooler-design.md)

## Summary

Draw national borders on the braille map beneath the coastline, in their own
muted ink, at regional zoom levels only.

## Motivation

The map answers "where on earth is this address" but not "which country is
that". A pin in central Europe is ambiguous without borders, and the panel's
country code is a poor substitute for seeing the shape.

## Evidence

Prototyped against the real dataset and the real canvas before designing, on a
78x20 cell map. Inked cells, coastline alone versus coastline plus borders:

| LonSpan | coast | + borders | added |
| ---: | ---: | ---: | ---: |
| 360 | 544 | 653 | +109 (20%) |
| 240 | 525 | 752 | +227 (43%) |
| 120 | 396 | 603 | +207 (52%) |
| 60 | 413 | 628 | +215 (52%) |
| 30 | 284 | 561 | +277 (98%) |
| 15 | 41 | 294 | +253 (617%) |

Two things fall out of this. The proportion added *rises* as you zoom in, which
looks alarming until you see why: at LonSpan 15 there are only 41 cells of
coastline on screen, so borders are carrying almost all the information. That is
exactly when they are most wanted.

And rendering the world view with borders in the coastline's own ink made Europe
and the Middle East unreadable — not because of the extra ink, but because a
national border and a shoreline were indistinguishable. That observation drove
both the separate ink and the zoom gate.

## Goals

- See which country a pin sits in, at the zoom levels where that question arises
- Keep the coastline the dominant shape
- No new dependencies, no runtime downloads, no configuration

## Non-goals

- Sub-national boundaries (states, provinces)
- Country labels or names — there is no room at braille density
- Disputed-boundary styling: Natural Earth's `boundary_lines_land` is taken as-is
- A user-facing toggle. The zoom gate is automatic; revisit only if it proves wrong.

## Decisions

### Data: Natural Earth 110m admin_0 boundary lines

333 polylines, 3,108 coordinate pairs, roughly 26KB packed as `float32` — smaller
than the 41KB coastline. Embedded as `internal/world/borders.bin` in the same
length-prefixed binary format, public domain, no attribution obligation in code.

**110m specifically**, matching the coastline. Mixing 50m borders with a 110m coast
would render detail on one layer that the other lacks, and 50m is 19,859 points —
six times the data for resolution a braille dot cannot show.

### A fourth ink, below the coastline

`canvas` gains `InkBorder`. Precedence becomes:

```
InkNone < InkBorder < InkLand < InkPin
```

A terminal cell carries one foreground colour, so where a border runs along a
shoreline the shoreline wins it, and the pin still beats everything. This
renumbers the two existing constants; the canvas tests assert the *behaviour*
that higher ink wins rather than any numeric value, so they continue to hold.

### The zoom gate: `BorderMaxSpan = 120`

`DrawBorders` draws nothing when `v.LonSpan > BorderMaxSpan`. Borders appear from
the 120-degree rung inward — including `FitTo`'s 60, so every lookup gets them —
and stay off at 240 and 360, where the map is being used to orient rather than to
read a region.

The gate lives beside the data rather than in the UI so that the caller draws
unconditionally and there is exactly one place the policy is written down.

## Architecture

No new packages. Three existing ones change.

```
cmd/genworld/          gains LineString / MultiLineString and an output flag
internal/canvas/       gains InkBorder; existing ink constants renumbered
internal/world/        gains borders.bin, Borders(), DrawBorders(),
                       and a shared drawPolylines() helper
internal/ui/           gains borderStyle; colorize becomes an ink -> style lookup
```

### Drawing

`DrawBorders` uses the same `LonDelta` / `UnwrapLonDelta` / `ProjectDelta`
sequence as `Draw`. This is not optional: per-vertex normalisation alone paints
false lines across the map wherever a polyline crosses the antimeridian, which
cost two fix rounds on the coastline. Rather than re-derive it, `Draw` and
`DrawBorders` both call one `drawPolylines(c, v, lines, ink)`, so the unwrapping
exists once. `Draw` currently inlines that loop; extracting it is a targeted tidy
of code this change already touches.

### Styling

`borderStyle` is `lipgloss.AdaptiveColor{Light: "245", Dark: "242"}` — a mid
grey that recedes against the amber coastline (Dark 214) and the cyan pin (Dark
87), on both light and dark terminals. Dimmer than either, deliberately: borders
are context, not the subject. Tunable in one line if it reads too faint in
practice. `colorize` currently branches
pin-or-land; it becomes a lookup from ink to style so a fourth ink does not mean
a third branch.

## Error handling

| Case | Behaviour |
| --- | --- |
| `LonSpan > BorderMaxSpan` | `DrawBorders` returns without drawing |
| Zero-size canvas | Returns immediately, as `Draw` already does |
| Truncated `borders.bin` | Decodes what it can and returns it, matching `Coastlines()`. The blob is embedded at compile time and pinned by a test, so this is unreachable in practice. |

## Testing

- **canvas** — precedence across all four inks, including that a border loses its
  cell to coastline and that the pin still wins over both.
- **world** — `Borders()` decodes to exactly 333 polylines and 3,108 points;
  `DrawBorders` draws nothing at LonSpan 360 and 240 and something at 120 and 60;
  and the existing no-full-width-streak assertion runs over borders too, since
  they pass through the same unwrapping that produced the coastline's antimeridian
  bug.
- **ui** — a rendered frame at LonSpan 60 contains border ink; one at world view
  does not.
- **genworld** — `LineString` and `MultiLineString` both convert.

Every new test is to be checked by mutation: revert the behaviour it names and
confirm it fails. Seven tests in this project have shipped green against the exact
bug they claimed to pin, so a test nobody has watched fail is not evidence.

## Open questions

None.
