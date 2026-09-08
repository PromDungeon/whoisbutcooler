package lookup

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/netip"
)

// IPWhois queries ipwho.is. It is the primary provider because it is keyless
// and serves over HTTPS.
type IPWhois struct {
	BaseURL string
	HTTP    *http.Client
}

// NewIPWhois returns a provider pointed at the live service.
func NewIPWhois() *IPWhois {
	return &IPWhois{BaseURL: "https://ipwho.is", HTTP: defaultHTTPClient()}
}

func (p *IPWhois) Name() string { return "ipwho.is" }

type ipwhoisResponse struct {
	Success     bool    `json:"success"`
	Message     string  `json:"message"`
	Country     string  `json:"country"`
	CountryCode string  `json:"country_code"`
	Region      string  `json:"region"`
	City        string  `json:"city"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	Connection  struct {
		ASN int    `json:"asn"` // a JSON number, not "AS15169"
		Org string `json:"org"`
		ISP string `json:"isp"`
	} `json:"connection"`
	Timezone struct {
		ID string `json:"id"`
	} `json:"timezone"`
}

func (p *IPWhois) Fetch(ctx context.Context, ip netip.Addr) (*GeoData, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.BaseURL+"/"+ip.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ipwho.is: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ipwho.is: HTTP %d", resp.StatusCode)
	}

	var body ipwhoisResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("ipwho.is: %w", err)
	}
	// Failure arrives as HTTP 200 with success:false, so the status code alone
	// is not enough to tell a hit from a miss.
	if !body.Success {
		msg := body.Message
		if msg == "" {
			msg = "lookup failed"
		}
		return nil, fmt.Errorf("ipwho.is: %s", msg)
	}

	g := &GeoData{
		Lat:         body.Latitude,
		Lon:         body.Longitude,
		City:        body.City,
		Region:      body.Region,
		Country:     body.Country,
		CountryCode: body.CountryCode,
		ISP:         body.Connection.ISP,
		Timezone:    body.Timezone.ID,
	}
	if g.ISP == "" {
		g.ISP = body.Connection.Org
	}
	if body.Connection.ASN != 0 {
		g.ASN = fmt.Sprintf("AS%d", body.Connection.ASN)
	}
	return g, nil
}
