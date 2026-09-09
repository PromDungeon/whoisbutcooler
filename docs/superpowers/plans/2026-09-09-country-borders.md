# Country Borders Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Draw national borders on the braille map beneath the coastline, in their own muted ink, at regional zoom levels only.

**Architecture:** No new packages. `cmd/genworld` learns line geometries and produces a second embedded blob; `canvas` gains a fourth ink that loses shared cells to coastline; `world` gains `Borders()` and a self-gating `DrawBorders()`, with the antimeridian-safe segment loop extracted so it exists once rather than twice; `ui` gains a border style and an ink-to-style lookup.

**Tech Stack:** Go 1.25, Bubble Tea v1.3.10, Lipgloss v1.1.0. Natural Earth 110m admin_0 boundary lines (public domain).

**Spec:** `docs/superpowers/specs/2026-09-09-country-borders-design.md`

## Global Constraints

- Module path: `github.com/PromDungeon/whoisbutcooler`
- Go 1.25 or later
- Direct dependencies limited to `charmbracelet/bubbletea`, `charmbracelet/bubbles`, `charmbracelet/lipgloss`, `mattn/go-isatty`. Everything else comes from the standard library.
- All comments explain *why*, not *what*. No comment restates the line below it.
- **No test may touch the live network.** The generator's one-off download is a manual build step, not a test.
- `gofmt -l ./internal/ ./cmd/ .` must print nothing; `go vet ./...` and `staticcheck ./...` silent.
- Ink precedence is `InkNone < InkBorder < InkLand < InkPin`.
- `BorderMaxSpan = 120`: borders draw when `v.LonSpan <= BorderMaxSpan`.
- Border style is `lipgloss.AdaptiveColor{Light: "245", Dark: "242"}`.
- Expected border dataset shape: **333 polylines, 3108 points**.

## The one thing most likely to break this

`world.Draw` does not project vertices independently. Each segment's far endpoint is expressed relative to its near one through `geo.UnwrapLonDelta`, because per-vertex wrapping leaves a discontinuity at the viewport's antipodal meridian *and* another at exactly ±180 where the dataset is clipped — and either one paints a false line clean across the map. That cost two fix rounds on the coastline and was missed by a whole test suite.

`DrawBorders` must use the identical sequence. This plan extracts it into one `drawPolylines` helper rather than letting a second copy exist, and Task 3 re-runs the existing streak assertion over borders.

---

### Task 1: genworld learns line geometries, and the border blob

**Files:**
- Modify: `cmd/genworld/main.go`
- Create: `cmd/genworld/main_test.go`
- Create (generated): `internal/world/borders.bin`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `func polylines(fc featureCollection) ([][][2]float64, error)` in package `main` of `cmd/genworld`; the committed `internal/world/borders.bin` in the existing length-prefixed binary format (`uint32` polyline count, then per polyline a `uint32` point count and that many `float32` lon/lat pairs, little-endian).

- [ ] **Step 1: Write the failing test**

Create `cmd/genworld/main_test.go`:

