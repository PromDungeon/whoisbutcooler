package lookup

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/netip"
)

// IPAPI queries ip-api.com. It exists only as a fallback: the free tier is
// HTTP-only, so a lookup through it crosses the network in cleartext. Whenever
// it answers, Result.GeoSource records it and the UI says so.
type IPAPI struct {
	BaseURL string
	HTTP    *http.Client
}

// NewIPAPI returns the fallback provider. The base URL is http:// because the
// free tier does not offer TLS.
func NewIPAPI() *IPAPI {
	return &IPAPI{BaseURL: "http://ip-api.com/json", HTTP: defaultHTTPClient()}
}

func (p *IPAPI) Name() string { return "ip-api.com" }

type ipapiResponse struct {
	Status     string  `json:"status"`
	Message    string  `json:"message"`
	Country    string  `json:"country"`
	CountryCode string  `json:"countryCode"`
	RegionName string  `json:"regionName"`
	City       string  `json:"city"`
	Lat        float64 `json:"lat"`
	Lon        float64 `json:"lon"`
	Timezone   string  `json:"timezone"`
	ISP        string  `json:"isp"`
	AS         string  `json:"as"` // "AS15169 Google LLC"
}

func (p *IPAPI) Fetch(ctx context.Context, ip netip.Addr) (*GeoData, error) {
	url := p.BaseURL + "/" + ip.String() + "?fields=status,message,country,countryCode,regionName,city,lat,lon,timezone,isp,as"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ip-api: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ip-api: HTTP %d", resp.StatusCode)
	}
	var body ipapiResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("ip-api: %w", err)
	}
	if body.Status != "success" {
		msg := body.Message
		if msg == "" {
			msg = "lookup failed"
		}
		return nil, fmt.Errorf("ip-api: %s", msg)
	}
	g := &GeoData{
		Lat: body.Lat, Lon: body.Lon,
		City: body.City, Region: body.RegionName,
		Country: body.Country, CountryCode: body.CountryCode,
		ISP: body.ISP, Timezone: body.Timezone,
	}
	// The "as" field is "AS15169 Google LLC"; keep only the number.
	if body.AS != "" {
		if i := indexByte(body.AS, ' '); i > 0 {
			g.ASN = body.AS[:i]
		} else {
			g.ASN = body.AS
		}
	}
	return g, nil
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}
