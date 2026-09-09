// Package world owns the embedded coastline dataset and draws it through the
// canvas rasterizer using the geo projection. It is the only package that
// knows the map data exists.
package world

import (
	"bytes"
	_ "embed"
	"encoding/binary"
	"sync"

	"github.com/PromDungeon/whoisbutcooler/internal/canvas"
	"github.com/PromDungeon/whoisbutcooler/internal/geo"
)

// Natural Earth 110m land, public domain. Regenerate with cmd/genworld.
//
//go:embed world.bin
var worldBin []byte

// Natural Earth 110m admin_0 boundary lines, public domain. Regenerate with
// cmd/genworld.
//
//go:embed borders.bin
var bordersBin []byte

// Point is a coastline vertex. float32 keeps the blob small and is far finer
// than a braille dot at any zoom this tool offers.
type Point struct{ Lon, Lat float32 }

var (
	coastOnce   sync.Once
	coastLines  [][]Point
	borderOnce  sync.Once
	borderLines [][]Point
)

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

// Draw rasterizes every coastline segment visible in the viewport.
func Draw(c *canvas.Canvas, v geo.Viewport) {
	drawPolylines(c, v, Coastlines(), canvas.InkLand)
}

// BorderMaxSpan is the widest viewport that draws borders. Above it the map is
// being used to orient rather than to read a region, and borders there cost ink
// without adding legibility. The gate lives here rather than in the UI so that
// callers draw unconditionally and the policy is written down once.
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
// a discontinuity at the viewport's antipodal meridian, and another at exactly
// ±180 for the coastline, which is clipped there and so has vertices sitting on
// it. Either one paints a false line clean across the map. (The border data
// spans only -141 to +141, so the ±180 half does not arise for it — but it runs
// through the same code, and the antipodal half applies to both.) Unwrapping is per segment, not
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
