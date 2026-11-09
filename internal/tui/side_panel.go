package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// panelIdleStyle is a session with nothing to show. The panel is a list to
// read, so it is lighter than the bar's dim gray, but still darker than a
// seen or highlighted session.
var panelIdleStyle = lipgloss.NewStyle().Foreground(cFg3)

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
	h := m.height - barHeight
	rows := m.sidePanelRows(h)
	texts := make([]string, len(rows))
	for i, r := range rows {
		texts[i] = r.text
	}

	if cursor != nil {
		cursor.X += m.sidePanelW()
	}
	return joinVerticalSplit(strings.Join(texts, "\n"), body, cFaint, h, sidePanelWidth), cursor
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
	if h < 2 {
		return nil
	}

	blank := sidePanelRow{text: strings.Repeat(" ", sidePanelWidth)}
	layout := m.sidePanelLayout()

	if room := h - 1; len(layout) > room {
		hidden := 0
		for _, r := range layout[room-1:] {
			if r.group != nil {
				hidden++
			}
		}
		more := sidePanelRow{text: padLine(dimStyle.Render(fmt.Sprintf(" +%d more", hidden)), sidePanelWidth)}
		layout = append(layout[:room-1], more)
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
			rows = append(rows, sidePanelRow{text: sidePanelRepo(g)})
		}

		inStack := g.stack.Index > 0 && g.members == nil
		if inStack && (i == 0 || !sameGhStack(groups[i-1], g)) {
			name := dimStyle.Render("   " + truncateTo(ghStackIcon+" "+g.stack.Name(), sidePanelWidth-3))
			rows = append(rows, sidePanelRow{text: padLine(name, sidePanelWidth)})
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
	sign, signSt, st := m.stripLook(g, panelIdleStyle)
	if sign == "" {
		sign = " "
	}

	name := g.worktree
	switch {
	case g.key == m.homeWSKey:
		name = "~"
	case g.members != nil:
		name = ghStackIcon + " " + g.worktree
	}
	text := " " + signSt.Render(sign) + " " + indent + st.Render(truncateTo(name, sidePanelWidth-3-len(indent)))
	return padLine(text, sidePanelWidth)
}

// sidePanelRepo renders a repo's header row. An extra Claude account's block
// names the account after the repo, as the panel has no background to mark it.
func sidePanelRepo(g agentGroup) string {
	tag := ""
	if g.account != "" {
		tag = " · " + g.account
	}
	repo := truncateTo(g.repo, sidePanelWidth-1-lipgloss.Width(tag))
	return padLine(repoHdrStyle.Render(" "+repo)+dimStyle.Render(tag), sidePanelWidth)
}
