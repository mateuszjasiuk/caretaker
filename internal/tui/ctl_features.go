package tui

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/KCaverly/caretaker/internal/ctl"
	"github.com/KCaverly/caretaker/internal/repo"
)

// ctlList answers `ct ctl list`, all worktrees or only one feature's.
func (m Model) ctlList(p *ctl.Pending) {
	worktrees := m.ctlWorktrees()
	if p.Req.Feature == "" {
		p.Reply(ctl.Response{OK: true, Worktrees: worktrees})
		return
	}

	if _, ok := m.state.Features[p.Req.Feature]; !ok {
		p.Fail(fmt.Errorf("no feature %s (run `ct ctl feature list`)", p.Req.Feature))
		return
	}
	var out []ctl.WorktreeInfo
	for _, w := range worktrees {
		if w.Feature == p.Req.Feature {
			out = append(out, w)
		}
	}
	p.Reply(ctl.Response{OK: true, Worktrees: out})
}

func (m Model) ctlFeatureNew(p *ctl.Pending) (tea.Model, tea.Cmd) {
	req := p.Req
	repoName, name, err := m.ctlFeatureKey(req.Feature)
	if err != nil {
		p.Fail(err)
		return m, nil
	}
	if err := repo.ValidateWorktreeName(name); err != nil {
		p.Fail(fmt.Errorf("invalid feature name: %w", err))
		return m, nil
	}
	manager, err := m.ctlLookup(m.ctlKey(req.Manager))
	if err != nil {
		p.Fail(fmt.Errorf("manager: %w", err))
		return m, nil
	}
	if manager.repo.Name != repoName {
		p.Fail(fmt.Errorf("manager %s is not in repo %s; a feature lives in one repo", req.Manager, repoName))
		return m, nil
	}

	if err := m.state.NewFeature(req.Feature, wsKey(manager.repo.Name, manager.view.WT.Name), req.Notes); err != nil {
		p.Fail(err)
		return m, nil
	}
	p.Reply(ctl.Response{OK: true, Message: fmt.Sprintf("created feature %s, managed by %s", req.Feature, m.ctlShownKey(manager))})
	return m, m.writeStateCmd()
}

func (m Model) ctlFeatureAdd(p *ctl.Pending) (tea.Model, tea.Cmd) {
	req := p.Req
	repoName, _, err := m.ctlFeatureKey(req.Feature)
	if err != nil {
		p.Fail(err)
		return m, nil
	}
	it, err := m.ctlLookup(req.Worktree)
	if err != nil {
		p.Fail(err)
		return m, nil
	}
	if it.repo.Name != repoName {
		p.Fail(fmt.Errorf("%s is not in repo %s; a feature lives in one repo", m.ctlShownKey(it), repoName))
		return m, nil
	}

	if err := m.state.AddToFeature(req.Feature, req.Worktree); err != nil {
		p.Fail(err)
		return m, nil
	}
	p.Reply(ctl.Response{OK: true, Message: fmt.Sprintf("added %s to feature %s", m.ctlShownKey(it), req.Feature)})
	return m, m.writeStateCmd()
}

func (m Model) ctlFeatureDrop(p *ctl.Pending) (tea.Model, tea.Cmd) {
	if err := m.state.DropFeature(p.Req.Feature); err != nil {
		p.Fail(err)
		return m, nil
	}
	p.Reply(ctl.Response{OK: true, Message: "dropped feature " + p.Req.Feature + "; its worktrees are unchanged"})
	return m, m.writeStateCmd()
}

// ctlFeatures lists every feature. Members whose worktree was removed are left
// out, and a removed manager shows as none.
func (m Model) ctlFeatures() []ctl.FeatureInfo {
	keys := make([]string, 0, len(m.state.Features))
	for key := range m.state.Features {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make([]ctl.FeatureInfo, 0, len(keys))
	for _, key := range keys {
		f := m.state.Features[key]
		repoName, name, _ := strings.Cut(key, "/")
		info := ctl.FeatureInfo{Key: key, Repo: repoName, Name: name, Notes: f.Notes}

		var items []activeItem
		if it, err := m.ctlLookup(f.Manager); err == nil {
			info.Manager = m.ctlShownKey(it)
			items = append(items, it)
		}
		for _, w := range f.Worktrees {
			if it, err := m.ctlLookup(w); err == nil {
				info.Worktrees = append(info.Worktrees, m.ctlShownKey(it))
				items = append(items, it)
			}
		}
		for _, it := range items {
			pos, ok := m.ghStackPosition(it.repo.Name, it.view.WT.Branch)
			if ok && !slices.Contains(info.Stacks, pos.Name()) {
				info.Stacks = append(info.Stacks, pos.Name())
			}
		}
		out = append(out, info)
	}
	return out
}

// ctlSpawnFeature checks spawn's --feature before the worktree is made, and
// returns the feature's "repo/name". The name alone means repoName's feature.
func (m Model) ctlSpawnFeature(repoName, feature string) (string, error) {
	if feature == "" {
		return "", nil
	}
	if !strings.Contains(feature, "/") {
		feature = repoName + "/" + feature
	}

	if r, _, _ := strings.Cut(feature, "/"); r != repoName {
		return "", fmt.Errorf("feature %s is not in repo %s; a feature lives in one repo", feature, repoName)
	}
	if _, ok := m.state.Features[feature]; !ok {
		return "", fmt.Errorf("no feature %s (run `ct ctl feature list`)", feature)
	}
	return feature, nil
}

// ctlFeatureKey splits a feature's "repo/name" and checks that the repo exists.
func (m Model) ctlFeatureKey(key string) (repoName, name string, err error) {
	repoName, name, ok := strings.Cut(key, "/")
	if !ok || repoName == "" || name == "" {
		return "", "", fmt.Errorf("feature %q should be written as repo/name", key)
	}
	if !slices.ContainsFunc(m.groups, func(g Group) bool { return g.Repo.Name == repoName }) {
		return "", "", fmt.Errorf("no repo %q (run `ct ctl list`)", repoName)
	}
	return repoName, name, nil
}
