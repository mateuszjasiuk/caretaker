package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KCaverly/caretaker/internal/config"
	"github.com/KCaverly/caretaker/internal/ctl"
	"github.com/KCaverly/caretaker/internal/repo"
	"github.com/KCaverly/caretaker/internal/session"
	"github.com/KCaverly/caretaker/internal/state"
)

func TestClaudeProjectName(t *testing.T) {
	got := claudeProjectName("/Users/me/Projects/caretaker/.worktrees/orchestrator")
	want := "-Users-me-Projects-caretaker--worktrees-orchestrator"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestLastAssistantText(t *testing.T) {
	transcript := strings.Join([]string{
		`{"type":"user","message":{"role":"user","content":"hi"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"First reply."}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"x","name":"Bash","input":{}}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Done: "},{"type":"text","text":"tests pass."}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"y","name":"Read","input":{}}]}}`,
		`not json at all`,
	}, "\n")
	got := lastAssistantText(strings.NewReader(transcript))
	if got != "Done:\ntests pass." {
		t.Fatalf("got %q", got)
	}
	if lastAssistantText(strings.NewReader("")) != "" {
		t.Fatal("empty transcript should give no message")
	}
}

func TestLastAssistantTextCaps(t *testing.T) {
	long := strings.Repeat("x", lastMessageMax+50)
	line := `{"type":"assistant","message":{"content":[{"type":"text","text":"` + long + `"}]}}`
	got := lastAssistantText(strings.NewReader(line))
	if len([]rune(got)) != lastMessageMax+2 || !strings.HasSuffix(got, " …") {
		t.Fatalf("got length %d", len([]rune(got)))
	}
}

// ctlModel is a deck with one repo and two closed worktrees, ready for a
// control request.
func ctlModel(t *testing.T) Model {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	m := New(&Controller{}, session.NewManager())
	m.groups = []Group{{
		Repo: repo.Repo{Name: "r", Path: "/r"},
		Worktrees: []WorktreeView{
			{WT: repo.Worktree{Repo: "r", Name: "(main)", Path: "/r", IsMain: true}},
			{WT: repo.Worktree{Repo: "r", Name: "a", Path: "/r/.worktrees/a", Branch: "a"}, HasBase: true, BaseBranch: "main", Ahead: 2},
			{WT: repo.Worktree{Repo: "r", Name: "b", Path: "/r/.worktrees/b", Branch: "b"}, Dirty: true, Add: 4, Del: 1},
		},
	}}
	m.groupsLoaded = true
	m.recomputeActive()
	return m
}

func pending(req ctl.Request) (*ctl.Pending, <-chan ctl.Response) {
	p, ch := ctl.NewPendingForTest(req)
	return p, ch
}

func TestCtlListSkipsMainAndReportsQueue(t *testing.T) {
	m := ctlModel(t)
	m.state.Enqueue("r/b", "later")
	p, ch := pending(ctl.Request{Op: ctl.OpList})
	m.handleCtl(p)
	resp := <-ch
	if !resp.OK || len(resp.Worktrees) != 2 {
		t.Fatalf("resp %+v", resp)
	}
	a, b := resp.Worktrees[0], resp.Worktrees[1]
	if a.Key != "r/a" || a.Base != "main" || a.Ahead != 2 || a.Open {
		t.Fatalf("a = %+v", a)
	}
	if b.Key != "r/b" || !b.Dirty || b.Added != 4 || b.Queued != 1 {
		t.Fatalf("b = %+v", b)
	}
}

func TestCtlUnknownWorktree(t *testing.T) {
	m := ctlModel(t)
	for _, op := range []ctl.Op{ctl.OpSummary, ctl.OpInterrupt, ctl.OpStop, ctl.OpRemove} {
		p, ch := pending(ctl.Request{Op: op, Worktree: "r/zzz"})
		m.handleCtl(p)
		if resp := <-ch; resp.OK || !strings.Contains(resp.Error, "no worktree") {
			t.Fatalf("%s: resp %+v", op, resp)
		}
	}
}

func TestCtlSpawnRefusesExistingAndBadNames(t *testing.T) {
	m := ctlModel(t)
	p, ch := pending(ctl.Request{Op: ctl.OpSpawn, Repo: "r", Name: "a", Text: "x"})
	m.handleCtl(p)
	if resp := <-ch; resp.OK || !strings.Contains(resp.Error, "already exists") {
		t.Fatalf("resp %+v", resp)
	}
	p, ch = pending(ctl.Request{Op: ctl.OpSpawn, Repo: "nope", Name: "c", Text: "x"})
	m.handleCtl(p)
	if resp := <-ch; resp.OK || !strings.Contains(resp.Error, "no repo") {
		t.Fatalf("resp %+v", resp)
	}
	p, ch = pending(ctl.Request{Op: ctl.OpSpawn, Repo: "r", Name: "../c", Text: "x"})
	m.handleCtl(p)
	if resp := <-ch; resp.OK || !strings.Contains(resp.Error, "invalid name") {
		t.Fatalf("resp %+v", resp)
	}
}

func TestCtlStopNeedsOpenWorkspace(t *testing.T) {
	m := ctlModel(t)
	p, ch := pending(ctl.Request{Op: ctl.OpStop, Worktree: "r/a"})
	m.handleCtl(p)
	if resp := <-ch; resp.OK || !strings.Contains(resp.Error, "not open") {
		t.Fatalf("resp %+v", resp)
	}
}

func TestCtlRemoveParksUntilDeckIdle(t *testing.T) {
	m := ctlModel(t)
	m.screen = screenAgent // the user is inside a session
	p, ch := pending(ctl.Request{Op: ctl.OpRemove, Worktree: "r/b"})
	mm, _ := m.handleCtl(p)
	m = mm.(Model)
	if resp := <-ch; !resp.OK || !strings.Contains(resp.Message, "confirm") {
		t.Fatalf("resp %+v", resp)
	}
	if m.ctlRemoveKey != "r/b" || m.mode != modeNormal {
		t.Fatalf("expected a parked request, got key %q mode %v", m.ctlRemoveKey, m.mode)
	}

	// Back on the deck, the confirm panel opens for that worktree.
	mm, _ = m.returnToDeck()
	m = mm.(Model)
	if m.mode != modeConfirmRemove || m.ctlRemoveKey != "" {
		t.Fatalf("mode %v, parked %q", m.mode, m.ctlRemoveKey)
	}
	if it, ok := m.selectedActive(); !ok || it.view.WT.Name != "b" {
		t.Fatalf("confirm targets %+v", it)
	}
	if !strings.Contains(strings.Join(m.confirm.context, "\n"), "orchestrator") {
		t.Fatalf("context %v", m.confirm.context)
	}
}

func TestCtlRemoveShowsAtOnceOnIdleDeck(t *testing.T) {
	m := ctlModel(t)
	p, ch := pending(ctl.Request{Op: ctl.OpRemove, Worktree: "r/a"})
	mm, _ := m.handleCtl(p)
	m = mm.(Model)
	<-ch
	if m.mode != modeConfirmRemove {
		t.Fatalf("mode %v", m.mode)
	}
}

func TestQueueStateRoundTrip(t *testing.T) {
	m := ctlModel(t)
	m.state.Enqueue("r/a", "one")
	m.state.Enqueue("r/a", "two")
	if got := m.state.Queue("r/a"); len(got) != 2 {
		t.Fatalf("queue %v", got)
	}
	if text, ok := m.state.Dequeue("r/a"); !ok || text != "one" {
		t.Fatalf("dequeue %q %v", text, ok)
	}
	if text, ok := m.state.Dequeue("r/a"); !ok || text != "two" {
		t.Fatalf("dequeue %q %v", text, ok)
	}
	if _, ok := m.state.Dequeue("r/a"); ok {
		t.Fatal("queue should be empty")
	}
}

func TestOrchestratorReposListEveryRepo(t *testing.T) {
	m := ctlModel(t)
	m.groups = append(m.groups, Group{Repo: repo.Repo{Name: "s", Path: "/s"}})
	got := m.orchestratorRepos()
	want := []OrchestratorRepo{{Name: "r", Path: "/r"}, {Name: "s", Path: "/s"}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("repos = %v, want %v", got, want)
	}
}

// TestSavedAgentsDropOldOrchestrators checks that an orchestrator saved in a
// main checkout's pool (from when each repo ran its own) is not restored
// there, and that the focused agent still points at the same agent.
func TestSavedAgentsDropOldOrchestrators(t *testing.T) {
	m := ctlModel(t)
	pool := []state.AgentState{
		{Provider: "claude", SessionID: "o", Role: orchestratorRole},
		{Provider: "claude", SessionID: "a"},
		{Provider: "claude", SessionID: "b"},
	}
	m.state.SetAgents("r/(main)", pool, 2)
	saved, active := m.savedAgents("r/(main)")
	if len(saved) != 2 || saved[0].SessionID != "a" || saved[active].SessionID != "b" {
		t.Fatalf("saved = %+v active = %d", saved, active)
	}
	if m.onlyOldOrchestrators("r/(main)") {
		t.Fatal("a pool with plain agents should be restored")
	}
	m.state.SetAgents("r/a", pool[:1], 0)
	if !m.onlyOldOrchestrators("r/a") {
		t.Fatal("a pool with only an old orchestrator should not be restored")
	}
	m.state.SetAgents(orchestratorKey, pool[:1], 0)
	if saved, _ := m.savedAgents(orchestratorKey); len(saved) != 1 || saved[0].Role != orchestratorRole {
		t.Fatalf("orchestrator workspace lost its orchestrator: %+v", saved)
	}
}

func TestOrchestratorAgentSpecCarriesRoleModelAndNotes(t *testing.T) {
	cfgDir := t.TempDir()
	t.Setenv("CT_CONFIG", filepath.Join(cfgDir, "config.toml"))
	cfg := config.Default()
	cfg.Agent = "claude"
	c := NewController(cfg)
	spec, err := c.OrchestratorAgentSpec([]OrchestratorRepo{{Name: "r", Path: "/r"}, {Name: "s", Path: "/s"}}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if spec.Role != orchestratorRole || spec.Title == "" {
		t.Fatalf("spec role/title = %q %q", spec.Role, spec.Title)
	}
	argv := strings.Join(spec.Argv, " ")
	for _, want := range []string{"--model sonnet", "--add-dir /r", "--add-dir /s", filepath.Join(cfgDir, "orchestrator")} {
		if !strings.Contains(argv, want) {
			t.Fatalf("argv missing %q: %s", want, argv)
		}
	}
	for _, r := range []string{"r", "s"} {
		tasks := filepath.Join(cfgDir, "orchestrator", r, "tasks.md")
		if !strings.Contains(argv, tasks) {
			t.Fatalf("instructions do not name %s", tasks)
		}
		if _, err := os.Stat(tasks); err != nil {
			t.Fatalf("tasks.md not seeded: %v", err)
		}
	}
	if idx := orchestratorIndex(&session.Workspace{Agents: []*session.Session{{}, {Role: orchestratorRole}}}); idx != 1 {
		t.Fatalf("orchestratorIndex = %d, want 1", idx)
	}
	if repoOfKey("r/a") != "r" {
		t.Fatal("repoOfKey")
	}
}

func TestWorktreePathIncludesMainCheckout(t *testing.T) {
	m := ctlModel(t)
	if p, ok := m.worktreePath("r/(main)"); !ok || p != "/r" {
		t.Fatalf("main checkout path = %q %v", p, ok)
	}
	if p, ok := m.worktreePath("r/a"); !ok || p != "/r/.worktrees/a" {
		t.Fatalf("worktree path = %q %v", p, ok)
	}
	if _, ok := m.worktreePath("r/gone"); ok {
		t.Fatal("a missing worktree should not resolve")
	}
}

func TestCtlOpenLeavesAnOpenWorkspaceAlone(t *testing.T) {
	m := ctlModel(t)
	if _, err := m.mgr.Activate("r/a", t.TempDir(), nil, 80, 24); err != nil {
		t.Fatal(err)
	}
	p, ch := pending(ctl.Request{Op: ctl.OpOpen, Worktree: "r/a"})
	m.handleCtl(p)
	if resp := <-ch; !resp.OK || !strings.Contains(resp.Message, "already open") {
		t.Fatalf("resp %+v", resp)
	}
	for _, op := range []ctl.Op{ctl.OpOpen, ctl.OpReopen} {
		p, ch := pending(ctl.Request{Op: op, Worktree: "r/nope"})
		m.handleCtl(p)
		if resp := <-ch; resp.OK {
			t.Fatalf("%s on an unknown worktree should fail: %+v", op, resp)
		}
	}
}
