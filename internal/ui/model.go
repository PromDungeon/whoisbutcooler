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
	// seq identifies which lookup produced this. Results arrive in whatever
	// order the network returns them, so a slow earlier query can land after
	// a fast later one; without this the last arrival would win and quietly
	// show the user a different address than the one they asked about.
	seq int
	res *lookup.Result
	err error
}

// Result wraps a completed lookup as a message this model will accept,
// stamped with its current sequence. The one-shot renderer drives the model
// by hand rather than running a Bubble Tea program, so it has no other way to
// hand a finished lookup back without the staleness filter discarding it.
func (m Model) Result(res *lookup.Result) tea.Msg {
	return lookupMsg{seq: m.seq, res: res}
}

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

	// seq counts lookups so a superseded one can be recognised on arrival;
	// cancel stops the request behind it rather than leaving it to run out
	// its own timeout holding a connection.
	seq    int
	ctx    context.Context
	cancel context.CancelFunc

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
// command.
//
// submitted says the user handed this query over — by pressing Enter, or by
// naming it on the command line. Two things follow from that and from nothing
// else: q joins the history ring, and the prompt is cleared ready for the next
// query. Retry is the case where neither holds. It re-runs a query already in
// the ring, on a keystroke the user aimed at the map, so wiping whatever they
// had half-typed at the prompt would be destroying input they never submitted.
func (m Model) beginLookup(q string, submitted bool) Model {
	if m.cancel != nil {
		m.cancel()
	}
	m.seq++
	m.ctx, m.cancel = context.WithCancel(context.Background())
	if submitted {
		m.history = append(m.history, q)
		m.histIdx = len(m.history)
		m.input.SetValue("")
	}
	m.lastQuery = q
	m.status, m.isError = "Looking up "+q+"…", false
	return m
}

// doLookup runs the query off the render goroutine so the UI stays responsive.
func (m Model) doLookup(query string) tea.Cmd {
	client, ctx, seq := m.client, m.ctx, m.seq
	return func() tea.Msg {
		if client == nil {
			return lookupMsg{seq: seq, err: errors.New("no lookup client configured")}
		}
		res, err := client.Lookup(ctx, query)
		return lookupMsg{seq: seq, res: res, err: err}
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
	// A result from a lookup the user has already replaced is not news, and
	// its error is not their error either.
	if msg.seq != m.seq {
		return m
	}
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
		return m, m.quit()
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
		return m, m.quit()
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

// quit cancels any lookup still running so its goroutine and connection are
// released now rather than when the request times out.
func (m Model) quit() tea.Cmd {
	if m.cancel != nil {
		m.cancel()
	}
	return tea.Quit
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
	// Borders first, as the background layer. Order does not affect color —
	// the canvas keeps the highest ink per cell either way — but it matches
	// how the map reads.
	world.DrawBorders(c, m.view)
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

	// The help line is wider than a narrow terminal, and a line wider than the
	// screen pushes the whole frame out of shape rather than just itself.
	help := truncate("tab focus · ←↑↓→/hjkl pan · +/- zoom · 0 fit · r retry · q quit", m.w)

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
			style, ok := inkStyles[c.InkAt(start, y)]
			if !ok {
				// InkNone: the run is blank, so the style never shows.
				style = landStyle
			}
			b.WriteString(style.Render(seg))
			start = x
		}
		out[y] = b.String()
	}
	return out
}
