package tui

import (
	"slices"
	"sort"

	tea "charm.land/bubbletea/v2"

	"github.com/KCaverly/caretaker/internal/ghstack"
)

// ghStackIcon marks a gh stack in the bar's agent strip and on the deck.
const ghStackIcon = "⛁"

// stripEntries is what the bar's agent strip shows and what the worktree keys
// move through: stripGroups, with each gh stack you're not in collapsed into one
// entry, unless stacks are set to show in full.
func (m Model) stripEntries() []agentGroup {
	groups := m.stripGroups()
	if m.stacksExpanded {
		return groups
	}

	var entries []agentGroup
	for i := 0; i < len(groups); {
		end := i + 1
		for end < len(groups) && sameGhStack(groups[i], groups[end]) {
			end++
		}

		run := groups[i:end]
		if len(run) == 1 || m.holdsCurrent(run) {
			entries = append(entries, run...)
		} else {
			entries = append(entries, collapseGhStack(run))
		}
		i = end
	}
	return entries
}

// toggleStacks switches the agent strip between collapsing the gh stacks you're
// not in and showing every stack in full, and remembers the choice.
func (m Model) toggleStacks() (tea.Model, tea.Cmd) {
	m.stacksExpanded = !m.stacksExpanded
	if m.state != nil {
		m.state.StacksExpanded = m.stacksExpanded
	}
	return m, m.writeStateCmd()
}

func (m Model) holdsCurrent(groups []agentGroup) bool {
	if m.current == nil {
		return false
	}
	return slices.ContainsFunc(groups, func(g agentGroup) bool { return g.key == m.current.key })
}

// groupStripStacks prepares the strip's worktrees for stripEntries: a gh stack
// with one open worktree is a plain worktree, and the others keep their worktrees
// next to each other, in stack order.
func groupStripStacks(groups []agentGroup) []agentGroup {
	open := map[string]int{}
	for _, g := range groups {
		open[ghStackID(g)]++
	}
	for i, g := range groups {
		if open[ghStackID(g)] < 2 {
			groups[i].stack = ghstack.Position{}
		}
	}
	return keepStacksTogether(groups, func(g agentGroup) (string, int) {
		return ghStackID(g), g.stack.Index
	})
}

// keepStacksTogether moves each stack's items next to the first one, in stack
// order, and leaves everything else where it is. stackOf returns an item's stack
// ("" for none) and its place in that stack.
func keepStacksTogether[T any](items []T, stackOf func(T) (string, int)) []T {
	members := map[string][]T{}
	for _, it := range items {
		if id, _ := stackOf(it); id != "" {
			members[id] = append(members[id], it)
		}
	}

	out := make([]T, 0, len(items))
	for _, it := range items {
		id, _ := stackOf(it)
		if id == "" {
			out = append(out, it)
			continue
		}
		stack, ok := members[id]
		if !ok {
			continue
		}
		delete(members, id)
		sort.SliceStable(stack, func(i, j int) bool {
			_, a := stackOf(stack[i])
			_, b := stackOf(stack[j])
			return a < b
		})
		out = append(out, stack...)
	}
	return out
}

// ghStackID identifies a worktree's stack across repos, "" when it isn't in one.
func ghStackID(g agentGroup) string {
	if g.stack.Index == 0 {
		return ""
	}
	return g.repo + "/" + g.stack.Bottom
}

func sameGhStack(a, b agentGroup) bool {
	return ghStackID(a) != "" && ghStackID(a) == ghStackID(b)
}

// collapseGhStack makes one strip entry for a stack's worktrees, showing the most
// urgent state among them.
func collapseGhStack(run []agentGroup) agentGroup {
	first := run[0]
	entry := agentGroup{
		key: "stack:" + ghStackID(first), repo: first.repo, worktree: first.stack.Name(),
		account: first.account, stack: first.stack, members: run,
	}
	for _, g := range run {
		entry.attn = max(entry.attn, g.attn)
		entry.busy = entry.busy || g.busy
		entry.highlighted = entry.highlighted || g.highlighted
	}
	return entry
}

// lastUsedGroup is the worktree opened most recently, the first one when none
// has been opened.
func lastUsedGroup(groups []agentGroup) agentGroup {
	best := groups[0]
	for _, g := range groups[1:] {
		if g.opened > best.opened {
			best = g
		}
	}
	return best
}
