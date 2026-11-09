package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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

// openSidePanel opens the side panel; a no-op when it is already open.
func (m Model) openSidePanel() (tea.Model, tea.Cmd) {
	if m.sidePanel {
		return m, nil
	}
	return m.toggleSidePanel()
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
		b.WriteString(rows[i].text + div + l)
	}

	if cursor != nil {
		cursor.X += pw
	}
	return b.String(), cursor
}

// sidePanelClick handles a left click on the side panel: a session's row opens
// that session, like a click on the agent strip. Other rows do nothing.
func (m Model) sidePanelClick(x, y int) (tea.Model, tea.Cmd, bool) {
	if x >= m.sidePanelW() || y < barHeight || !m.sidePanelShown() {
		return m, nil, false
	}

	rows := m.sidePanelRows(m.height - barHeight)
	if i := y - barHeight; i < len(rows) && rows[i].group != nil {
		mm, cmd := m.openStripGroup(*rows[i].group)
		return mm, cmd, true
	}
	return m, nil, true
}

// sidePanelShown reports whether the side panel is on screen: open, with room,
// and next to a session rather than under the deck or an overlay.
func (m Model) sidePanelShown() bool {
	return m.sidePanelW() > 0 && m.screen != screenPicker && m.screen != screenSetup &&
		!m.overlayOpen() && !m.confirmActive() && m.activeSession() != nil
}

// sidePanelRows lays out the panel's h rows, each sidePanelWidth wide: a blank
// row under the bar, then the sessions, with "+N more" when they don't fit.
func (m Model) sidePanelRows(h int) []sidePanelRow {
	blank := sidePanelRow{text: strings.Repeat(" ", sidePanelWidth)}
	layout := m.sidePanelLayout()

	if room := h - 1; len(layout) > room {
		hidden := 0
		for _, r := range layout[room-1:] {
			if r.group != nil {
				hidden++
			}
		}
		more := sidePanelRow{text: panelLine(lipgloss.NewStyle(), dimStyle.Render(fmt.Sprintf(" +%d more", hidden)))}
		layout = append(layout[:room-1:room-1], more)
	}

	rows := append([]sidePanelRow{blank}, layout...)
	for len(rows) < h {
		rows = append(rows, blank)
	}
	return rows[:h]
}

// sidePanelRow is one row of the side panel. group is the session the row
// opens, nil for a repo or gh stack header.
type sidePanelRow struct {
	text  string
	group *agentGroup
}

// sidePanelLayout lists the panel's rows: the agent strip's entries in the same
// order, each repo under its name, and the worktrees of the gh stack you're in
// under the stack's name.
func (m Model) sidePanelLayout() []sidePanelRow {
	groups := m.stripEntries()
	var rows []sidePanelRow
	for i, g := range groups {
		newRepo := i == 0 || groups[i-1].repo != g.repo || groups[i-1].account != g.account
		if newRepo && g.key != m.homeWSKey {
			acc := g.account != ""
			name := onAccount(dimStyle.Bold(true), acc).Render(" " + truncateTo(g.repo, sidePanelWidth-1))
			rows = append(rows, sidePanelRow{text: panelLine(onAccount(lipgloss.NewStyle(), acc), name)})
		}

		inStack := g.stack.Index > 0 && g.members == nil
		if inStack && (i == 0 || !sameGhStack(groups[i-1], g)) {
			bg := lipgloss.NewStyle().Background(cStackBg)
			name := barSep.Background(cStackBg).Render("   " + truncateTo(ghStackIcon+" "+g.stack.Name(), sidePanelWidth-3))
			rows = append(rows, sidePanelRow{text: panelLine(bg, name)})
		}

		indent := ""
		if inStack {
			indent = "  "
		}
		rows = append(rows, sidePanelRow{text: m.sidePanelEntry(g, indent), group: &groups[i]})
	}
	return rows
}

// sidePanelEntry renders one session's row: its attention sign in the first
// column, then its name in the agent strip's colours.
func (m Model) sidePanelEntry(g agentGroup, indent string) string {
	bg := stripBg(lipgloss.NewStyle(), g)
	sign, signSt, st := " ", dimStyle, dimStyle
	switch {
	case g.attn == attnWaiting:
		sign, signSt, st = "!", boldRed, boldRed
	case g.attn == attnDone:
		sign, signSt, st = "✓", boldGreen, boldGreen
	case g.busy:
		sign, signSt, st = "●", dirtyStyle, dirtyStyle
	case g.attn == attnSeen || g.highlighted:
		st = nameStyle
	}
	if m.current != nil && g.key == m.current.key {
		st = st.Foreground(cAccent).Underline(true)
	}

	name := g.worktree
	switch {
	case g.key == m.homeWSKey:
		name = "~"
	case g.members != nil:
		name = ghStackIcon + " " + g.worktree
	}
	text := bg.Render(" ") + stripBg(signSt, g).Render(sign) + bg.Render(" "+indent) +
		stripBg(st, g).Render(truncateTo(name, sidePanelWidth-3-len(indent)))
	return panelLine(bg, text)
}

// panelLine pads a rendered row to the panel's width on the row's background.
func panelLine(bg lipgloss.Style, text string) string {
	return text + bg.Render(strings.Repeat(" ", max(0, sidePanelWidth-lipgloss.Width(text))))
}
