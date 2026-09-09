package ui

import (
	"errors"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/PromDungeon/whoisbutcooler/internal/canvas"
	"github.com/PromDungeon/whoisbutcooler/internal/geo"
	"github.com/PromDungeon/whoisbutcooler/internal/lookup"
	"github.com/PromDungeon/whoisbutcooler/internal/world"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	// ErrPrivateRange's raw text already contains "private", so asserting
	// only that would pass even if applyLookup's friendly branch were
	// deleted. Pin wording unique to the friendly message instead, and
	// confirm the raw error text is not what actually gets displayed.
	m := sized(New(nil, ""), 120, 34)
	next, _ := m.Update(lookupMsg{err: lookup.ErrPrivateRange})
	got := next.(Model)
	out := got.View()
	if !strings.Contains(strings.ToLower(out), "no public database can place it") {
		t.Fatalf("private-range error not explained in friendly terms:\n%s", out)
	}
	if got.status == lookup.ErrPrivateRange.Error() {
		t.Fatalf("status is the raw error text, not a friendly explanation: %q", got.status)
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

func TestDownArrowDoesNotClobberInProgressTyping(t *testing.T) {
	// histIdx == len(history) is the normal state at startup and after every
	// submission — nothing has been recalled. Down must be a no-op there
	// rather than clearing text the user is mid-typing.
	m := sized(New(nil, ""), 120, 34)
	m.history = []string{"1.1.1.1", "8.8.8.8"}
	m.histIdx = len(m.history)

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("9.9.9.9")})
	typed := next.(Model)
	if got := typed.input.Value(); got != "9.9.9.9" {
		t.Fatalf("typed text = %q, want %q", got, "9.9.9.9")
	}

	after, _ := typed.Update(tea.KeyMsg{Type: tea.KeyDown})
	if got := after.(Model).input.Value(); got != "9.9.9.9" {
		t.Fatalf("Down destroyed in-progress input: got %q, want %q", got, "9.9.9.9")
	}
}

// submitted returns the model that typing q at the prompt and pressing Enter
// produces — the reference state the command-line path has to match.
func submitted(t *testing.T, q string) Model {
	t.Helper()
	m := sized(New(nil, ""), 120, 34)
	typed, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(q)})
	entered, _ := typed.(Model).Update(tea.KeyMsg{Type: tea.KeyEnter})
	return entered.(Model)
}

func TestInitialQueryLeavesTheSameStateAsPressingEnter(t *testing.T) {
	// `whoisbutcooler 8.8.8.8` is the usual invocation, and Init returns only
	// a Cmd, so whatever the Enter path records has to be recorded in New or
	// it is simply absent. It was: r retried nothing, up recalled nothing,
	// the prompt still held the query after the result landed, and no status
	// said a lookup was running.
	//
	// Comparing against the Enter path rather than against four hardcoded
	// values keeps the two from drifting apart again the next time one of
	// them changes.
	const q = "8.8.8.8"
	want := submitted(t, q)
	got := sized(New(nil, q), 120, 34)

	if got.lastQuery != want.lastQuery {
		t.Errorf("lastQuery = %q, want %q — r is a dead key without it", got.lastQuery, want.lastQuery)
	}
	if !slices.Equal(got.history, want.history) {
		t.Errorf("history = %v, want %v — up recalls nothing without it", got.history, want.history)
	}
	if got.histIdx != want.histIdx {
		t.Errorf("histIdx = %d, want %d", got.histIdx, want.histIdx)
	}
	if got.input.Value() != want.input.Value() {
		t.Errorf("input = %q, want %q — the prompt should not still hold the query", got.input.Value(), want.input.Value())
	}
	if got.status != want.status {
		t.Errorf("status = %q, want %q", got.status, want.status)
	}
	if !strings.Contains(got.status, q) {
		t.Errorf("status %q does not say what is being looked up", got.status)
	}
}

func TestInitialQueryIsWhatInitLooksUp(t *testing.T) {
	// New clears the input, so Init can no longer read the query from there.
	m := New(nil, "  8.8.8.8  ")
	if m.Init() == nil {
		t.Fatal("Init issued no command for a command-line query")
	}
	if got := New(nil, "   ").Init(); got == nil {
		t.Fatal("Init returned no command at all without a query")
	}
	if m.lastQuery != "8.8.8.8" {
		t.Fatalf("lastQuery = %q, want the trimmed query", m.lastQuery)
	}
}

