package ui

import "github.com/charmbracelet/lipgloss"

// PanelWidth is fixed so the map area does not resize between lookups.
const PanelWidth = 34

// missing is what an unavailable field renders as. Present-but-empty is
// meaningfully different from absent, and both beat a collapsing layout.
const missing = "—"

var (
	landStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("60"))
	pinStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
	labelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	valueStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	titleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
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
