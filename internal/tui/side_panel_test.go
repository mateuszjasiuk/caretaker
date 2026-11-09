package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/KCaverly/caretaker/internal/config"
	"github.com/KCaverly/caretaker/internal/session"
)

// sidePanelModel is a model with one open workspace on the terminal screen.
func sidePanelModel(t *testing.T, width int) (Model, *Controller, *session.Manager) {
	t.Helper()
	ctrl := &Controller{cfg: config.Config{
		Editor: "cat", Agent: "cat", Shell: "sh", Keys: config.Default().Keys,
	}}
	mgr := session.NewManager()
	t.Cleanup(mgr.CloseAll)

	m := New(ctrl, mgr)
	m.sidePanel = false
	mm, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
	m = mm.(Model)
	mm, _ = m.activate("repo", "wt", t.TempDir())
	m = mm.(Model)
	m.screen = screenTerminal
	return m, ctrl, mgr
}

func pressSidePanel(t *testing.T, m Model) Model {
	t.Helper()
	mm, _ := m.handleKey(tea.KeyPressMsg{Code: '\\', Mod: tea.ModCtrl})
	return mm.(Model)
}

func TestSidePanelNarrowsSessionsAndIsSaved(t *testing.T) {
	m, _, _ := sidePanelModel(t, 120)

	m = pressSidePanel(t, m)
	if w, _ := m.sessionSize(); w != 120-sidePanelWidth-1 {
		t.Fatalf("session width with the panel = %d, want %d", w, 120-sidePanelWidth-1)
	}
	if !m.state.SidePanel {
		t.Fatal("the choice should be kept in the state")
	}
	if cols, _ := m.current.ws.Terms[0].Size(); cols != 120-sidePanelWidth-1 {
		t.Fatalf("terminal width = %d, want it resized to %d", cols, 120-sidePanelWidth-1)
	}
	lines := strings.Split(m.View().Content, "\n")
	if row := ansi.Strip(lines[barHeight+1]); !strings.HasPrefix(row, " repo ") || ansi.StringWidth(row) > 120 {
		t.Fatalf("the second body row should start with the panel and fit the screen: %q", row)
	}

	m = pressSidePanel(t, m)
	if w, _ := m.sessionSize(); w != 120 {
		t.Fatalf("closing the panel should give the full width back, got %d", w)
	}
}

func TestSidePanelHidesInANarrowTerminal(t *testing.T) {
	m, _, _ := sidePanelModel(t, 80)

	m = pressSidePanel(t, m)
	if w, _ := m.sessionSize(); w != 80 {
		t.Fatalf("an 80-column terminal has no room for the panel, got session width %d", w)
	}
	if !m.sidePanel {
		t.Fatal("the panel should stay on, to show again when the terminal is wider")
	}
}

func TestSidePanelShiftsClicksIntoPanes(t *testing.T) {
	m, ctrl, mgr := sidePanelModel(t, 120)
	m = pressSidePanel(t, m)
	w, h := m.sessionSize()
	if _, err := mgr.SplitTermPane("repo/wt", t.TempDir(), ctrl.TermSpec(), session.SplitV, w, h); err != nil {
		t.Fatal(err)
	}
	ws := m.current.ws

	// A click on the panel stays out of the panes.
	m.handleMouseClick(tea.MouseClickMsg{X: 5, Y: barHeight + 3, Button: tea.MouseLeft})
	if ws.ActiveTerm != 1 {
		t.Fatalf("a click on the panel should not move pane focus, active=%d", ws.ActiveTerm)
	}

	m.handleMouseClick(tea.MouseClickMsg{X: m.sidePanelW() + 5, Y: barHeight + 3, Button: tea.MouseLeft})
	if ws.ActiveTerm != 0 {
		t.Fatalf("a click just right of the panel should focus the left pane, active=%d", ws.ActiveTerm)
	}
}

// panelRows is the panel's rows as plain text, without the padding.
func panelRows(m Model) []string {
	var rows []string
	for _, r := range m.sidePanelRows(m.height - barHeight) {
		rows = append(rows, strings.TrimRight(ansi.Strip(r.text), " "))
	}
	return rows
}

func TestSidePanelListsSessionsUnderTheirRepo(t *testing.T) {
	m := stripModelKeys(t, 140, "api/zeta", "api/alpha", "web/main")
	m.sidePanel = true
	ws, _ := m.mgr.Workspace("api/zeta")
	if len(ws.Agents) == 0 {
		t.Skip("workspace started without an agent")
	}
	m.agentStatus = map[int]AgentStatus{ws.Agents[0].Pid(): {Status: "waiting"}}

	got := strings.Join(panelRows(m)[:6], "\n")
	want := strings.Join([]string{"", " api", "   alpha", " ! zeta", " web", "   main"}, "\n")
	if got != want {
		t.Fatalf("panel rows:\n%s\nwant:\n%s", got, want)
	}
	for _, r := range m.sidePanelRows(m.height - barHeight) {
		if w := ansi.StringWidth(r.text); w != sidePanelWidth {
			t.Fatalf("every row should be %d wide, got %d: %q", sidePanelWidth, w, r.text)
		}
	}
}

func TestSidePanelFollowsTheStackToggle(t *testing.T) {
	m := stackStripModel(t)
	m.sidePanel = true

	got := strings.Join(panelRows(m)[:5], "\n")
	want := strings.Join([]string{"", " repo", "   ⛁ #4", "   beta", "   zed"}, "\n")
	if got != want {
		t.Fatalf("a stack you're not in should be one row:\n%s\nwant:\n%s", got, want)
	}

	m = pressCtrl(t, m, 'y')
	got = strings.Join(panelRows(m)[:9], "\n")
	want = strings.Join([]string{"", " repo", "   ⛁ #4", "     a", "     b", "     c", "     d", "   beta", "   zed"}, "\n")
	if got != want {
		t.Fatalf("ctrl+y should show the stack in full:\n%s\nwant:\n%s", got, want)
	}
}

