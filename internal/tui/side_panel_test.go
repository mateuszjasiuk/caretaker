package tui

import (
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
	if row := ansi.Strip(lines[barHeight]); !strings.HasPrefix(row, " sessions") || ansi.StringWidth(row) > 120 {
		t.Fatalf("first body row should start with the panel and fit the screen: %q", row)
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
