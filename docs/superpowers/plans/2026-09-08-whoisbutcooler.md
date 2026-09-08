# whoisbutcooler Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A Go TUI that takes an IP or hostname and renders a braille-vector world map with a pin at its geolocated position, alongside a seven-line summary replacing raw whois output.

**Architecture:** Four internal packages with one job each. `canvas` is a pure braille rasterizer that knows nothing about maps. `geo` is projection and viewport math with no I/O. `world` owns the embedded coastline dataset and draws it through the other two. `lookup` talks to HTTP APIs and knows nothing about terminals. Only `ui` depends on both halves, which keeps map math testable without a network and the lookup merge testable without a terminal.

**Tech Stack:** Go 1.25, Bubble Tea v1.3.10, Bubbles v1.0.0, Lipgloss v1.1.0. Natural Earth 110m land (public domain) as coastline source. ipwho.is + rdap.org over HTTPS, no API keys.

## Global Constraints

- Module path: `github.com/PromDungeon/whoisbutcooler`
- Go 1.25 or later; toolchain present is go1.25.1 darwin/arm64
- **No API keys, no signup, no config file.** The tool must work the instant it is installed.
- **No test touches the live network.** Every HTTP test uses `httptest` with recorded fixtures.
- Direct dependencies are limited to `charmbracelet/bubbletea`, `charmbracelet/bubbles`, and `charmbracelet/lipgloss`. Everything else comes from the standard library.
- All comments explain *why*, not *what*. No comment restates the line below it.

## Deviations from the approved spec

One structural change, flagged for veto:

- The spec called for `teatest` golden-frame tests in the UI. This plan asserts against `Model.View()` directly instead. Reason: `View()` is a pure function of model state, so driving it with injected messages is deterministic and needs no dependency, whereas golden frames over a live program are timing-sensitive and regenerate noisily. Coverage is the same or better.
- The spec placed `coastline.go` and `world.bin` inside `internal/canvas`. This plan moves them to their own package, **`internal/world`**. Reason: it keeps `canvas` a pure rasterizer with zero dependencies, while `world` depends on `canvas` *and* `geo` to do its drawing. Merging them would give `canvas` a dependency on `geo` and a 41KB embedded data blob, for no gain. No other package's responsibilities change.

## Findings from live API probes

These were verified against the real services on 2026-09-08 and **contradict the illustrative examples in the spec**. The fixtures below are real responses, not invented ones.

1. **ipwho.is returns `connection.asn` as a JSON number** (`15169`), not the string `"AS15169"`. It must be formatted, and `0` means absent.
2. **ipwho.is signals failure with HTTP 200 and `{"success": false, "message": "..."}`.** Checking the status code alone will treat errors as successes.
3. **The RDAP abuse entity is nested inside the registrant entity**, not at the top level. For `8.8.8.8` the top-level entity has `roles: ["registrant"]`, and the abuse contact is at `entities[0].entities[0]`. **A flat scan of top-level entities finds nothing and silently returns an empty abuse contact for every lookup.** The walk must be recursive. This is the single most likely bug in the project.
4. The real abuse contact for `8.8.8.8` is `network-abuse@google.com`, and ipwho.is reports the city as San Jose. The spec's `abuse@google.com` / Mountain View were illustrative.
5. Natural Earth 110m land is 127 polygons totalling 5,143 coordinate pairs — about 41KB as packed `float32`.

---

### Task 1: Braille canvas

The rasterizer everything else draws through. Scaffolding folds in here because this is the first task that needs a module.

**Files:**
- Create: `go.mod`, `internal/canvas/braille.go`, `internal/canvas/braille_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type Ink uint8` with `InkNone`, `InkLand`, `InkPin` (higher wins on collision)
  - `const DotsX = 2`, `DotsY = 4`
  - `func New(w, h int) *Canvas` — dimensions in **cells**
  - `func (c *Canvas) Size() (dotW, dotH int)`
  - `func (c *Canvas) Set(x, y int, ink Ink)` — **dot** coordinates, out-of-range is a no-op
  - `func (c *Canvas) InkAt(col, row int) Ink` — **cell** coordinates
  - `func (c *Canvas) Line(x0, y0, x1, y1 int, ink Ink)`
  - `func (c *Canvas) LineF(x0, y0, x1, y1 float64, ink Ink)` — clips before rasterizing
  - `func (c *Canvas) Render() []string` — one string per cell row

- [ ] **Step 1: Scaffold the module**

```bash
cd /Users/billketchum/terminal_projects/whoisbutcooler
go mod init github.com/PromDungeon/whoisbutcooler
go get github.com/charmbracelet/bubbletea@v1.3.10 \
       github.com/charmbracelet/bubbles@v1.0.0 \
       github.com/charmbracelet/lipgloss@v1.1.0
mkdir -p internal/canvas internal/geo internal/world internal/lookup internal/ui cmd/genworld
```

- [ ] **Step 2: Write the failing tests**

Create `internal/canvas/braille_test.go`:

```go
package canvas

import (
	"strings"
	"testing"
)

func TestEmptyCellRendersSpaceNotBlankBraille(t *testing.T) {
	// U+2800 is a legitimate "blank braille" glyph, but many terminal fonts
	// draw it as a visible box. Empty cells must be ordinary spaces.
	c := New(2, 1)
	got := c.Render()
	if len(got) != 1 || got[0] != "  " {
		t.Fatalf("empty canvas = %q, want one row of two spaces", got)
	}
}

func TestSingleDotBitmask(t *testing.T) {
	// The braille bit layout is not sequential: dots 1-6 fill the top three
	// rows column-major, then dots 7-8 were appended below. Each of the eight
	// positions is pinned here because guessing the layout produces a map
	// that looks almost right, which is the worst kind of wrong.
	cases := []struct {
		name string
		x, y int
		want rune
	}{
		{"dot1 col0 row0", 0, 0, '⠁'},
		{"dot2 col0 row1", 0, 1, '⠂'},
		{"dot3 col0 row2", 0, 2, '⠄'},
		{"dot7 col0 row3", 0, 3, '⡀'},
		{"dot4 col1 row0", 1, 0, '⠈'},
		{"dot5 col1 row1", 1, 1, '⠐'},
		{"dot6 col1 row2", 1, 2, '⠠'},
		{"dot8 col1 row3", 1, 3, '⢀'},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := New(1, 1)
			c.Set(tc.x, tc.y, InkLand)
			got := []rune(c.Render()[0])[0]
			if got != tc.want {
				t.Fatalf("Set(%d,%d) = U+%04X, want U+%04X", tc.x, tc.y, got, tc.want)
			}
		})
	}
}

func TestAllDotsSetRendersFullCell(t *testing.T) {
	c := New(1, 1)
	for x := 0; x < DotsX; x++ {
		for y := 0; y < DotsY; y++ {
			c.Set(x, y, InkLand)
		}
	}
	if got := []rune(c.Render()[0])[0]; got != '⣿' {
		t.Fatalf("full cell = U+%04X, want U+28FF", got)
	}
}

func TestSetOutOfRangeIsNoOp(t *testing.T) {
	c := New(1, 1)
	for _, p := range [][2]int{{-1, 0}, {0, -1}, {2, 0}, {0, 4}, {99, 99}} {
		c.Set(p[0], p[1], InkLand) // must not panic
	}
	if got := c.Render()[0]; got != " " {
		t.Fatalf("out-of-range writes leaked: %q", got)
	}
}

func TestHigherInkWinsWithinACell(t *testing.T) {
	// A terminal cell carries one foreground color, so the pin must survive
	// sharing a cell with coastline.
	c := New(1, 1)
	c.Set(0, 0, InkPin)
	c.Set(1, 1, InkLand)
	if got := c.InkAt(0, 0); got != InkPin {
		t.Fatalf("InkAt = %v, want InkPin", got)
	}
}

func TestLineFClipsFarOffscreenSegment(t *testing.T) {
	// Projection can yield coordinates millions of dots away when a coastline
	// vertex sits outside a zoomed-in viewport. Rasterizing that unclipped
	// would walk every intermediate pixel.
	c := New(4, 2)
	c.LineF(-1e6, -1e6, -999000, -999000, InkLand)
	for _, row := range c.Render() {
		for _, r := range row {
			if r != ' ' {
				t.Fatalf("offscreen segment drew %q", row)
			}
		}
	}
}

func TestLineFDrawsClippedCrossingSegment(t *testing.T) {
	c := New(4, 2)
	c.LineF(-1e5, 4, 1e5, 4, InkLand) // horizontal line straight across
	rows := c.Render()
	if strings.TrimSpace(rows[1]) == "" {
		t.Fatalf("crossing segment drew nothing: %q", rows)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/canvas/ -v`
Expected: FAIL — `undefined: New`, `undefined: InkLand`, etc.

- [ ] **Step 4: Implement the canvas**

Create `internal/canvas/braille.go`:

