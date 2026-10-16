package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/KCaverly/caretaker/internal/agent"
	"github.com/KCaverly/caretaker/internal/ctl"
	"github.com/KCaverly/caretaker/internal/repo"
	"github.com/KCaverly/caretaker/internal/session"
	"github.com/KCaverly/caretaker/internal/state"
)

// The control socket lets `ct ctl` (and through it the orchestrator agent) act
// on the running ct. Every request arrives on the UI goroutine as a
// ctlRequestMsg, so handlers read and change the model like a keypress would;
// slow work (git, transcript reads) runs in a command and replies from there.

// ctlRequestMsg carries one request taken from the socket server.
type ctlRequestMsg struct{ p *ctl.Pending }

// ctlSpawnedMsg reports that spawn's `git worktree add` finished.
type ctlSpawnedMsg struct {
	p        *ctl.Pending
	repo     repo.Repo
	name     string
	wt       repo.Worktree
	err      error
	provider agent.Provider
	prompt   string
}

// ctlEnterMsg presses Enter in s a moment after a queued message was pasted,
// giving the agent's input time to take the paste as one block.
type ctlEnterMsg struct{ s *session.Session }

// queueRetry is how long a delivered message is trusted to have started a
// turn before the queue tries the next one. The status poll normally sees the
// agent go busy well within it; the limit only matters when a paste was lost.
const queueRetry = 20 * time.Second

// enterDelay separates the paste from the Enter that submits it.
const enterDelay = 250 * time.Millisecond

// WithCtl attaches the control socket server. Init starts reading from it.
func (m Model) WithCtl(srv *ctl.Server) Model {
	m.ctlSrv = srv
	return m
}

// watchCtl waits for the next request. It re-arms itself from handleCtl.
func (m Model) watchCtl() tea.Cmd {
	if m.ctlSrv == nil {
		return nil
	}
	ch := m.ctlSrv.Requests()
	return func() tea.Msg {
		p, ok := <-ch
		if !ok {
			return nil
		}
		return ctlRequestMsg{p: p}
	}
}

func (m Model) handleCtl(p *ctl.Pending) (tea.Model, tea.Cmd) {
	rearm := m.watchCtl()
	var (
		mm  tea.Model = m
		cmd tea.Cmd
	)
	switch p.Req.Op {
	case ctl.OpList:
		p.Reply(ctl.Response{OK: true, Worktrees: m.ctlWorktrees()})
	case ctl.OpSummary:
		mm, cmd = m.ctlSummary(p)
	case ctl.OpSpawn:
		mm, cmd = m.ctlSpawn(p)
	case ctl.OpSend:
		mm, cmd = m.ctlSend(p)
	case ctl.OpInterrupt:
		mm, cmd = m.ctlInterrupt(p)
	case ctl.OpStop:
		mm, cmd = m.ctlStop(p)
	case ctl.OpRemove:
		mm, cmd = m.ctlRemove(p)
	default:
		p.Fail(fmt.Errorf("unknown operation %q", p.Req.Op))
	}
	return mm, tea.Batch(rearm, cmd)
}

// ctlWorktrees lists every non-main worktree the deck knows, open or not.
func (m Model) ctlWorktrees() []ctl.WorktreeInfo {
	out := make([]ctl.WorktreeInfo, 0, len(m.active))
	for _, it := range m.active {
		out = append(out, m.ctlInfo(it))
	}
	return out
}

// ctlInfo describes one worktree: the deck's git columns plus the agents ct
// hosts for it and their live status.
func (m Model) ctlInfo(it activeItem) ctl.WorktreeInfo {
	key := wsKey(it.repo.Name, it.view.WT.Name)
	info := ctl.WorktreeInfo{
		Key: key, Repo: it.repo.Name, Name: it.view.WT.Name, Path: it.view.WT.Path,
		Branch: it.view.WT.Branch, Dirty: it.view.Dirty,
		Added: it.view.Add, Deleted: it.view.Del, Subject: it.view.Subject,
	}
	if it.view.HasBase {
		info.Base, info.Ahead, info.Behind = it.view.BaseBranch, it.view.Ahead, it.view.Behind
	}
	info.Open, info.Agents = m.ctlAgents(key)
	if m.state != nil {
		info.Queued = len(m.state.Queue(key))
	}
	return info
}

