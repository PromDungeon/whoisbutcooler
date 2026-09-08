package lookup

import (
	"context"
	"encoding/json"
	"fmt"
	"math/bits"
	"net/http"
	"net/netip"
	"slices"
)

// RegistryData is what RDAP contributes: the allocation, not the location.
type RegistryData struct {
	Network string
	NetName string
	Abuse   string
}

// RDAP queries the registry. rdap.org is a bootstrap service that redirects to
// whichever RIR is authoritative for the address, which saves us maintaining
// the RIR table ourselves.
type RDAP struct {
	BaseURL string
	HTTP    *http.Client
}

// NewRDAP returns a client pointed at the live bootstrap service.
func NewRDAP() *RDAP {
	return &RDAP{BaseURL: "https://rdap.org/ip", HTTP: defaultHTTPClient()}
}

type rdapEntity struct {
	Handle   string            `json:"handle"`
	Roles    []string          `json:"roles"`
	Entities []rdapEntity      `json:"entities"`
	VCard    []json.RawMessage `json:"vcardArray"`
}

type rdapResponse struct {
	Name         string `json:"name"`
	StartAddress string `json:"startAddress"`
	EndAddress   string `json:"endAddress"`
	CIDRs        []struct {
		V4Prefix string `json:"v4prefix"`
		V6Prefix string `json:"v6prefix"`
		Length   int    `json:"length"`
	} `json:"cidr0_cidrs"`
	Entities []rdapEntity `json:"entities"`
}

func (c *RDAP) Fetch(ctx context.Context, ip netip.Addr) (*RegistryData, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/"+ip.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/rdap+json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("rdap: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rdap: HTTP %d", resp.StatusCode)
	}

	var body rdapResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("rdap: %w", err)
	}
	return &RegistryData{
		NetName: body.Name,
		Network: networkOf(&body),
		Abuse:   findAbuseEmail(body.Entities),
	}, nil
}

// networkOf prefers the cidr0 extension and falls back to deriving the prefix
// from the address range, which some registries return instead.
func networkOf(r *rdapResponse) string {
	if len(r.CIDRs) > 0 {
		c := r.CIDRs[0]
		prefix := c.V4Prefix
		if prefix == "" {
			prefix = c.V6Prefix
		}
		if prefix != "" {
			return fmt.Sprintf("%s/%d", prefix, c.Length)
		}
	}
	return cidrFromRange(r.StartAddress, r.EndAddress)
}

// cidrFromRange derives the covering prefix from an inclusive address range by
// counting the bits the endpoints share.
func cidrFromRange(startStr, endStr string) string {
	start, err1 := netip.ParseAddr(startStr)
	end, err2 := netip.ParseAddr(endStr)
	if err1 != nil || err2 != nil || start.BitLen() != end.BitLen() {
		return ""
	}
	sb, eb := start.AsSlice(), end.AsSlice()
	ones := start.BitLen()
	for i := range sb {
		if d := sb[i] ^ eb[i]; d != 0 {
			ones = i*8 + bits.LeadingZeros8(d)
			break
		}
	}
	p, err := start.Prefix(ones)
	if err != nil {
		return ""
	}
	return p.String()
}

// findAbuseEmail walks the entity tree depth-first. The search must recurse:
// registries nest the abuse contact inside the registrant entity, so a flat
// scan of the top level finds only roles:["registrant"] and reports no abuse
// contact for every address on the internet.
func findAbuseEmail(entities []rdapEntity) string {
	for _, e := range entities {
		if slices.Contains(e.Roles, "abuse") {
			if email := vcardEmail(e.VCard); email != "" {
				return email
			}
		}
		if email := findAbuseEmail(e.Entities); email != "" {
			return email
		}
	}
	return ""
}

// vcardEmail pulls the first email out of a jCard array. The format is
// ["vcard", [[name, params, type, value], ...]] where value's type varies by
// entry: a string for email, a nested array for adr. Entries whose value will
// not unmarshal into a string are skipped rather than treated as an error.
func vcardEmail(vcard []json.RawMessage) string {
	if len(vcard) < 2 {
		return ""
	}
	var entries [][]json.RawMessage
	if err := json.Unmarshal(vcard[1], &entries); err != nil {
		return ""
	}
	for _, entry := range entries {
		if len(entry) < 4 {
			continue
		}
		var name string
		if json.Unmarshal(entry[0], &name) != nil || name != "email" {
			continue
		}
		var value string
		if json.Unmarshal(entry[3], &value) == nil && value != "" {
			return value
		}
	}
	return ""
}
