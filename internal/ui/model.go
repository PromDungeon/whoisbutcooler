package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/PromDungeon/whoisbutcooler/internal/canvas"
	"github.com/PromDungeon/whoisbutcooler/internal/geo"
	"github.com/PromDungeon/whoisbutcooler/internal/lookup"
	"github.com/PromDungeon/whoisbutcooler/internal/world"
)

// primaryProvider is the only provider whose use goes unmentioned. Anything
// else answering means the HTTPS primary failed, which the user should see.
const primaryProvider = "ipwho.is"

// Rows consumed by the input, status, and help lines.
const chromeRows = 3

type focusArea int

const (
	focusInput focusArea = iota
	focusMap
)

// lookupMsg carries a finished lookup back to the update loop.
type lookupMsg struct {
	res *lookup.Result
	err error
}

// LookupResult wraps a completed lookup as a message, so callers outside the
// package (the one-shot renderer) can drive the model without running a full
// Bubble Tea program.
func LookupResult(res *lookup.Result) tea.Msg { return lookupMsg{res: res} }

// Model is the whole application state.
type Model struct {
	client *lookup.Client
	input  textinput.Model

	view geo.Viewport
	res  *lookup.Result

	status  string
	isError bool

	history []string
	histIdx int

	focus     focusArea
	lastQuery string
	w, h      int
}

// New builds the model. initialQuery, when non-empty, is looked up on start.
func New(client *lookup.Client, initialQuery string) Model {
	in := textinput.New()
	in.Placeholder = "IP address or hostname"
	in.Prompt = "› "
	in.Focus()

	m := Model{
		client: client,
		input:  in,
		view:   geo.World(),
		w:      80,
		h:      24,
	}
	// Init returns a Cmd and cannot mutate the model, so a query given on the
	// command line has to be recorded here or `whoisbutcooler 8.8.8.8` — the
	// most common invocation there is — lands in a state no key press can
	// produce: r retries nothing, up recalls nothing, and the prompt still
	// holds the query the panel is already showing.
	if q := strings.TrimSpace(initialQuery); q != "" {
		m = m.beginLookup(q, true)
	}
	return m
}

func (m Model) Init() tea.Cmd {
	if m.lastQuery == "" {
		return textinput.Blink
	}
	return tea.Batch(textinput.Blink, m.doLookup(m.lastQuery))
}

// beginLookup puts the model into the "a lookup is running" state for q,
// without running it: New needs exactly this state but cannot return a
// command. remember says whether q joins the history ring, which a retry must
// not do — the query is already in there.
func (m Model) beginLookup(q string, remember bool) Model {
	if remember {
		m.history = append(m.history, q)
		m.histIdx = len(m.history)
	}
	m.lastQuery = q
	m.status, m.isError = "Looking up "+q+"…", false
	m.input.SetValue("")
	return m
}

// doLookup runs the query off the render goroutine so the UI stays responsive.
func (m Model) doLookup(query string) tea.Cmd {
	client := m.client
	return func() tea.Msg {
		if client == nil {
			return lookupMsg{err: errors.New("no lookup client configured")}
		}
		res, err := client.Lookup(context.Background(), query)
		return lookupMsg{res: res, err: err}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.input.Width = max(10, msg.Width-6)
		return m, nil

	case lookupMsg:
		return m.applyLookup(msg), nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// ErrorMessage turns a lookup failure into the sentence a person should read.
// Exported because the one-shot renderer reports its error itself, and a user
// who runs --once deserves the same explanation as one at the prompt rather
// than the raw wrapped error.
func ErrorMessage(err error) string {
	if errors.Is(err, lookup.ErrPrivateRange) {
		return "That address is in a private or reserved range, so no public database can place it."
	}
	return err.Error()
}

func (m Model) applyLookup(msg lookupMsg) Model {
	if msg.err != nil {
		m.isError = true
		m.status = ErrorMessage(msg.err)
		return m
	}
	m.res = msg.res
	dw, dh := m.mapDots()
	m.view = geo.FitTo(msg.res.Lat, msg.res.Lon).Clamp(dw, dh)
	m.isError = false
	m.status = ""
	if msg.res.GeoSource != "" && msg.res.GeoSource != primaryProvider {
		// The fallback is HTTP-only; saying so is the whole point of tracking
		// which provider answered.
		m.status = fmt.Sprintf("HTTPS provider unavailable — answered by %s over plain HTTP.", msg.res.GeoSource)
	}
	return m
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	if msg.Type == tea.KeyTab {
		// Blur returns nothing while Focus returns a command, so these two
		// branches cannot be collapsed.
		if m.focus == focusInput {
			m.focus = focusMap
			m.input.Blur()
			return m, nil
		}
		m.focus = focusInput
		return m, m.input.Focus()
	}

	if m.focus == focusMap {
		return m.handleMapKey(msg)
	}
	return m.handleInputKey(msg)
}

func (m Model) handleMapKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	dw, dh := m.mapDots()
	const step = 0.25 // a quarter-viewport nudge

	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "esc":
		m.focus = focusInput
		return m, m.input.Focus()
	case "left", "h":
		m.view = m.view.Pan(-step, 0, dw, dh)
	case "right", "l":
		m.view = m.view.Pan(step, 0, dw, dh)
	case "up", "k":
		m.view = m.view.Pan(0, step, dw, dh)
	case "down", "j":
		m.view = m.view.Pan(0, -step, dw, dh)
	case "+", "=":
		m.view = m.view.ZoomIn().Clamp(dw, dh)
	case "-", "_":
		m.view = m.view.ZoomOut().Clamp(dw, dh)
	case "0":
		if m.res != nil {
			m.view = geo.FitTo(m.res.Lat, m.res.Lon).Clamp(dw, dh)
		} else {
			m.view = geo.World().Clamp(dw, dh)
		}
	case "r":
		if m.lastQuery != "" {
			q := m.lastQuery
			return m.beginLookup(q, false), m.doLookup(q)
		}
	}
	return m, nil
}