// ctlAgents lists the hosted agents of key with their polled status.
func (m Model) ctlAgents(key string) (open bool, agents []ctl.AgentInfo) {
	ws, ok := m.mgr.Workspace(key)
	if !ok {
		return false, nil
	}
	for i, s := range ws.Agents {
		pid := s.Pid()
		st := m.agentStatus[pid]
		agents = append(agents, ctl.AgentInfo{
			Provider: providerName(normalizedProvider(s.Provider)), Label: s.Title,
			SessionID: s.SessionID, Pid: pid, Status: st.Status, WaitingFor: st.WaitingFor,
			Active: i == ws.ActiveAgent,
		})
	}
	return true, agents
}

// ctlLookup resolves a "repo/name" key to its deck item.
func (m Model) ctlLookup(key string) (activeItem, error) {
	if it, ok := m.activeByKey(key); ok {
		return it, nil
	}
	if !m.groupsLoaded {
		return activeItem{}, fmt.Errorf("ct is still scanning repos; try again")
	}
	return activeItem{}, fmt.Errorf("no worktree %q (run `ct ctl list`)", key)
}

func (m Model) ctlSummary(p *ctl.Pending) (tea.Model, tea.Cmd) {
	it, err := m.ctlLookup(p.Req.Worktree)
	if err != nil {
		p.Fail(err)
		return m, nil
	}
	info := m.ctlInfo(it)
	var queue []string
	if m.state != nil {
		queue = append([]string(nil), m.state.Queue(info.Key)...)
	}
	// The active agent's transcript carries the "last message"; a worktree
	// with no open sessions falls back to the pool saved for its next open.
	cwd, sid := it.view.WT.Path, ""
	if ws, ok := m.mgr.Workspace(info.Key); ok {
		if s := ws.ActiveAgentSession(); s != nil {
			sid = s.SessionID
		}
	} else if m.state != nil {
		if saved, active := m.state.Agents(info.Key); active < len(saved) {
			sid = saved[active].SessionID
		}
	}
	lines := p.Req.Lines
	if lines <= 0 {
		lines = 20
	}
	wt, base, opts, ctrl := it.view.WT, info.Base, m.diffOptions(), m.ctrl
	return m, func() tea.Msg {
		s := ctl.Summary{WorktreeInfo: info, Queue: queue}
		if base != "" {
			if out, err := repo.Git(wt.Path, "log", "--oneline", "--no-decorate",
				"-n", fmt.Sprint(lines), base+"..HEAD"); err == nil {
				s.Commits = gitLines(out)
			}
			if files, err := repo.NumstatAgainstBase(wt, base, opts); err == nil {
				s.Files = ctlFileStats(files)
			}
		}
		if files, err := repo.NumstatUncommitted(wt, opts); err == nil {
			s.Uncommitted = ctlFileStats(files)
		}
		if sid != "" {
			if msg, err := ctrl.LastAssistantMessage(cwd, sid); err == nil {
				s.LastMessage = msg
			}
		}
		p.Reply(ctl.Response{OK: true, Summary: &s})
		return nil
	}
}

func ctlFileStats(files []repo.FileStat) []ctl.FileStat {
	out := make([]ctl.FileStat, 0, len(files))
	for _, f := range files {
		out = append(out, ctl.FileStat{Path: f.Path, Added: f.Add, Deleted: f.Del})
	}
	return out
}

