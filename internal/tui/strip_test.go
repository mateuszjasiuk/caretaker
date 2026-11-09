package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/KCaverly/caretaker/internal/config"
	"github.com/KCaverly/caretaker/internal/ghstack"
	"github.com/KCaverly/caretaker/internal/repo"
	"github.com/KCaverly/caretaker/internal/session"
)

// stripModel returns a model w columns wide with the agent strip on and one
// open workspace per name in wts (all in repo "repo"); the last one is current.
func stripModel(t *testing.T, w int, wts ...string) Model {
	t.Helper()
	var keys []string
	for _, name := range wts {
		keys = append(keys, "repo/"+name)
	}
	return stripModelKeys(t, w, keys...)
}

// stripModelKeys is stripModel with "repo/worktree" keys, for several repos.
func stripModelKeys(t *testing.T, w int, keys ...string) Model {
	t.Helper()
	ctrl := &Controller{cfg: config.Config{
		Editor: "cat", Agent: "cat", Shell: "sh",
		Keys:    config.Default().Keys,
		Display: config.Display{AgentStrip: true},
	}}
	mgr := session.NewManager()
	t.Cleanup(mgr.CloseAll)

	m := New(ctrl, mgr)
	m.sidePanel = false // New reads the real state file
	mm, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: 24})
	m = mm.(Model)
	for _, key := range keys {
		repoName, name, _ := strings.Cut(key, "/")
		dir := t.TempDir()
		m.active = append(m.active, activeItem{
			repo: repo.Repo{Name: repoName},
			view: WorktreeView{WT: repo.Worktree{Name: name, Path: dir}},
		})
		mm, _ = m.activate(repoName, name, dir)
		m = mm.(Model)
	}
	m.screen = screenEditor
	return m
}

func barFirstRow(m Model) string {
	return ansi.Strip(strings.SplitN(m.renderBar(), "\n", 2)[0])
}

// barStripRow is the bar's second row, the separator line with the agent strip.
func barStripRow(m Model) string {
	return ansi.Strip(strings.SplitN(m.renderBar(), "\n", 2)[1])
}

func TestAgentStripListsWorktreesByName(t *testing.T) {
	m := stripModel(t, 120, "zeta", "alpha", "mid")
	row := barStripRow(m)
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
	if row := barStripRow(m); !strings.Contains(row, "! alpha") {
		t.Fatalf("a waiting worktree should show \"! alpha\": %q", row)
	}
}

func TestAgentStripMarksCurrentWorktree(t *testing.T) {
	m := stripModel(t, 120, "alpha", "beta")
	raw, _ := m.agentStripLayout()
	blue := "38;2;131;165;152" // cAccent as a truecolor foreground

	// The style is the escape code right before each name. Underlined text is
	// written one letter at a time, so look for the first letter only.
	styleOf := func(name string) string {
		at := strings.Index(raw, "m"+name[:1])
		if at < 0 {
			t.Fatalf("no %s in %q", name, raw)
		}
		return raw[strings.LastIndex(raw[:at], "\x1b["):at]
	}
	if st := styleOf("beta"); !strings.Contains(st, blue) || !strings.HasPrefix(st, "\x1b[4;") {
		t.Fatalf("the current worktree should be blue and underlined: %q", st)
	}
	if st := styleOf("alpha"); strings.Contains(st, blue) {
		t.Fatalf("another worktree should not be blue: %q", st)
	}
}

func TestAgentStripOverflow(t *testing.T) {
	names := []string{"one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten"}
	m := stripModel(t, 40, names...)
	row := barStripRow(m)
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
	mm, _ := m.handleMouseClick(tea.MouseClickMsg{X: target.start, Y: 1, Button: tea.MouseLeft})
	m = mm.(Model)
	if m.current == nil || m.current.key != "repo/alpha" {
		t.Fatalf("clicking alpha should open it, current = %v", m.current)
	}
}

func pressCtrl(t *testing.T, m Model, key rune) Model {
	t.Helper()
	mm, _ := m.handleKey(tea.KeyPressMsg{Code: key, Mod: tea.ModCtrl})
	return mm.(Model)
}