```go
// Package canvas rasterizes points and lines onto a grid of Unicode braille
// cells. It is deliberately ignorant of maps, coordinates, and IP addresses:
// its only domain knowledge is the braille encoding itself.
package canvas

import "math"

// Ink identifies what was drawn into a cell. Higher values win when two inks
// land in the same cell, because a terminal cell carries only one foreground
// color and the pin must not be swallowed by coastline.
type Ink uint8

const (
	InkNone Ink = iota
	InkLand
	InkPin
)

// Dots per cell in each axis.
const (
	DotsX = 2
	DotsY = 4
)

// dotBit maps a dot position within a cell to its bit in the braille bitmask.
// The layout is not sequential. Dots 1-6 fill the top three rows in
// column-major order, and dots 7-8 were bolted on below when 6-dot braille was
// extended to 8, landing at 0x40 and 0x80.
var dotBit = [DotsX][DotsY]uint8{
	{0x01, 0x02, 0x04, 0x40},
	{0x08, 0x10, 0x20, 0x80},
}

const brailleBase = 0x2800

// Canvas is a fixed-size grid of braille cells.
type Canvas struct {
	w, h int // in cells
	bits []uint8
	ink  []Ink
}

// New returns a canvas w cells wide and h cells tall.
func New(w, h int) *Canvas {
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	return &Canvas{w: w, h: h, bits: make([]uint8, w*h), ink: make([]Ink, w*h)}
}

// Size returns the drawable area in dots.
func (c *Canvas) Size() (int, int) { return c.w * DotsX, c.h * DotsY }

// Set lights the dot at dot-coordinates (x, y). Out-of-range coordinates are
// ignored so that callers may clip lazily.
func (c *Canvas) Set(x, y int, ink Ink) {
	dw, dh := c.Size()
	if x < 0 || y < 0 || x >= dw || y >= dh || ink == InkNone {
		return
	}
	i := (y/DotsY)*c.w + x/DotsX
	c.bits[i] |= dotBit[x%DotsX][y%DotsY]
	if ink > c.ink[i] {
		c.ink[i] = ink
	}
}

// InkAt reports the ink of the cell at cell-coordinates (col, row).
func (c *Canvas) InkAt(col, row int) Ink {
	if col < 0 || row < 0 || col >= c.w || row >= c.h {
		return InkNone
	}
	return c.ink[row*c.w+col]
}

// Render returns one string per cell row. Cells with no dots render as a space
// rather than U+2800, which some fonts draw as a visible box.
func (c *Canvas) Render() []string {
	rows := make([]string, c.h)
	buf := make([]rune, c.w)
	for y := 0; y < c.h; y++ {
		for x := 0; x < c.w; x++ {
			if b := c.bits[y*c.w+x]; b == 0 {
				buf[x] = ' '
			} else {
				buf[x] = rune(brailleBase + int(b))
			}
		}
		rows[y] = string(buf)
	}
	return rows
}

// Line rasterizes a segment between two dot coordinates with Bresenham's
// algorithm. Callers with coordinates that may fall far outside the canvas
// should use LineF, which clips first.
func (c *Canvas) Line(x0, y0, x1, y1 int, ink Ink) {
	dx, dy := abs(x1-x0), -abs(y1-y0)
	sx, sy := 1, 1
	if x0 > x1 {
		sx = -1
	}
	if y0 > y1 {
		sy = -1
	}
	err := dx + dy
	for {
		c.Set(x0, y0, ink)
		if x0 == x1 && y0 == y1 {
			return
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			err += dx
			y0 += sy
		}
	}
}

// Cohen-Sutherland region codes.
const (
	clipInside = 0
	clipLeft   = 1 << 0
	clipRight  = 1 << 1
	clipBelow  = 1 << 2
	clipAbove  = 1 << 3
)

func (c *Canvas) outCode(x, y float64) int {
	dw, dh := c.Size()
	code := clipInside
	if x < 0 {
		code |= clipLeft
	} else if x > float64(dw-1) {
		code |= clipRight
	}
	if y < 0 {
		code |= clipAbove
	} else if y > float64(dh-1) {
		code |= clipBelow
	}
	return code
}

// LineF rasterizes a segment given in floating-point dot coordinates, clipping
// it to the canvas with Cohen-Sutherland first. Projection routinely produces
// coordinates far outside the viewport, and rasterizing those unclipped would
// walk every intermediate dot between here and nowhere.
func (c *Canvas) LineF(x0, y0, x1, y1 float64, ink Ink) {
	dw, dh := c.Size()
	if dw == 0 || dh == 0 {
		return
	}
	if math.IsNaN(x0) || math.IsNaN(y0) || math.IsNaN(x1) || math.IsNaN(y1) {
		return
	}
	xmax, ymax := float64(dw-1), float64(dh-1)
	o0, o1 := c.outCode(x0, y0), c.outCode(x1, y1)
	for {
		if o0|o1 == clipInside {
			break // wholly inside
		}
		if o0&o1 != 0 {
			return // wholly outside one edge
		}
		o := o0
		if o == clipInside {
			o = o1
		}
		var x, y float64
		switch {
		case o&clipAbove != 0:
			x, y = x0+(x1-x0)*(0-y0)/(y1-y0), 0
		case o&clipBelow != 0:
			x, y = x0+(x1-x0)*(ymax-y0)/(y1-y0), ymax
		case o&clipLeft != 0:
			x, y = 0, y0+(y1-y0)*(0-x0)/(x1-x0)
		case o&clipRight != 0:
			x, y = xmax, y0+(y1-y0)*(xmax-x0)/(x1-x0)
		}
		if o == o0 {
			x0, y0, o0 = x, y, c.outCode(x, y)
		} else {
			x1, y1, o1 = x, y, c.outCode(x, y)
		}
	}
	c.Line(int(math.Round(x0)), int(math.Round(y0)), int(math.Round(x1)), int(math.Round(y1)), ink)
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/canvas/ -v`
Expected: PASS, all seven tests.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/canvas/
git commit -m "feat(canvas): braille rasterizer with clipped line drawing

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 2: Projection and viewport

Pure math, no I/O, no drawing. Equirectangular because braille resolution does not reward Mercator and a braille dot is very nearly square, so the world renders un-stretched with no aspect fudge factor.

**Files:**
- Create: `internal/geo/viewport.go`, `internal/geo/viewport_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type Viewport struct { CenterLat, CenterLon, LonSpan float64 }`
  - `var ZoomLadder = []float64{360, 240, 120, 60, 30, 15, 8, 4, 2}`
  - `const FitSpan = 60.0`
  - `func World() Viewport`
  - `func FitTo(lat, lon float64) Viewport`
  - `func (v Viewport) LatSpan(dotW, dotH int) float64`
  - `func (v Viewport) Project(lat, lon float64, dotW, dotH int) (x, y float64, visible bool)`
  - `func (v Viewport) ZoomIn() Viewport`
  - `func (v Viewport) ZoomOut() Viewport`
  - `func (v Viewport) Pan(fracLon, fracLat float64, dotW, dotH int) Viewport`
  - `func (v Viewport) Clamp(dotW, dotH int) Viewport`
  - `func NormLon(lon float64) float64` — wraps to [-180, 180)

- [ ] **Step 1: Write the failing tests**

Create `internal/geo/viewport_test.go`:

```go
package geo

import (
	"math"
	"testing"
)

const eps = 1e-9

func TestNormLonWraps(t *testing.T) {
	cases := []struct{ in, want float64 }{
		{0, 0}, {179, 179}, {180, -180}, {181, -179},
		{-181, 179}, {360, 0}, {540, -180},
	}
	for _, tc := range cases {
		if got := NormLon(tc.in); math.Abs(got-tc.want) > eps {
			t.Errorf("NormLon(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestProjectCenterLandsAtCanvasCenter(t *testing.T) {
	v := Viewport{CenterLat: 37.34, CenterLon: -121.89, LonSpan: 60}
	x, y, vis := v.Project(37.34, -121.89, 200, 100)
	if !vis {
		t.Fatal("viewport center is not visible")
	}
	if math.Abs(x-100) > eps || math.Abs(y-50) > eps {
		t.Fatalf("center projected to (%v,%v), want (100,50)", x, y)
	}
}

func TestProjectHandlesAntimeridianWrap(t *testing.T) {
	// A viewport centred on the date line must place +179 and -179 on
	// opposite sides of centre, not 358 degrees apart.
	v := Viewport{CenterLat: 0, CenterLon: 180, LonSpan: 60}
	xEast, _, visE := v.Project(0, 179, 200, 100)
	xWest, _, visW := v.Project(0, -179, 200, 100)
	if !visE || !visW {
		t.Fatalf("expected both visible, got %v %v", visE, visW)
	}
	if !(xEast < 100 && xWest > 100) {
		t.Fatalf("wrap broken: 179 -> %v, -179 -> %v (centre 100)", xEast, xWest)
	}
}

func TestProjectReportsOffscreenAsNotVisible(t *testing.T) {
	v := Viewport{CenterLat: 0, CenterLon: 0, LonSpan: 10}
	if _, _, vis := v.Project(0, 90, 200, 100); vis {
		t.Fatal("point 90 degrees away reported visible in a 10-degree span")
	}
}

func TestLatIncreasesUpward(t *testing.T) {
	// Screen y grows downward while latitude grows northward; getting this
	// backwards flips the map vertically and still looks plausible.
	v := World()
	_, yNorth, _ := v.Project(45, 0, 200, 100)
	_, ySouth, _ := v.Project(-45, 0, 200, 100)
	if !(yNorth < ySouth) {
		t.Fatalf("north (y=%v) is not above south (y=%v)", yNorth, ySouth)
	}
}

func TestFitToUsesRegionalSpan(t *testing.T) {
	v := FitTo(51.5, -0.12)
	if v.LonSpan != FitSpan {
		t.Fatalf("FitTo span = %v, want %v", v.LonSpan, FitSpan)
	}
	if math.Abs(v.CenterLon-(-0.12)) > eps {
		t.Fatalf("FitTo centre lon = %v, want -0.12", v.CenterLon)
	}
}

func TestZoomClampsToLadderEnds(t *testing.T) {
	v := World()
	for i := 0; i < 20; i++ {
		v = v.ZoomIn()
	}
	if v.LonSpan != ZoomLadder[len(ZoomLadder)-1] {
		t.Fatalf("over-zoomed to %v, want %v", v.LonSpan, ZoomLadder[len(ZoomLadder)-1])
	}
	for i := 0; i < 20; i++ {
		v = v.ZoomOut()
	}
	if v.LonSpan != ZoomLadder[0] {
		t.Fatalf("over-zoomed-out to %v, want %v", v.LonSpan, ZoomLadder[0])
	}
}

func TestPanClampsAtPoles(t *testing.T) {
	v := Viewport{CenterLat: 0, CenterLon: 0, LonSpan: 60}
	for i := 0; i < 50; i++ {
		v = v.Pan(0, 1, 200, 100) // pan north repeatedly
	}
	half := v.LatSpan(200, 100) / 2
	if v.CenterLat+half > 90+eps {
		t.Fatalf("viewport ran past the north pole: centre %v, half-span %v", v.CenterLat, half)
	}
}

func TestPanWrapsLongitudeInsteadOfClamping(t *testing.T) {
	v := Viewport{CenterLat: 0, CenterLon: 170, LonSpan: 60}
	for i := 0; i < 10; i++ {
		v = v.Pan(1, 0, 200, 100)
	}
	if v.CenterLon < -180 || v.CenterLon >= 180 {
		t.Fatalf("centre lon %v escaped [-180,180)", v.CenterLon)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/geo/ -v`
Expected: FAIL — `undefined: NormLon`, `undefined: Viewport`, etc.

- [ ] **Step 3: Implement the viewport**

Create `internal/geo/viewport.go`:

```go
// Package geo projects latitude/longitude onto a dot grid and manages the
// pan/zoom viewport. It performs no I/O and draws nothing.
package geo

import "math"

// ZoomLadder lists the selectable longitude spans, widest first.
var ZoomLadder = []float64{360, 240, 120, 60, 30, 15, 8, 4, 2}

// FitSpan is the span FitTo opens at: enough context to recognise the region,
// tight enough that a city is roughly locatable.
const FitSpan = 60.0

// Viewport is the visible window, described by its centre and how many degrees
// of longitude it spans. Latitude span is derived from the canvas aspect so
// that the projection stays isotropic.
type Viewport struct {
	CenterLat, CenterLon float64
	LonSpan              float64
}

// World returns the whole-earth view.
func World() Viewport { return Viewport{LonSpan: ZoomLadder[0]} }

// FitTo frames a pin at regional zoom.
func FitTo(lat, lon float64) Viewport {
	return Viewport{CenterLat: lat, CenterLon: NormLon(lon), LonSpan: FitSpan}
}

// NormLon wraps a longitude into [-180, 180).
func NormLon(lon float64) float64 {
	lon = math.Mod(lon+180, 360)
	if lon < 0 {
		lon += 360
	}
	return lon - 180
}

// LatSpan derives the visible latitude range from the longitude span and the
// canvas shape. A braille dot is half a cell wide and a quarter of a cell tall,
// and a terminal cell is roughly twice as tall as it is wide, so a dot is very
// nearly square and no correction factor is needed.
func (v Viewport) LatSpan(dotW, dotH int) float64 {
	if dotW <= 0 {
		return v.LonSpan
	}
	return v.LonSpan * float64(dotH) / float64(dotW)
}

// Project maps a coordinate to dot-space. Longitude difference is normalised
// first so that a viewport straddling the antimeridian works without special
// cases. visible reports whether the result lands on the canvas.
func (v Viewport) Project(lat, lon float64, dotW, dotH int) (x, y float64, visible bool) {
	latSpan := v.LatSpan(dotW, dotH)
	if v.LonSpan == 0 || latSpan == 0 {
		return 0, 0, false
	}
	dLon := NormLon(lon - v.CenterLon)
	x = (dLon/v.LonSpan + 0.5) * float64(dotW)
	// Screen y grows downward, latitude grows northward, hence the inversion.
	y = (0.5 - (lat-v.CenterLat)/latSpan) * float64(dotH)
	visible = x >= 0 && x < float64(dotW) && y >= 0 && y < float64(dotH)
	return x, y, visible
}

// ZoomIn moves one step down the ladder, stopping at the tightest span.
func (v Viewport) ZoomIn() Viewport { return v.zoom(+1) }

// ZoomOut moves one step up the ladder, stopping at the whole world.
func (v Viewport) ZoomOut() Viewport { return v.zoom(-1) }

func (v Viewport) zoom(dir int) Viewport {
	i := nearestRung(v.LonSpan) + dir
	if i < 0 {
		i = 0
	}
	if i >= len(ZoomLadder) {
		i = len(ZoomLadder) - 1
	}
	v.LonSpan = ZoomLadder[i]
	return v
}

func nearestRung(span float64) int {
	best, bestDiff := 0, math.Inf(1)
	for i, s := range ZoomLadder {
		if d := math.Abs(s - span); d < bestDiff {
			best, bestDiff = i, d
		}
	}
	return best
}

// Pan shifts the centre by a fraction of the current span. Longitude wraps;
// latitude clamps, because there is nothing past a pole.
func (v Viewport) Pan(fracLon, fracLat float64, dotW, dotH int) Viewport {
	v.CenterLon = NormLon(v.CenterLon + fracLon*v.LonSpan)
	v.CenterLat += fracLat * v.LatSpan(dotW, dotH)
	return v.Clamp(dotW, dotH)
}

// Clamp keeps the window inside [-90, 90], centring it when the span is taller
// than the planet. Callers apply it after any change that did not already know
// the canvas dimensions, which is why FitTo and the zoom steps leave latitude
// alone: neither knows how tall the map area is.
func (v Viewport) Clamp(dotW, dotH int) Viewport {
	half := v.LatSpan(dotW, dotH) / 2
	if half >= 90 {
		v.CenterLat = 0
		return v
	}
	v.CenterLat = math.Max(-90+half, math.Min(90-half, v.CenterLat))
	return v
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/geo/ -v`
Expected: PASS, all nine tests.