func TestRetryAfterAnInitialQueryReRunsIt(t *testing.T) {
	// The end of the chain finding 6 is really about: r must work on the very
	// first frame after a command-line invocation.
	m := sized(New(nil, "8.8.8.8"), 120, 34)
	done, _ := m.Update(lookupMsg{res: fullResult()})
	toMap, _ := done.(Model).Update(tea.KeyMsg{Type: tea.KeyTab})
	retried, cmd := toMap.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if cmd == nil {
		t.Fatal("r issued no lookup after a command-line query")
	}
	if got := retried.(Model).status; !strings.Contains(got, "8.8.8.8") {
		t.Errorf("status = %q, want it to name the query being retried", got)
	}
	// A retry re-runs a query already in the ring; it must not push a
	// duplicate onto it.
	if got := retried.(Model).history; !slices.Equal(got, []string{"8.8.8.8"}) {
		t.Errorf("history = %v, want a single entry", got)
	}
}

func TestHistoryRecallsAnInitialQuery(t *testing.T) {
	m := sized(New(nil, "8.8.8.8"), 120, 34)
	done, _ := m.Update(lookupMsg{res: fullResult()})
	recalled, _ := done.(Model).Update(tea.KeyMsg{Type: tea.KeyUp})
	if got := recalled.(Model).input.Value(); got != "8.8.8.8" {
		t.Fatalf("up recalled %q, want the command-line query", got)
	}
}

func TestRetryDoesNotClobberInProgressTyping(t *testing.T) {
	// Same data-loss class as TestDownArrowDoesNotClobberInProgressTyping:
	// r is aimed at the map and re-runs a query the user already submitted,
	// so it must leave text they are mid-typing alone. Enter clearing the
	// prompt is right — that query has moved into the history ring and the
	// prompt is ready for the next one — but routing r through the same path
	// borrowed a behaviour it should not have.
	m := sized(New(nil, "8.8.8.8"), 120, 34)
	done, _ := m.Update(lookupMsg{res: fullResult()})

	typed, _ := done.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("9.9.9.9")})
	if got := typed.(Model).input.Value(); got != "9.9.9.9" {
		t.Fatalf("typed text = %q, want %q", got, "9.9.9.9")
	}

	// Tab only blurs the input; the value it holds survives the focus change.
	toMap, _ := typed.(Model).Update(tea.KeyMsg{Type: tea.KeyTab})
	retried, cmd := toMap.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if cmd == nil {
		t.Fatal("r issued no lookup")
	}
	if got := retried.(Model).input.Value(); got != "9.9.9.9" {
		t.Fatalf("r destroyed unsubmitted input: got %q, want %q", got, "9.9.9.9")
	}
	// The retry must still be of the last submitted query, not of the text
	// sitting unsubmitted at the prompt.
	if got := retried.(Model).lastQuery; got != "8.8.8.8" {
		t.Errorf("lastQuery = %q, want the last submitted query", got)
	}
}

// minFrameWidth is the narrowest frame the layout can produce: mapCells floors
// the map at 10 cells, and the panel is a fixed PanelWidth beside it. Below
// this the frame cannot shrink further, so the invariant only binds above it.
const minFrameWidth = 10 + PanelWidth

func TestNoRenderedLineExceedsTheTerminalWidth(t *testing.T) {
	// The help line is a fixed 63 columns. Anything narrower than that and
	// wider than the layout minimum used to push the whole frame off screen.
	for _, w := range []int{minFrameWidth, 50, 60, 62, 63, 100} {
		m := sized(New(nil, ""), w, 20)
		for i, line := range strings.Split(m.View(), "\n") {
			if got := lipgloss.Width(line); got > w {
				t.Errorf("terminal %d cols: line %d renders %d cols", w, i, got)
			}
		}
	}
}

func TestAStaleResultDoesNotOverwriteANewerOne(t *testing.T) {
	// Lookups run off the render goroutine with no ordering guarantee, so a
	// slow earlier query can return after a fast later one. Whichever arrives
	// last used to win, silently showing the user the wrong address.
	m := sized(New(nil, ""), 120, 34)
	m = m.beginLookup("1.1.1.1", true)
	stale := m.seq
	m = m.beginLookup("8.8.8.8", true)

	fresh := fullResult()
	fresh.City = "San Jose"
	old := fullResult()
	old.City = "Brisbane"

	next, _ := m.Update(lookupMsg{seq: m.seq, res: fresh})
	next, _ = next.(Model).Update(lookupMsg{seq: stale, res: old})

	if got := next.(Model).res.City; got != "San Jose" {
		t.Fatalf("a superseded lookup overwrote the current one: showing %q", got)
	}
}