func TestWorktreeKeysMoveLikeTabsAndWrap(t *testing.T) {
	m := stripModel(t, 120, "alpha", "beta", "gamma") // gamma is current

	want := []struct {
		key  rune
		name string
	}{
		{'l', "alpha"}, // wraps past the last
		{'l', "beta"},
		{'h', "alpha"},
		{'h', "gamma"}, // wraps past the first
	}
	for i, w := range want {
		m = pressCtrl(t, m, w.key)
		if m.current == nil || m.current.worktree != w.name {
			t.Fatalf("press %d: current = %v, want %s", i, m.current, w.name)
		}
	}
}

func TestWorktreeKeysReachWorktreesHiddenFromTheBar(t *testing.T) {
	m := stripModel(t, 40, "alpha-long-worktree", "beta-long-worktree", "gamma-long-worktree", "delta-long-worktree")
	_, items := m.agentStripLayout()
	if len(items) >= 4 {
		t.Fatalf("test needs a bar too narrow for every worktree, shows %d", len(items))
	}

	seen := map[string]bool{}
	for range 4 {
		m = pressCtrl(t, m, 'l')
		seen[m.current.worktree] = true
	}
	if len(seen) != 4 {
		t.Fatalf("four presses should visit all four worktrees, saw %v", seen)
	}
}

func TestWorktreeKeysWithoutCurrentWorktree(t *testing.T) {
	m := stripModel(t, 120, "alpha", "beta", "gamma")
	m.current, m.screen = nil, screenPicker
	if got := pressCtrl(t, m, 'l'); got.current == nil || got.current.worktree != "alpha" {
		t.Fatalf("ctrl+l should open the first worktree, current = %v", got.current)
	}
	if got := pressCtrl(t, m, 'h'); got.current == nil || got.current.worktree != "gamma" {
		t.Fatalf("ctrl+h should open the last worktree, current = %v", got.current)
	}
}

func TestWorktreeKeysLeavePromptsAlone(t *testing.T) {
	m := stripModel(t, 120, "alpha", "beta")
	m.screen = screenPicker
	m.mode = modeCreateName
	if got := pressCtrl(t, m, 'h'); got.current.worktree != "beta" || got.mode != modeCreateName {
		t.Fatalf("ctrl+h in the name input must not switch: %s, mode %v", got.current.worktree, got.mode)
	}

	m.screen = screenEditor
	m.mode = modeConfirmTermClose
	if got := pressCtrl(t, m, 'l'); got.current.worktree != "beta" {
		t.Fatalf("ctrl+l in a confirm prompt must not switch: %s", got.current.worktree)
	}
}

func TestWorktreeKeysFireThroughHelp(t *testing.T) {
	m := stripModel(t, 120, "alpha", "beta")
	m.helpOpen = true
	got := pressCtrl(t, m, 'l')
	if got.helpOpen || got.current.worktree != "alpha" {
		t.Fatalf("ctrl+l should close help and move, help %v, current %s", got.helpOpen, got.current.worktree)
	}
}

func TestAgentStripOff(t *testing.T) {
	m := stripModel(t, 120, "alpha")
	m.agentStrip = false
	if s, _ := m.agentStripLayout(); s != "" {
		t.Fatalf("strip should be empty when off: %q", s)
	}
}

func TestAgentStripGroupsByRepo(t *testing.T) {
	m := stripModelKeys(t, 140, "front/zeta", "back/alpha", "front/beta")
	row := barStripRow(m)
	want := "front: beta · zeta │ back: alpha"
	if !strings.Contains(row, want) {
		t.Fatalf("strip should group worktrees under their repo, want %q in %q", want, row)
	}

	// One repo needs no repo name.
	m = stripModel(t, 140, "alpha", "beta")
	if row := barStripRow(m); strings.Contains(row, "repo:") {
		t.Fatalf("a single repo should not be named: %q", row)
	}
}

func TestAgentStripDropsRepoNamesFirst(t *testing.T) {
	m := stripModelKeys(t, 140, "front/zeta", "back/alpha", "front/beta")
	// Narrow the bar until the repo names no longer fit.
	row := barStripRow(m)
	for strings.Contains(row, "front:") && m.width > minViableWidth {
		m.width--
		row = barStripRow(m)
	}
	if !strings.Contains(row, "beta · zeta · alpha") {
		t.Fatalf("repo names should go before worktree names: %q", row)
	}
}