- [ ] **Step 5: Commit**

```bash
git add internal/geo/
git commit -m "feat(geo): equirectangular projection with pan/zoom viewport

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 3: World data and coastline drawing

Turns Natural Earth GeoJSON into a 41KB embedded blob and draws it. Binary rather than GeoJSON so startup is a slice walk, not a JSON parse.

**Files:**
- Create: `cmd/genworld/main.go`, `internal/world/world.go`, `internal/world/world_test.go`
- Create (generated): `internal/world/world.bin`

**Interfaces:**
- Consumes: `canvas.Canvas`, `canvas.InkLand`, `canvas.InkPin`, `canvas.LineF`, `canvas.Set`, `canvas.Size`; `geo.Viewport`, `geo.Viewport.Project`.
- Produces:
  - `type Point struct { Lon, Lat float32 }`
  - `func Coastlines() [][]Point`
  - `func Draw(c *canvas.Canvas, v geo.Viewport)`
  - `func DrawPin(c *canvas.Canvas, v geo.Viewport, lat, lon float64) bool`

**Binary format:** little-endian. `uint32` polyline count, then per polyline a `uint32` point count followed by that many `float32` lon, `float32` lat pairs.

- [ ] **Step 1: Write the generator**

Create `cmd/genworld/main.go`:

```go
// Command genworld converts a Natural Earth land GeoJSON file into the compact
// binary polyline format embedded by internal/world. It is committed so the
// derivation is reproducible, but it runs rarely: coastlines do not move.
//
// Usage: go run ./cmd/genworld -in ne_110m_land.geojson -out internal/world/world.bin
package main

import (
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
)

type featureCollection struct {
	Features []struct {
		Geometry struct {
			Type        string          `json:"type"`
			Coordinates json.RawMessage `json:"coordinates"`
		} `json:"geometry"`
	} `json:"features"`
}

func main() {
	in := flag.String("in", "", "path to Natural Earth land GeoJSON")
	out := flag.String("out", "", "path to write the binary blob")
	flag.Parse()
	if *in == "" || *out == "" {
		log.Fatal("both -in and -out are required")
	}

	raw, err := os.ReadFile(*in)
	if err != nil {
		log.Fatal(err)
	}
	var fc featureCollection
	if err := json.Unmarshal(raw, &fc); err != nil {
		log.Fatal(err)
	}

	var rings [][][2]float64
	for _, f := range fc.Features {
		switch f.Geometry.Type {
		case "Polygon":
			var p [][][2]float64
			if err := json.Unmarshal(f.Geometry.Coordinates, &p); err != nil {
				log.Fatal(err)
			}
			rings = append(rings, p...)
		case "MultiPolygon":
			var mp [][][][2]float64
			if err := json.Unmarshal(f.Geometry.Coordinates, &mp); err != nil {
				log.Fatal(err)
			}
			for _, p := range mp {
				rings = append(rings, p...)
			}
		default:
			log.Fatalf("unsupported geometry %q", f.Geometry.Type)
		}
	}

	fh, err := os.Create(*out)
	if err != nil {
		log.Fatal(err)
	}
	defer fh.Close()

	if err := binary.Write(fh, binary.LittleEndian, uint32(len(rings))); err != nil {
		log.Fatal(err)
	}
	total := 0
	for _, ring := range rings {
		if err := binary.Write(fh, binary.LittleEndian, uint32(len(ring))); err != nil {
			log.Fatal(err)
		}
		for _, pt := range ring {
			// float32 is ~7 significant digits, far finer than a braille dot
			// at any zoom this tool offers.
			if err := binary.Write(fh, binary.LittleEndian, [2]float32{float32(pt[0]), float32(pt[1])}); err != nil {
				log.Fatal(err)
			}
		}
		total += len(ring)
	}
	fmt.Printf("wrote %d polylines, %d points to %s\n", len(rings), total, *out)
}
```

- [ ] **Step 2: Generate the dataset**

```bash
curl -sL -o /tmp/ne_110m_land.geojson \
  https://raw.githubusercontent.com/nvkelso/natural-earth-vector/master/geojson/ne_110m_land.geojson
go run ./cmd/genworld -in /tmp/ne_110m_land.geojson -out internal/world/world.bin
ls -l internal/world/world.bin
```

Expected: `wrote 127 polylines, 5143 points`, and a file of roughly 41KB.

- [ ] **Step 3: Write the failing tests**

Create `internal/world/world_test.go`:

```go
package world

import (
	"strings"
	"testing"

	"github.com/PromDungeon/whoisbutcooler/internal/canvas"
	"github.com/PromDungeon/whoisbutcooler/internal/geo"
)

func TestCoastlinesDecodeToExpectedShape(t *testing.T) {
	lines := Coastlines()
	if len(lines) != 127 {
		t.Fatalf("got %d polylines, want 127", len(lines))
	}
	total := 0
	for _, l := range lines {
		total += len(l)
	}
	if total != 5143 {
		t.Fatalf("got %d points, want 5143", total)
	}
}

func TestCoastlineCoordinatesAreInRange(t *testing.T) {
	for i, l := range Coastlines() {
		for j, p := range l {
			if p.Lon < -180 || p.Lon > 180 || p.Lat < -90 || p.Lat > 90 {
				t.Fatalf("polyline %d point %d out of range: %+v", i, j, p)
			}
		}
	}
}

func TestDrawWorldProducesPlausibleInkCoverage(t *testing.T) {
	// Coastlines are outlines, not fills, so a world view should mark a
	// meaningful but modest fraction of cells. All-blank means projection or
	// decoding is broken; near-total means coordinates are being smeared
	// across the canvas.
	c := canvas.New(100, 30)
	Draw(c, geo.World())
	filled, total := 0, 0
	for _, row := range c.Render() {
		for _, r := range row {
			total++
			if r != ' ' {
				filled++
			}
		}
	}
	ratio := float64(filled) / float64(total)
	if ratio < 0.05 || ratio > 0.60 {
		t.Fatalf("ink coverage %.3f outside plausible range [0.05, 0.60]", ratio)
	}
}

func TestDrawIsDeterministic(t *testing.T) {
	render := func() string {
		c := canvas.New(60, 20)
		Draw(c, geo.World())
		return strings.Join(c.Render(), "\n")
	}
	if render() != render() {
		t.Fatal("two identical renders differed")
	}
}

func TestDrawPinMarksTheProjectedCell(t *testing.T) {
	c := canvas.New(80, 24)
	v := geo.FitTo(37.34, -121.89)
	if !DrawPin(c, v, 37.34, -121.89) {
		t.Fatal("DrawPin reported the centred pin as offscreen")
	}
	dw, dh := c.Size()
	x, y, _ := v.Project(37.34, -121.89, dw, dh)
	if got := c.InkAt(int(x)/canvas.DotsX, int(y)/canvas.DotsY); got != canvas.InkPin {
		t.Fatalf("pin cell ink = %v, want InkPin", got)
	}
}

func TestDrawPinReportsOffscreen(t *testing.T) {
	c := canvas.New(80, 24)
	v := geo.Viewport{CenterLat: 0, CenterLon: 0, LonSpan: 4}
	if DrawPin(c, v, -80, 170) {
		t.Fatal("a pin far outside a 4-degree viewport reported visible")
	}
}
```

- [ ] **Step 4: Run tests to verify they fail**

Run: `go test ./internal/world/ -v`
Expected: FAIL — `undefined: Coastlines`, `undefined: Draw`, `undefined: DrawPin`.

- [ ] **Step 5: Implement the world package**

Create `internal/world/world.go`:

```go
// Package world owns the embedded coastline dataset and draws it through the
// canvas rasterizer using the geo projection. It is the only package that
// knows the map data exists.
package world

import (
	"bytes"
	"encoding/binary"
	_ "embed"
	"sync"

	"github.com/PromDungeon/whoisbutcooler/internal/canvas"
	"github.com/PromDungeon/whoisbutcooler/internal/geo"
)

// Natural Earth 110m land, public domain. Regenerate with cmd/genworld.
//
//go:embed world.bin
var worldBin []byte

// Point is a coastline vertex. float32 keeps the blob small and is far finer
// than a braille dot at any zoom this tool offers.
type Point struct{ Lon, Lat float32 }

var (
	once  sync.Once
	lines [][]Point
)

// Coastlines returns the decoded polylines, decoding once on first use.
func Coastlines() [][]Point {
	once.Do(func() { lines = decode(worldBin) })
	return lines
}

func decode(b []byte) [][]Point {
	r := bytes.NewReader(b)
	var n uint32
	if binary.Read(r, binary.LittleEndian, &n) != nil {
		return nil
	}
	out := make([][]Point, 0, n)
	for i := uint32(0); i < n; i++ {
		var count uint32
		if binary.Read(r, binary.LittleEndian, &count) != nil {
			return out
		}
		pts := make([]Point, count)
		if binary.Read(r, binary.LittleEndian, &pts) != nil {
			return out
		}
		out = append(out, pts)
	}
	return out
}

// Draw rasterizes every coastline segment visible in the viewport. Segments
// are handed to LineF unclipped; the canvas rejects the offscreen ones, which
// is cheaper than testing visibility twice.
func Draw(c *canvas.Canvas, v geo.Viewport) {
	dw, dh := c.Size()
	if dw == 0 || dh == 0 {
		return
	}
	for _, line := range Coastlines() {
		if len(line) < 2 {
			continue
		}
		px, py, _ := v.Project(float64(line[0].Lat), float64(line[0].Lon), dw, dh)
		for _, pt := range line[1:] {
			x, y, _ := v.Project(float64(pt.Lat), float64(pt.Lon), dw, dh)
			// A segment whose endpoints land on opposite edges after
			// longitude wrapping would be drawn straight across the map.
			// Skip those rather than painting a false horizontal line.
			if absF(x-px) < float64(dw)/2 {
				c.LineF(px, py, x, y, canvas.InkLand)
			}
			px, py = x, y
		}
	}
}

