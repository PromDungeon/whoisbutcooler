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
	if !finite(x0) || !finite(y0) || !finite(x1) || !finite(y1) {
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

// finite rejects NaN and either infinity. An infinite endpoint is worse than
// it looks: subtracting infinities inside the clip yields NaN, NaN compares
// false against every bound so outCode calls it inside, and the segment then
// reaches Bresenham as a coordinate near the smallest int.
func finite(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) }

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