```go
package main

import (
	"encoding/json"
	"testing"
)

func parse(t *testing.T, js string) featureCollection {
	t.Helper()
	var fc featureCollection
	if err := json.Unmarshal([]byte(js), &fc); err != nil {
		t.Fatal(err)
	}
	return fc
}

func TestPolylinesFlattensEveryGeometryNaturalEarthUses(t *testing.T) {
	// Land files arrive as polygons whose rings each become a polyline;
	// boundary files arrive as line strings, which are already polylines.
	// One generator has to read both.
	cases := []struct {
		name  string
		js    string
		lines int
		pts   int
	}{
		{
			name:  "polygon with two rings",
			js:    `{"features":[{"geometry":{"type":"Polygon","coordinates":[[[0,0],[1,1],[0,0]],[[2,2],[3,3],[2,2]]]}}]}`,
			lines: 2, pts: 6,
		},
		{
			name:  "multipolygon",
			js:    `{"features":[{"geometry":{"type":"MultiPolygon","coordinates":[[[[0,0],[1,1]]],[[[2,2],[3,3]]]]}}]}`,
			lines: 2, pts: 4,
		},
		{
			name:  "linestring",
			js:    `{"features":[{"geometry":{"type":"LineString","coordinates":[[0,0],[1,1],[2,2]]}}]}`,
			lines: 1, pts: 3,
		},
		{
			name:  "multilinestring",
			js:    `{"features":[{"geometry":{"type":"MultiLineString","coordinates":[[[0,0],[1,1]],[[2,2],[3,3]],[[4,4],[5,5]]]}}]}`,
			lines: 3, pts: 6,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := polylines(parse(t, tc.js))
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != tc.lines {
				t.Fatalf("got %d polylines, want %d", len(got), tc.lines)
			}
			n := 0
			for _, l := range got {
				n += len(l)
			}
			if n != tc.pts {
				t.Fatalf("got %d points, want %d", n, tc.pts)
			}
		})
	}
}

func TestPolylinesRejectsGeometryItCannotFlatten(t *testing.T) {
	// Silently dropping a geometry would produce a blob that is quietly
	// missing coastline, which is far harder to notice than a failed build.
	if _, err := polylines(parse(t, `{"features":[{"geometry":{"type":"Point","coordinates":[0,0]}}]}`)); err == nil {
		t.Fatal("an unsupported geometry was accepted")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/genworld/ -v`
Expected: FAIL to build — `undefined: polylines`.

- [ ] **Step 3: Extract the flattening and add the line geometries**

In `cmd/genworld/main.go`, replace the `var rings [][][2]float64` block inside `main` with a call, and add the function. The `main` body becomes:

```go
	var fc featureCollection
	if err := json.Unmarshal(raw, &fc); err != nil {
		log.Fatal(err)
	}

	rings, err := polylines(fc)
	if err != nil {
		log.Fatal(err)
	}
```

And add, after `main`:

```go
// polylines flattens a feature collection into bare polylines. Natural Earth
// ships land as polygons, whose every ring is a closed polyline, and boundaries
// as line strings, which already are polylines. One generator reads both so the
// two blobs cannot drift into different formats.
func polylines(fc featureCollection) ([][][2]float64, error) {
	var out [][][2]float64
	for _, f := range fc.Features {
		switch f.Geometry.Type {
		case "Polygon":
			var p [][][2]float64
			if err := json.Unmarshal(f.Geometry.Coordinates, &p); err != nil {
				return nil, err
			}
			out = append(out, p...)
		case "MultiPolygon":
			var mp [][][][2]float64
			if err := json.Unmarshal(f.Geometry.Coordinates, &mp); err != nil {
				return nil, err
			}
			for _, p := range mp {
				out = append(out, p...)
			}
		case "LineString":
			var l [][2]float64
			if err := json.Unmarshal(f.Geometry.Coordinates, &l); err != nil {
				return nil, err
			}
			out = append(out, l)
		case "MultiLineString":
			var ml [][][2]float64
			if err := json.Unmarshal(f.Geometry.Coordinates, &ml); err != nil {
				return nil, err
			}
			out = append(out, ml...)
		default:
			return nil, fmt.Errorf("unsupported geometry %q", f.Geometry.Type)
		}
	}
	return out, nil
}
```

Update the usage line in the package comment to mention both blobs:

```go
// Usage:
//   go run ./cmd/genworld -in ne_110m_land.geojson -out internal/world/world.bin
//   go run ./cmd/genworld -in ne_110m_admin_0_boundary_lines_land.geojson -out internal/world/borders.bin
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/genworld/ -v`
Expected: PASS, five subtests plus the rejection test.

- [ ] **Step 5: Generate the border blob**

```bash
curl -sL -o /tmp/ne_110m_borders.geojson \
  https://raw.githubusercontent.com/nvkelso/natural-earth-vector/master/geojson/ne_110m_admin_0_boundary_lines_land.geojson
go run ./cmd/genworld -in /tmp/ne_110m_borders.geojson -out internal/world/borders.bin
ls -l internal/world/borders.bin
```

Expected: `wrote 333 polylines, 3108 points`, and a file of roughly 26KB.

