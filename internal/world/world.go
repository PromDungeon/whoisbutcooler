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
//
// There is deliberately no antimeridian special case. Project normalises each
// endpoint's longitude relative to the viewport centre, so a segment crossing
// the date line lands on two adjacent positions rather than opposite edges —
// the false horizontal line it would otherwise paint cannot arise.
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
			c.LineF(px, py, x, y, canvas.InkLand)
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
