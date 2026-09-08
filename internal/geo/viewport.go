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

// LonDelta is lon's signed offset from the viewport centre, wrapped into
// [-180, 180).
func (v Viewport) LonDelta(lon float64) float64 { return NormLon(lon - v.CenterLon) }

// UnwrapLonDelta adjusts d by whole turns so that it lands within 180 degrees
// of prev, and exists because wrapping each vertex of a polyline on its own is
// not enough to make the antimeridian a non-event.
//
// Wrapping relative to the viewport centre does not remove the discontinuity,
// it relocates it: to the centre's antipodal meridian, and to exactly +/-180,
// where NormLon maps +180 to -180. Either one turns a one-degree coastline
// segment into endpoints a full turn apart, which the rasterizer faithfully
// paints as a line clean across the map. Consecutive vertices of real
// coastline are never 180 degrees apart, so re-expressing the second endpoint
// relative to the first removes the jump without discarding any segment --
// unlike a distance threshold, which cannot tell the two cases apart.
func UnwrapLonDelta(prev, d float64) float64 { return prev + NormLon(d-prev) }

// Project maps a coordinate to dot-space. visible reports whether the result
// lands on the canvas.
func (v Viewport) Project(lat, lon float64, dotW, dotH int) (x, y float64, visible bool) {
	return v.ProjectDelta(lat, v.LonDelta(lon), dotW, dotH)
}

// ProjectDelta is Project for a caller that has already worked out the
// longitude offset from the viewport centre, which is how a polyline draws an
// unwrapped run of vertices (see UnwrapLonDelta) without every package that
// draws one growing its own copy of the projection. dLon is deliberately not
// re-normalised: an offset outside [-180, 180) is the caller saying "past the
// edge in this direction", and the resulting off-canvas x is what makes the
// segment clip instead of teleport.
func (v Viewport) ProjectDelta(lat, dLon float64, dotW, dotH int) (x, y float64, visible bool) {
	latSpan := v.LatSpan(dotW, dotH)
	if v.LonSpan == 0 || latSpan == 0 {
		return 0, 0, false
	}
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
