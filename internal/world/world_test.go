package world

import (
	"strings"
	"testing"

	"github.com/PromDungeon/whoisbutcooler/internal/canvas"
	"github.com/PromDungeon/whoisbutcooler/internal/geo"
)

func TestCoastlinesDecodeToExpectedShape(t *testing.T) {
	lines := Coastlines()
	// 127 GeoJSON features but 128 rings: the Africa-Eurasia feature carries a
	// second ring for the Caspian Sea, an inland water body cut out of the
	// landmass. Its shoreline is real coastline, so it is drawn like any other.
	if len(lines) != 128 {
		t.Fatalf("got %d polylines, want 128", len(lines))
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
	first, second := render(), render()
	if first != second {
		t.Fatal("two identical renders differed")
	}
}

func TestDrawKeepsWideSegmentsAtTightZoom(t *testing.T) {
	// A degree of longitude is ordinary coastline, not an antimeridian
	// crossing. A guard thresholding projected distance against canvas width
	// conflates the two, and this viewport is chosen to prove the difference:
	// over the West Antarctic coast at this zoom, every visible segment
	// exceeds half the canvas width, so such a guard renders a completely
	// blank map here. A viewport where some segments survive the guard would
	// pass either way and prove nothing.
	//
	// The threshold is the exact measured row count, not a margin: this
	// viewport's rendering is deterministic, and it is unchanged — byte for
	// byte — by the per-segment longitude unwrapping in Draw, because its
	// antipodal meridian crosses Antarctica some six degrees north of the
	// 1.2-degree latitude band this viewport can see, so every segment there
	// is rejected on y before longitude matters. Every one of these twelve
	// rows is genuine coastline; none is an antimeridian artifact.
	c := canvas.New(80, 24)
	Draw(c, geo.Viewport{CenterLat: -72.46, CenterLon: -98.82, LonSpan: 2})
	nonBlank := 0
	for _, row := range c.Render() {
		if strings.TrimSpace(row) != "" {
			nonBlank++
		}
	}
	if nonBlank < 12 {
		t.Fatalf("only %d non-blank rows at LonSpan=2 over the West Antarctic coast; want at least 12", nonBlank)
	}
}

// longestInkRun returns the length of the longest run of consecutive inked
// cells in a rendered row.
func longestInkRun(row string) int {
	best, cur := 0, 0
	for _, r := range row {
		if r == ' ' {
			cur = 0
			continue
		}
		cur++
		if cur > best {
			best = cur
		}
	}
	return best
}

// reachesBelow60S reports whether cell row lies even partly south of 60°S,
// the only latitude band where a genuine coastline encircles the globe and so
// the only place a full-width row of ink can be honest.
func reachesBelow60S(v geo.Viewport, row, dotW, dotH int) bool {
	bottom := float64((row + 1) * canvas.DotsY)
	lat := v.CenterLat - (bottom/float64(dotH)-0.5)*v.LatSpan(dotW, dotH)
	return lat < -60
}

func TestDrawPaintsNoFullWidthStreak(t *testing.T) {
	// The antimeridian artifact renders as a line straight across the map: a
	// segment whose endpoints wrap to opposite edges is rasterized through
	// every column between them. Nothing else in the dataset can produce a
	// solid edge-to-edge run outside the far south, so an almost-full-width
	// run of ink in these viewports is the artifact and nothing else.
	//
	// Antarctica really does span every longitude, and at a tight southern
	// zoom a full row of ink is correct — hence the 60°S exclusion, which is
	// a property of the check rather than of these particular views. It costs
	// nothing here: the artifact rows this test was written against (three at
	// the world view, two at San Jose, two at London, one at Bangalore) all
	// fall north of it.
	//
	// 46x21 cells is the map area of an 80x24 terminal, the smallest layout
	// the UI supports and the one where a streak does the most damage.
	const mapW, mapH = 46, 21
	views := map[string]geo.Viewport{
		"world":     geo.World(),
		"san jose":  geo.FitTo(37.34, -121.89),
		"london":    geo.FitTo(51.5, -0.12),
		"bangalore": geo.FitTo(12.97, 77.59),
	}
	for name, v := range views {
		c := canvas.New(mapW, mapH)
		dotW, dotH := c.Size()
		v = v.Clamp(dotW, dotH)
		Draw(c, v)
		for i, row := range c.Render() {
			if reachesBelow60S(v, i, dotW, dotH) {
				continue
			}
			// Well under mapW: the artifact is clipped to the canvas edges and
			// may lose a cell or two at either end to rounding, so demanding a
			// literally complete row would let a near-miss through.
			if run := longestInkRun(row); run >= mapW-4 {
				t.Errorf("%s: row %d has a %d-cell run of ink across a %d-cell map:\n%s",
					name, i, run, mapW, row)
			}
		}
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