**If the counts differ, stop and report it** rather than adjusting anything to match — it means the upstream data changed and the plan's expected shape needs revisiting. (A prior version of this project asserted a wrong count for the coastline because it counted GeoJSON *features* rather than the rings the generator emits. Line files have no such trap: 329 `LineString` features plus 2 `MultiLineString` features holding 2 lines each gives 333.)

- [ ] **Step 6: Confirm the coastline blob is unchanged**

The generator was refactored, so prove it still produces byte-identical output for the input it already had:

```bash
curl -sL -o /tmp/ne_110m_land.geojson \
  https://raw.githubusercontent.com/nvkelso/natural-earth-vector/master/geojson/ne_110m_land.geojson
go run ./cmd/genworld -in /tmp/ne_110m_land.geojson -out /tmp/world_check.bin
cmp /tmp/world_check.bin internal/world/world.bin && echo "coastline blob unchanged"
```

Expected: `wrote 128 polylines, 5143 points` and `coastline blob unchanged`.

- [ ] **Step 7: Run the full suite and commit**

```bash
go test ./... && gofmt -l ./internal/ ./cmd/ . && go vet ./...
git add cmd/genworld/ internal/world/borders.bin
git commit -m "feat(genworld): read line geometries, add the border blob

Natural Earth ships land as polygons and boundaries as line strings. The
flattening moves into one polylines() function that reads both, so the two
embedded blobs cannot drift into different formats.

Verified the refactor is behaviour-preserving: regenerating world.bin from
the same input is byte-identical to the committed file.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 2: A fourth ink

**Files:**
- Modify: `internal/canvas/braille.go`
- Modify: `internal/canvas/braille_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `canvas.InkBorder`, with the constant order `InkNone`, `InkBorder`, `InkLand`, `InkPin`.

- [ ] **Step 1: Write the failing test**

Replace `TestHigherInkWinsWithinACell` in `internal/canvas/braille_test.go` with:

```go
func TestHigherInkWinsWithinACell(t *testing.T) {
	// A terminal cell carries one foreground color, so when two things share
	// a cell exactly one of them decides its color. The order is deliberate:
	// a border loses to coastline because a shoreline is the more important
	// fact, and the pin loses to nothing.
	cases := []struct {
		name       string
		first, second, want Ink
	}{
		{"land over border", InkBorder, InkLand, InkLand},
		{"border under land, drawn in the other order", InkLand, InkBorder, InkLand},
		{"pin over land", InkLand, InkPin, InkPin},
		{"pin over border", InkBorder, InkPin, InkPin},
		{"pin survives being drawn first", InkPin, InkLand, InkPin},
		{"border over nothing", InkNone, InkBorder, InkBorder},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := New(1, 1)
			c.Set(0, 0, tc.first)
			c.Set(1, 1, tc.second)
			if got := c.InkAt(0, 0); got != tc.want {
				t.Fatalf("InkAt = %v, want %v", got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/canvas/ -run HigherInkWins -v`
Expected: FAIL to build — `undefined: InkBorder`.

- [ ] **Step 3: Add the ink**

In `internal/canvas/braille.go`, replace the `Ink` constant block and its comment:

```go
// Ink identifies what was drawn into a cell. Higher values win when two inks
// land in the same cell, because a terminal cell carries only one foreground
// color. The order encodes which fact matters more where two overlap: a
// national border yields to a shoreline, and the pin yields to nothing.
type Ink uint8

const (
	InkNone Ink = iota
	InkBorder
	InkLand
	InkPin
)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/canvas/ -v`
Expected: PASS. Then `go test ./...` — the other packages must still pass, since nothing depends on the numeric values.

- [ ] **Step 5: Verify the test discriminates**

Reverse the ink order so `InkLand` sorts below `InkBorder`, re-run `go test ./internal/canvas/ -run HigherInkWins`, and confirm the "land over border" subtests fail. Restore the correct order. Record the output in your report.

- [ ] **Step 6: Commit**

