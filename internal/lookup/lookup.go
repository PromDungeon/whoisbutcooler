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
	return !a.IsValid() || a.IsPrivate() || a.IsLoopback() ||
		a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() ||
		a.IsMulticast() || a.IsUnspecified() || a.IsInterfaceLocalMulticast()
}
