package ui

import (
	"net/netip"
	"strings"
	"testing"
	"unicode/utf8"

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

// longResult is a real-shaped result whose values are long enough to wrap:
// a German residential IPv6 allocation. Every fixture in this file used to be
// short enough to fit, which is why the height tests passed while the panel
// reflowed from nine rows to thirteen on results like this one.
func longResult() *lookup.Result {
	return &lookup.Result{
		Query: "2a02:8108:9640:2000:1234:5678:9abc:def0",
		IP:    netip.MustParseAddr("2a02:8108:9640:2000:1234:5678:9abc:def0"),
		City:  "Frankfurt am Main", Region: "Hessen",
		Country: "Germany", CountryCode: "DE",
		ISP: "Deutsche Telekom AG Internet Service Provider", ASN: "AS3320",
		Timezone: "America/Argentina/ComodRivadavia",
		Network:  "2a02:8108:9640:2000::/64", NetName: "DTAG-STATIC-IP-POOL",
		Abuse:     "abuse-response-team@deutsche-telekom-ag.example.com",
		GeoSource: "ipwho.is",
	}
}

// fixtures are every result shape the panel must render at a constant height.
func fixtures() map[string]*lookup.Result {
	sparse := fullResult()
	sparse.Network, sparse.NetName, sparse.Abuse, sparse.Timezone = "", "", "", ""
	return map[string]*lookup.Result{
		"nil":    nil,
		"full":   fullResult(),
		"sparse": sparse,
		"long":   longResult(),
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

// panelLines splits a rendered panel into its lines, measured in runes rather
// than bytes: the border and rule are drawn with multi-byte box-drawing
// characters, so len() would overcount every one of them.
func panelLines(rendered string) []string {
	return strings.Split(rendered, "\n")
}

func TestPanelWidthMatchesPanelWidthConstant(t *testing.T) {
	// mapCells() in model.go reserves exactly PanelWidth columns for this
	// block. If the rendered block is narrower, that reservation starves the
	// map of columns it could use; if it is wider, the layout overflows.
	for name, res := range fixtures() {
		for _, line := range panelLines(RenderPanel(res, PanelWidth)) {
			if w := utf8.RuneCountInString(line); w != PanelWidth {
				t.Errorf("%s: line %q is %d columns wide, want %d", name, line, w, PanelWidth)
			}
		}
	}
}

func TestPanelLineCountIsConstantAcrossAnyResult(t *testing.T) {
	// The whole point of the fixed seven-line body is that the block's height
	// never changes, whatever lands in it: no result at all, a partially
	// populated one, or the long real-world values of longResult, which wrap
	// to thirteen rows unless the body lines are truncated first. Two
	// populated-but-short fixtures alone would not catch that, since both
	// happen to wrap by the same amount — namely none.
	const wantLines = 9 // 7 body rows plus the top and bottom border
	for name, res := range fixtures() {
		if got := len(panelLines(RenderPanel(res, PanelWidth))); got != wantLines {
			t.Errorf("%s result rendered %d lines, want %d:\n%s",
				name, got, wantLines, RenderPanel(res, PanelWidth))
		}
	}
}

func TestPanelTruncatesRatherThanDroppingTheFieldEntirely(t *testing.T) {
	// Truncation must still leave the value recognisable: enough of the ISP
	// name to identify the operator, and a visible mark that there is more.
	got := RenderPanel(longResult(), PanelWidth)
	if !strings.Contains(got, "Deutsche Telekom AG") {
		t.Errorf("long ISP name was not shown at all:\n%s", got)
	}
	if !strings.Contains(got, "…") {
		t.Errorf("truncation was not marked with an ellipsis:\n%s", got)
	}
}
