// Command whoisbutcooler looks up an IP address and shows where it is on a
// braille world map, alongside a seven-line summary of the registry data worth
// reading.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-isatty"

	"github.com/PromDungeon/whoisbutcooler/internal/lookup"
	"github.com/PromDungeon/whoisbutcooler/internal/ui"
)

const usage = `whoisbutcooler — IP lookups on a map

  whoisbutcooler              open the interactive prompt
  whoisbutcooler <ip|host>    look it up, then stay interactive
  whoisbutcooler --once <ip>  render one frame and exit

Piping output implies --once, since there is nobody to type at a prompt.`

// errHelp is not a failure, so it exits zero and prints to stdout.
var errHelp = errors.New("help requested")

func main() {
	stdoutIsTTY := isTerminal(os.Stdout)
	query, once, err := parseArgs(os.Args[1:], stdoutIsTTY)
	if errors.Is(err, errHelp) {
		fmt.Println(usage)
		return
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintln(os.Stderr, "\n"+usage)
		os.Exit(2)
	}

	client := lookup.NewClient()

	if once {
		if err := renderOnce(client, query); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	p := tea.NewProgram(ui.New(client, query), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// parseArgs resolves the invocation into a query and whether to render a
// single frame. A non-TTY stdout implies --once; both forms require a query,
// because there is no prompt to fall back to.
func parseArgs(args []string, stdoutIsTTY bool) (query string, once bool, err error) {
	for _, a := range args {
		switch a {
		case "--once":
			once = true
		case "-h", "--help":
			return "", false, errHelp
		default:
			if len(a) > 0 && a[0] == '-' {
				return "", false, fmt.Errorf("unknown flag %q", a)
			}
			if query != "" {
				return "", false, errors.New("only one query may be given")
			}
			query = a
		}
	}
	if !stdoutIsTTY {
		once = true
	}
	if once && query == "" {
		return "", false, errors.New("a query is required when output is not an interactive terminal")
	}
	return query, once, nil
}

// renderOnce draws a single frame at a fixed size and exits. Braille is
// ordinary text, so this redirects and pipes cleanly; lipgloss drops color on
// its own when the destination is not a terminal.
func renderOnce(client *lookup.Client, query string) error {
	res, err := client.Lookup(context.Background(), query)
	if err != nil {
		// Same sentence the interactive status line shows. A one-shot run is
		// the same person asking the same question, so it should not get the
		// raw error text where the prompt gets an explanation.
		return errors.New(ui.ErrorMessage(err))
	}
	m := ui.New(client, query)
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	ready := sized.(ui.Model)
	final, _ := ready.Update(ready.Result(res))
	fmt.Println(final.(ui.Model).View())
	return nil
}

// isTerminal reports whether f is a character device. Checking the file mode
// keeps the dependency list to the standard library.
func isTerminal(f *os.File) bool {
	return isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd())
}