```bash
git add internal/canvas/
git commit -m "feat(canvas): add InkBorder, ranked below coastline

A cell shared by a national border and a shoreline renders as the
shoreline, which is the more important geographic fact. The pin still
outranks both.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 3: Borders in the world package

**Files:**
- Modify: `internal/world/world.go`
- Modify: `internal/world/world_test.go`

**Interfaces:**
- Consumes: `canvas.InkBorder` and `canvas.InkLand` (Task 2); `internal/world/borders.bin` (Task 1).
- Produces:
  - `const BorderMaxSpan = 120.0`
  - `func Borders() [][]Point`
  - `func DrawBorders(c *canvas.Canvas, v geo.Viewport)`
  - unexported `func drawPolylines(c *canvas.Canvas, v geo.Viewport, lines [][]Point, ink canvas.Ink)`

- [ ] **Step 1: Write the failing tests**

Append to `internal/world/world_test.go`:

```go
func TestBordersDecodeToExpectedShape(t *testing.T) {
	lines := Borders()
	if len(lines) != 333 {
		t.Fatalf("got %d polylines, want 333", len(lines))
	}
	total := 0
	for _, l := range lines {
		total += len(l)
	}
	if total != 3108 {
		t.Fatalf("got %d points, want 3108", total)
	}
}

func TestBorderCoordinatesAreInRange(t *testing.T) {
	for i, l := range Borders() {
		for j, p := range l {
			if p.Lon < -180 || p.Lon > 180 || p.Lat < -90 || p.Lat > 90 {
				t.Fatalf("polyline %d point %d out of range: %+v", i, j, p)
			}
		}
	}
}

func inkedCells(c *canvas.Canvas) int {
	n := 0
	for _, row := range c.Render() {
		for _, r := range row {
			if r != ' ' {
				n++
			}
		}
	}
	return n
}

func TestDrawBordersRespectsTheZoomGate(t *testing.T) {
	// At the two widest rungs the map is being used to orient, and borders
	// there add ink without adding legibility. Centred on Europe, where there
	// is plenty of border to draw at any of these spans.
	cases := []struct {
		span float64
		want bool
	}{
		{360, false},
		{240, false},
		{BorderMaxSpan, true},
		{60, true},
		{30, true},
	}
	for _, tc := range cases {
		c := canvas.New(60, 20)
		v := geo.Viewport{CenterLat: 50, CenterLon: 10, LonSpan: tc.span}
		dw, dh := c.Size()
		DrawBorders(c, v.Clamp(dw, dh))
		drew := inkedCells(c) > 0
		if drew != tc.want {
			t.Errorf("LonSpan %.0f: drew=%v, want %v", tc.span, drew, tc.want)
		}
	}
}

func TestDrawBordersPaintsNoFullWidthStreak(t *testing.T) {
	// Borders go through the same per-segment longitude unwrapping as the
	// coastline, and the coastline shipped an antimeridian bug that painted a
	// line clean across the map. The same assertion has to cover this layer.
	const mapW, mapH = 46, 21
	views := map[string]geo.Viewport{
		"europe":   {CenterLat: 50, CenterLon: 10, LonSpan: BorderMaxSpan},
		"pacific":  {CenterLat: 0, CenterLon: 180, LonSpan: BorderMaxSpan},
		"berlin":   geo.FitTo(52.5, 13.4),
		"far east": geo.FitTo(66, 179),
	}
	for name, v := range views {
		c := canvas.New(mapW, mapH)
		dotW, dotH := c.Size()
		v = v.Clamp(dotW, dotH)
		DrawBorders(c, v)
		for i, row := range c.Render() {
			if reachesBelow60S(v, i, dotW, dotH) {
				continue
			}
			if run := longestInkRun(row); run >= mapW-4 {
				t.Errorf("%s: row %d has a %d-cell run of ink across a %d-cell map:\n%s",
					name, i, run, mapW, row)
			}
		}
	}
}

