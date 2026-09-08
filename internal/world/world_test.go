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
	if render() != render() {
		t.Fatal("two identical renders differed")
	}
}

func TestDrawKeepsWideSegmentsAtTightZoom(t *testing.T) {
	// Several degrees of longitude is ordinary coastline, not an antimeridian
	// crossing. A guard that thresholds projected distance against canvas
	// width conflates the two and erases real coastline as zoom tightens.
	c := canvas.New(80, 24)
	Draw(c, geo.Viewport{CenterLat: 72.5, CenterLon: 145.0, LonSpan: 8})
	for _, row := range c.Render() {
		if strings.TrimSpace(row) != "" {
			return
		}
	}
	t.Fatal("no coastline drawn at LonSpan=8 over the Siberian coast")
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