func TestAStaleErrorDoesNotClobberANewerResult(t *testing.T) {
	m := sized(New(nil, ""), 120, 34)
	m = m.beginLookup("1.1.1.1", true)
	stale := m.seq
	m = m.beginLookup("8.8.8.8", true)

	next, _ := m.Update(lookupMsg{seq: m.seq, res: fullResult()})
	next, _ = next.(Model).Update(lookupMsg{seq: stale, err: errors.New("timed out")})

	got := next.(Model)
	if got.isError {
		t.Fatalf("a superseded error was surfaced: %q", got.status)
	}
	if got.res == nil {
		t.Fatal("a superseded error cleared the current result")
	}
}

func TestSupersedingALookupCancelsTheOneItReplaced(t *testing.T) {
	// Dropping a stale result stops it being shown, but the request behind it
	// keeps running and holding a connection until its own timeout. Quitting
	// mid-lookup leaked it the same way.
	m := sized(New(nil, ""), 120, 34)
	m = m.beginLookup("1.1.1.1", true)
	first := m.ctx
	m = m.beginLookup("8.8.8.8", true)

	select {
	case <-first.Done():
	default:
		t.Fatal("the superseded lookup's context was not cancelled")
	}
}

func TestTheExportedResultHookIsNotDroppedAsStale(t *testing.T) {
	// The one-shot renderer drives the model by hand: New starts a lookup,
	// then the finished result is handed back through the exported hook. If
	// that message does not carry the model's current sequence, the staleness
	// filter treats it as superseded and --once prints an empty panel.
	m := sized(New(nil, "8.8.8.8"), 120, 34)
	next, _ := m.Update(m.Result(fullResult()))
	if next.(Model).res == nil {
		t.Fatal("the finished result was dropped as stale")
	}
}

// countBraille counts the braille glyphs in a rendered frame, ignoring the
// panel, borders and help text around the map.
func countBraille(frame string) int {
	n := 0
	for _, r := range frame {
		if r >= 0x2801 && r <= 0x28FF {
			n++
		}
	}
	return n
}

func TestBordersAppearExactlyAtTheZoomGate(t *testing.T) {
	// Comparing two viewports a thousandth of a degree apart isolates the
	// borders: the coastline they render is identical at both spans, so any
	// difference in ink is the border layer switching on. A wider comparison
	// would confound borders with the different coastline a different zoom
	// shows.
	m := sized(New(nil, ""), 120, 34)
	centre := geo.Viewport{CenterLat: 50, CenterLon: 10}

	on := centre
	on.LonSpan = world.BorderMaxSpan
	m.view = on
	inked := countBraille(m.View())

	off := centre
	off.LonSpan = world.BorderMaxSpan + 0.001
	m.view = off
	bare := countBraille(m.View())

	if inked <= bare {
		t.Fatalf("no extra ink at the gate: %d inked at LonSpan %v, %d just above it",
			inked, world.BorderMaxSpan, bare)
	}
}

func TestBordersAreAbsentFromTheWorldView(t *testing.T) {
	m := sized(New(nil, ""), 120, 34)
	m.view = geo.World()
	withUI := countBraille(m.View())

	mapW, mapH := m.mapCells()
	c := canvas.New(mapW, mapH)
	world.Draw(c, m.view)
	coastOnly := 0
	for _, row := range c.Render() {
		coastOnly += countBraille(row)
	}

	if withUI != coastOnly {
		t.Fatalf("world view drew %d glyphs, coastline alone draws %d — borders leaked past the gate",
			withUI, coastOnly)
	}
}

func TestEveryInkHasItsOwnStyle(t *testing.T) {
	// Test colors, not appearance: lipgloss renders styles identically when
	// the profile has no color, which it does under `go test`, so a rendered
	// frame cannot tell these apart.
	for _, ink := range []canvas.Ink{canvas.InkBorder, canvas.InkLand, canvas.InkPin} {
		if _, ok := inkStyles[ink]; !ok {
			t.Errorf("ink %v has no style", ink)
		}
	}
	if inkStyles[canvas.InkBorder].GetForeground() == inkStyles[canvas.InkLand].GetForeground() {
		t.Error("borders and coastline share a color, so a border is indistinguishable from a shoreline")
	}
	if inkStyles[canvas.InkBorder].GetForeground() == inkStyles[canvas.InkPin].GetForeground() {
		t.Error("borders and the pin share a color")
	}
}
