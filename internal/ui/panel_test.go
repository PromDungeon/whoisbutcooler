package ui

import (
	"net/netip"
	"strings"
	"testing"

	"github.com/PromDungeon/whoisbutcooler/internal/lookup"
)

func fullResult() *lookup.Result {
	return &lookup.Result{
		Query: "8.8.8.8", IP: netip.MustParseAddr("8.8.8.8"),
		City: "San Jose", Region: "California",
		Country: "United States", CountryCode: "US",
		ISP: "Google LLC", ASN: "AS15169", Timezone: "America/Los_Angeles",
		Network: "8.8.8.0/24", NetName: "GOGL",
		Abuse: "network-abuse@google.com", GeoSource: "ipwho.is",
	}
}

func TestPanelShowsAllSevenFacts(t *testing.T) {
	got := RenderPanel(fullResult(), PanelWidth)
	for _, want := range []string{
		"8.8.8.8", "San Jose", "California", "US",
		"Google LLC", "AS15169", "8.8.8.0/24", "GOGL",
		"network-abuse@google.com", "America/Los_Angeles",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("panel is missing %q:\n%s", want, got)
		}
	}
}

func TestPanelHeightIsConstantRegardlessOfMissingFields(t *testing.T) {
	// Unavailable registry fields render as an em dash rather than
	// disappearing, so the panel does not reflow between lookups.
	full := strings.Count(RenderPanel(fullResult(), PanelWidth), "\n")
	sparse := fullResult()
	sparse.Network, sparse.NetName, sparse.Abuse, sparse.Timezone = "", "", "", ""
	got := strings.Count(RenderPanel(sparse, PanelWidth), "\n")
	if full != got {
		t.Fatalf("panel height changed with missing fields: %d vs %d", full, got)
	}
	if !strings.Contains(RenderPanel(sparse, PanelWidth), "—") {
		t.Error("missing fields did not render as an em dash")
	}
}

func TestPanelIsBlankWithoutAResult(t *testing.T) {
	if got := RenderPanel(nil, PanelWidth); strings.Contains(got, "AS") {
		t.Fatalf("nil result rendered content: %q", got)
	}
}