func TestDrawStillWorksAfterTheExtraction(t *testing.T) {
	// Draw's segment loop moves into drawPolylines in this task. Coastline
	// output must not shift as a result.
	c := canvas.New(60, 20)
	Draw(c, geo.World())
	if got := inkedCells(c); got < 100 {
		t.Fatalf("world coastline rendered only %d inked cells", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/world/ -v`
Expected: FAIL to build — `undefined: Borders`, `undefined: DrawBorders`, `undefined: BorderMaxSpan`.

- [ ] **Step 3: Implement**

In `internal/world/world.go`, add the embed beside the existing one:

```go
// Natural Earth 110m admin_0 boundary lines, public domain. Regenerate with
// cmd/genworld.
//
//go:embed borders.bin
var bordersBin []byte
```

Replace the `var (once ...)` block with:

```go
var (
	coastOnce   sync.Once
	coastLines  [][]Point
	borderOnce  sync.Once
	borderLines [][]Point
)
```

Update `Coastlines` to use the renamed vars, and add `Borders`:

```go
// Coastlines returns the decoded polylines, decoding once on first use.
func Coastlines() [][]Point {
	coastOnce.Do(func() { coastLines = decode(worldBin) })
	return coastLines
}

// Borders returns the decoded national boundary polylines, decoding once on
// first use.
func Borders() [][]Point {
	borderOnce.Do(func() { borderLines = decode(bordersBin) })
	return borderLines
}
```

Replace the body of `Draw` (keeping its doc comment, but moving the paragraph about unwrapping onto `drawPolylines`) and add the two new functions:

```go
// Draw rasterizes every coastline segment visible in the viewport.
func Draw(c *canvas.Canvas, v geo.Viewport) {
	drawPolylines(c, v, Coastlines(), canvas.InkLand)
}

// BorderMaxSpan is the widest viewport that draws borders. Above it the map is
// being used to orient rather than to read a region, and borders there cost
// ink without adding legibility. The gate lives here rather than in the UI so
// that callers draw unconditionally and the policy is written down once.
const BorderMaxSpan = 120.0

// DrawBorders rasterizes national boundaries, or nothing at all when the
// viewport is wider than BorderMaxSpan.
func DrawBorders(c *canvas.Canvas, v geo.Viewport) {
	if v.LonSpan > BorderMaxSpan {
		return
	}
	drawPolylines(c, v, Borders(), canvas.InkBorder)
}

// drawPolylines rasterizes every segment of every line. Segments are handed to
// LineF unclipped; the canvas rejects the offscreen ones, which is cheaper than
// testing visibility twice.
//
// Each segment's far endpoint is expressed relative to its near one via
// geo.UnwrapLonDelta rather than wrapped on its own: wrapping per vertex leaves
// a discontinuity at the viewport's antipodal meridian and another at exactly
// ±180 (the datasets are clipped there, so vertices sit on it), and either one
// paints a false line clean across the map. Unwrapping is per segment, not
// cumulative along the line, so a coastline that genuinely runs off one edge —
// Antarctica does, at every centre — still re-enters at the other.
//
// Both layers draw through here so that this reasoning exists once.
func drawPolylines(c *canvas.Canvas, v geo.Viewport, lines [][]Point, ink canvas.Ink) {
	dw, dh := c.Size()
	if dw == 0 || dh == 0 {
		return
	}
	for _, line := range lines {
		for i := 1; i < len(line); i++ {
			a, b := line[i-1], line[i]
			da := v.LonDelta(float64(a.Lon))
			db := geo.UnwrapLonDelta(da, v.LonDelta(float64(b.Lon)))
			ax, ay, _ := v.ProjectDelta(float64(a.Lat), da, dw, dh)
			bx, by, _ := v.ProjectDelta(float64(b.Lat), db, dw, dh)
			c.LineF(ax, ay, bx, by, ink)
		}
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/world/ -v`
Expected: PASS, including the pre-existing coastline tests.

- [ ] **Step 5: Verify the tests discriminate**

Run each of these in a scratch copy, confirm the named test fails, then discard the copy. Record the results in your report.

1. Remove the `if v.LonSpan > BorderMaxSpan { return }` guard → `TestDrawBordersRespectsTheZoomGate` must fail on the 360 and 240 rows.
2. In `drawPolylines`, replace `db := geo.UnwrapLonDelta(da, v.LonDelta(float64(b.Lon)))` with `db := v.LonDelta(float64(b.Lon))` → `TestDrawBordersPaintsNoFullWidthStreak` **and** the existing `TestDrawPaintsNoFullWidthStreak` must both fail. That single mutation failing both tests is the evidence that the two layers really do share one code path.
3. Change `DrawBorders` to pass `canvas.InkLand` → no test need fail here; note it, because it means nothing in this package pins the ink. Task 4 covers that at the UI level.

- [ ] **Step 6: Commit**

```bash
git add internal/world/
git commit -m "feat(world): draw national borders below the zoom gate

Borders draw at LonSpan 120 and tighter and not above it, where the map is
being used to orient. The gate sits beside the data so callers draw
unconditionally.

Draw's segment loop moves into drawPolylines, shared with DrawBorders, so
the per-segment longitude unwrapping that the coastline needed exists once
rather than being re-derived for a second layer.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 4: Wire it into the UI

**Files:**
- Modify: `internal/ui/styles.go`
- Modify: `internal/ui/model.go`
- Modify: `internal/ui/model_test.go`

**Interfaces:**
- Consumes: `world.DrawBorders(c, v)` and `world.BorderMaxSpan` (Task 3); `canvas.InkBorder` (Task 2).
- Produces: nothing later tasks depend on. This is the last task.

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/model_test.go`:

```go
// countBraille counts the braille glyphs in a rendered frame, ignoring the
// panel, borders and help text around the map.
func countBraille(frame string) int {
	n := 0
	for _, r := range frame {
		if r >= 0x2801 && r <= 0x28FF {
			n++
		}
	}
	return n
}

func TestBordersAppearExactlyAtTheZoomGate(t *testing.T) {
	// Comparing two viewports a thousandth of a degree apart isolates the
	// borders: the coastline they render is identical at both spans, so any
	// difference in ink is the border layer switching on. A wider comparison
	// would confound borders with the different coastline a different zoom
	// shows.
	m := sized(New(nil, ""), 120, 34)
	centre := geo.Viewport{CenterLat: 50, CenterLon: 10}

	on := centre
	on.LonSpan = world.BorderMaxSpan
	m.view = on
	inked := countBraille(m.View())

	off := centre
	off.LonSpan = world.BorderMaxSpan + 0.001
	m.view = off
	bare := countBraille(m.View())

	if inked <= bare {
		t.Fatalf("no extra ink at the gate: %d inked at LonSpan %v, %d just above it",
			inked, world.BorderMaxSpan, bare)
	}
}

func TestBordersAreAbsentFromTheWorldView(t *testing.T) {
	m := sized(New(nil, ""), 120, 34)
	m.view = geo.World()
	withUI := countBraille(m.View())

	mapW, mapH := m.mapCells()
	c := canvas.New(mapW, mapH)
	world.Draw(c, m.view)
	coastOnly := 0
	for _, row := range c.Render() {
		for _, r := range row {
			if r >= 0x2801 && r <= 0x28FF {
				coastOnly++
			}
		}
	}

	if withUI != coastOnly {
		t.Fatalf("world view drew %d glyphs, coastline alone draws %d — borders leaked past the gate",
			withUI, coastOnly)
	}
}

func TestEveryInkHasItsOwnStyle(t *testing.T) {
	// Test colors, not appearance: lipgloss renders styles identically when
	// the profile has no color, which it does under `go test`, so a rendered
	// frame cannot tell these apart.
	for _, ink := range []canvas.Ink{canvas.InkBorder, canvas.InkLand, canvas.InkPin} {
		if _, ok := inkStyles[ink]; !ok {
			t.Errorf("ink %v has no style", ink)
		}
	}
	if inkStyles[canvas.InkBorder].GetForeground() == inkStyles[canvas.InkLand].GetForeground() {
		t.Error("borders and coastline share a color, so a border is indistinguishable from a shoreline")
	}
	if inkStyles[canvas.InkBorder].GetForeground() == inkStyles[canvas.InkPin].GetForeground() {
		t.Error("borders and the pin share a color")
	}
}
```

Add `"github.com/PromDungeon/whoisbutcooler/internal/canvas"` and `"github.com/PromDungeon/whoisbutcooler/internal/world"` to the test file's imports.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/ -v`
Expected: FAIL to build — `undefined: inkStyles`.

- [ ] **Step 3: Add the border style and the lookup**

In `internal/ui/styles.go`, add after `pinStyle`:

```go
	// Borders are context rather than the subject, so they sit below both the
	// amber coastline and the cyan pin in weight as well as in ink precedence.
	borderStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "245", Dark: "242"})
```

And at the end of the file, after the `var (...)` block:

```go
// inkStyles maps each ink to how it renders. A lookup rather than a chain of
// comparisons, so a new ink is one line here instead of another branch in
// colorize.
var inkStyles = map[canvas.Ink]lipgloss.Style{
	canvas.InkBorder: borderStyle,
	canvas.InkLand:   landStyle,
	canvas.InkPin:    pinStyle,
}
```

Add `"github.com/PromDungeon/whoisbutcooler/internal/canvas"` to `styles.go`'s imports.

- [ ] **Step 4: Use the lookup in colorize**

In `internal/ui/model.go`, replace the pin-or-land branch inside `colorize`:

```go
			seg := string(runes[start:x])
			style, ok := inkStyles[c.InkAt(start, y)]
			if !ok {
				// InkNone: the run is blank, so the style never shows.
				style = landStyle
			}
			b.WriteString(style.Render(seg))
			start = x
```

- [ ] **Step 5: Draw the borders**

In `internal/ui/model.go`, in `View`, add the call before the coastline:

```go
	c := canvas.New(mapW, mapH)
	world.DrawBorders(c, m.view)
	world.Draw(c, m.view)
```

Order does not affect color — the canvas keeps the highest ink per cell either way — but drawing the background layer first matches how the map reads.

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/ui/ -v`, then `go test ./...`
Expected: PASS throughout.

- [ ] **Step 7: Verify the tests discriminate**

In a scratch copy, confirm each of these fails the named test, then discard the copy:

1. Remove the `world.DrawBorders(c, m.view)` call → `TestBordersAppearExactlyAtTheZoomGate` fails.
2. Change `DrawBorders` in `internal/world/world.go` to pass `canvas.InkLand` → `TestBordersAppearExactlyAtTheZoomGate` still passes (it counts glyphs, not colors), but this is the case Task 3 flagged as unpinned: confirm `TestEveryInkHasItsOwnStyle` does **not** catch it either, and say so in your report. It is a known, accepted gap — the ink a layer draws with is verified by eye in Step 9, not by the suite.
3. Point `borderStyle` at the same `AdaptiveColor` as `landStyle` → `TestEveryInkHasItsOwnStyle` fails.

- [ ] **Step 8: Full verification**

```bash
go test ./... && go test -race ./internal/... && gofmt -l ./internal/ ./cmd/ . && go vet ./... && staticcheck ./...
```

- [ ] **Step 9: Look at it**

```bash
go run . --once 8.8.8.8      # California — coast, few borders nearby
go run . --once 78.46.0.1    # Germany — border-rich, the case this feature is for
```

Expected: at `FitTo`'s 60-degree span, national borders visible in a dimmer grey beneath the amber coastline, with the cyan pin still the most prominent thing on the map. Paste the frame into your report.

Then check the gate by eye: run `go run .`, press `tab` to focus the map, `0` to reset to the world view, and confirm borders are absent; press `+` until the span reaches 120 and confirm they appear.

- [ ] **Step 10: Update the README and commit**

Add a sentence to the README's description of the map noting that national borders appear once zoomed in past a continental view. Then:

```bash
git add internal/ui/ README.md
git commit -m "feat(ui): render national borders beneath the coastline

Borders draw in a muted grey below the amber coastline, appearing once the
viewport is a continental view or tighter — which every lookup is, since
FitTo opens at 60 degrees.

colorize picks its style from a lookup rather than a branch per ink.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

## Verification checklist

- [ ] `go test ./...` passes
- [ ] `go test -race ./internal/...` passes
- [ ] `gofmt -l ./internal/ ./cmd/ .` prints nothing
- [ ] `go vet ./...` and `staticcheck ./...` are silent
- [ ] `internal/world/borders.bin` is committed and about 26KB
- [ ] Regenerating `world.bin` from the same input is byte-identical to the committed file
- [ ] `go run . --once 8.8.8.8` shows borders under the coastline, pin still dominant
- [ ] World view (`0` with no result) shows no borders
- [ ] Every new test was confirmed to fail against the defect it names
