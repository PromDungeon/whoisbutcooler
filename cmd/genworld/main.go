// Command genworld converts a Natural Earth land GeoJSON file into the compact
// binary polyline format embedded by internal/world. It is committed so the
// derivation is reproducible, but it runs rarely: coastlines do not move.
//
// Usage:
//
//	go run ./cmd/genworld -in ne_110m_land.geojson -out internal/world/world.bin
//	go run ./cmd/genworld -in ne_110m_admin_0_boundary_lines_land.geojson -out internal/world/borders.bin
package main

import (
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
)

type featureCollection struct {
	Features []struct {
		Geometry struct {
			Type        string          `json:"type"`
			Coordinates json.RawMessage `json:"coordinates"`
		} `json:"geometry"`
	} `json:"features"`
}

func main() {
	in := flag.String("in", "", "path to Natural Earth land GeoJSON")
	out := flag.String("out", "", "path to write the binary blob")
	flag.Parse()
	if *in == "" || *out == "" {
		log.Fatal("both -in and -out are required")
	}

	raw, err := os.ReadFile(*in)
	if err != nil {
		log.Fatal(err)
	}
	var fc featureCollection
	if err := json.Unmarshal(raw, &fc); err != nil {
		log.Fatal(err)
	}

	rings, err := polylines(fc)
	if err != nil {
		log.Fatal(err)
	}

	fh, err := os.Create(*out)
	if err != nil {
		log.Fatal(err)
	}
	defer fh.Close()

	if err := binary.Write(fh, binary.LittleEndian, uint32(len(rings))); err != nil {
		log.Fatal(err)
	}
	total := 0
	for _, ring := range rings {
		if err := binary.Write(fh, binary.LittleEndian, uint32(len(ring))); err != nil {
			log.Fatal(err)
		}
		for _, pt := range ring {
			// float32 is ~7 significant digits, far finer than a braille dot
			// at any zoom this tool offers.
			if err := binary.Write(fh, binary.LittleEndian, [2]float32{float32(pt[0]), float32(pt[1])}); err != nil {
				log.Fatal(err)
			}
		}
		total += len(ring)
	}
	fmt.Printf("wrote %d polylines, %d points to %s\n", len(rings), total, *out)
}

// polylines flattens a feature collection into bare polylines. Natural Earth
// ships land as polygons, whose every ring is a closed polyline, and boundaries
// as line strings, which already are polylines. One generator reads both so the
// two blobs cannot drift into different formats.
func polylines(fc featureCollection) ([][][2]float64, error) {
	var out [][][2]float64
	for _, f := range fc.Features {
		switch f.Geometry.Type {
		case "Polygon":
			var p [][][2]float64
			if err := json.Unmarshal(f.Geometry.Coordinates, &p); err != nil {
				return nil, err
			}
			out = append(out, p...)
		case "MultiPolygon":
			var mp [][][][2]float64
			if err := json.Unmarshal(f.Geometry.Coordinates, &mp); err != nil {
				return nil, err
			}
			for _, p := range mp {
				out = append(out, p...)
			}
		case "LineString":
			var l [][2]float64
			if err := json.Unmarshal(f.Geometry.Coordinates, &l); err != nil {
				return nil, err
			}
			out = append(out, l)
		case "MultiLineString":
			var ml [][][2]float64
			if err := json.Unmarshal(f.Geometry.Coordinates, &ml); err != nil {
				return nil, err
			}
			out = append(out, ml...)
		default:
			return nil, fmt.Errorf("unsupported geometry %q", f.Geometry.Type)
		}
	}
	return out, nil
}
