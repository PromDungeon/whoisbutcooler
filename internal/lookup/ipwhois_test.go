package lookup

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

// Real ipwho.is response for 8.8.8.8, trimmed to the fields consumed.
const ipwhoisOK = `{"ip":"8.8.8.8","success":true,"type":"IPv4",
"country":"United States","country_code":"US","region":"California",
"city":"San Jose","latitude":37.3393939,"longitude":-121.8949553,
"connection":{"asn":15169,"org":"Google LLC","isp":"Google LLC","domain":"google.com"},
"timezone":{"id":"America/Los_Angeles","abbr":"PDT"}}`

func newIPWhoisTest(t *testing.T, status int, body string) *IPWhois {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	p := NewIPWhois()
	p.BaseURL = srv.URL
	return p
}

func TestIPWhoisParsesRealResponse(t *testing.T) {
	p := newIPWhoisTest(t, 200, ipwhoisOK)
	got, err := p.Fetch(context.Background(), netip.MustParseAddr("8.8.8.8"))
	if err != nil {
		t.Fatal(err)
	}
	if got.City != "San Jose" || got.Region != "California" || got.CountryCode != "US" {
		t.Errorf("location = %q/%q/%q", got.City, got.Region, got.CountryCode)
	}
	if got.Lat != 37.3393939 || got.Lon != -121.8949553 {
		t.Errorf("coords = %v,%v", got.Lat, got.Lon)
	}
	if got.ISP != "Google LLC" {
		t.Errorf("ISP = %q", got.ISP)
	}
	// The API returns asn as a JSON number; it must be formatted, not
	// string-copied.
	if got.ASN != "AS15169" {
		t.Errorf("ASN = %q, want AS15169", got.ASN)
	}
	if got.Timezone != "America/Los_Angeles" {
		t.Errorf("timezone = %q", got.Timezone)
	}
}

func TestIPWhoisTreatsSuccessFalseAsError(t *testing.T) {
	// ipwho.is reports failure with HTTP 200 and success:false. Trusting the
	// status code alone would surface an empty result as a valid lookup.
	p := newIPWhoisTest(t, 200, `{"success":false,"message":"404 not found"}`)
	_, err := p.Fetch(context.Background(), netip.MustParseAddr("8.8.8.8"))
	if err == nil {
		t.Fatal("success:false with HTTP 200 was accepted as a valid response")
	}
}

func TestIPWhoisOmitsUnknownASN(t *testing.T) {
	p := newIPWhoisTest(t, 200, `{"success":true,"latitude":1,"longitude":2,"connection":{"asn":0}}`)
	got, err := p.Fetch(context.Background(), netip.MustParseAddr("8.8.8.8"))
	if err != nil {
		t.Fatal(err)
	}
	if got.ASN != "" {
		t.Fatalf("ASN = %q, want empty for asn 0", got.ASN)
	}
}

func TestIPWhoisRejectsNon200(t *testing.T) {
	// A valid success body under a 5xx: with an unparseable body this passes
	// even with the status-code check deleted, pinning the JSON decoder
	// rather than the check it names.
	p := newIPWhoisTest(t, 500, ipwhoisOK)
	if _, err := p.Fetch(context.Background(), netip.MustParseAddr("8.8.8.8")); err == nil {
		t.Fatal("HTTP 500 was accepted")
	}
}

func TestIPWhoisRejectsMalformedJSON(t *testing.T) {
	p := newIPWhoisTest(t, 200, `{"success":true,`)
	if _, err := p.Fetch(context.Background(), netip.MustParseAddr("8.8.8.8")); err == nil {
		t.Fatal("truncated JSON was accepted")
	}
}

func TestIPWhoisName(t *testing.T) {
	if got := NewIPWhois().Name(); got != "ipwho.is" {
		t.Fatalf("Name = %q", got)
	}
}
