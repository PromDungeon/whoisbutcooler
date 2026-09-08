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
