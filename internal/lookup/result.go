// Package lookup resolves an IP or hostname into the handful of facts worth
// showing. It performs no rendering and knows nothing about terminals.
package lookup

import (
	"context"
	"net/http"
	"net/netip"
	"time"
)

// Result is the merged view assembled from a geo provider and RDAP. Fields
// left empty are rendered as an em dash by the UI rather than being hidden, so
// the panel height stays constant between lookups.
type Result struct {
	Query string     // exactly what the user typed
	IP    netip.Addr // resolved address

	Lat, Lon float64

	City        string
	Region      string
	Country     string
	CountryCode string
	ISP         string
	ASN         string
	Timezone    string

	Network string // e.g. "8.8.8.0/24"
	NetName string // e.g. "GOGL"
	Abuse   string // e.g. "network-abuse@google.com"

	// GeoSource names the provider that answered. The UI surfaces it whenever
	// it is not the primary, so a cleartext fallback is never silent.
	GeoSource string
}

// GeoData is what a geo provider contributes.
type GeoData struct {
	Lat, Lon    float64
	City        string
	Region      string
	Country     string
	CountryCode string
	ISP         string
	ASN         string
	Timezone    string
}

// GeoProvider is one source of geolocation. Two implement it: ipwho.is over
// HTTPS as primary, ip-api.com as fallback.
type GeoProvider interface {
	Name() string
	Fetch(ctx context.Context, ip netip.Addr) (*GeoData, error)
}

// requestTimeout bounds a single provider call. The UI stays responsive
// because lookups run off the render goroutine, but a hung provider must not
// pin a spinner forever.
const requestTimeout = 5 * time.Second

func defaultHTTPClient() *http.Client { return &http.Client{Timeout: requestTimeout} }
