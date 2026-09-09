package canvas

import (
	"math"
	"slices"
	"strings"
	"testing"
	"time"
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
	// A terminal cell carries one foreground color, so when two things share
	// a cell exactly one of them decides its color. The order is deliberate:
	// a border loses to coastline because a shoreline is the more important
	// fact, and the pin loses to nothing.
	cases := []struct {
		name                string
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

// The dot sets below are derived by hand from the line, not read back from
// this package, so they check the rasterizer rather than describe it. Each
// case names the reasoning that produced it.
func TestLineRasterizesExactGeometry(t *testing.T) {
	cases := []struct {
		name           string
		w, h           int
		x0, y0, x1, y1 int
		want           []string
		why            string
	}{
		{
			name: "horizontal", w: 3, h: 1, x0: 0, y0: 0, x1: 5, y1: 0,
			want: []string{"⠉⠉⠉"},
			why:  "dots (0..5,0); each cell holds an even x (0x01) and an odd x (0x08) = 0x09",
		},
		{
			name: "vertical", w: 1, h: 1, x0: 0, y0: 0, x1: 0, y1: 3,
			want: []string{"⡇"},
			why:  "dots (0,0..3) in column 0 = 0x01|0x02|0x04|0x40 = 0x47",
		},
		{
			name: "45 degree diagonal", w: 2, h: 1, x0: 0, y0: 0, x1: 3, y1: 3,
			want: []string{"⠑⢄"},
			why:  "dots (0,0),(1,1),(2,2),(3,3); cell0 = 0x01|0x10, cell1 = 0x04|0x80",
		},
		{
			name: "steep 1:2", w: 1, h: 1, x0: 0, y0: 0, x1: 1, y1: 2,
			want: []string{"⠱"},
			why:  "ideal x per y is 0,0.5,1 rounded up = 0,1,1; dots (0,0),(1,1),(1,2) = 0x01|0x10|0x20",
		},
		{
			name: "shallow 5:1", w: 3, h: 1, x0: 0, y0: 0, x1: 5, y1: 1,
			want: []string{"⠉⠑⠒"},
			why:  "ideal y runs 0,.2,.4,.6,.8,1 so it steps at x=3 with no tie to break",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := New(tc.w, tc.h)
			c.Line(tc.x0, tc.y0, tc.x1, tc.y1, InkLand)
			got := c.Render()
			if !slices.Equal(got, tc.want) {
				t.Fatalf("Line(%d,%d,%d,%d) = %q, want %q\n  %s",
					tc.x0, tc.y0, tc.x1, tc.y1, got, tc.want, tc.why)
			}
		})
	}
}

func TestLineBreaksTiesUpward(t *testing.T) {
	// From (0,0) to (2,1) the ideal y at x=1 is exactly 0.5, so rounding could
	// go either way and both results are legitimate lines. This pins the
	// round-half-up convention the rest of the map is drawn with, so a change
	// to the tie-break shows up here rather than as coastline that shifts by a
	// dot for reasons nobody can find.
	c := New(2, 1)
	c.Line(0, 0, 2, 1, InkLand)
	if got, want := c.Render(), []string{"⠑⠂"}; !slices.Equal(got, want) {
		t.Fatalf("tie broke downward: got %q, want %q (dots (0,0),(1,1),(2,1))", got, want)
	}
}

func TestLineAlwaysLightsBothEndpoints(t *testing.T) {
	for _, tc := range [][4]int{{0, 0, 5, 3}, {5, 3, 0, 0}, {0, 3, 5, 0}, {3, 0, 3, 3}, {2, 2, 2, 2}} {
		c := New(3, 1)
		c.Line(tc[0], tc[1], tc[2], tc[3], InkLand)
		for _, p := range [][2]int{{tc[0], tc[1]}, {tc[2], tc[3]}} {
			if c.InkAt(p[0]/DotsX, p[1]/DotsY) == InkNone {
				t.Errorf("Line%v left endpoint (%d,%d) unlit", tc, p[0], p[1])
			}
		}
	}
}

func TestLineFIgnoresInfiniteEndpoints(t *testing.T) {
	// Inf minus Inf is NaN, and outCode reads NaN as inside, so an infinite
	// endpoint slips past the clip and reaches Bresenham as a coordinate near
	// the smallest int — which it then walks one dot at a time. That hangs the
	// render rather than failing it, so assert on returning at all.
	c := New(4, 2)
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.LineF(math.Inf(-1), math.Inf(-1), math.Inf(1), 2, InkLand)
		c.LineF(0, 0, math.Inf(1), math.Inf(1), InkLand)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("LineF did not return on infinite endpoints")
	}
	for _, row := range c.Render() {
		if strings.TrimSpace(row) != "" {
			t.Fatalf("an infinite segment drew %q", row)
		}
	}
}
