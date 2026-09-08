package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/PromDungeon/whoisbutcooler/internal/lookup"
)

// RenderPanel draws the seven-line summary. Every line is always emitted, with
// an em dash standing in for anything unavailable, so the panel's height never
// changes between lookups.
func RenderPanel(res *lookup.Result, width int) string {
	if res == nil {
		return panelStyle.Render(strings.Repeat("\n", 6))
	}

	place := joinNonEmpty(", ", res.City, res.Region)
	if res.CountryCode != "" {
		place = joinNonEmpty(" · ", place, res.CountryCode)
	}

	// A Result built by hand may carry no parsed address; fall back to the
	// text the user typed rather than titling the panel "invalid IP".
	title := res.Query
	if res.IP.IsValid() {
		title = res.IP.String()
	}

	// Everything is cut to the content width first. panelStyle's Width wraps
	// rather than truncates, and a wrapped line is an extra row: an ISP name
	// over about twenty-two characters alongside an ASN — Deutsche Telekom,
	// say — turned this nine-line block into thirteen, reflowing the layout
	// between lookups. That reflow is the exact thing the em dashes exist to
	// prevent, so losing the tail of a long value is the lesser evil.
	body := contentWidth(width)
	lines := []string{
		titleStyle.Render(truncate(title, body)),
		ruleStyle.Render(strings.Repeat("─", max(1, body))),
		valueStyle.Render(truncate(orMissing(place), body)),
		valueStyle.Render(truncate(orMissing(joinNonEmpty(" · ", res.ISP, res.ASN)), body)),
		valueStyle.Render(truncate(orMissing(joinNonEmpty(" · ", res.Network, res.NetName)), body)),
		valueStyle.Render(truncate(orMissing(res.Abuse), body)),
		labelStyle.Render(truncate(orMissing(res.Timezone), body)),
	}
	return panelStyle.Render(strings.Join(lines, "\n"))
}

// contentWidth is how many columns a body line may occupy: the block's width
// less its border and its one column of padding on each side.
func contentWidth(width int) int { return width - 4 }

// truncate cuts s to at most width columns, marking the cut with an ellipsis.
// Measurement is by rendered width rather than rune count, since a panel value
// may carry a non-Latin city name whose glyphs are two columns wide.
func truncate(s string, width int) string {
	if width <= 0 || lipgloss.Width(s) <= width {
		return s
	}
	if width == 1 {
		return missing
	}
	var (
		b    strings.Builder
		used int
	)
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if used+rw > width-1 { // -1 leaves room for the ellipsis
			break
		}
		b.WriteRune(r)
		used += rw
	}
	return b.String() + "…"
}

func orMissing(s string) string {
	if strings.TrimSpace(s) == "" {
		return missing
	}
	return s
}

func joinNonEmpty(sep string, parts ...string) string {
	kept := parts[:0]
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, sep)
}
