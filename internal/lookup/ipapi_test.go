package lookup

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

// Real ip-api.com response for 8.8.8.8, trimmed to the fields consumed.
const ipapiOK = `{"status":"success","country":"United States","countryCode":"US",
"regionName":"Virginia","city":"Ashburn","lat":39.03,"lon":-77.5,
"timezone":"America/New_York","isp":"Google LLC","as":"AS15169 Google LLC"}`

// newIPAPITest points the provider at a stub server and records the path and
// query it was asked for, so the ?fields= list can be asserted without a live
// request.
func newIPAPITest(t *testing.T, status int, body string) (*IPAPI, *string) {
	t.Helper()
	var gotURI string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURI = r.URL.RequestURI()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	p := NewIPAPI()
	p.BaseURL = srv.URL
	return p, &gotURI
}

func TestIPAPIParsesRealResponse(t *testing.T) {
	p, _ := newIPAPITest(t, 200, ipapiOK)
	got, err := p.Fetch(context.Background(), netip.MustParseAddr("8.8.8.8"))
	if err != nil {
		t.Fatal(err)
	}
	// regionName, not region: ip-api's "region" is the two-letter code.
	if got.City != "Ashburn" || got.Region != "Virginia" || got.CountryCode != "US" {
		t.Errorf("location = %q/%q/%q", got.City, got.Region, got.CountryCode)
	}
	if got.Lat != 39.03 || got.Lon != -77.5 {
		t.Errorf("coords = %v,%v", got.Lat, got.Lon)
	}
	if got.ISP != "Google LLC" || got.Timezone != "America/New_York" {
		t.Errorf("ISP/timezone = %q/%q", got.ISP, got.Timezone)
	}
}

func TestIPAPIKeepsOnlyTheASNumber(t *testing.T) {
	// The "as" field is "AS15169 Google LLC" -- number and org name in one
	// string. Copying it whole puts the org name in the panel's ASN slot,
	// where it duplicates the ISP line beside it.
	p, _ := newIPAPITest(t, 200, ipapiOK)
	got, err := p.Fetch(context.Background(), netip.MustParseAddr("8.8.8.8"))
	if err != nil {
		t.Fatal(err)
	}
	if got.ASN != "AS15169" {
		t.Fatalf("ASN = %q, want AS15169", got.ASN)
	}
}

func TestIPAPIHandlesASFieldWithoutAnOrgName(t *testing.T) {
	// Some allocations answer with the number alone, and one with no space in
	// it must not be truncated to nothing.
	cases := map[string]string{
		`{"status":"success","as":"AS15169"}`: "AS15169",
		`{"status":"success","as":""}`:        "",
	}
	for body, want := range cases {
		p, _ := newIPAPITest(t, 200, body)
		got, err := p.Fetch(context.Background(), netip.MustParseAddr("8.8.8.8"))
		if err != nil {
			t.Fatal(err)
		}
		if got.ASN != want {
			t.Errorf("%s: ASN = %q, want %q", body, got.ASN, want)
		}
	}
}

func TestIPAPIRequestsOnlyTheFieldsUsed(t *testing.T) {
	// ip-api is the cleartext fallback, so the query string is the thing
	// crossing the network in the clear: it must name the address once and
	// ask for nothing beyond the fields the panel shows.
	p, uri := newIPAPITest(t, 200, ipapiOK)
	if _, err := p.Fetch(context.Background(), netip.MustParseAddr("8.8.8.8")); err != nil {
		t.Fatal(err)
	}
	path, query, found := strings.Cut(*uri, "?")
	if !found {
		t.Fatalf("no query string in %q", *uri)
	}
	if path != "/8.8.8.8" {
		t.Errorf("path = %q, want /8.8.8.8", path)
	}
	want := "fields=status,message,country,countryCode,regionName,city,lat,lon,timezone,isp,as"
	if query != want {
		t.Errorf("query = %q, want %q", query, want)
	}
}

func TestIPAPITreatsStatusFailAsError(t *testing.T) {
	// Failure arrives as HTTP 200 with status:"fail", so the status code
	// alone would surface an all-zero result -- and zero lat/lon is a real
	// place in the Gulf of Guinea, which the map would happily pin.
	p, _ := newIPAPITest(t, 200, `{"status":"fail","message":"reserved range"}`)
	_, err := p.Fetch(context.Background(), netip.MustParseAddr("10.0.0.1"))
	if err == nil {
		t.Fatal(`status:"fail" with HTTP 200 was accepted as a valid response`)
	}
	if !strings.Contains(err.Error(), "reserved range") {
		t.Errorf("error = %v, want it to carry the API's message", err)
	}
}

func TestIPAPIFallsBackToAGenericMessage(t *testing.T) {
	p, _ := newIPAPITest(t, 200, `{"status":"fail"}`)
	_, err := p.Fetch(context.Background(), netip.MustParseAddr("10.0.0.1"))
	if err == nil || !strings.Contains(err.Error(), "lookup failed") {
		t.Fatalf("err = %v, want a generic message when the API sends none", err)
	}
}

func TestIPAPIRejectsNon200(t *testing.T) {
	// The body is a perfectly good success payload: rate limiting is what
	// this provider does under load, and only the status code says so. A body
	// that also failed to parse would pass this test with the status check
	// deleted.
	p, _ := newIPAPITest(t, 429, ipapiOK)
	if _, err := p.Fetch(context.Background(), netip.MustParseAddr("8.8.8.8")); err == nil {
		t.Fatal("HTTP 429 was accepted")
	}
}

func TestIPAPIRejectsMalformedJSON(t *testing.T) {
	p, _ := newIPAPITest(t, 200, `{"status":"success",`)
	if _, err := p.Fetch(context.Background(), netip.MustParseAddr("8.8.8.8")); err == nil {
		t.Fatal("truncated JSON was accepted")
	}
}

func TestIPAPIName(t *testing.T) {
	// Result.GeoSource is compared against this string to decide whether to
	// warn that the answer came over plain HTTP.
	if got := NewIPAPI().Name(); got != "ip-api.com" {
		t.Fatalf("Name = %q", got)
	}
}

func TestIPAPIDefaultBaseURLIsTheDocumentedEndpoint(t *testing.T) {
	// http:// is not an oversight -- the free tier offers no TLS -- but it is
	// the reason the UI announces this provider, so pin it rather than let it
	// drift silently either way.
	if got := NewIPAPI().BaseURL; got != "http://ip-api.com/json" {
		t.Fatalf("BaseURL = %q", got)
	}
}
