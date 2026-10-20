package tui

import (
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/KCaverly/caretaker/internal/session"
)

// Image pass-through (see internal/session/graphics.go): ct asks the real
// terminal for its name at startup, then lets sessions forward kitty image
// commands through graphicsOut. They are written out in arrival order, since
// a large image arrives as many chunks.

// WithGraphics turns on image pass-through for this run. It is opt-in so
// that a model without a reader on graphicsOut (tests) never blocks a session.
func (m Model) WithGraphics() Model {
	m.graphicsOut = make(chan []byte, 64)
	return m
}

// graphicsMsg carries forwarded image commands to write to the terminal.
type graphicsMsg string

// enableGraphics turns on pass-through once the real terminal has named
// itself.
func (m Model) enableGraphics(version string) tea.Cmd {
	if m.graphicsOut == nil {
		return nil
	}
	out := m.graphicsOut
	session.EnableGraphics(version, func(b []byte) { out <- b })
	return m.watchGraphics()
}

// watchGraphics waits for forwarded commands and hands over everything
// queued so far as one write.
func (m Model) watchGraphics() tea.Cmd {
	out := m.graphicsOut
	return func() tea.Msg {
		var buf strings.Builder
		buf.Write(<-out)
		for {
			select {
			case b := <-out:
				buf.Write(b)
			default:
				return graphicsMsg(buf.String())
			}
		}
	}
}

// measureCells refreshes the cell pixel size that sessions report to their
// programs.
func measureCells() { session.MeasureCells(os.Stdout) }
