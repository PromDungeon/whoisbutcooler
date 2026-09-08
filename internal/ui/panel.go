package ui

import (
	"strings"

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

	lines := []string{
		titleStyle.Render(title),
		ruleStyle.Render(strings.Repeat("─", max(1, width-4))),
		valueStyle.Render(orMissing(place)),
		valueStyle.Render(orMissing(joinNonEmpty(" · ", res.ISP, res.ASN))),
		valueStyle.Render(orMissing(joinNonEmpty(" · ", res.Network, res.NetName))),
		valueStyle.Render(orMissing(res.Abuse)),
		labelStyle.Render(orMissing(res.Timezone)),
	}
	return panelStyle.Render(strings.Join(lines, "\n"))
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
