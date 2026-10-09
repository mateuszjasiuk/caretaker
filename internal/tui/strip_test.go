package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/KCaverly/caretaker/internal/config"
	"github.com/KCaverly/caretaker/internal/repo"
	"github.com/KCaverly/caretaker/internal/session"
)

// stripModel returns a model w columns wide with the agent strip on and one
// open workspace per name in wts (all in repo "repo"); the last one is current.
func stripModel(t *testing.T, w int, wts ...string) Model {
	t.Helper()
	ctrl := &Controller{cfg: config.Config{
		Editor: "cat", Agent: "cat", Shell: "sh",
		Keys:    config.Default().Keys,
		Display: config.Display{AgentStrip: true},
	}}
	mgr := session.NewManager()
	t.Cleanup(mgr.CloseAll)

	m := New(ctrl, mgr)
	mm, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: 24})
	m = mm.(Model)
	for _, name := range wts {
		dir := t.TempDir()
		m.active = append(m.active, activeItem{
			repo: repo.Repo{Name: "repo"},
			view: WorktreeView{WT: repo.Worktree{Name: name, Path: dir}},
		})
		mm, _ = m.activate("repo", name, dir)
		m = mm.(Model)
	}
	m.screen = screenEditor
	return m
}

func barFirstRow(m Model) string {
	return ansi.Strip(strings.SplitN(m.renderBar(), "\n", 2)[0])
}

func TestAgentStripListsWorktreesByName(t *testing.T) {
	m := stripModel(t, 120, "zeta", "alpha", "mid")
	row := barFirstRow(m)
	a, z, mid := strings.Index(row, "alpha"), strings.Index(row, "zeta"), strings.Index(row, "mid")
	if a < 0 || z < 0 || mid < 0 || !(a < mid && mid < z) {
		t.Fatalf("strip should list worktrees by name: %q", row)
	}
	if got := ansi.StringWidth(row); got > m.width {
		t.Fatalf("bar row width %d > %d: %q", got, m.width, row)
	}
}

func TestAgentStripShowsAttention(t *testing.T) {
	m := stripModel(t, 120, "alpha", "beta")
	ws, _ := m.mgr.Workspace("repo/alpha")
	if len(ws.Agents) == 0 {
		t.Skip("workspace started without an agent")
	}
	m.agentStatus = map[int]AgentStatus{ws.Agents[0].Pid(): {Status: "waiting"}}
	if row := barFirstRow(m); !strings.Contains(row, "! alpha") {
		t.Fatalf("a waiting worktree should show \"! alpha\": %q", row)
	}
}

func TestAgentStripOverflow(t *testing.T) {
	names := []string{"one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten"}
	m := stripModel(t, 80, names...)
	row := barFirstRow(m)
	if !strings.Contains(row, "+") {
		t.Fatalf("a strip too wide for the bar should end with +N: %q", row)
	}
	if got := ansi.StringWidth(row); got > m.width {
		t.Fatalf("bar row width %d > %d: %q", got, m.width, row)
	}
}

func TestAgentStripClickOpensWorktree(t *testing.T) {
	m := stripModel(t, 120, "alpha", "beta")
	_, items := m.agentStripLayout()
	var target stripItem
	for _, it := range items {
		if it.group.key == "repo/alpha" {
			target = it
		}
	}
	if target.end == 0 {
		t.Fatal("strip has no alpha item")
	}
	mm, _ := m.handleMouseClick(tea.MouseClickMsg{X: target.start, Y: 0, Button: tea.MouseLeft})
	m = mm.(Model)
	if m.current == nil || m.current.key != "repo/alpha" {
		t.Fatalf("clicking alpha should open it, current = %v", m.current)
	}
}

func TestAgentStripOff(t *testing.T) {
	m := stripModel(t, 120, "alpha")
	m.agentStrip = false
	if s, _ := m.agentStripLayout(); s != "" {
		t.Fatalf("strip should be empty when off: %q", s)
	}
}
