package lookup

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

// Real rdap.org response for 8.8.8.8, trimmed to the consumed fields. Note the
// abuse entity is nested one level down, inside the registrant.
const rdapOK = `{
  "objectClassName": "ip network",
  "handle": "NET-8-8-8-0-2",
  "name": "GOGL",
  "startAddress": "8.8.8.0",
  "endAddress": "8.8.8.255",
  "ipVersion": "v4",
  "type": "DIRECT ALLOCATION",
  "cidr0_cidrs": [{"v4prefix": "8.8.8.0", "length": 24}],
  "entities": [
    {
      "objectClassName": "entity",
      "handle": "GOGL",
      "roles": ["registrant"],
      "entities": [
        {
          "objectClassName": "entity",
          "handle": "ABUSE5250-ARIN",
          "roles": ["abuse"],
          "vcardArray": ["vcard", [
            ["version", {}, "text", "4.0"],
            ["adr", {"label": "1600 Amphitheatre Parkway"}, "text", ["", "", "", "", "", "", ""]],
            ["fn", {}, "text", "Abuse"],
            ["kind", {}, "text", "group"],
            ["email", {}, "text", "network-abuse@google.com"]
          ]]
        }
      ]
    }
  ]
}`

func newRDAPTest(t *testing.T, status int, body string) *RDAP {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rdap+json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	c := NewRDAP()
	c.BaseURL = srv.URL
	return c
}

func TestRDAPFindsNestedAbuseContact(t *testing.T) {
	// The abuse entity sits inside the registrant, not at the top level. A
	// flat scan of entities finds only roles:["registrant"] and returns an
	// empty abuse contact for every address on the internet.
	c := newRDAPTest(t, 200, rdapOK)
	got, err := c.Fetch(context.Background(), netip.MustParseAddr("8.8.8.8"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Abuse != "network-abuse@google.com" {
		t.Fatalf("Abuse = %q, want network-abuse@google.com", got.Abuse)
	}
}

func TestRDAPExtractsNameAndCIDR(t *testing.T) {
	c := newRDAPTest(t, 200, rdapOK)
	got, err := c.Fetch(context.Background(), netip.MustParseAddr("8.8.8.8"))
	if err != nil {
		t.Fatal(err)
	}
	if got.NetName != "GOGL" {
		t.Errorf("NetName = %q", got.NetName)
	}
	if got.Network != "8.8.8.0/24" {
		t.Errorf("Network = %q", got.Network)
	}
}

func TestRDAPSkipsNonStringVCardValues(t *testing.T) {
	// The adr entry's value is an array, not a string. Naive indexing into
	// entry[3] as a string panics or yields garbage.
	c := newRDAPTest(t, 200, `{"name":"X","entities":[{"roles":["abuse"],"vcardArray":["vcard",[["adr",{},"text",["","","x"]],["email",{},"text","a@b.c"]]]}]}`)
	got, err := c.Fetch(context.Background(), netip.MustParseAddr("1.1.1.1"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Abuse != "a@b.c" {
		t.Fatalf("Abuse = %q, want a@b.c", got.Abuse)
	}
}

func TestRDAPFallsBackToAddressRangeForCIDR(t *testing.T) {
	// Not every RIR emits the cidr0_cidrs extension.
	c := newRDAPTest(t, 200, `{"name":"Y","startAddress":"1.0.0.0","endAddress":"1.255.255.255"}`)
	got, err := c.Fetch(context.Background(), netip.MustParseAddr("1.1.1.1"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Network != "1.0.0.0/8" {
		t.Fatalf("Network = %q, want 1.0.0.0/8", got.Network)
	}
}

func TestCIDRFromRange(t *testing.T) {
	cases := []struct{ start, end, want string }{
		{"8.8.8.0", "8.8.8.255", "8.8.8.0/24"},
		{"1.0.0.0", "1.255.255.255", "1.0.0.0/8"},
		{"192.168.1.1", "192.168.1.1", "192.168.1.1/32"},
		{"2001:db8::", "2001:db8::ffff", "2001:db8::/112"},
		{"bogus", "1.0.0.0", ""},
		{"1.0.0.0", "2001:db8::", ""},
	}
	for _, tc := range cases {
		if got := cidrFromRange(tc.start, tc.end); got != tc.want {
			t.Errorf("cidrFromRange(%q,%q) = %q, want %q", tc.start, tc.end, got, tc.want)
		}
	}
}

func TestRDAPMissingAbuseIsEmptyNotAnError(t *testing.T) {
	c := newRDAPTest(t, 200, `{"name":"Z","startAddress":"1.0.0.0","endAddress":"1.0.0.255"}`)
	got, err := c.Fetch(context.Background(), netip.MustParseAddr("1.0.0.1"))
	if err != nil {
		t.Fatalf("absent abuse contact treated as an error: %v", err)
	}
	if got.Abuse != "" {
		t.Fatalf("Abuse = %q, want empty", got.Abuse)
	}
}

func TestRDAPRejectsNon200(t *testing.T) {
	c := newRDAPTest(t, 404, `{}`)
	if _, err := c.Fetch(context.Background(), netip.MustParseAddr("1.1.1.1")); err == nil {
		t.Fatal("HTTP 404 was accepted")
	}
}
