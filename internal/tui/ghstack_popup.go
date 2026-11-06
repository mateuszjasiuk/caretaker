package tui

import (
	"fmt"
	"maps"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KCaverly/caretaker/internal/ghstack"
	"github.com/KCaverly/caretaker/internal/stack"
)

// ghPRsFresh is how long fetched review state is shown before opening the popup
// fetches it again.
const ghPRsFresh = 60 * time.Second

// ghStackPopup is the open gh stack popup: every branch of one stack, bottom to
// top, with what GitHub says about each one's PR.
type ghStackPopup struct {
	repo, repoPath string
	stack          ghstack.Position
	branches       []string
	cursor         int
}

// id is the stack's key in Model.ghPRs.
func (p ghStackPopup) id() string { return p.repo + "/" + p.stack.Bottom }

// ghPRFetch is the last answer from GitHub for one stack. Only memory holds it.
type ghPRFetch struct {
	prs     map[string]stack.BranchPR
	err     error
	at      time.Time
	loading bool
}

type ghPRsMsg struct {
	id  string
	prs map[string]stack.BranchPR
	err error
}

// openGhStackPopup opens the popup for the stack of the deck's selected worktree,
// or of the current one in a session, and fetches its review state unless that
// is still fresh.
func (m Model) openGhStackPopup() (tea.Model, tea.Cmd) {
	repoName, branch := m.focusedBranch()
	pos, ok := m.ghStackPosition(repoName, branch)
	if !ok {
		return m, m.flashCmd("not in a gh stack")
	}

	var repoPath string
	for _, g := range m.groups {
		if g.Repo.Name == repoName {
			repoPath = g.Repo.Path
		}
	}
	m.ghPopup = &ghStackPopup{
		repo: repoName, repoPath: repoPath, stack: pos,
		branches: m.ghStackBranches(repoName, pos.Bottom), cursor: pos.Index - 1,
	}
	return m.fetchGhPRs(false)
}

// handleGhStackPopup routes keys while the popup is open. It swallows every
// other key so none reaches the session underneath.
func (m Model) handleGhStackPopup(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	p := *m.ghPopup
	branch := p.branches[p.cursor]

	switch msg.String() {
	case "esc", m.keys.StackPopup:
		m.ghPopup = nil
	case "up", "k":
		p.cursor = max(0, p.cursor-1)
		m.ghPopup = &p
	case "down", "j":
		p.cursor = min(len(p.branches)-1, p.cursor+1)
		m.ghPopup = &p
	case "r":
		return m.fetchGhPRs(true)
	case "o":
		pr, ok := m.ghPRs[p.id()].prs[branch]
		if !ok || pr.URL == "" {
			return m, m.flashCmd("no PR for " + branch)
		}
		return m, openURLCmd(pr.URL)
	case "enter":
		it, ok := m.worktreeOnBranch(p.repo, branch)
		if !ok {
			return m, m.flashCmd("no worktree for " + branch)
		}
		m.ghPopup = nil
		return m.activate(it.repo.Name, it.view.WT.Name, it.view.WT.Path)
	}
	return m, nil
}

// applyGhPRs stores a fetch's answer. A failed fetch keeps the PRs fetched
// before, so the popup still has something to show.
func (m Model) applyGhPRs(msg ghPRsMsg) Model {
	entry := m.ghPRs[msg.id]
	entry.loading, entry.err, entry.at = false, msg.err, time.Now()
	if msg.err == nil {
		entry.prs = msg.prs
	}

	m.ghPRs = maps.Clone(m.ghPRs)
	if m.ghPRs == nil {
		m.ghPRs = map[string]ghPRFetch{}
	}
	m.ghPRs[msg.id] = entry
	return m
}

// renderGhStackPopup draws the popup: one row per branch, then the fetch's state
// and the keys.
func (m Model) renderGhStackPopup(h int) string {
	p := m.ghPopup
	fetch := m.ghPRs[p.id()]
	innerW := panelInnerWidth(m.width, 76)

	nameW := 0
	for _, b := range p.branches {
		nameW = max(nameW, len(b))
	}
	nameW = min(nameW, 28)

	rows := []string{
		header("stack", -1), "",
		"  " + repoHdrStyle.Render(ghStackIcon+" "+p.stack.Name()) + dimStyle.Render("  "+p.repo), "",
	}
	for i, b := range p.branches {
		rows = append(rows, m.ghStackPopupRow(p, i, b, nameW, fetch))
	}

	rows = append(rows, "")
	switch {
	case fetch.loading:
		rows = append(rows, dimStyle.Render("  loading…"))
	case fetch.err != nil:
		rows = append(rows, errStyle.Render(truncateTo("  couldn't fetch from GitHub: "+fetch.err.Error(), innerW)))
	}
	rows = append(rows, "  "+strings.Join([]string{
		keyhint("enter", "open session"), keyhint("o", "open PR"), keyhint("r", "refresh"), keyhint("esc", "close"),
	}, helpStyle.Render(" · ")))
	return renderPanel(rows, innerW, m.width, h)
}