func gitLines(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// ctlProvider picks the provider a request asked for, or the default.
func (m Model) ctlProvider(name string) (agent.Provider, error) {
	if name == "" {
		return m.defaultAgentProvider, nil
	}
	provider := agent.Provider(name)
	if !providerIn(provider, m.agentProviders) {
		return "", fmt.Errorf("agent provider %q is not enabled", name)
	}
	return provider, nil
}

func (m Model) ctlSpawn(p *ctl.Pending) (tea.Model, tea.Cmd) {
	req := p.Req
	var r *repo.Repo
	for i := range m.groups {
		if m.groups[i].Repo.Name == req.Repo {
			r = &m.groups[i].Repo
			for _, wv := range m.groups[i].Worktrees {
				if wv.WT.Name == req.Name {
					p.Fail(fmt.Errorf("worktree %s/%s already exists; use send", req.Repo, req.Name))
					return m, nil
				}
			}
		}
	}
	if r == nil {
		p.Fail(fmt.Errorf("no repo %q (run `ct ctl list`)", req.Repo))
		return m, nil
	}
	if err := repo.ValidateWorktreeName(req.Name); err != nil {
		p.Fail(fmt.Errorf("invalid name: %w", err))
		return m, nil
	}
	provider, err := m.ctlProvider(req.Provider)
	if err != nil {
		p.Fail(err)
		return m, nil
	}
	m.beginCreating(r.Name, req.Name)
	rr, name, base, prompt := *r, req.Name, req.Base, req.Text
	create := func() tea.Msg {
		wt, err := m.ctrl.Create(rr, name, base)
		return ctlSpawnedMsg{p: p, repo: rr, name: name, wt: wt, err: err, provider: provider, prompt: prompt}
	}
	return m, tea.Batch(create, m.flashCmd("creating "+name+"…"))
}

func (m Model) handleCtlSpawned(msg ctlSpawnedMsg) (tea.Model, tea.Cmd) {
	m.endCreating(msg.repo.Name, msg.name)
	if msg.err != nil {
		msg.p.Fail(fmt.Errorf("create worktree: %w", msg.err))
		return m, m.loadCmd()
	}
	key := wsKey(msg.wt.Repo, msg.wt.Name)
	mm, cmd, err := m.openBackground(key, msg.wt.Path, msg.provider, msg.prompt)
	if err != nil {
		msg.p.Fail(fmt.Errorf("worktree created at %s but its sessions failed to start: %w", msg.wt.Path, err))
		return mm, tea.Batch(cmd, mm.loadCmd())
	}
	msg.p.Reply(ctl.Response{OK: true, Message: fmt.Sprintf("created %s at %s and started a %s agent", key, msg.wt.Path, providerName(msg.provider))})
	return mm, tea.Batch(cmd, mm.loadCmd(), mm.flashCmd("orchestrator opened "+key))
}

// openBackground starts sessions for key without switching the user's view,
// the way activate would. An open workspace gets one more agent on prompt; a
// closed one is opened with its saved pool, the prompt going to the agent
// that was focused (or to a fresh one when nothing is saved).
func (m Model) openBackground(key, dir string, provider agent.Provider, prompt string) (Model, tea.Cmd, error) {
	w, h := m.sessionSize()
	if m.mgr.Has(key) {
		spec, err := m.ctrl.NewProviderAgentSpec(provider, randomAgentTitle(), prompt)
		if err != nil {
			return m, nil, err
		}
		if spec, err = m.prepareAgentSpec(dir, spec); err != nil {
			return m, nil, err
		}
		spawned, err := m.mgr.SpawnAgent(key, dir, spec, w, h)
		if err != nil {
			return m, nil, err
		}
		return m, tea.Batch(m.saveAgents(key), m.watchAgentEvents(spawned)), nil
	}
	specs, err := m.workspaceSpecsWithPrompt(key, provider, prompt)
	if err != nil {
		return m, nil, err
	}
	if specs, err = m.prepareWorkspaceSpecs(dir, specs); err != nil {
		return m, nil, err
	}
	ws, err := m.mgr.Activate(key, dir, specs, w, h)
	if err != nil {
		return m, nil, err
	}
	if saved := m.savedActiveAgent(key); saved < len(ws.Agents) {
		ws.ActiveAgent = saved
	}
	var save tea.Cmd
	if m.state != nil {
		m.state.Touch(key)
		m.persistAgents(key)
		save = m.writeStateCmd()
	}
	return m, tea.Batch(save, m.watchWorkspaceAgents(ws)), nil
}

// workspaceSpecsWithPrompt is workspaceSpecs for a background open that
// carries a first prompt: the saved pool is restored with the prompt handed
// to its focused agent, or one fresh agent of provider is started on it.
func (m Model) workspaceSpecsWithPrompt(key string, provider agent.Provider, prompt string) ([]session.Spec, error) {
	specs := []session.Spec{m.ctrl.EditorSpec()}
	var saved []stateAgent
	active := 0
	if m.state != nil {
		agents, idx := m.state.Agents(key)
		for _, a := range agents {
			saved = append(saved, stateAgent{provider: normalizedProvider(a.Provider), id: a.SessionID, label: a.Label})
		}
		active = idx
	}
	if len(saved) == 0 {
		spec, err := m.ctrl.NewProviderAgentSpec(provider, randomAgentTitle(), prompt)
		if err != nil {
			return nil, err
		}
		specs = append(specs, spec)
	} else {
		for i, a := range saved {
			first := ""
			if i == active {
				first = prompt
			}
			spec, err := m.ctrl.RestoreProviderAgentSpec(a.provider, a.id, a.label, first)
			if err != nil {
				return nil, err
			}
			specs = append(specs, spec)
		}
	}
	return append(specs, m.ctrl.TermSpec()), nil
}

type stateAgent struct {
	provider  agent.Provider
	id, label string
}

// ctlTarget resolves a key that may also name a synthetic workspace (the
// home or orchestrator one) to its directory.
func (m Model) ctlTarget(key string) (dir string, err error) {
	if key == m.homeWSKey && key != "" {
		return m.homeWSPath, nil
	}
	it, err := m.ctlLookup(key)
	if err != nil {
		return "", err
	}
	return it.view.WT.Path, nil
}

func (m Model) ctlSend(p *ctl.Pending) (tea.Model, tea.Cmd) {
	key, text := p.Req.Worktree, strings.TrimSpace(p.Req.Text)
	if text == "" {
		p.Fail(fmt.Errorf("nothing to send"))
		return m, nil
	}
	dir, err := m.ctlTarget(key)
	if err != nil {
		p.Fail(err)
		return m, nil
	}
	if !m.mgr.Has(key) {
		if key == m.homeWSKey {
			p.Fail(fmt.Errorf("%s is not open", key))
			return m, nil
		}
		provider, err := m.ctlProvider(p.Req.Provider)
		if err != nil {
			p.Fail(err)
			return m, nil
		}
		mm, cmd, err := m.openBackground(key, dir, provider, text)
		if err != nil {
			p.Fail(fmt.Errorf("open %s: %w", key, err))
			return mm, cmd
		}
		p.Reply(ctl.Response{OK: true, Message: "opened " + key + " and started its agent on the message"})
		return mm, tea.Batch(cmd, mm.loadCmd(), mm.flashCmd("orchestrator opened "+key))
	}
	mm, cmd, now := m.enqueue(key, text)
	if now {
		p.Reply(ctl.Response{OK: true, Message: "sent to " + key})
	} else {
		n := 0
		if mm.state != nil {
			n = len(mm.state.Queue(key))
		}
		p.Reply(ctl.Response{OK: true, Message: fmt.Sprintf("queued for %s (%d waiting; delivered when the agent is idle)", key, n)})
	}
	return mm, cmd
}

// enqueue files text for key's active agent, delivering it at once when the
// agent is idle and nothing is in flight. now reports which happened.
func (m Model) enqueue(key, text string) (Model, tea.Cmd, bool) {
	if m.state == nil {
		return m, nil, false
	}
	m.state.Enqueue(key, text)
	cmd := m.deliverQueued()
	delivered := len(m.state.Queue(key)) == 0
	return m, tea.Batch(m.writeStateCmd(), cmd), delivered
}

// deliverQueued hands each workspace's next queued message to its active agent
// when that agent is idle. Called after every status change; the queue is
// otherwise left alone, so a message never lands mid-turn or on a permission
// prompt. m is a pointer receiver because delivery pops the queue.
func (m *Model) deliverQueued() tea.Cmd {
	if m.state == nil {
		return nil
	}
	var cmds []tea.Cmd
	for key := range m.state.Queues {
		ws, ok := m.mgr.Workspace(key)
		if !ok {
			continue
		}
		s := ws.ActiveAgentSession()
		if s == nil || !s.Alive() {
			continue
		}
		st, tracked := m.agentStatus[s.Pid()]
		if !tracked || st.Status != "idle" {
			if st.Status == "busy" {
				delete(m.queueSent, key) // the last delivery took: ready for the next
			}
			continue
		}
		if at, ok := m.queueSent[key]; ok && time.Since(at) < queueRetry {
			continue
		}
		queued := m.state.Drain(key)
		if len(queued) == 0 {
			continue
		}
		if m.queueSent == nil {
			m.queueSent = map[string]time.Time{}
		}
		m.queueSent[key] = time.Now()
		// Everything queued goes as one message, so a burst of notes costs the
		// orchestrator one turn instead of one per note.
		s.Paste(strings.Join(queued, "\n\n"))
		cmds = append(cmds, tea.Tick(enterDelay, func(time.Time) tea.Msg { return ctlEnterMsg{s: s} }))
		cmds = append(cmds, m.flashCmd("delivered queued message to "+key))
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(append(cmds, m.writeStateCmd())...)
}

func (m Model) handleCtlEnter(msg ctlEnterMsg) (tea.Model, tea.Cmd) {
	if msg.s != nil && msg.s.Alive() {
		msg.s.SendKey(uv.KeyPressEvent{Code: uv.KeyEnter})
	}
	return m, nil
}

func (m Model) ctlInterrupt(p *ctl.Pending) (tea.Model, tea.Cmd) {
	key := p.Req.Worktree
	if _, err := m.ctlTarget(key); err != nil {
		p.Fail(err)
		return m, nil
	}
	ws, ok := m.mgr.Workspace(key)
	if !ok {
		p.Fail(fmt.Errorf("%s is not open", key))
		return m, nil
	}
	s := ws.ActiveAgentSession()
	if s == nil || !s.Alive() {
		p.Fail(fmt.Errorf("%s has no running agent", key))
		return m, nil
	}
	s.SendKey(uv.KeyPressEvent{Code: uv.KeyEscape})
	p.Reply(ctl.Response{OK: true, Message: "sent Escape to " + key})
	return m, nil
}

func (m Model) ctlStop(p *ctl.Pending) (tea.Model, tea.Cmd) {
	key := p.Req.Worktree
	if _, err := m.ctlTarget(key); err != nil {
		p.Fail(err)
		return m, nil
	}
	if !m.mgr.Has(key) {
		p.Fail(fmt.Errorf("%s is not open", key))
		return m, nil
	}
	if n := m.busyHostedAgents(key); n > 0 && !p.Req.Force {
		p.Fail(fmt.Errorf("%s has %d busy agent(s); interrupt first, or stop with --force", key, n))
		return m, nil
	}
	m.stopWorkspace(key)
	if m.state != nil {
		m.state.ClearQueue(key)
	}
	p.Reply(ctl.Response{OK: true, Message: "stopped " + key + " (worktree and branch kept)"})
	return m, tea.Batch(m.loadCmd(), m.writeStateCmd(), m.flashCmd("orchestrator stopped "+key))
}

// ctlRemove never deletes on its own: it parks the request and raises ct's
// usual remove-confirm panel in the deck, now or when the user next returns
// there. The reply says so, and the caller sees the result in a later list.
func (m Model) ctlRemove(p *ctl.Pending) (tea.Model, tea.Cmd) {
	it, err := m.ctlLookup(p.Req.Worktree)
	if err != nil {
		p.Fail(err)
		return m, nil
	}
	if m.ctlRemoveKey != "" && m.ctlRemoveKey != p.Req.Worktree {
		p.Fail(fmt.Errorf("the user is still being asked about removing %s", m.ctlRemoveKey))
		return m, nil
	}
	key := wsKey(it.repo.Name, it.view.WT.Name)
	m.ctlRemoveKey = key
	p.Reply(ctl.Response{OK: true, Message: "asked the user to confirm removing " + key + " in the deck; check `ct ctl list` later to see whether it is gone"})
	if m.screen == screenPicker && m.mode == modeNormal && !m.overlayOpen() {
		return m.showCtlRemove(), nil
	}
	return m, m.flashCmd("orchestrator asks to remove " + key + " — confirm in the deck")
}

// showCtlRemove opens the remove-confirm panel for the parked request. It is
// called whenever the deck becomes idle again.
func (m Model) showCtlRemove() Model {
	key := m.ctlRemoveKey
	if key == "" || m.screen != screenPicker || m.mode != modeNormal || m.overlayOpen() {
		return m
	}
	m.ctlRemoveKey = ""
	if !m.selectActiveByKey(key) {
		return m
	}
	it, ok := m.selectedActive()
	if !ok {
		return m
	}
	m = m.beginRemove(it)
	m.confirm.context = append(m.confirm.context, "requested by the orchestrator")
	return m
}

// orchestratorRepo picks the repo whose orchestrator the user means: the
// current workspace's repo, else the deck selection, else the only repo.
func (m Model) orchestratorRepo() (Group, bool) {
	var name string
	switch {
	case m.current != nil && m.current.key != m.homeWSKey:
		name = m.current.repo
	case m.screen == screenPicker:
		if it, ok := m.selectedActive(); ok {
			name = it.repo.Name
		}
	}
	if name == "" && len(m.groups) == 1 {
		name = m.groups[0].Repo.Name
	}
	for _, g := range m.groups {
		if g.Repo.Name == name {
			return g, true
		}
	}
	return Group{}, false
}

// mainWorktree returns the repo's main checkout, where its orchestrator runs.
func mainWorktree(g Group) (WorktreeView, bool) {
	for _, wv := range g.Worktrees {
		if wv.WT.IsMain {
			return wv, true
		}
	}
	return WorktreeView{}, false
}

// onOrchestrator reports whether the orchestrator agent is on screen.
func (m Model) onOrchestrator() bool {
	if m.screen != screenAgent || m.current == nil || m.current.ws == nil {
		return false
	}
	a := m.current.ws.ActiveAgentSession()
	return a != nil && a.Role == orchestratorRole
}

// orchestratorIndex finds the orchestrator agent in ws, or -1.
func orchestratorIndex(ws *session.Workspace) int {
	if ws == nil {
		return -1
	}
	for i, s := range ws.Agents {
		if s.Role == orchestratorRole {
			return i
		}
	}
	return -1
}

// openOrchestrator opens the repo's main checkout and focuses its orchestrator
// agent, starting one when the workspace has none. The orchestrator is an
// ordinary agent there: it shows in the board and the bar like any other, and
// it resumes across runs.
func (m Model) openOrchestrator() (tea.Model, tea.Cmd) {
	if m.ctlSrv == nil {
		return m, m.flashCmd("orchestrator needs the control socket, which did not start")
	}
	g, ok := m.orchestratorRepo()
	if !ok {
		return m, m.flashCmd("select a repo on the deck first")
	}
	main, ok := mainWorktree(g)
	if !ok {
		return m, m.flashCmd("no main checkout found for " + g.Repo.Name)
	}
	key := wsKey(g.Repo.Name, main.WT.Name)
	// A first open of a workspace with no saved agents would start the default
	// agent. Seed the pool with the orchestrator instead, so activation starts
	// it and nothing else.
	if m.state != nil && !m.mgr.Has(key) {
		if saved, _ := m.state.Agents(key); len(saved) == 0 {
			m.state.SetAgents(key, []state.AgentState{{
				Provider: agent.Claude, SessionID: newSessionID(), Label: orchestratorLabel, Role: orchestratorRole,
			}}, 0)
		}
	}
	mm, cmd := m.activate(g.Repo.Name, main.WT.Name, main.WT.Path)
	model := mm.(Model)
	if model.current == nil || model.current.key != key || model.current.ws == nil {
		return model, cmd // activation failed and reported it
	}
	ws := model.current.ws
	if idx := orchestratorIndex(ws); idx >= 0 {
		ws.ActiveAgent = idx
		model.screen = screenAgent
		model.clearWorkspaceAttention()
		return model, tea.Batch(cmd, model.saveAgents(key))
	}
	spawned, err := model.spawnOrchestrator(g.Repo.Name, key, main.WT.Path)
	if err != nil {
		model.setError("orchestrator error: " + err.Error())
		return model, cmd
	}
	ws.ActiveAgent = len(ws.Agents) - 1
	model.screen = screenAgent
	model.clearWorkspaceAttention()
	return model, tea.Batch(cmd, model.saveAgents(key), model.watchAgentEvents(spawned), model.flashCmd("orchestrator started"))
}

// spawnOrchestrator adds a fresh orchestrator agent to the open workspace key.
func (m Model) spawnOrchestrator(repoName, key, dir string) (*session.Session, error) {
	spec, err := m.ctrl.OrchestratorAgentSpec(repoName, "", "")
	if err != nil {
		return nil, err
	}
	spec, err = m.prepareAgentSpec(dir, spec)
	if err != nil {
		return nil, err
	}
	w, h := m.sessionSize()
	return m.mgr.SpawnAgent(key, dir, spec, w, h)
}

// resetOrchestrator starts the repo's orchestrator over with a fresh Claude
// session: same instructions, empty context. tasks.md stays, so the new
// session picks up from there. The old session is replaced in place; without
// one this is the same as opening the orchestrator.
func (m Model) resetOrchestrator() (tea.Model, tea.Cmd) {
	g, ok := m.orchestratorRepo()
	if !ok {
		return m, m.flashCmd("select a repo on the deck first")
	}
	main, ok := mainWorktree(g)
	if !ok {
		return m, m.flashCmd("no main checkout found for " + g.Repo.Name)
	}
	key := wsKey(g.Repo.Name, main.WT.Name)
	ws, open := m.mgr.Workspace(key)
	idx := orchestratorIndex(ws)
	if !open || idx < 0 {
		return m.openOrchestrator()
	}
	spec, err := m.ctrl.OrchestratorAgentSpec(g.Repo.Name, "", "")
	if err == nil {
		spec, err = m.prepareAgentSpec(main.WT.Path, spec)
	}
	if err != nil {
		m.setError("orchestrator restart error: " + err.Error())
		return m, nil
	}
	old := ws.Agents[idx]
	w, h := m.sessionSize()
	replacement, err := m.mgr.ReplaceAgent(key, main.WT.Path, idx, spec, w, h)
	if err != nil {
		m.setError("orchestrator restart error: " + err.Error())
		return m, nil
	}
	m.clearAgentTracking(old.Pid())
	mm, cmd := m.openOrchestrator()
	model := mm.(Model)
	return model, tea.Batch(cmd, model.saveAgents(key), model.watchAgentEvents(replacement), model.flashCmd("orchestrator started over"))
}
