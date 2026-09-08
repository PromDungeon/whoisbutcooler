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
	for name, res := range map[string]*lookup.Result{"full": fullResult(), "nil": nil} {
		for _, line := range panelLines(RenderPanel(res, PanelWidth)) {
			if w := utf8.RuneCountInString(line); w != PanelWidth {
				t.Errorf("%s: line %q is %d columns wide, want %d", name, line, w, PanelWidth)
			}
		}
	}
}

func TestPanelLineCountIsConstantAcrossAnyResult(t *testing.T) {
	// The whole point of the fixed seven-line body is that the block's
	// height never changes, including between no result at all and a fully
	// populated one — not just between two populated-but-partial results,
	// which TestPanelHeightIsConstantRegardlessOfMissingFields alone would
	// not catch if both happened to wrap by the same amount.
	nilLines := len(panelLines(RenderPanel(nil, PanelWidth)))
	fullLines := len(panelLines(RenderPanel(fullResult(), PanelWidth)))
	if nilLines != fullLines {
		t.Fatalf("panel line count changed: nil result = %d lines, full result = %d lines", nilLines, fullLines)
	}
}
