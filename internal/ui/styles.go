package ui

import "github.com/charmbracelet/lipgloss"

// PanelWidth is fixed so the map area does not resize between lookups.
const PanelWidth = 34

// missing is what an unavailable field renders as. Present-but-empty is
// meaningfully different from absent, and both beat a collapsing layout.
const missing = "—"

var (
	// Coastline is turquoise: a tertiary hue bright enough to read at braille
	// density, and far enough around the wheel from the magenta pin that the
	// pin still reads as the one thing that matters. The previous slate 60
	// (#5f5f87) sat barely above a dark terminal background. Adaptive because
	// a value bright enough for a dark terminal washes out on a light one.
	// Other tertiary casts, if you want a different look: amber {172, 214},
	// chartreuse {64, 155}, violet {92, 141}.
	landStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "30", Dark: "80"})
	pinStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "162", Dark: "213"}).
			Bold(true)
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
