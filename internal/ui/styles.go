package ui

import (
	"github.com/PromDungeon/whoisbutcooler/internal/canvas"
	"github.com/charmbracelet/lipgloss"
)

// PanelWidth is fixed so the map area does not resize between lookups.
const PanelWidth = 34

// missing is what an unavailable field renders as. Present-but-empty is
// meaningfully different from absent, and both beat a collapsing layout.
const missing = "—"

var (
	// Coastline is amber: a tertiary hue bright enough to read at braille
	// density. The cyan pin sits opposite it on the wheel, so the pin still
	// reads as the one thing that matters. The original slate 60
	// (#5f5f87) sat barely above a dark terminal background. Adaptive because
	// a value bright enough for a dark terminal washes out on a light one.
	// Other tertiary casts, if you want a different look: turquoise {30, 80},
	// chartreuse {64, 155}, violet {92, 141}.
	landStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "172", Dark: "214"})
	// The pin is cyan: near-complementary to the amber coastline, so it
	// separates by hue rather than relying on brightness alone at the one
	// or two cells it occupies.
	pinStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "37", Dark: "87"}).
			Bold(true)
	// Borders are context rather than the subject, so they sit below both the
	// amber coastline and the cyan pin in weight as well as in ink precedence.
	borderStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "245", Dark: "242"})
	labelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	valueStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	titleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "162", Dark: "213"}).
			Bold(true)
	ruleStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	errStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	warnStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	helpStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("238")).
			Padding(0, 1).
			Width(PanelWidth - 2)
)

// inkStyles maps each ink to how it renders. A lookup rather than a chain of
// comparisons, so a new ink is one line here instead of another branch in
// colorize.
var inkStyles = map[canvas.Ink]lipgloss.Style{
	canvas.InkBorder: borderStyle,
	canvas.InkLand:   landStyle,
	canvas.InkPin:    pinStyle,
}