// DrawPin marks a location with a small cross and reports whether it landed on
// the canvas. The cross is drawn dot by dot so it stays legible against
// coastline, which shares its cells.
func DrawPin(c *canvas.Canvas, v geo.Viewport, lat, lon float64) bool {
	dw, dh := c.Size()
	x, y, visible := v.Project(lat, lon, dw, dh)
	if !visible {
		return false
	}
	cx, cy := int(x), int(y)
	c.Set(cx, cy, canvas.InkPin)
	for d := 1; d <= 2; d++ {
		c.Set(cx-d, cy, canvas.InkPin)
		c.Set(cx+d, cy, canvas.InkPin)
		c.Set(cx, cy-d, canvas.InkPin)
		c.Set(cx, cy+d, canvas.InkPin)
	}
	return true
}

func absF(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/world/ -v`
Expected: PASS, all six tests.

- [ ] **Step 7: Commit**

```bash
git add cmd/genworld/ internal/world/
git commit -m "feat(world): embedded Natural Earth coastlines with pin drawing

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 4: ipwho.is geo provider

**Files:**
- Create: `internal/lookup/result.go`, `internal/lookup/ipwhois.go`, `internal/lookup/ipwhois_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces:
  - `type Result struct` — full field list below
  - `type GeoData struct { Lat, Lon float64; City, Region, Country, CountryCode, ISP, ASN, Timezone string }`
  - `type GeoProvider interface { Name() string; Fetch(ctx context.Context, ip netip.Addr) (*GeoData, error) }`
  - `func NewIPWhois() *IPWhois` and `type IPWhois struct { BaseURL string; HTTP *http.Client }`

- [ ] **Step 1: Write the failing tests**

Create `internal/lookup/ipwhois_test.go`:

```go
package lookup

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

// Real ipwho.is response for 8.8.8.8, trimmed to the fields consumed.
const ipwhoisOK = `{"ip":"8.8.8.8","success":true,"type":"IPv4",
"country":"United States","country_code":"US","region":"California",
"city":"San Jose","latitude":37.3393939,"longitude":-121.8949553,
"connection":{"asn":15169,"org":"Google LLC","isp":"Google LLC","domain":"google.com"},
"timezone":{"id":"America/Los_Angeles","abbr":"PDT"}}`

func newIPWhoisTest(t *testing.T, status int, body string) *IPWhois {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	p := NewIPWhois()
	p.BaseURL = srv.URL
	return p
}

func TestIPWhoisParsesRealResponse(t *testing.T) {
	p := newIPWhoisTest(t, 200, ipwhoisOK)
	got, err := p.Fetch(context.Background(), netip.MustParseAddr("8.8.8.8"))
	if err != nil {
		t.Fatal(err)
	}
	if got.City != "San Jose" || got.Region != "California" || got.CountryCode != "US" {
		t.Errorf("location = %q/%q/%q", got.City, got.Region, got.CountryCode)
	}
	if got.Lat != 37.3393939 || got.Lon != -121.8949553 {
		t.Errorf("coords = %v,%v", got.Lat, got.Lon)
	}
	if got.ISP != "Google LLC" {
		t.Errorf("ISP = %q", got.ISP)
	}
	// The API returns asn as a JSON number; it must be formatted, not
	// string-copied.
	if got.ASN != "AS15169" {
		t.Errorf("ASN = %q, want AS15169", got.ASN)
	}
	if got.Timezone != "America/Los_Angeles" {
		t.Errorf("timezone = %q", got.Timezone)
	}
}

func TestIPWhoisTreatsSuccessFalseAsError(t *testing.T) {
	// ipwho.is reports failure with HTTP 200 and success:false. Trusting the
	// status code alone would surface an empty result as a valid lookup.
	p := newIPWhoisTest(t, 200, `{"success":false,"message":"404 not found"}`)
	_, err := p.Fetch(context.Background(), netip.MustParseAddr("8.8.8.8"))
	if err == nil {
		t.Fatal("success:false with HTTP 200 was accepted as a valid response")
	}
}

func TestIPWhoisOmitsUnknownASN(t *testing.T) {
	p := newIPWhoisTest(t, 200, `{"success":true,"latitude":1,"longitude":2,"connection":{"asn":0}}`)
	got, err := p.Fetch(context.Background(), netip.MustParseAddr("8.8.8.8"))
	if err != nil {
		t.Fatal(err)
	}
	if got.ASN != "" {
		t.Fatalf("ASN = %q, want empty for asn 0", got.ASN)
	}
}

func TestIPWhoisRejectsNon200(t *testing.T) {
	p := newIPWhoisTest(t, 500, `nope`)
	if _, err := p.Fetch(context.Background(), netip.MustParseAddr("8.8.8.8")); err == nil {
		t.Fatal("HTTP 500 was accepted")
	}
}

func TestIPWhoisRejectsMalformedJSON(t *testing.T) {
	p := newIPWhoisTest(t, 200, `{"success":true,`)
	if _, err := p.Fetch(context.Background(), netip.MustParseAddr("8.8.8.8")); err == nil {
		t.Fatal("truncated JSON was accepted")
	}
}

func TestIPWhoisName(t *testing.T) {
	if got := NewIPWhois().Name(); got != "ipwho.is" {
		t.Fatalf("Name = %q", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/lookup/ -v`
Expected: FAIL — `undefined: NewIPWhois`, `undefined: IPWhois`.

- [ ] **Step 3: Implement the result model**

Create `internal/lookup/result.go`:

```go
// Package lookup resolves an IP or hostname into the handful of facts worth
// showing. It performs no rendering and knows nothing about terminals.
package lookup

import (
	"context"
	"net/http"
	"net/netip"
	"time"
)

// Result is the merged view assembled from a geo provider and RDAP. Fields
// left empty are rendered as an em dash by the UI rather than being hidden, so
// the panel height stays constant between lookups.
type Result struct {
	Query string     // exactly what the user typed
	IP    netip.Addr // resolved address

	Lat, Lon float64

	City        string
	Region      string
	Country     string
	CountryCode string
	ISP         string
	ASN         string
	Timezone    string

	Network string // e.g. "8.8.8.0/24"
	NetName string // e.g. "GOGL"
	Abuse   string // e.g. "network-abuse@google.com"

	// GeoSource names the provider that answered. The UI surfaces it whenever
	// it is not the primary, so a cleartext fallback is never silent.
	GeoSource string
}

// GeoData is what a geo provider contributes.
type GeoData struct {
	Lat, Lon    float64
	City        string
	Region      string
	Country     string
	CountryCode string
	ISP         string
	ASN         string
	Timezone    string
}

// GeoProvider is one source of geolocation. Two implement it: ipwho.is over
// HTTPS as primary, ip-api.com as fallback.
type GeoProvider interface {
	Name() string
	Fetch(ctx context.Context, ip netip.Addr) (*GeoData, error)
}

// requestTimeout bounds a single provider call. The UI stays responsive
// because lookups run off the render goroutine, but a hung provider must not
// pin a spinner forever.
const requestTimeout = 5 * time.Second

func defaultHTTPClient() *http.Client { return &http.Client{Timeout: requestTimeout} }
```

- [ ] **Step 4: Implement the ipwho.is provider**

Create `internal/lookup/ipwhois.go`:

```go
package lookup

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/netip"
)

// IPWhois queries ipwho.is. It is the primary provider because it is keyless
// and serves over HTTPS.
type IPWhois struct {
	BaseURL string
	HTTP    *http.Client
}

// NewIPWhois returns a provider pointed at the live service.
func NewIPWhois() *IPWhois {
	return &IPWhois{BaseURL: "https://ipwho.is", HTTP: defaultHTTPClient()}
}

func (p *IPWhois) Name() string { return "ipwho.is" }

type ipwhoisResponse struct {
	Success     bool    `json:"success"`
	Message     string  `json:"message"`
	Country     string  `json:"country"`
	CountryCode string  `json:"country_code"`
	Region      string  `json:"region"`
	City        string  `json:"city"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	Connection  struct {
		ASN int    `json:"asn"` // a JSON number, not "AS15169"
		Org string `json:"org"`
		ISP string `json:"isp"`
	} `json:"connection"`
	Timezone struct {
		ID string `json:"id"`
	} `json:"timezone"`
}

func (p *IPWhois) Fetch(ctx context.Context, ip netip.Addr) (*GeoData, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.BaseURL+"/"+ip.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ipwho.is: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ipwho.is: HTTP %d", resp.StatusCode)
	}

	var body ipwhoisResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("ipwho.is: %w", err)
	}
	// Failure arrives as HTTP 200 with success:false, so the status code alone
	// is not enough to tell a hit from a miss.
	if !body.Success {
		msg := body.Message
		if msg == "" {
			msg = "lookup failed"
		}
		return nil, fmt.Errorf("ipwho.is: %s", msg)
	}

	g := &GeoData{
		Lat:         body.Latitude,
		Lon:         body.Longitude,
		City:        body.City,
		Region:      body.Region,
		Country:     body.Country,
		CountryCode: body.CountryCode,
		ISP:         body.Connection.ISP,
		Timezone:    body.Timezone.ID,
	}
	if g.ISP == "" {
		g.ISP = body.Connection.Org
	}
	if body.Connection.ASN != 0 {
		g.ASN = fmt.Sprintf("AS%d", body.Connection.ASN)
	}
	return g, nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/lookup/ -v`
Expected: PASS, all six tests.

- [ ] **Step 6: Commit**

```bash
git add internal/lookup/
git commit -m "feat(lookup): ipwho.is geo provider over HTTPS

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 5: RDAP registry client

The trickiest task. Two things here are easy to get wrong and silently ship: the abuse entity is **nested**, and the jCard value format is a mixed-type array.

**Files:**
- Create: `internal/lookup/rdap.go`, `internal/lookup/rdap_test.go`

**Interfaces:**
- Consumes: `defaultHTTPClient()` from Task 4.
- Produces:
  - `type RegistryData struct { Network, NetName, Abuse string }`
  - `func NewRDAP() *RDAP` and `type RDAP struct { BaseURL string; HTTP *http.Client }`
  - `func (r *RDAP) Fetch(ctx context.Context, ip netip.Addr) (*RegistryData, error)`

- [ ] **Step 1: Write the failing tests**

Create `internal/lookup/rdap_test.go`:

```go
package lookup

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

// Real rdap.org response for 8.8.8.8, trimmed to the consumed fields. Note the
// abuse entity is nested one level down, inside the registrant.
const rdapOK = `{
  "objectClassName": "ip network",
  "handle": "NET-8-8-8-0-2",
  "name": "GOGL",
  "startAddress": "8.8.8.0",
  "endAddress": "8.8.8.255",
  "ipVersion": "v4",
  "type": "DIRECT ALLOCATION",
  "cidr0_cidrs": [{"v4prefix": "8.8.8.0", "length": 24}],
  "entities": [
    {
      "objectClassName": "entity",
      "handle": "GOGL",
      "roles": ["registrant"],
      "entities": [
        {
          "objectClassName": "entity",
          "handle": "ABUSE5250-ARIN",
          "roles": ["abuse"],
          "vcardArray": ["vcard", [
            ["version", {}, "text", "4.0"],
            ["adr", {"label": "1600 Amphitheatre Parkway"}, "text", ["", "", "", "", "", "", ""]],
            ["fn", {}, "text", "Abuse"],
            ["kind", {}, "text", "group"],
            ["email", {}, "text", "network-abuse@google.com"]
          ]]
        }
      ]
    }
  ]
}`

func newRDAPTest(t *testing.T, status int, body string) *RDAP {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rdap+json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	c := NewRDAP()
	c.BaseURL = srv.URL
	return c
}

func TestRDAPFindsNestedAbuseContact(t *testing.T) {
	// The abuse entity sits inside the registrant, not at the top level. A
	// flat scan of entities finds only roles:["registrant"] and returns an
	// empty abuse contact for every address on the internet.
	c := newRDAPTest(t, 200, rdapOK)
	got, err := c.Fetch(context.Background(), netip.MustParseAddr("8.8.8.8"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Abuse != "network-abuse@google.com" {
		t.Fatalf("Abuse = %q, want network-abuse@google.com", got.Abuse)
	}
}

func TestRDAPExtractsNameAndCIDR(t *testing.T) {
	c := newRDAPTest(t, 200, rdapOK)
	got, err := c.Fetch(context.Background(), netip.MustParseAddr("8.8.8.8"))
	if err != nil {
		t.Fatal(err)
	}
	if got.NetName != "GOGL" {
		t.Errorf("NetName = %q", got.NetName)
	}
	if got.Network != "8.8.8.0/24" {
		t.Errorf("Network = %q", got.Network)
	}
}

func TestRDAPSkipsNonStringVCardValues(t *testing.T) {
	// The adr entry's value is an array, not a string. Naive indexing into
	// entry[3] as a string panics or yields garbage.
	c := newRDAPTest(t, 200, `{"name":"X","entities":[{"roles":["abuse"],"vcardArray":["vcard",[["adr",{},"text",["","","x"]],["email",{},"text","a@b.c"]]]}]}`)
	got, err := c.Fetch(context.Background(), netip.MustParseAddr("1.1.1.1"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Abuse != "a@b.c" {
		t.Fatalf("Abuse = %q, want a@b.c", got.Abuse)
	}
}

func TestRDAPFallsBackToAddressRangeForCIDR(t *testing.T) {
	// Not every RIR emits the cidr0_cidrs extension.
	c := newRDAPTest(t, 200, `{"name":"Y","startAddress":"1.0.0.0","endAddress":"1.255.255.255"}`)
	got, err := c.Fetch(context.Background(), netip.MustParseAddr("1.1.1.1"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Network != "1.0.0.0/8" {
		t.Fatalf("Network = %q, want 1.0.0.0/8", got.Network)
	}
}

func TestCIDRFromRange(t *testing.T) {
	cases := []struct{ start, end, want string }{
		{"8.8.8.0", "8.8.8.255", "8.8.8.0/24"},
		{"1.0.0.0", "1.255.255.255", "1.0.0.0/8"},
		{"192.168.1.1", "192.168.1.1", "192.168.1.1/32"},
		{"2001:db8::", "2001:db8::ffff", "2001:db8::/112"},
		{"bogus", "1.0.0.0", ""},
		{"1.0.0.0", "2001:db8::", ""},
	}
	for _, tc := range cases {
		if got := cidrFromRange(tc.start, tc.end); got != tc.want {
			t.Errorf("cidrFromRange(%q,%q) = %q, want %q", tc.start, tc.end, got, tc.want)
		}
	}
}

func TestRDAPMissingAbuseIsEmptyNotAnError(t *testing.T) {
	c := newRDAPTest(t, 200, `{"name":"Z","startAddress":"1.0.0.0","endAddress":"1.0.0.255"}`)
	got, err := c.Fetch(context.Background(), netip.MustParseAddr("1.0.0.1"))
	if err != nil {
		t.Fatalf("absent abuse contact treated as an error: %v", err)
	}
	if got.Abuse != "" {
		t.Fatalf("Abuse = %q, want empty", got.Abuse)
	}
}

func TestRDAPRejectsNon200(t *testing.T) {
	c := newRDAPTest(t, 404, `{}`)
	if _, err := c.Fetch(context.Background(), netip.MustParseAddr("1.1.1.1")); err == nil {
		t.Fatal("HTTP 404 was accepted")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/lookup/ -run RDAP -v` and `go test ./internal/lookup/ -run CIDR -v`
Expected: FAIL — `undefined: NewRDAP`, `undefined: cidrFromRange`.

- [ ] **Step 3: Implement the RDAP client**

Create `internal/lookup/rdap.go`:

```go
package lookup

import (
	"context"
	"encoding/json"
	"fmt"
	"math/bits"
	"net/http"
	"net/netip"
	"slices"
)

// RegistryData is what RDAP contributes: the allocation, not the location.
type RegistryData struct {
	Network string
	NetName string
	Abuse   string
}

// RDAP queries the registry. rdap.org is a bootstrap service that redirects to
// whichever RIR is authoritative for the address, which saves us maintaining
// the RIR table ourselves.
type RDAP struct {
	BaseURL string
	HTTP    *http.Client
}

// NewRDAP returns a client pointed at the live bootstrap service.
func NewRDAP() *RDAP {
	return &RDAP{BaseURL: "https://rdap.org/ip", HTTP: defaultHTTPClient()}
}

type rdapEntity struct {
	Handle   string            `json:"handle"`
	Roles    []string          `json:"roles"`
	Entities []rdapEntity      `json:"entities"`
	VCard    []json.RawMessage `json:"vcardArray"`
}

type rdapResponse struct {
	Name         string `json:"name"`
	StartAddress string `json:"startAddress"`
	EndAddress   string `json:"endAddress"`
	CIDRs        []struct {
		V4Prefix string `json:"v4prefix"`
		V6Prefix string `json:"v6prefix"`
		Length   int    `json:"length"`
	} `json:"cidr0_cidrs"`
	Entities []rdapEntity `json:"entities"`
}

func (c *RDAP) Fetch(ctx context.Context, ip netip.Addr) (*RegistryData, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/"+ip.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/rdap+json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("rdap: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rdap: HTTP %d", resp.StatusCode)
	}

	var body rdapResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("rdap: %w", err)
	}
	return &RegistryData{
		NetName: body.Name,
		Network: networkOf(&body),
		Abuse:   findAbuseEmail(body.Entities),
	}, nil
}

// networkOf prefers the cidr0 extension and falls back to deriving the prefix
// from the address range, which some registries return instead.
func networkOf(r *rdapResponse) string {
	if len(r.CIDRs) > 0 {
		c := r.CIDRs[0]
		prefix := c.V4Prefix
		if prefix == "" {
			prefix = c.V6Prefix
		}
		if prefix != "" {
			return fmt.Sprintf("%s/%d", prefix, c.Length)
		}
	}
	return cidrFromRange(r.StartAddress, r.EndAddress)
}

// cidrFromRange derives the covering prefix from an inclusive address range by
// counting the bits the endpoints share.
func cidrFromRange(startStr, endStr string) string {
	start, err1 := netip.ParseAddr(startStr)
	end, err2 := netip.ParseAddr(endStr)
	if err1 != nil || err2 != nil || start.BitLen() != end.BitLen() {
		return ""
	}
	sb, eb := start.AsSlice(), end.AsSlice()
	ones := start.BitLen()
	for i := range sb {
		if d := sb[i] ^ eb[i]; d != 0 {
			ones = i*8 + bits.LeadingZeros8(d)
			break
		}
	}
	p, err := start.Prefix(ones)
	if err != nil {
		return ""
	}
	return p.String()
}

// findAbuseEmail walks the entity tree depth-first. The search must recurse:
// registries nest the abuse contact inside the registrant entity, so a flat
// scan of the top level finds only roles:["registrant"] and reports no abuse
// contact for every address on the internet.
func findAbuseEmail(entities []rdapEntity) string {
	for _, e := range entities {
		if slices.Contains(e.Roles, "abuse") {
			if email := vcardEmail(e.VCard); email != "" {
				return email
			}
		}
		if email := findAbuseEmail(e.Entities); email != "" {
			return email
		}
	}
	return ""
}

// vcardEmail pulls the first email out of a jCard array. The format is
// ["vcard", [[name, params, type, value], ...]] where value's type varies by
// entry: a string for email, a nested array for adr. Entries whose value will
// not unmarshal into a string are skipped rather than treated as an error.
func vcardEmail(vcard []json.RawMessage) string {
	if len(vcard) < 2 {
		return ""
	}
	var entries [][]json.RawMessage
	if err := json.Unmarshal(vcard[1], &entries); err != nil {
		return ""
	}
	for _, entry := range entries {
		if len(entry) < 4 {
			continue
		}
		var name string
		if json.Unmarshal(entry[0], &name) != nil || name != "email" {
			continue
		}
		var value string
		if json.Unmarshal(entry[3], &value) == nil && value != "" {
			return value
		}
	}
	return ""
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/lookup/ -v`
Expected: PASS, all thirteen tests across both files.

- [ ] **Step 5: Commit**

```bash
git add internal/lookup/rdap.go internal/lookup/rdap_test.go
git commit -m "feat(lookup): RDAP client with recursive abuse-contact search

The abuse entity is nested inside the registrant rather than sitting at
the top level, so the entity walk recurses. A flat scan returns an empty
abuse contact for every address.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 6: Lookup orchestration

Resolves the query, short-circuits private ranges, runs both sources concurrently, and merges. Partial success is the normal case.

**Files:**
- Create: `internal/lookup/ipapi.go`, `internal/lookup/lookup.go`, `internal/lookup/lookup_test.go`

**Interfaces:**
- Consumes: `Result`, `GeoData`, `GeoProvider`, `IPWhois`, `RDAP`, `RegistryData`, `defaultHTTPClient()`.
- Produces:
  - `type Client struct { Geo []GeoProvider; Registry *RDAP; Resolve func(ctx context.Context, host string) ([]netip.Addr, error) }`
  - `func NewClient() *Client`
  - `func (c *Client) Lookup(ctx context.Context, query string) (*Result, error)`
  - `var ErrPrivateRange error`
  - `func NewIPAPI() *IPAPI` (fallback provider, `Name() == "ip-api.com"`)

- [ ] **Step 1: Write the failing tests**

Create `internal/lookup/lookup_test.go`:

```go
package lookup

import (
	"context"
	"errors"
	"net/netip"
	"testing"
)

type stubGeo struct {
	name string
	data *GeoData
	err  error
	hits *int
}

func (s stubGeo) Name() string { return s.name }
func (s stubGeo) Fetch(context.Context, netip.Addr) (*GeoData, error) {
	if s.hits != nil {
		*s.hits++
	}
	return s.data, s.err
}

func testClient(geo ...GeoProvider) *Client {
	c := NewClient()
	c.Geo = geo
	c.Registry = nil // exercised separately; nil means "registry unavailable"
	return c
}

func TestLookupRejectsPrivateAddressWithoutCallingProviders(t *testing.T) {
	// Querying a public API about 192.168.1.1 leaks nothing useful and
	// returns nothing useful. Catch it locally.
	hits := 0
	c := testClient(stubGeo{name: "stub", data: &GeoData{}, hits: &hits})
	for _, addr := range []string{"192.168.1.1", "10.0.0.1", "127.0.0.1", "::1", "169.254.1.1"} {
		if _, err := c.Lookup(context.Background(), addr); !errors.Is(err, ErrPrivateRange) {
			t.Errorf("Lookup(%q) err = %v, want ErrPrivateRange", addr, err)
		}
	}
	if hits != 0 {
		t.Fatalf("provider was called %d times for private addresses", hits)
	}
}

func TestLookupFallsBackToSecondProvider(t *testing.T) {
	primary := stubGeo{name: "primary", err: errors.New("down")}
	fallback := stubGeo{name: "ip-api.com", data: &GeoData{Lat: 1, Lon: 2, City: "Berlin"}}
	got, err := testClient(primary, fallback).Lookup(context.Background(), "8.8.8.8")
	if err != nil {
		t.Fatal(err)
	}
	if got.City != "Berlin" {
		t.Errorf("City = %q", got.City)
	}
	// The fallback is HTTP-only, so the UI must be able to say so.
	if got.GeoSource != "ip-api.com" {
		t.Fatalf("GeoSource = %q, want ip-api.com", got.GeoSource)
	}
}

func TestLookupUsesPrimaryWithoutTouchingFallback(t *testing.T) {
	hits := 0
	primary := stubGeo{name: "ipwho.is", data: &GeoData{Lat: 1, Lon: 2}}
	fallback := stubGeo{name: "ip-api.com", data: &GeoData{}, hits: &hits}
	got, err := testClient(primary, fallback).Lookup(context.Background(), "8.8.8.8")
	if err != nil {
		t.Fatal(err)
	}
	if got.GeoSource != "ipwho.is" {
		t.Errorf("GeoSource = %q", got.GeoSource)
	}
	if hits != 0 {
		t.Fatalf("fallback called %d times despite primary succeeding", hits)
	}
}

func TestLookupFailsWhenEveryProviderFails(t *testing.T) {
	c := testClient(
		stubGeo{name: "a", err: errors.New("down")},
		stubGeo{name: "b", err: errors.New("also down")},
	)
	if _, err := c.Lookup(context.Background(), "8.8.8.8"); err == nil {
		t.Fatal("expected an error when no provider answered")
	}
}

func TestLookupSurvivesRegistryFailure(t *testing.T) {
	// RDAP failing must never blank the map. Registry fields stay empty and
	// the geo half renders.
	c := testClient(stubGeo{name: "ipwho.is", data: &GeoData{Lat: 51.5, Lon: -0.12, City: "London"}})
	got, err := c.Lookup(context.Background(), "8.8.8.8")
	if err != nil {
		t.Fatalf("registry failure propagated as a fatal error: %v", err)
	}
	if got.City != "London" || got.Lat != 51.5 {
		t.Errorf("geo half lost: %+v", got)
	}
	if got.Network != "" || got.Abuse != "" {
		t.Errorf("registry fields populated from a nil registry: %+v", got)
	}
}

func TestLookupResolvesHostnames(t *testing.T) {
	c := testClient(stubGeo{name: "ipwho.is", data: &GeoData{Lat: 1, Lon: 2}})
	c.Resolve = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
	}
	got, err := c.Lookup(context.Background(), "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got.IP.String() != "93.184.216.34" {
		t.Errorf("IP = %v", got.IP)
	}
	if got.Query != "example.com" {
		t.Errorf("Query = %q, want the original text", got.Query)
	}
}

func TestLookupRejectsUnresolvableInput(t *testing.T) {
	c := testClient(stubGeo{name: "ipwho.is", data: &GeoData{}})
	c.Resolve = func(context.Context, string) ([]netip.Addr, error) {
		return nil, errors.New("no such host")
	}
	if _, err := c.Lookup(context.Background(), "not a real host"); err == nil {
		t.Fatal("expected an error for unresolvable input")
	}
}

func TestLookupRejectsEmptyQuery(t *testing.T) {
	c := testClient(stubGeo{name: "ipwho.is", data: &GeoData{}})
	if _, err := c.Lookup(context.Background(), "   "); err == nil {
		t.Fatal("expected an error for a blank query")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/lookup/ -run Lookup -v`
Expected: FAIL — `undefined: NewClient`, `undefined: ErrPrivateRange`.

- [ ] **Step 3: Implement the ip-api fallback provider**

Create `internal/lookup/ipapi.go`:

```go
package lookup

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/netip"
)

// IPAPI queries ip-api.com. It exists only as a fallback: the free tier is
// HTTP-only, so a lookup through it crosses the network in cleartext. Whenever
// it answers, Result.GeoSource records it and the UI says so.
type IPAPI struct {
	BaseURL string
	HTTP    *http.Client
}

// NewIPAPI returns the fallback provider. The base URL is http:// because the
// free tier does not offer TLS.
func NewIPAPI() *IPAPI {
	return &IPAPI{BaseURL: "http://ip-api.com/json", HTTP: defaultHTTPClient()}
}

func (p *IPAPI) Name() string { return "ip-api.com" }

type ipapiResponse struct {
	Status     string  `json:"status"`
	Message    string  `json:"message"`
	Country    string  `json:"country"`
	CountryCode string `json:"countryCode"`
	RegionName string  `json:"regionName"`
	City       string  `json:"city"`
	Lat        float64 `json:"lat"`
	Lon        float64 `json:"lon"`
	Timezone   string  `json:"timezone"`
	ISP        string  `json:"isp"`
	AS         string  `json:"as"` // "AS15169 Google LLC"
}

func (p *IPAPI) Fetch(ctx context.Context, ip netip.Addr) (*GeoData, error) {
	url := p.BaseURL + "/" + ip.String() + "?fields=status,message,country,countryCode,regionName,city,lat,lon,timezone,isp,as"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ip-api: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ip-api: HTTP %d", resp.StatusCode)
	}
	var body ipapiResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("ip-api: %w", err)
	}
	if body.Status != "success" {
		msg := body.Message
		if msg == "" {
			msg = "lookup failed"
		}
		return nil, fmt.Errorf("ip-api: %s", msg)
	}
	g := &GeoData{
		Lat: body.Lat, Lon: body.Lon,
		City: body.City, Region: body.RegionName,
		Country: body.Country, CountryCode: body.CountryCode,
		ISP: body.ISP, Timezone: body.Timezone,
	}
	// The "as" field is "AS15169 Google LLC"; keep only the number.
	if body.AS != "" {
		if i := indexByte(body.AS, ' '); i > 0 {
			g.ASN = body.AS[:i]
		} else {
			g.ASN = body.AS
		}
	}
	return g, nil
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 4: Implement the orchestrator**

Create `internal/lookup/lookup.go`:

```go
package lookup

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"
)

// ErrPrivateRange marks an address that no public database can locate.
var ErrPrivateRange = errors.New("address is in a private or reserved range")

// Client fans a query out to a geo provider and the registry.
type Client struct {
	// Geo is tried in order; the first success wins.
	Geo      []GeoProvider
	Registry *RDAP
	// Resolve turns a hostname into addresses. Swappable so tests never touch
	// DNS.
	Resolve func(ctx context.Context, host string) ([]netip.Addr, error)
}

// NewClient wires the live providers: ipwho.is over HTTPS first, ip-api.com as
// a cleartext fallback.
func NewClient() *Client {
	return &Client{
		Geo:      []GeoProvider{NewIPWhois(), NewIPAPI()},
		Registry: NewRDAP(),
		Resolve: func(ctx context.Context, host string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		},
	}
}

// Lookup resolves the query and assembles a Result. The geo half is required;
// the registry half is not, because RDAP failing must never blank the map.
func (c *Client) Lookup(ctx context.Context, query string) (*Result, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("enter an IP address or hostname")
	}

	addr, err := c.resolve(ctx, query)
	if err != nil {
		return nil, err
	}
	if isUnlocatable(addr) {
		return nil, fmt.Errorf("%s: %w", addr, ErrPrivateRange)
	}

	var (
		wg  sync.WaitGroup
		reg *RegistryData
	)
	if c.Registry != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// A registry error is discarded on purpose: the fields simply stay
			// empty and the UI renders them as em dashes.
			if r, err := c.Registry.Fetch(ctx, addr); err == nil {
				reg = r
			}
		}()
	}

	geo, source, geoErr := c.fetchGeo(ctx, addr)
	wg.Wait()

	if geoErr != nil {
		return nil, geoErr
	}

	res := &Result{
		Query: query, IP: addr,
		Lat: geo.Lat, Lon: geo.Lon,
		City: geo.City, Region: geo.Region,
		Country: geo.Country, CountryCode: geo.CountryCode,
		ISP: geo.ISP, ASN: geo.ASN, Timezone: geo.Timezone,
		GeoSource: source,
	}
	if reg != nil {
		res.Network, res.NetName, res.Abuse = reg.Network, reg.NetName, reg.Abuse
	}
	return res, nil
}

func (c *Client) resolve(ctx context.Context, query string) (netip.Addr, error) {
	if addr, err := netip.ParseAddr(query); err == nil {
		return addr.Unmap(), nil
	}
	if c.Resolve == nil {
		return netip.Addr{}, fmt.Errorf("%q is not a valid IP address", query)
	}
	addrs, err := c.Resolve(ctx, query)
	if err != nil || len(addrs) == 0 {
		return netip.Addr{}, fmt.Errorf("could not resolve %q", query)
	}
	return addrs[0].Unmap(), nil
}

func (c *Client) fetchGeo(ctx context.Context, addr netip.Addr) (*GeoData, string, error) {
	var errs []error
	for _, p := range c.Geo {
		g, err := p.Fetch(ctx, addr)
		if err == nil && g != nil {
			return g, p.Name(), nil
		}
		errs = append(errs, err)
	}
	if len(errs) == 0 {
		return nil, "", errors.New("no geolocation provider configured")
	}
	return nil, "", errors.Join(errs...)
}

// isUnlocatable reports addresses that no public database can place. Catching
// them locally avoids a pointless round trip and a confusing empty answer.
func isUnlocatable(a netip.Addr) bool {
	return !a.IsValid() || a.IsPrivate() || a.IsLoopback() ||
		a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() ||
		a.IsMulticast() || a.IsUnspecified() || a.IsInterfaceLocalMulticast()
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/lookup/ -v`
Expected: PASS, all twenty-one tests.

- [ ] **Step 6: Commit**

```bash
git add internal/lookup/
git commit -m "feat(lookup): concurrent geo+registry orchestration with fallback

Registry failure is non-fatal; private ranges short-circuit before any
network call; the answering provider is recorded so the UI can flag the
cleartext fallback.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 7: Terminal UI

**Files:**
- Create: `internal/ui/styles.go`, `internal/ui/panel.go`, `internal/ui/model.go`, `internal/ui/panel_test.go`, `internal/ui/model_test.go`

**Interfaces:**
- Consumes: `canvas.New/Size/Render/InkAt/InkPin/DotsX/DotsY`, `geo.World/FitTo/Viewport`, `world.Draw/DrawPin`, `lookup.Client/Result/ErrPrivateRange`.
- Produces:
  - `func New(client *lookup.Client, initialQuery string) Model`
  - `func (m Model) Init() tea.Cmd`, `Update`, `View` — the `tea.Model` interface
  - `func RenderPanel(res *lookup.Result, width int) string`
  - `const PanelWidth = 34`

**Layout:** input row, then a body splitting the width between map and a fixed-width panel, then a status row, then a help row.

- [ ] **Step 1: Write the failing panel tests**

Create `internal/ui/panel_test.go`:

```go
package ui

import (
	"net/netip"
	"strings"
	"testing"

	"github.com/PromDungeon/whoisbutcooler/internal/lookup"
)

func fullResult() *lookup.Result {
	return &lookup.Result{
		Query: "8.8.8.8", IP: netip.MustParseAddr("8.8.8.8"),
		City: "San Jose", Region: "California",
		Country: "United States", CountryCode: "US",
		ISP: "Google LLC", ASN: "AS15169", Timezone: "America/Los_Angeles",
		Network: "8.8.8.0/24", NetName: "GOGL",
		Abuse: "network-abuse@google.com", GeoSource: "ipwho.is",
	}
}

func TestPanelShowsAllSevenFacts(t *testing.T) {
	got := RenderPanel(fullResult(), PanelWidth)
	for _, want := range []string{
		"8.8.8.8", "San Jose", "California", "US",
		"Google LLC", "AS15169", "8.8.8.0/24", "GOGL",
		"network-abuse@google.com", "America/Los_Angeles",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("panel is missing %q:\n%s", want, got)
		}
	}
}

func TestPanelHeightIsConstantRegardlessOfMissingFields(t *testing.T) {
	// Unavailable registry fields render as an em dash rather than
	// disappearing, so the panel does not reflow between lookups.
	full := strings.Count(RenderPanel(fullResult(), PanelWidth), "\n")
	sparse := fullResult()
	sparse.Network, sparse.NetName, sparse.Abuse, sparse.Timezone = "", "", "", ""
	got := strings.Count(RenderPanel(sparse, PanelWidth), "\n")
	if full != got {
		t.Fatalf("panel height changed with missing fields: %d vs %d", full, got)
	}
	if !strings.Contains(RenderPanel(sparse, PanelWidth), "—") {
		t.Error("missing fields did not render as an em dash")
	}
}

func TestPanelIsBlankWithoutAResult(t *testing.T) {
	if got := RenderPanel(nil, PanelWidth); strings.Contains(got, "AS") {
		t.Fatalf("nil result rendered content: %q", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/ -v`
Expected: FAIL — `undefined: RenderPanel`, `undefined: PanelWidth`.

- [ ] **Step 3: Implement styles**

Create `internal/ui/styles.go`:

```go
package ui

import "github.com/charmbracelet/lipgloss"

// PanelWidth is fixed so the map area does not resize between lookups.
const PanelWidth = 34

// missing is what an unavailable field renders as. Present-but-empty is
// meaningfully different from absent, and both beat a collapsing layout.
const missing = "—"

var (
	landStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("60"))
	pinStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
	labelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	valueStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	titleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
	ruleStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	errStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	warnStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	helpStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("238")).
			Padding(0, 1).
			Width(PanelWidth - 4)
)
```

- [ ] **Step 4: Implement the panel**

Create `internal/ui/panel.go`:

```go
package ui

import (
	"strings"

	"github.com/PromDungeon/whoisbutcooler/internal/lookup"
)

// RenderPanel draws the seven-line summary. Every line is always emitted, with
// an em dash standing in for anything unavailable, so the panel's height never
// changes between lookups.
func RenderPanel(res *lookup.Result, width int) string {
	if res == nil {
		return panelStyle.Render(strings.Repeat("\n", 6))
	}

	place := joinNonEmpty(", ", res.City, res.Region)
	if res.CountryCode != "" {
		place = joinNonEmpty(" · ", place, res.CountryCode)
	}

	// A Result built by hand may carry no parsed address; fall back to the
	// text the user typed rather than titling the panel "invalid IP".
	title := res.Query
	if res.IP.IsValid() {
		title = res.IP.String()
	}

	lines := []string{
		titleStyle.Render(title),
		ruleStyle.Render(strings.Repeat("─", max(1, width-4))),
		valueStyle.Render(orMissing(place)),
		valueStyle.Render(orMissing(joinNonEmpty(" · ", res.ISP, res.ASN))),
		valueStyle.Render(orMissing(joinNonEmpty(" · ", res.Network, res.NetName))),
		valueStyle.Render(orMissing(res.Abuse)),
		labelStyle.Render(orMissing(res.Timezone)),
	}
	return panelStyle.Render(strings.Join(lines, "\n"))
}

func orMissing(s string) string {
	if strings.TrimSpace(s) == "" {
		return missing
	}
	return s
}

func joinNonEmpty(sep string, parts ...string) string {
	kept := parts[:0]
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, sep)
}
```

- [ ] **Step 5: Run panel tests to verify they pass**

Run: `go test ./internal/ui/ -v`
Expected: PASS, three tests.

- [ ] **Step 6: Write the failing model tests**

Append to a new file `internal/ui/model_test.go`:

```go
package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/PromDungeon/whoisbutcooler/internal/geo"
	"github.com/PromDungeon/whoisbutcooler/internal/lookup"
)

func sized(m Model, w, h int) Model {
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return next.(Model)
}

func TestViewRendersMapAndPanelTogether(t *testing.T) {
	m := sized(New(nil, ""), 120, 34)
	res := fullResult()
	res.Lat, res.Lon = 37.34, -121.89
	next, _ := m.Update(lookupMsg{res: res})
	out := next.(Model).View()

	if !strings.ContainsFunc(out, func(r rune) bool { return r >= 0x2801 && r <= 0x28FF }) {
		t.Error("no braille in the rendered frame")
	}
	if !strings.Contains(out, "AS15169") {
		t.Error("panel content missing from the frame")
	}
}

func TestSuccessfulLookupFitsViewportToPin(t *testing.T) {
	m := sized(New(nil, ""), 120, 34)
	res := fullResult()
	res.Lat, res.Lon = 51.5, -0.12
	next, _ := m.Update(lookupMsg{res: res})
	got := next.(Model).view
	if got.LonSpan != geo.FitSpan {
		t.Errorf("span = %v, want %v", got.LonSpan, geo.FitSpan)
	}
	if got.CenterLon != -0.12 {
		t.Errorf("centre lon = %v, want -0.12", got.CenterLon)
	}
}

func TestPrivateRangeErrorIsExplainedNotDumped(t *testing.T) {
	m := sized(New(nil, ""), 120, 34)
	next, _ := m.Update(lookupMsg{err: lookup.ErrPrivateRange})
	out := next.(Model).View()
	if !strings.Contains(strings.ToLower(out), "private") {
		t.Fatalf("private-range error not surfaced:\n%s", out)
	}
}

func TestFallbackProviderIsSurfacedInStatus(t *testing.T) {
	// The fallback is HTTP-only. Using it must never be silent.
	m := sized(New(nil, ""), 120, 34)
	res := fullResult()
	res.GeoSource = "ip-api.com"
	next, _ := m.Update(lookupMsg{res: res})
	if !strings.Contains(next.(Model).View(), "ip-api.com") {
		t.Fatal("fallback provider not named anywhere in the frame")
	}
}

func TestPrimaryProviderIsNotAdvertised(t *testing.T) {
	m := sized(New(nil, ""), 120, 34)
	next, _ := m.Update(lookupMsg{res: fullResult()})
	if strings.Contains(next.(Model).View(), "ipwho.is") {
		t.Fatal("primary provider named in the status line; only the fallback should be")
	}
}

func TestTabSwitchesFocusAndArrowsThenPan(t *testing.T) {
	m := sized(New(nil, ""), 120, 34)
	before := m.view.CenterLon

	// While the input is focused, arrows belong to the text field.
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	if next.(Model).view.CenterLon != before {
		t.Fatal("arrow panned the map while the input was focused")
	}

	next, _ = next.(Model).Update(tea.KeyMsg{Type: tea.KeyTab})
	panned, _ := next.(Model).Update(tea.KeyMsg{Type: tea.KeyRight})
	if panned.(Model).view.CenterLon == before {
		t.Fatal("arrow did not pan the map after focus moved to it")
	}
}

func TestZoomKeysWalkTheLadder(t *testing.T) {
	m := sized(New(nil, ""), 120, 34)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	zoomed, _ := next.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'+'}})
	if zoomed.(Model).view.LonSpan >= m.view.LonSpan {
		t.Fatalf("'+' did not zoom in: %v -> %v", m.view.LonSpan, zoomed.(Model).view.LonSpan)
	}
	reset, _ := zoomed.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'0'}})
	if reset.(Model).view.LonSpan != geo.World().LonSpan {
		t.Error("'0' without a result should reset to the world view")
	}
}

func TestHistoryRecallWalksBackwards(t *testing.T) {
	m := sized(New(nil, ""), 120, 34)
	m.history = []string{"1.1.1.1", "8.8.8.8"}
	m.histIdx = len(m.history)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if got := next.(Model).input.Value(); got != "8.8.8.8" {
		t.Fatalf("first recall = %q, want the most recent entry", got)
	}
	next, _ = next.(Model).Update(tea.KeyMsg{Type: tea.KeyUp})
	if got := next.(Model).input.Value(); got != "1.1.1.1" {
		t.Fatalf("second recall = %q", got)
	}
}
```

- [ ] **Step 7: Run tests to verify they fail**

Run: `go test ./internal/ui/ -run 'View|Lookup|Private|Fallback|Primary|Tab|Zoom|History' -v`
Expected: FAIL — `undefined: New`, `undefined: lookupMsg`.

- [ ] **Step 8: Implement the model**

Create `internal/ui/model.go`:

```go
package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/PromDungeon/whoisbutcooler/internal/canvas"
	"github.com/PromDungeon/whoisbutcooler/internal/geo"
	"github.com/PromDungeon/whoisbutcooler/internal/lookup"
	"github.com/PromDungeon/whoisbutcooler/internal/world"
)

// primaryProvider is the only provider whose use goes unmentioned. Anything
// else answering means the HTTPS primary failed, which the user should see.
const primaryProvider = "ipwho.is"

// Rows consumed by the input, status, and help lines.
const chromeRows = 3

type focusArea int

const (
	focusInput focusArea = iota
	focusMap
)

// lookupMsg carries a finished lookup back to the update loop.
type lookupMsg struct {
	res *lookup.Result
	err error
}

// Model is the whole application state.
type Model struct {
	client *lookup.Client
	input  textinput.Model

	view geo.Viewport
	res  *lookup.Result

	status  string
	isError bool
	loading bool

	history []string
	histIdx int

	focus     focusArea
	lastQuery string
	w, h      int
}

// New builds the model. initialQuery, when non-empty, is looked up on start.
func New(client *lookup.Client, initialQuery string) Model {
	in := textinput.New()
	in.Placeholder = "IP address or hostname"
	in.Prompt = "› "
	in.Focus()
	in.SetValue(initialQuery)

	return Model{
		client: client,
		input:  in,
		view:   geo.World(),
		w:      80,
		h:      24,
	}
}

func (m Model) Init() tea.Cmd {
	if strings.TrimSpace(m.input.Value()) == "" {
		return textinput.Blink
	}
	return tea.Batch(textinput.Blink, m.doLookup(m.input.Value()))
}

// doLookup runs the query off the render goroutine so the UI stays responsive.
func (m Model) doLookup(query string) tea.Cmd {
	client := m.client
	return func() tea.Msg {
		if client == nil {
			return lookupMsg{err: errors.New("no lookup client configured")}
		}
		res, err := client.Lookup(context.Background(), query)
		return lookupMsg{res: res, err: err}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.input.Width = max(10, msg.Width-6)
		return m, nil

	case lookupMsg:
		return m.applyLookup(msg), nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) applyLookup(msg lookupMsg) Model {
	m.loading = false
	if msg.err != nil {
		m.isError = true
		if errors.Is(msg.err, lookup.ErrPrivateRange) {
			m.status = "That address is in a private or reserved range, so no public database can place it."
		} else {
			m.status = msg.err.Error()
		}
		return m
	}
	m.res = msg.res
	dw, dh := m.mapDots()
	m.view = geo.FitTo(msg.res.Lat, msg.res.Lon).Clamp(dw, dh)
	m.isError = false
	m.status = ""
	if msg.res.GeoSource != "" && msg.res.GeoSource != primaryProvider {
		// The fallback is HTTP-only; saying so is the whole point of tracking
		// which provider answered.
		m.status = fmt.Sprintf("HTTPS provider unavailable — answered by %s over plain HTTP.", msg.res.GeoSource)
	}
	return m
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	if msg.Type == tea.KeyTab {
		// Blur returns nothing while Focus returns a command, so these two
		// branches cannot be collapsed.
		if m.focus == focusInput {
			m.focus = focusMap
			m.input.Blur()
			return m, nil
		}
		m.focus = focusInput
		return m, m.input.Focus()
	}

	if m.focus == focusMap {
		return m.handleMapKey(msg)
	}
	return m.handleInputKey(msg)
}

func (m Model) handleMapKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	dw, dh := m.mapDots()
	const step = 0.25 // a quarter-viewport nudge

	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "esc":
		m.focus = focusInput
		return m, m.input.Focus()
	case "left", "h":
		m.view = m.view.Pan(-step, 0, dw, dh)
	case "right", "l":
		m.view = m.view.Pan(step, 0, dw, dh)
	case "up", "k":
		m.view = m.view.Pan(0, step, dw, dh)
	case "down", "j":
		m.view = m.view.Pan(0, -step, dw, dh)
	case "+", "=":
		m.view = m.view.ZoomIn().Clamp(dw, dh)
	case "-", "_":
		m.view = m.view.ZoomOut().Clamp(dw, dh)
	case "0":
		if m.res != nil {
			m.view = geo.FitTo(m.res.Lat, m.res.Lon).Clamp(dw, dh)
		} else {
			m.view = geo.World().Clamp(dw, dh)
		}
	case "r":
		if m.lastQuery != "" {
			m.loading, m.status, m.isError = true, "Looking up "+m.lastQuery+"…", false
			return m, m.doLookup(m.lastQuery)
		}
	}
	return m, nil
}

func (m Model) handleInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		q := strings.TrimSpace(m.input.Value())
		if q == "" {
			return m, nil
		}
		m.history = append(m.history, q)
		m.histIdx = len(m.history)
		m.lastQuery = q
		m.loading, m.status, m.isError = true, "Looking up "+q+"…", false
		m.input.SetValue("")
		return m, m.doLookup(q)

	case tea.KeyUp:
		if m.histIdx > 0 {
			m.histIdx--
			m.input.SetValue(m.history[m.histIdx])
		}
		return m, nil

	case tea.KeyDown:
		if m.histIdx < len(m.history)-1 {
			m.histIdx++
			m.input.SetValue(m.history[m.histIdx])
		} else {
			m.histIdx = len(m.history)
			m.input.SetValue("")
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// mapDots returns the map area in dots.
func (m Model) mapDots() (int, int) {
	w, h := m.mapCells()
	return w * canvas.DotsX, h * canvas.DotsY
}

func (m Model) mapCells() (int, int) {
	w := m.w - PanelWidth
	if w < 10 {
		w = 10
	}
	h := m.h - chromeRows
	if h < 3 {
		h = 3
	}
	return w, h
}

func (m Model) View() string {
	mapW, mapH := m.mapCells()
	c := canvas.New(mapW, mapH)
	world.Draw(c, m.view)
	if m.res != nil {
		world.DrawPin(c, m.view, m.res.Lat, m.res.Lon)
	}

	body := lipgloss.JoinHorizontal(
		lipgloss.Top,
		strings.Join(colorize(c), "\n"),
		RenderPanel(m.res, PanelWidth),
	)

	status := m.status
	style := labelStyle
	switch {
	case m.isError:
		style = errStyle
	case m.res != nil && m.res.GeoSource != "" && m.res.GeoSource != primaryProvider:
		style = warnStyle
	}

	help := "tab focus · ←↑↓→/hjkl pan · +/- zoom · 0 fit · r retry · q quit"

	return strings.Join([]string{
		m.input.View(),
		body,
		style.Render(status),
		helpStyle.Render(help),
	}, "\n")
}

// colorize applies one style per run of same-inked cells. A terminal cell
// carries a single foreground color, so runs are the finest granularity
// available and coalescing them keeps the escape-sequence count sane.
func colorize(c *canvas.Canvas) []string {
	rows := c.Render()
	out := make([]string, len(rows))
	for y, row := range rows {
		var b strings.Builder
		runes := []rune(row)
		start := 0
		for x := 1; x <= len(runes); x++ {
			if x < len(runes) && c.InkAt(x, y) == c.InkAt(start, y) {
				continue
			}
			seg := string(runes[start:x])
			if c.InkAt(start, y) == canvas.InkPin {
				b.WriteString(pinStyle.Render(seg))
			} else {
				b.WriteString(landStyle.Render(seg))
			}
			start = x
		}
		out[y] = b.String()
	}
	return out
}
```

- [ ] **Step 9: Run tests to verify they pass**

Run: `go test ./internal/ui/ -v`
Expected: PASS, eleven tests.

- [ ] **Step 10: Commit**

```bash
git add internal/ui/
git commit -m "feat(ui): map, panel, input focus model and keybindings

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 8: Entry point, one-shot mode, release config

**Files:**
- Create: `main.go`, `main_test.go`, `.goreleaser.yaml`, `README.md`

**Interfaces:**
- Consumes: `ui.New`, `lookup.NewClient`, `tea.NewProgram`.
- Produces: the `whoisbutcooler` binary.

- [ ] **Step 1: Write the failing test**

Create `main_test.go`:

```go
package main

import "testing"

func TestParseArgs(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		isTTY   bool
		wantQ   string
		wantOne bool
		wantErr bool
	}{
		{name: "bare interactive", args: nil, isTTY: true},
		{name: "query interactive", args: []string{"8.8.8.8"}, isTTY: true, wantQ: "8.8.8.8"},
		{name: "explicit once", args: []string{"--once", "8.8.8.8"}, isTTY: true, wantQ: "8.8.8.8", wantOne: true},
		// A non-TTY stdout means output is being piped, so there is nobody to
		// type at a prompt.
		{name: "piped with query", args: []string{"1.1.1.1"}, isTTY: false, wantQ: "1.1.1.1", wantOne: true},
		{name: "piped without query", args: nil, isTTY: false, wantErr: true},
		{name: "once without query", args: []string{"--once"}, isTTY: true, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q, once, err := parseArgs(tc.args, tc.isTTY)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if q != tc.wantQ || once != tc.wantOne {
				t.Fatalf("got (%q, %v), want (%q, %v)", q, once, tc.wantQ, tc.wantOne)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test . -v`
Expected: FAIL — `undefined: parseArgs`.

- [ ] **Step 3: Implement main**

Create `main.go`:

```go
// Command whoisbutcooler looks up an IP address and shows where it is on a
// braille world map, alongside a seven-line summary of the registry data worth
// reading.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/PromDungeon/whoisbutcooler/internal/lookup"
	"github.com/PromDungeon/whoisbutcooler/internal/ui"
)

const usage = `whoisbutcooler — IP lookups on a map

  whoisbutcooler              open the interactive prompt
  whoisbutcooler <ip|host>    look it up, then stay interactive
  whoisbutcooler --once <ip>  render one frame and exit

Piping output implies --once, since there is nobody to type at a prompt.`

// errHelp is not a failure, so it exits zero and prints to stdout.
var errHelp = errors.New("help requested")

func main() {
	stdoutIsTTY := isTerminal(os.Stdout)
	query, once, err := parseArgs(os.Args[1:], stdoutIsTTY)
	if errors.Is(err, errHelp) {
		fmt.Println(usage)
		return
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintln(os.Stderr, "\n"+usage)
		os.Exit(2)
	}

	client := lookup.NewClient()

	if once {
		if err := renderOnce(client, query); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	p := tea.NewProgram(ui.New(client, query), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// parseArgs resolves the invocation into a query and whether to render a
// single frame. A non-TTY stdout implies --once; both forms require a query,
// because there is no prompt to fall back to.
func parseArgs(args []string, stdoutIsTTY bool) (query string, once bool, err error) {
	for _, a := range args {
		switch a {
		case "--once":
			once = true
		case "-h", "--help":
			return "", false, errHelp
		default:
			if len(a) > 0 && a[0] == '-' {
				return "", false, fmt.Errorf("unknown flag %q", a)
			}
			if query != "" {
				return "", false, errors.New("only one query may be given")
			}
			query = a
		}
	}
	if !stdoutIsTTY {
		once = true
	}
	if once && query == "" {
		return "", false, errors.New("a query is required when output is not an interactive terminal")
	}
	return query, once, nil
}

// renderOnce draws a single frame at a fixed size and exits. Braille is
// ordinary text, so this redirects and pipes cleanly; lipgloss drops color on
// its own when the destination is not a terminal.
func renderOnce(client *lookup.Client, query string) error {
	res, err := client.Lookup(context.Background(), query)
	if err != nil {
		return err
	}
	m := ui.New(client, query)
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	final, _ := sized.(ui.Model).Update(ui.LookupResult(res))
	fmt.Println(final.(ui.Model).View())
	return nil
}

// isTerminal reports whether f is a character device. Checking the file mode
// keeps the dependency list to the standard library.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
```

- [ ] **Step 4: Export the message constructor the entry point needs**

`renderOnce` must hand a finished result to the model, but `lookupMsg` is unexported. Add to `internal/ui/model.go`:

```go
// LookupResult wraps a completed lookup as a message, so callers outside the
// package (the one-shot renderer) can drive the model without running a full
// Bubble Tea program.
func LookupResult(res *lookup.Result) tea.Msg { return lookupMsg{res: res} }
```

- [ ] **Step 5: Run the full suite**

Run: `go build ./... && go vet ./... && go test ./... -v`
Expected: PASS across all five packages.

- [ ] **Step 6: Verify against the live services by hand**

```bash
go run . --once 8.8.8.8
```

Expected: a braille world map framed on California with a pin, and a panel reading `San Jose`, `Google LLC · AS15169`, `8.8.8.0/24 · GOGL`, `network-abuse@google.com`. **If the abuse line shows an em dash, the RDAP entity walk is not recursing** — see Task 5.

```bash
go run . --once 192.168.1.1   # expect the private-range message, exit 1
go run . --once 1.1.1.1 | cat # expect plain text, no ANSI escapes
```

- [ ] **Step 7: Write the release config and README**

Create `.goreleaser.yaml`:

```yaml
version: 2
before:
  hooks:
    - go mod tidy
builds:
  - env: [CGO_ENABLED=0]
    goos: [linux, darwin, windows]
    goarch: [amd64, arm64]
archives:
  - formats: [tar.gz]
    name_template: "{{ .ProjectName }}_{{ .Os }}_{{ .Arch }}"
checksum:
  name_template: checksums.txt
changelog:
  sort: asc
  filters:
    exclude: ["^docs:", "^test:"]
```

Create `README.md` covering: what it does, `go install github.com/PromDungeon/whoisbutcooler@latest`, the three invocation forms, the keybinding table from the spec, the note that ipwho.is and rdap.org need no key, and Natural Earth attribution (public domain).

- [ ] **Step 8: Commit**

```bash
git add main.go main_test.go .goreleaser.yaml README.md internal/ui/model.go
git commit -m "feat: entry point with one-shot and non-TTY rendering

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

## Verification checklist

Before calling the project done, confirm each of these by running the command and reading the output:

- [ ] `go build ./...` succeeds
- [ ] `go vet ./...` is silent
- [ ] `go test ./...` passes in all five packages
- [ ] `go run . --once 8.8.8.8` shows a real abuse contact, not an em dash
- [ ] `go run . --once 192.168.1.1` explains the private range and exits non-zero
- [ ] `go run . --once 1.1.1.1 | cat` emits no ANSI escapes
- [ ] `go run .` opens the prompt; tab moves focus; arrows pan; `+`/`-` zoom; `q` quits
- [ ] `go run . 2001:4860:4860::8888` works for IPv6
- [ ] Resizing the terminal reflows the map without corrupting the panel