func (m Model) handleInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		q := strings.TrimSpace(m.input.Value())
		if q == "" {
			return m, nil
		}
		return m.beginLookup(q, true), m.doLookup(q)

	case tea.KeyUp:
		if m.histIdx > 0 {
			m.histIdx--
			m.input.SetValue(m.history[m.histIdx])
		}
		return m, nil

	case tea.KeyDown:
		// histIdx == len(history) means nothing is recalled — the normal
		// state at startup and right after a submission. Down must leave
		// whatever the user is typing alone rather than clearing it.
		switch {
		case m.histIdx >= len(m.history):
			return m, nil
		case m.histIdx < len(m.history)-1:
			m.histIdx++
			m.input.SetValue(m.history[m.histIdx])
		default:
			// Stepping past the newest recalled entry clears back to an
			// empty prompt, since that entry was recalled text, not typed.
			m.histIdx = len(m.history)
			m.input.SetValue("")
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// mapDots returns the map area in dots.
func (m Model) mapDots() (int, int) {
	w, h := m.mapCells()
	return w * canvas.DotsX, h * canvas.DotsY
}

func (m Model) mapCells() (int, int) {
	w := m.w - PanelWidth
	if w < 10 {
		w = 10
	}
	h := m.h - chromeRows
	if h < 3 {
		h = 3
	}
	return w, h
}

func (m Model) View() string {
	mapW, mapH := m.mapCells()
	c := canvas.New(mapW, mapH)
	world.Draw(c, m.view)
	if m.res != nil {
		world.DrawPin(c, m.view, m.res.Lat, m.res.Lon)
	}

	body := lipgloss.JoinHorizontal(
		lipgloss.Top,
		strings.Join(colorize(c), "\n"),
		RenderPanel(m.res, PanelWidth),
	)

	status := m.status
	style := labelStyle
	switch {
	case m.isError:
		style = errStyle
	case m.res != nil && m.res.GeoSource != "" && m.res.GeoSource != primaryProvider:
		style = warnStyle
	}

	help := "tab focus · ←↑↓→/hjkl pan · +/- zoom · 0 fit · r retry · q quit"

	return strings.Join([]string{
		m.input.View(),
		body,
		style.Render(status),
		helpStyle.Render(help),
	}, "\n")
}

// colorize applies one style per run of same-inked cells. A terminal cell
// carries a single foreground color, so runs are the finest granularity
// available and coalescing them keeps the escape-sequence count sane.
func colorize(c *canvas.Canvas) []string {
	rows := c.Render()
	out := make([]string, len(rows))
	for y, row := range rows {
		var b strings.Builder
		runes := []rune(row)
		start := 0
		for x := 1; x <= len(runes); x++ {
			if x < len(runes) && c.InkAt(x, y) == c.InkAt(start, y) {
				continue
			}
			seg := string(runes[start:x])
			if c.InkAt(start, y) == canvas.InkPin {
				b.WriteString(pinStyle.Render(seg))
			} else {
				b.WriteString(landStyle.Render(seg))
			}
			start = x
		}
		out[y] = b.String()
	}
	return out
}
