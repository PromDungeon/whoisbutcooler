package ui

import (
	"math"
	"strings"
	"testing"

	"github.com/PromDungeon/whoisbutcooler/internal/geo"
	"github.com/PromDungeon/whoisbutcooler/internal/lookup"
	tea "github.com/charmbracelet/bubbletea"
)

func sized(m Model, w, h int) Model {
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return next.(Model)
}

func TestViewRendersMapAndPanelTogether(t *testing.T) {
	m := sized(New(nil, ""), 120, 34)
	res := fullResult()
	res.Lat, res.Lon = 37.34, -121.89
	next, _ := m.Update(lookupMsg{res: res})
	out := next.(Model).View()

	if !strings.ContainsFunc(out, func(r rune) bool { return r >= 0x2801 && r <= 0x28FF }) {
		t.Error("no braille in the rendered frame")
	}
	if !strings.Contains(out, "AS15169") {
		t.Error("panel content missing from the frame")
	}
}

func TestSuccessfulLookupFitsViewportToPin(t *testing.T) {
	m := sized(New(nil, ""), 120, 34)
	res := fullResult()
	res.Lat, res.Lon = 51.5, -0.12
	next, _ := m.Update(lookupMsg{res: res})
	got := next.(Model).view
	if got.LonSpan != geo.FitSpan {
		t.Errorf("span = %v, want %v", got.LonSpan, geo.FitSpan)
	}
	// geo.FitTo routes the longitude through NormLon (a Mod-360 then a
	// subtraction), which does not round-trip -0.12 bit-exactly; geo's own
	// TestFitToUsesRegionalSpan uses the same epsilon comparison for the
	// identical input.
	if math.Abs(got.CenterLon-(-0.12)) > 1e-9 {
		t.Errorf("centre lon = %v, want -0.12", got.CenterLon)
	}
}

func TestPrivateRangeErrorIsExplainedNotDumped(t *testing.T) {
	m := sized(New(nil, ""), 120, 34)
	next, _ := m.Update(lookupMsg{err: lookup.ErrPrivateRange})
	out := next.(Model).View()
	if !strings.Contains(strings.ToLower(out), "private") {
		t.Fatalf("private-range error not surfaced:\n%s", out)
	}
}

func TestFallbackProviderIsSurfacedInStatus(t *testing.T) {
	// The fallback is HTTP-only. Using it must never be silent.
	m := sized(New(nil, ""), 120, 34)
	res := fullResult()
	res.GeoSource = "ip-api.com"
	next, _ := m.Update(lookupMsg{res: res})
	if !strings.Contains(next.(Model).View(), "ip-api.com") {
		t.Fatal("fallback provider not named anywhere in the frame")
	}
}

func TestPrimaryProviderIsNotAdvertised(t *testing.T) {
	m := sized(New(nil, ""), 120, 34)
	next, _ := m.Update(lookupMsg{res: fullResult()})
	if strings.Contains(next.(Model).View(), "ipwho.is") {
		t.Fatal("primary provider named in the status line; only the fallback should be")
	}
}

func TestTabSwitchesFocusAndArrowsThenPan(t *testing.T) {
	m := sized(New(nil, ""), 120, 34)
	before := m.view.CenterLon

	// While the input is focused, arrows belong to the text field.
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	if next.(Model).view.CenterLon != before {
		t.Fatal("arrow panned the map while the input was focused")
	}

	next, _ = next.(Model).Update(tea.KeyMsg{Type: tea.KeyTab})
	panned, _ := next.(Model).Update(tea.KeyMsg{Type: tea.KeyRight})
	if panned.(Model).view.CenterLon == before {
		t.Fatal("arrow did not pan the map after focus moved to it")
	}
}

func TestZoomKeysWalkTheLadder(t *testing.T) {
	m := sized(New(nil, ""), 120, 34)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	zoomed, _ := next.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'+'}})
	if zoomed.(Model).view.LonSpan >= m.view.LonSpan {
		t.Fatalf("'+' did not zoom in: %v -> %v", m.view.LonSpan, zoomed.(Model).view.LonSpan)
	}
	reset, _ := zoomed.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'0'}})
	if reset.(Model).view.LonSpan != geo.World().LonSpan {
		t.Error("'0' without a result should reset to the world view")
	}
}

func TestHistoryRecallWalksBackwards(t *testing.T) {
	m := sized(New(nil, ""), 120, 34)
	m.history = []string{"1.1.1.1", "8.8.8.8"}
	m.histIdx = len(m.history)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if got := next.(Model).input.Value(); got != "8.8.8.8" {
		t.Fatalf("first recall = %q, want the most recent entry", got)
	}
	next, _ = next.(Model).Update(tea.KeyMsg{Type: tea.KeyUp})
	if got := next.(Model).input.Value(); got != "1.1.1.1" {
		t.Fatalf("second recall = %q", got)
	}
}