// ghStackPopupRow is one branch: cursor, session, position, name, PR and, once
// fetched, the PR's state, review and checks.
func (m Model) ghStackPopupRow(p *ghStackPopup, i int, branch string, nameW int, fetch ghPRFetch) string {
	cursor := " "
	if i == p.cursor {
		cursor = accentStyle.Render("›")
	}
	session := " "
	if it, ok := m.worktreeOnBranch(p.repo, branch); ok {
		session = dimStyle.Render("○")
		if it.view.Live {
			session = liveStyle.Render("●")
		}
	}
	parts := []string{
		cursor + " " + session,
		dimStyle.Render(fmt.Sprintf("%d/%d", i+1, len(p.branches))),
		nameStyle.Render(padLine(truncateTo(branch, nameW), nameW)),
	}

	pr, fetched := fetch.prs[branch]
	number := pr.Number
	if !fetched {
		number = m.ghStacks[p.repo][branch].PR
	}
	if number == 0 {
		return "  " + strings.Join(append(parts, dimStyle.Render("no PR")), "  ")
	}
	parts = append(parts, padLine(fmt.Sprintf("#%d", number), 6))
	if fetched {
		parts = append(parts, prStateWord(pr), padLine(prReviewWord(pr.Review), len("changes requested")), prChecksWord(pr.Checks.Summary))
	}
	return "  " + strings.Join(parts, "  ")
}

// fetchGhPRs asks GitHub for the popup's PRs in the background, unless a fetch is
// running or, without force, the last answer is still fresh.
func (m Model) fetchGhPRs(force bool) (tea.Model, tea.Cmd) {
	p := m.ghPopup
	id := p.id()
	entry := m.ghPRs[id]
	if entry.loading || (!force && !entry.at.IsZero() && time.Since(entry.at) < ghPRsFresh) {
		return m, nil
	}

	entry.loading = true
	m.ghPRs = maps.Clone(m.ghPRs)
	if m.ghPRs == nil {
		m.ghPRs = map[string]ghPRFetch{}
	}
	m.ghPRs[id] = entry

	dir, branches := p.repoPath, p.branches
	return m, func() tea.Msg {
		prs, err := stack.BranchPRs(dir, branches)
		return ghPRsMsg{id: id, prs: prs, err: err}
	}
}

// focusedBranch is the deck's selected worktree on the deck, and the current
// worktree elsewhere.
func (m Model) focusedBranch() (repoName, branch string) {
	if m.screen == screenPicker {
		if it, ok := m.selectedActive(); ok {
			return it.repo.Name, it.view.WT.Branch
		}
		return "", ""
	}
	if m.current == nil {
		return "", ""
	}
	if it, ok := m.activeByKey(m.current.key); ok {
		return it.repo.Name, it.view.WT.Branch
	}
	return "", ""
}

// ghStackBranches lists a stack's branches, bottom to top.
func (m Model) ghStackBranches(repoName, bottom string) []string {
	var branches []string
	for b, pos := range m.ghStacks[repoName] {
		if pos.Bottom == bottom {
			branches = append(branches, b)
		}
	}
	sort.Slice(branches, func(i, j int) bool {
		return m.ghStacks[repoName][branches[i]].Index < m.ghStacks[repoName][branches[j]].Index
	})
	return branches
}

func (m Model) worktreeOnBranch(repoName, branch string) (activeItem, bool) {
	for _, it := range m.active {
		if it.repo.Name == repoName && it.view.WT.Branch == branch {
			return it, true
		}
	}
	return activeItem{}, false
}

func prStateWord(pr stack.BranchPR) string {
	switch {
	case pr.State == "MERGED":
		return purpleStyle.Render("merged")
	case pr.State == "CLOSED":
		return dimStyle.Render("closed")
	case pr.Draft:
		return dimStyle.Render("draft ")
	}
	return nameStyle.Render("open  ")
}

func prReviewWord(review string) string {
	switch review {
	case "APPROVED":
		return aheadStyle.Render("approved")
	case "CHANGES_REQUESTED":
		return errStyle.Render("changes requested")
	case "REVIEW_REQUIRED":
		return dirtyStyle.Render("review needed")
	}
	return ""
}

func prChecksWord(summary string) string {
	switch summary {
	case "passing":
		return aheadStyle.Render("✓ checks")
	case "failing":
		return errStyle.Render("✗ checks")
	case "pending":
		return dirtyStyle.Render("… checks")
	}
	return ""
}
