package lookup

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

type stubGeo struct {
	name string
	data *GeoData
	err  error
	hits *int
}

func (s stubGeo) Name() string { return s.name }
func (s stubGeo) Fetch(context.Context, netip.Addr) (*GeoData, error) {
	if s.hits != nil {
		*s.hits++
	}
	return s.data, s.err
}

func testClient(geo ...GeoProvider) *Client {
	c := NewClient()
	c.Geo = geo
	c.Registry = nil // exercised separately; nil means "registry unavailable"
	return c
}

func TestLookupRejectsPrivateAddressWithoutCallingProviders(t *testing.T) {
	// Querying a public API about 192.168.1.1 leaks nothing useful and
	// returns nothing useful. Catch it locally.
	hits := 0
	c := testClient(stubGeo{name: "stub", data: &GeoData{}, hits: &hits})
	for _, addr := range []string{"192.168.1.1", "10.0.0.1", "127.0.0.1", "::1", "169.254.1.1"} {
		if _, err := c.Lookup(context.Background(), addr); !errors.Is(err, ErrPrivateRange) {
			t.Errorf("Lookup(%q) err = %v, want ErrPrivateRange", addr, err)
		}
	}
	if hits != 0 {
		t.Fatalf("provider was called %d times for private addresses", hits)
	}
}

func TestLookupFallsBackToSecondProvider(t *testing.T) {
	primary := stubGeo{name: "primary", err: errors.New("down")}
	fallback := stubGeo{name: "ip-api.com", data: &GeoData{Lat: 1, Lon: 2, City: "Berlin"}}
	got, err := testClient(primary, fallback).Lookup(context.Background(), "8.8.8.8")
	if err != nil {
		t.Fatal(err)
	}
	if got.City != "Berlin" {
		t.Errorf("City = %q", got.City)
	}
	// The fallback is HTTP-only, so the UI must be able to say so.
	if got.GeoSource != "ip-api.com" {
		t.Fatalf("GeoSource = %q, want ip-api.com", got.GeoSource)
	}
}

func TestLookupUsesPrimaryWithoutTouchingFallback(t *testing.T) {
	hits := 0
	primary := stubGeo{name: "ipwho.is", data: &GeoData{Lat: 1, Lon: 2}}
	fallback := stubGeo{name: "ip-api.com", data: &GeoData{}, hits: &hits}
	got, err := testClient(primary, fallback).Lookup(context.Background(), "8.8.8.8")
	if err != nil {
		t.Fatal(err)
	}
	if got.GeoSource != "ipwho.is" {
		t.Errorf("GeoSource = %q", got.GeoSource)
	}
	if hits != 0 {
		t.Fatalf("fallback called %d times despite primary succeeding", hits)
	}
}

func TestLookupFailsWhenEveryProviderFails(t *testing.T) {
	c := testClient(
		stubGeo{name: "a", err: errors.New("down")},
		stubGeo{name: "b", err: errors.New("also down")},
	)
	if _, err := c.Lookup(context.Background(), "8.8.8.8"); err == nil {
		t.Fatal("expected an error when no provider answered")
	}
}

func TestLookupSurvivesRegistryFailure(t *testing.T) {
	// RDAP failing must never blank the map. Registry fields stay empty and
	// the geo half renders.
	c := testClient(stubGeo{name: "ipwho.is", data: &GeoData{Lat: 51.5, Lon: -0.12, City: "London"}})
	got, err := c.Lookup(context.Background(), "8.8.8.8")
	if err != nil {
		t.Fatalf("registry failure propagated as a fatal error: %v", err)
	}
	if got.City != "London" || got.Lat != 51.5 {
		t.Errorf("geo half lost: %+v", got)
	}
	if got.Network != "" || got.Abuse != "" {
		t.Errorf("registry fields populated from a nil registry: %+v", got)
	}
}

func TestLookupResolvesHostnames(t *testing.T) {
	c := testClient(stubGeo{name: "ipwho.is", data: &GeoData{Lat: 1, Lon: 2}})
	c.Resolve = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
	}
	got, err := c.Lookup(context.Background(), "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got.IP.String() != "93.184.216.34" {
		t.Errorf("IP = %v", got.IP)
	}
	if got.Query != "example.com" {
		t.Errorf("Query = %q, want the original text", got.Query)
	}
}

func TestLookupRejectsUnresolvableInput(t *testing.T) {
	c := testClient(stubGeo{name: "ipwho.is", data: &GeoData{}})
	c.Resolve = func(context.Context, string) ([]netip.Addr, error) {
		return nil, errors.New("no such host")
	}
	if _, err := c.Lookup(context.Background(), "not a real host"); err == nil {
		t.Fatal("expected an error for unresolvable input")
	}
}

func TestLookupSurvivesRegistryError(t *testing.T) {
	// Registry failures must not propagate. The goroutine runs on the
	// concurrency path, wg.Wait() synchronizes it, and the geo half renders
	// while registry fields stay empty.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	c := testClient(stubGeo{name: "ipwho.is", data: &GeoData{Lat: 51.5, Lon: -0.12, City: "London"}})
	c.Registry = &RDAP{BaseURL: server.URL, HTTP: defaultHTTPClient()}
	got, err := c.Lookup(context.Background(), "8.8.8.8")
	if err != nil {
		t.Fatalf("registry error propagated as fatal: %v", err)
	}
	if got.City != "London" || got.Lat != 51.5 {
		t.Errorf("geo half lost: %+v", got)
	}
	if got.Network != "" || got.Abuse != "" {
		t.Errorf("registry fields populated from a failed registry: %+v", got)
	}
}

func TestLookupRejectsEmptyQuery(t *testing.T) {
	c := testClient(stubGeo{name: "ipwho.is", data: &GeoData{}})
	if _, err := c.Lookup(context.Background(), "   "); err == nil {
		t.Fatal("expected an error for a blank query")
	}
}