// TestAgentStripGroupsByClaudeAccount checks that an extra account's worktrees
// come after the default account's and sit on their own background.
func TestAgentStripGroupsByClaudeAccount(t *testing.T) {
	m := stripModelKeys(t, 140, "front/zeta", "home/alpha", "back/beta")
	var homePath string
	for _, it := range m.active {
		if it.repo.Name == "home" {
			homePath = it.view.WT.Path
		}
	}
	m.ctrl.cfg.ClaudeAccounts = map[string]config.ClaudeAccount{
		"private": {ConfigDir: t.TempDir(), Folders: []string{homePath}},
	}

	if row, want := barStripRow(m), "front: zeta │ back: beta │ home: alpha"; !strings.Contains(row, want) {
		t.Fatalf("the private worktree should come last, want %q in %q", want, row)
	}
	raw := strings.SplitN(m.renderBar(), "\n", 2)[1]
	tint := "48;2;58;40;64" // cAccountBg as a truecolor background
	at := strings.Index(raw, tint)
	if at < 0 {
		t.Fatalf("no account background in %q", raw)
	}
	if before := ansi.Strip(raw[:at]); strings.Contains(before, "alpha") || !strings.Contains(before, "beta") {
		t.Fatalf("the background should start at the private block, before it: %q", before)
	}
}

// stackStripModel is stripModel with branches a, b, c and d in one gh stack, in
// that order, and "beta" and "zed" outside it. "beta" is current.
func stackStripModel(t *testing.T) Model {
	t.Helper()
	m := stripModel(t, 140, "d", "a", "c", "b", "zed", "beta")
	m.stacksExpanded = false
	for i := range m.active {
		m.active[i].view.WT.Branch = m.active[i].view.WT.Name
	}
	stack := ghstack.Stacks{}
	for i, name := range []string{"a", "b", "c", "d"} {
		stack[name] = ghstack.Position{Stack: 4, Index: i + 1, Total: 4, Bottom: "a"}
	}
	m.ghStacks = map[string]ghstack.Stacks{"repo": stack}
	return m
}

func TestStripCollapsesStackYouAreNotIn(t *testing.T) {
	m := stackStripModel(t)
	if row, want := barStripRow(m), "⛁ #4 · beta · zed"; !strings.Contains(row, want) {
		t.Fatalf("want %q in %q", want, row)
	}
}

func TestStripExpandsTheCurrentStackInStackOrder(t *testing.T) {
	m := stackStripModel(t)
	mm, _ := m.activate("repo", "c", m.active[2].view.WT.Path)
	m = mm.(Model)
	if row, want := barStripRow(m), "⛁ #4: a · b · c · d · beta · zed"; !strings.Contains(row, want) {
		t.Fatalf("want %q in %q", want, row)
	}
}

func TestStripCollapsedStackShowsMostUrgentState(t *testing.T) {
	m := stackStripModel(t)
	ws, _ := m.mgr.Workspace("repo/b")
	if len(ws.Agents) == 0 {
		t.Skip("workspace started without an agent")
	}
	m.agentStatus = map[int]AgentStatus{ws.Agents[0].Pid(): {Status: "waiting"}}
	if row := barStripRow(m); !strings.Contains(row, "! ⛁ #4") {
		t.Fatalf("a waiting worktree should mark its collapsed stack: %q", row)
	}
}

func TestStripUnsubmittedStackUsesFirstBranch(t *testing.T) {
	m := stackStripModel(t)
	stack := m.ghStacks["repo"]
	for name, pos := range stack {
		pos.Stack = 0
		stack[name] = pos
	}
	if row := barStripRow(m); !strings.Contains(row, "⛁ a ·") {
		t.Fatalf("an unsubmitted stack should show its first branch: %q", row)
	}
}

func TestStripStackWithOneOpenWorktreeIsPlain(t *testing.T) {
	m := stripModel(t, 140, "a", "beta")
	m.stacksExpanded = false
	m.active[0].view.WT.Branch = "a"
	m.ghStacks = map[string]ghstack.Stacks{"repo": {"a": {Stack: 4, Index: 1, Total: 2, Bottom: "a"}}}
	if row := barStripRow(m); strings.Contains(row, "⛁") || !strings.Contains(row, "a · beta") {
		t.Fatalf("one open worktree needs no stack: %q", row)
	}
}

