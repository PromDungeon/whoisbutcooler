package lookup

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"
)

// ErrPrivateRange marks an address that no public database can locate.
var ErrPrivateRange = errors.New("address is in a private or reserved range")

// Client fans a query out to a geo provider and the registry.
type Client struct {
	// Geo is tried in order; the first success wins.
	Geo      []GeoProvider
	Registry *RDAP
	// Resolve turns a hostname into addresses. Swappable so tests never touch
	// DNS.
	Resolve func(ctx context.Context, host string) ([]netip.Addr, error)
}

// NewClient wires the live providers: ipwho.is over HTTPS first, ip-api.com as
// a cleartext fallback.
func NewClient() *Client {
	return &Client{
		Geo:      []GeoProvider{NewIPWhois(), NewIPAPI()},
		Registry: NewRDAP(),
		Resolve: func(ctx context.Context, host string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		},
	}
}

// Lookup resolves the query and assembles a Result. The geo half is required;
// the registry half is not, because RDAP failing must never blank the map.
func (c *Client) Lookup(ctx context.Context, query string) (*Result, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("enter an IP address or hostname")
	}

	addr, err := c.resolve(ctx, query)
	if err != nil {
		return nil, err
	}
	if isUnlocatable(addr) {
		return nil, fmt.Errorf("%s: %w", addr, ErrPrivateRange)
	}

	var (
		wg  sync.WaitGroup
		reg *RegistryData
	)
	if c.Registry != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// A registry error is discarded on purpose: the fields simply stay
			// empty and the UI renders them as em dashes.
			if r, err := c.Registry.Fetch(ctx, addr); err == nil {
				reg = r
			}
		}()
	}

	geo, source, geoErr := c.fetchGeo(ctx, addr)
	wg.Wait()

	if geoErr != nil {
		return nil, geoErr
	}

	res := &Result{
		Query: query, IP: addr,
		Lat: geo.Lat, Lon: geo.Lon,
		City: geo.City, Region: geo.Region,
		Country: geo.Country, CountryCode: geo.CountryCode,
		ISP: geo.ISP, ASN: geo.ASN, Timezone: geo.Timezone,
		GeoSource: source,
	}
	if reg != nil {
		res.Network, res.NetName, res.Abuse = reg.Network, reg.NetName, reg.Abuse
	}
	return res, nil
}

func (c *Client) resolve(ctx context.Context, query string) (netip.Addr, error) {
	if addr, err := netip.ParseAddr(query); err == nil {
		return addr.Unmap(), nil
	}
	if c.Resolve == nil {
		return netip.Addr{}, fmt.Errorf("%q is not a valid IP address", query)
	}
	addrs, err := c.Resolve(ctx, query)
	if err != nil || len(addrs) == 0 {
		return netip.Addr{}, fmt.Errorf("could not resolve %q", query)
	}
	return addrs[0].Unmap(), nil
}

func (c *Client) fetchGeo(ctx context.Context, addr netip.Addr) (*GeoData, string, error) {
	var errs []error
	for _, p := range c.Geo {
		g, err := p.Fetch(ctx, addr)
		if err == nil && g != nil {
			return g, p.Name(), nil
		}
		if err == nil {
			err = fmt.Errorf("%s: returned no data", p.Name())
		}
		errs = append(errs, err)
	}
	if len(errs) == 0 {
		return nil, "", errors.New("no geolocation provider configured")
	}
	return nil, "", errors.Join(errs...)
}

// isUnlocatable reports addresses that no public database can place. Catching
// them locally avoids a pointless round trip and a confusing empty answer.
func isUnlocatable(a netip.Addr) bool {
	if !a.IsValid() || a.IsPrivate() || a.IsLoopback() ||
		a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() ||
		a.IsMulticast() || a.IsUnspecified() || a.IsInterfaceLocalMulticast() {
		return true
	}
	a = a.Unmap()
	for _, p := range reservedPrefixes {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// reservedPrefixes are ranges netip has no predicate for that are still not
// globally routable, so no public database can place them.
var reservedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),   // carrier-grade NAT, RFC 6598
	netip.MustParsePrefix("192.0.0.0/24"),    // IETF protocol assignments
	netip.MustParsePrefix("192.0.2.0/24"),    // TEST-NET-1
	netip.MustParsePrefix("198.18.0.0/15"),   // benchmarking, RFC 2544
	netip.MustParsePrefix("198.51.100.0/24"), // TEST-NET-2
	netip.MustParsePrefix("203.0.113.0/24"),  // TEST-NET-3
	netip.MustParsePrefix("240.0.0.0/4"),     // reserved, and 255.255.255.255 with it
	netip.MustParsePrefix("2001:db8::/32"),   // IPv6 documentation
	netip.MustParsePrefix("100::/64"),        // IPv6 discard-only
}
