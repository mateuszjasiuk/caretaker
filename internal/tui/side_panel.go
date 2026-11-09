package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

const (
	// sidePanelWidth is the side panel's width, without the divider after it.
	sidePanelWidth = 28
	// minSessionWidth is the narrowest a session gets next to the side panel.
	// In a narrower terminal the panel doesn't show.
	minSessionWidth = 60
)

// sidePanelW is how many columns the side panel takes from the sessions, the
// divider included: 0 when it is closed or the terminal is too narrow.
func (m Model) sidePanelW() int {
	if !m.sidePanel || m.width-sidePanelWidth-1 < minSessionWidth {
		return 0
	}
	return sidePanelWidth + 1
}

// toggleSidePanel opens or closes the side panel, gives the sessions the width
// that is left, and remembers the choice.
func (m Model) toggleSidePanel() (tea.Model, tea.Cmd) {
	m.sidePanel = !m.sidePanel
	if m.state != nil {
		m.state.SidePanel = m.sidePanel
	}
	m.resizeSessions()
	return m, m.writeStateCmd()
}

// withSidePanel draws the side panel to the left of a session body and moves
// the session's cursor to the right by the panel's width.
func (m Model) withSidePanel(body string, cursor *tea.Cursor) (string, *tea.Cursor) {
	pw := m.sidePanelW()
	if pw == 0 {
		return body, cursor
	}

	lines := strings.Split(body, "\n")
	for len(lines) < m.height-barHeight {
		lines = append(lines, "")
	}
	rows := m.sidePanelRows(len(lines))
	div := barSep.Render("│")
	var b strings.Builder
	for i, l := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(rows[i] + div + l)
	}

	if cursor != nil {
		cursor.X += pw
	}
	return b.String(), cursor
}

// sidePanelRows renders the panel's h rows, each sidePanelWidth wide.
func (m Model) sidePanelRows(h int) []string {
	rows := make([]string, h)
	blank := strings.Repeat(" ", sidePanelWidth)
	for i := range rows {
		rows[i] = blank
	}
	if h > 0 {
		rows[0] = dimStyle.Width(sidePanelWidth).Render(" sessions")
	}
	return rows
}