func TestToggleStacksShowsEveryStackAndIsSaved(t *testing.T) {
	m := stackStripModel(t)
	m = pressCtrl(t, m, 'y')
	if row, want := barStripRow(m), "⛁ #4: a · b · c · d · beta · zed"; !strings.Contains(row, want) {
		t.Fatalf("want %q in %q", want, row)
	}
	if !m.state.StacksExpanded {
		t.Fatal("the choice should be kept in the state")
	}

	m = pressCtrl(t, m, 'y')
	if row := barStripRow(m); !strings.Contains(row, "⛁ #4 · beta") {
		t.Fatalf("a second press should collapse again: %q", row)
	}
}

func TestWorktreeKeysTreatCollapsedStackAsOneStop(t *testing.T) {
	m := stackStripModel(t) // beta is current
	for _, name := range []string{"a", "b", "c", "d"} {
		m.state.LastOpened["repo/"+name] = 100
	}
	m.state.LastOpened["repo/c"] = 200

	want := []string{
		"zed",  // next after beta
		"c",    // wraps onto the collapsed stack: the worktree used last
		"d",    // now inside, so it goes through the stack
		"beta", // and out
	}
	for i, name := range want {
		m = pressCtrl(t, m, 'l')
		if m.current == nil || m.current.worktree != name {
			t.Fatalf("press %d: current = %v, want %s", i, m.current, name)
		}
		// Opening touches the state with the real time; keep c the last used.
		m.state.LastOpened["repo/c"] = 1 << 40
	}
}

func TestClickOnCollapsedStackOpensLastUsed(t *testing.T) {
	m := stackStripModel(t)
	for _, name := range []string{"a", "b", "c", "d"} {
		m.state.LastOpened["repo/"+name] = 100
	}
	m.state.LastOpened["repo/b"] = 200

	_, items := m.agentStripLayout()
	mm, _ := m.handleMouseClick(tea.MouseClickMsg{X: items[0].start, Y: 1, Button: tea.MouseLeft})
	m = mm.(Model)
	if m.current == nil || m.current.worktree != "b" {
		t.Fatalf("clicking the stack should open b, current = %v", m.current)
	}
}

// TestStripStackSitsOnItsOwnBackground checks that the stack's tint covers the
// stack from its name to its last worktree, and stops before the next one.
func TestStripStackSitsOnItsOwnBackground(t *testing.T) {
	m := stackStripModel(t)
	mm, _ := m.activate("repo", "c", m.active[2].view.WT.Path)
	m = mm.(Model)

	raw := strings.SplitN(m.renderBar(), "\n", 2)[1]
	tint := "48;2;31;58;54" // cStackBg as a truecolor background
	first, last := strings.Index(raw, tint), strings.LastIndex(raw, tint)
	if first < 0 {
		t.Fatalf("no stack background in %q", raw)
	}
	if before := ansi.Strip(raw[:first]); strings.Contains(before, "⛁") {
		t.Fatalf("the background should start at the stack's name, before it: %q", before)
	}
	tinted := ansi.Strip(raw[first:last])
	if !strings.Contains(tinted, "a · b · c") || strings.Contains(tinted, "beta") {
		t.Fatalf("the background should cover a to d and stop before beta: %q", tinted)
	}

	m = pressCtrl(t, m, 'l')
	m = pressCtrl(t, m, 'l') // to beta, so the stack collapses
	raw = strings.SplitN(m.renderBar(), "\n", 2)[1]
	if at := strings.Index(raw, tint); at < 0 || !strings.Contains(ansi.Strip(raw[at:]), "⛁ #4") {
		t.Fatalf("a collapsed stack keeps its background: %q", raw)
	}
}

func TestAgentStripSitsOnTheSeparatorLine(t *testing.T) {
	m := stripModel(t, 90, "alpha", "beta", "a-very-long-worktree-name-for-the-label")

	if row := barFirstRow(m); strings.Contains(row, "alpha") || !strings.Contains(row, "repo / a-very-long-worktree-name-for-the-label") {
		t.Fatalf("the first row should keep the full label and no strip: %q", row)
	}
	row := barStripRow(m)
	if !strings.HasPrefix(row, "── a-very-long-worktree-name-for-t… · alpha · beta ─") {
		t.Fatalf("the strip should sit on the separator line: %q", row)
	}
	if got := ansi.StringWidth(row); got != m.width {
		t.Fatalf("the separator line should be %d wide, got %d", m.width, got)
	}
}
