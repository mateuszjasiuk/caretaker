package tui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/KCaverly/caretaker/internal/config"
	"github.com/KCaverly/caretaker/internal/stack"
)

// popupModel is ghStackModel (stack #7: a with PR #11, then x with no worktree)
// with the deck cursor on a.
func popupModel(t *testing.T) Model {
	t.Helper()
	m, _ := ghStackModel(t)
	m.width, m.height = 100, 30
	m.keys = config.Default().Keys
	m.ctrl = &Controller{cfg: config.Config{Editor: "cat", Agent: "cat", Shell: "sh", Keys: m.keys}}
	t.Cleanup(m.mgr.CloseAll)
	for i := range m.active {
		m.active[i].view.WT.Path = t.TempDir()
	}
	m.selectActiveByKey("r/a")
	return m
}

func press(t *testing.T, m Model, msg tea.KeyPressMsg) (Model, tea.Cmd) {
	t.Helper()
	mm, cmd := m.handleKey(msg)
	return mm.(Model), cmd
}

func popupText(m Model) string {
	return ansi.Strip(m.renderGhStackPopup(m.height - m.barHeight()))
}

func TestStackPopupListsTheStackAndFetches(t *testing.T) {
	m, cmd := press(t, popupModel(t), tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if m.ghPopup == nil {
		t.Fatal("ctrl+s should open the popup")
	}
	if cmd == nil || !m.ghPRs["r/a"].loading {
		t.Fatal("opening should start a fetch")
	}

	text := popupText(m)
	for _, want := range []string{"⛁ #7", "1/2", "#11", "2/2", "no PR", "loading…"} {
		if !strings.Contains(text, want) {
			t.Fatalf("want %q in:\n%s", want, text)
		}
	}
	if !strings.Contains(text, "› ") {
		t.Fatalf("the cursor should be on a:\n%s", text)
	}
}

func TestStackPopupShowsReviewState(t *testing.T) {
	m, _ := press(t, popupModel(t), tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	mm, _ := m.Update(ghPRsMsg{id: "r/a", prs: map[string]stack.BranchPR{
		"a": {Number: 11, State: "OPEN", Review: "CHANGES_REQUESTED", Checks: stack.Checks{Summary: "failing"}},
		"x": {Number: 12, State: "MERGED", Review: "APPROVED", Checks: stack.Checks{Summary: "passing"}},
	}})
	m = mm.(Model)

	text := popupText(m)
	for _, want := range []string{"open", "changes requested", "✗ checks", "#12", "merged", "approved", "✓ checks"} {
		if !strings.Contains(text, want) {
			t.Fatalf("want %q in:\n%s", want, text)
		}
	}
	if strings.Contains(text, "loading") {
		t.Fatalf("loading should be gone:\n%s", text)
	}
}

func TestStackPopupKeepsOldPRsWhenAFetchFails(t *testing.T) {
	m, _ := press(t, popupModel(t), tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	mm, _ := m.Update(ghPRsMsg{id: "r/a", prs: map[string]stack.BranchPR{"a": {Number: 11, State: "OPEN", Review: "APPROVED"}}})
	mm, _ = mm.(Model).Update(ghPRsMsg{id: "r/a", err: errors.New("offline")})
	m = mm.(Model)

	if text := popupText(m); !strings.Contains(text, "approved") || !strings.Contains(text, "couldn't fetch from GitHub: offline") {
		t.Fatalf("want the old review and the error:\n%s", text)
	}
}

func TestStackPopupFetchesAtMostOnceAMinuteUnlessRefreshed(t *testing.T) {
	m, _ := press(t, popupModel(t), tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	mm, _ := m.Update(ghPRsMsg{id: "r/a", prs: map[string]stack.BranchPR{}})
	m = mm.(Model)

	m, _ = press(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.ghPopup != nil {
		t.Fatal("esc should close the popup")
	}
	m, cmd := press(t, m, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if cmd != nil {
		t.Fatal("reopening within a minute should not fetch again")
	}
	if _, cmd = press(t, m, tea.KeyPressMsg{Code: 'r', Text: "r"}); cmd == nil {
		t.Fatal("r should fetch again")
	}
}

func TestStackPopupEnterOpensTheBranchSession(t *testing.T) {
	m, _ := press(t, popupModel(t), tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})

	m, _ = press(t, m, tea.KeyPressMsg{Code: 'j', Text: "j"})
	m, _ = press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.ghPopup == nil || !strings.Contains(m.status, "no worktree for x") {
		t.Fatalf("x has no worktree: popup %v, status %q", m.ghPopup, m.status)
	}

	m, _ = press(t, m, tea.KeyPressMsg{Code: 'k', Text: "k"})
	m, _ = press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.ghPopup != nil || m.current == nil || m.current.worktree != "a" {
		t.Fatalf("enter on a should open it: popup %v, current %v", m.ghPopup, m.current)
	}
}

func TestStackPopupOutsideAStack(t *testing.T) {
	m := popupModel(t)
	m.selectActiveByKey("r/b")
	m, _ = press(t, m, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if m.ghPopup != nil || !strings.Contains(m.status, "not in a gh stack") {
		t.Fatalf("b is not in a stack: popup %v, status %q", m.ghPopup, m.status)
	}
}
