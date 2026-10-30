package tui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/KCaverly/caretaker/internal/agent"
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
	got := m.orchestratorRepos("")
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
	spec, err := c.OrchestratorAgentSpec("", []OrchestratorRepo{{Name: "r", Path: "/r"}, {Name: "s", Path: "/s"}}, "", "")
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

// TestClaudeAgentsGetTheirAccount checks that a Claude agent in a repo of an
// extra account starts with that account's config folder, also when the
// worktree sits outside the repo, and that other agents are left alone.
func TestClaudeAgentsGetTheirAccount(t *testing.T) {
	m := ctlModel(t)
	m.ctrl = &Controller{cfg: config.Config{ClaudeAccounts: map[string]config.ClaudeAccount{
		"private": {ConfigDir: "/home/.mjclaude", Folders: []string{"/r"}},
	}}}
	m.groups[0].Worktrees = append(m.groups[0].Worktrees,
		WorktreeView{WT: repo.Worktree{Repo: "r", Name: "far", Path: "/elsewhere/far"}})
	m.groups = append(m.groups, Group{
		Repo:      repo.Repo{Name: "s", Path: "/s"},
		Worktrees: []WorktreeView{{WT: repo.Worktree{Repo: "s", Name: "(main)", Path: "/s", IsMain: true}}},
	})
	cases := map[string]string{
		"/r/.worktrees/a": "/home/.mjclaude",
		"/elsewhere/far":  "/home/.mjclaude",
		"/s":              "",
	}
	for dir, want := range cases {
		spec, err := m.prepareAgentSpec(dir, session.Spec{Provider: agent.Claude})
		if err != nil {
			t.Fatal(err)
		}
		got := ""
		for _, e := range spec.Env {
			if v, ok := strings.CutPrefix(e, "CLAUDE_CONFIG_DIR="); ok {
				got = v
			}
		}
		if got != want {
			t.Errorf("%s: CLAUDE_CONFIG_DIR = %q, want %q", dir, got, want)
		}
	}
}

// TestOrchestratorPerClaudeAccount checks that each Claude account gets its
// own orchestrator: its key, its repos, its folder and its account.
func TestOrchestratorPerClaudeAccount(t *testing.T) {
	cfgDir := t.TempDir()
	t.Setenv("CT_CONFIG", filepath.Join(cfgDir, "config.toml"))
	privateDir := filepath.Join(cfgDir, ".mjclaude")
	if err := os.MkdirAll(privateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Claude writes .claude.json at login, before ct ever trusts a folder there.
	if err := os.WriteFile(filepath.Join(privateDir, ".claude.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.ClaudeAccounts = map[string]config.ClaudeAccount{"private": {ConfigDir: privateDir, Folders: []string{"/s"}}}
	m := ctlModel(t)
	m.ctrl = NewController(cfg)
	m.groups = append(m.groups, Group{
		Repo:      repo.Repo{Name: "s", Path: "/s"},
		Worktrees: []WorktreeView{{WT: repo.Worktree{Repo: "s", Name: "(main)", Path: "/s", IsMain: true}}},
	})

	for _, account := range []string{"", "private"} {
		if got, ok := orchestratorAccountOf(orchestratorKeyFor(account)); !ok || got != account {
			t.Errorf("key round trip for %q gave %q %v", account, got, ok)
		}
	}
	if _, ok := orchestratorAccountOf("r/a"); ok {
		t.Error("a worktree key is not an orchestrator")
	}
	if got := m.orchestratorRepos("private"); len(got) != 1 || got[0].Name != "s" {
		t.Errorf("private repos = %v", got)
	}
	if got := m.orchestratorRepos(""); len(got) != 1 || got[0].Name != "r" {
		t.Errorf("default repos = %v", got)
	}
	m.current = &workspaceRef{repo: "s", worktree: "(main)", key: "s/(main)", path: "/s"}
	if got := m.orchestratorAccount(); got != "private" {
		t.Errorf("orchestrator account from a private worktree = %q", got)
	}

	spec, err := m.ctrl.OrchestratorAgentSpec("private", m.orchestratorRepos("private"), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(spec.Env, "CLAUDE_CONFIG_DIR="+privateDir) {
		t.Errorf("private orchestrator env = %v", spec.Env)
	}
	tasks := filepath.Join(cfgDir, "orchestrator-private", "s", "tasks.md")
	if _, err := os.Stat(tasks); err != nil {
		t.Errorf("private tasks.md not seeded: %v", err)
	}
	dir, err := m.ctrl.EnsureOrchestratorDir("private")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(privateDir, ".claude.json"))
	if !strings.Contains(string(data), dir) {
		t.Errorf("private orchestrator folder not trusted in the private account: %s", data)
	}
	if got := m.orchDir(orchestratorKeyFor("private")); got != dir {
		t.Errorf("orchDir = %q, want %q", got, dir)
	}
}
