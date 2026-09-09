package main

import (
	"encoding/json"
	"testing"
)

func parse(t *testing.T, js string) featureCollection {
	t.Helper()
	var fc featureCollection
	if err := json.Unmarshal([]byte(js), &fc); err != nil {
		t.Fatal(err)
	}
	return fc
}

func TestPolylinesFlattensEveryGeometryNaturalEarthUses(t *testing.T) {
	// Land files arrive as polygons whose rings each become a polyline;
	// boundary files arrive as line strings, which are already polylines.
	// One generator has to read both.
	cases := []struct {
		name  string
		js    string
		lines int
		pts   int
	}{
		{
			name:  "polygon with two rings",
			js:    `{"features":[{"geometry":{"type":"Polygon","coordinates":[[[0,0],[1,1],[0,0]],[[2,2],[3,3],[2,2]]]}}]}`,
			lines: 2, pts: 6,
		},
		{
			name:  "multipolygon",
			js:    `{"features":[{"geometry":{"type":"MultiPolygon","coordinates":[[[[0,0],[1,1]]],[[[2,2],[3,3]]]]}}]}`,
			lines: 2, pts: 4,
		},
		{
			name:  "linestring",
			js:    `{"features":[{"geometry":{"type":"LineString","coordinates":[[0,0],[1,1],[2,2]]}}]}`,
			lines: 1, pts: 3,
		},
		{
			name:  "multilinestring",
			js:    `{"features":[{"geometry":{"type":"MultiLineString","coordinates":[[[0,0],[1,1]],[[2,2],[3,3]],[[4,4],[5,5]]]}}]}`,
			lines: 3, pts: 6,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := polylines(parse(t, tc.js))
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != tc.lines {
				t.Fatalf("got %d polylines, want %d", len(got), tc.lines)
			}
			n := 0
			for _, l := range got {
				n += len(l)
			}
			if n != tc.pts {
				t.Fatalf("got %d points, want %d", n, tc.pts)
			}
		})
	}
}

func TestPolylinesRejectsGeometryItCannotFlatten(t *testing.T) {
	// Silently dropping a geometry would produce a blob that is quietly
	// missing coastline, which is far harder to notice than a failed build.
	if _, err := polylines(parse(t, `{"features":[{"geometry":{"type":"Point","coordinates":[0,0]}}]}`)); err == nil {
		t.Fatal("an unsupported geometry was accepted")
	}
}