func TestSidePanelSaysHowManySessionsDontFit(t *testing.T) {
	var names []string
	for i := range 30 {
		names = append(names, fmt.Sprintf("wt%02d", i))
	}
	m := stripModel(t, 140, names...)
	m.sidePanel = true

	rows := panelRows(m)
	if len(rows) != m.height-barHeight {
		t.Fatalf("the panel should fill the height: %d rows, want %d", len(rows), m.height-barHeight)
	}
	// A blank row, the repo, 19 sessions, then "+11 more".
	if last := rows[len(rows)-1]; last != " +11 more" {
		t.Fatalf("last row = %q, want \" +11 more\"", last)
	}
}

func TestSidePanelClickOpensTheSession(t *testing.T) {
	m := stripModel(t, 140, "alpha", "beta", "gamma") // gamma is current
	m.sidePanel = true
	click := func(m Model, row int) Model {
		mm, _ := m.handleMouseClick(tea.MouseClickMsg{X: 4, Y: barHeight + row, Button: tea.MouseLeft})
		return mm.(Model)
	}

	// Rows: blank, "repo", alpha, beta, gamma.
	if m = click(m, 1); m.current.worktree != "gamma" {
		t.Fatalf("a click on the repo's row should do nothing, current = %q", m.current.worktree)
	}
	if m = click(m, 2); m.current.worktree != "alpha" {
		t.Fatalf("a click on alpha's row should open it, current = %q", m.current.worktree)
	}

	m.helpOpen = true
	if m = click(m, 3); m.current.worktree != "alpha" {
		t.Fatalf("the panel isn't drawn under help, so a click should do nothing, current = %q", m.current.worktree)
	}
}

func TestSidePanelHidesTheAgentStrip(t *testing.T) {
	m := stripModel(t, 140, "alpha", "beta")
	m.sidePanel = true
	if row := barStripRow(m); strings.Contains(row, "alpha") {
		t.Fatalf("the strip should be gone while the panel lists the sessions: %q", row)
	}
	if row := barFirstRow(m); !strings.Contains(row, "repo / beta") {
		t.Fatalf("the bar should keep its other parts: %q", row)
	}
	if _, _, ok := m.stripClick(10, 1); ok {
		t.Fatal("a hidden strip should take no clicks")
	}

	m.helpOpen = true
	if row := barStripRow(m); !strings.Contains(row, "alpha · beta") {
		t.Fatalf("under an overlay the panel isn't drawn, so the strip should be back: %q", row)
	}
}

func TestStripMoreOpensTheSidePanel(t *testing.T) {
	var names []string
	for i := range 12 {
		names = append(names, fmt.Sprintf("worktree-%02d", i))
	}
	m := stripModel(t, 120, names...)

	_, items := m.agentStripLayout()
	more := items[len(items)-1]
	if !more.more {
		t.Fatalf("the strip's last item should be \"+N\": %+v", more)
	}

	mm, _, ok := m.stripClick(more.start, 1)
	m = mm.(Model)
	if !ok || !m.sidePanel {
		t.Fatal(`a click on "+N" should open the side panel`)
	}

	// On the deck the panel isn't drawn and the strip is back, but "+N" can't
	// show the panel there, so it takes no click, open or closed.
	m.screen = screenPicker
	for _, open := range []bool{true, false} {
		m.sidePanel = open
		_, items = m.agentStripLayout()
		mm, _, ok = m.stripClick(items[len(items)-1].start, 1)
		if m = mm.(Model); ok || m.sidePanel != open {
			t.Fatalf(`on the deck "+N" should do nothing (panel open: %v)`, open)
		}
	}
}

func TestSidePanelInATinyTerminalDoesNotCrash(t *testing.T) {
	m := stripModel(t, 140, "alpha", "beta", "gamma")
	m.sidePanel = true
	for _, h := range []int{3, 2, 1} {
		m.height = h
		m.handleMouseClick(tea.MouseClickMsg{X: 4, Y: h - 1, Button: tea.MouseLeft})
	}
}

func TestSidePanelNamesTheAccountInsteadOfABackground(t *testing.T) {
	m := stackStripModel(t)
	m.sidePanel = true
	mm, _ := m.activate("repo", "c", m.active[2].view.WT.Path) // inside the stack
	m = mm.(Model)
	var zedPath string
	for _, it := range m.active {
		if it.view.WT.Name == "zed" {
			zedPath = it.view.WT.Path
		}
	}
	m.ctrl.cfg.ClaudeAccounts = map[string]config.ClaudeAccount{
		"private": {ConfigDir: t.TempDir(), Folders: []string{zedPath}},
	}

	got := strings.Join(panelRows(m)[:10], "\n")
	want := strings.Join([]string{"", " repo", "   ⛁ #4", "     a", "     b", "     c", "     d", "   beta", " repo · private", "   zed"}, "\n")
	if got != want {
		t.Fatalf("panel rows:\n%s\nwant:\n%s", got, want)
	}
	for _, r := range m.sidePanelRows(m.height - barHeight) {
		if strings.Contains(r.text, "48;2;") {
			t.Fatalf("panel rows should have no background: %q", r.text)
		}
	}
}
