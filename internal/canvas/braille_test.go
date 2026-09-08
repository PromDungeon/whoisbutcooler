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
