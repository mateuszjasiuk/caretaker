package tui

import (
	"strings"
	"testing"

	"github.com/KCaverly/caretaker/internal/config"
	"github.com/KCaverly/caretaker/internal/ctl"
	"github.com/KCaverly/caretaker/internal/repo"
)

// ctlDo runs one request against m and returns the updated model and the reply.
func ctlDo(t *testing.T, m Model, req ctl.Request) (Model, ctl.Response) {
	t.Helper()
	p, ch := pending(req)
	mm, _ := m.handleCtl(p)
	return mm.(Model), <-ch
}

// featureModel is ctlModel with feature r/f, managed from the main checkout,
// and worktree a in it. a is also the bottom of gh stack #7.
func featureModel(t *testing.T) Model {
	t.Helper()
	m, _ := ghStackModel(t)
	m, resp := ctlDo(t, m, ctl.Request{Op: ctl.OpFeatureNew, Feature: "r/f", Manager: "r/main", Notes: "~/notae/f"})
	if !resp.OK {
		t.Fatalf("feature new: %+v", resp)
	}
	m, resp = ctlDo(t, m, ctl.Request{Op: ctl.OpFeatureAdd, Feature: "r/f", Worktree: "r/a"})
	if !resp.OK {
		t.Fatalf("feature add: %+v", resp)
	}
	return m
}

func TestCtlFeatureListShowsManagerMembersAndStacks(t *testing.T) {
	m := featureModel(t)

	_, resp := ctlDo(t, m, ctl.Request{Op: ctl.OpFeatureList})
	if len(resp.Features) != 1 {
		t.Fatalf("features = %+v", resp.Features)
	}
	f := resp.Features[0]
	if f.Key != "r/f" || f.Manager != "r/main" || f.Notes != "~/notae/f" ||
		len(f.Worktrees) != 1 || f.Worktrees[0] != "r/a" || len(f.Stacks) != 1 || f.Stacks[0] != "#7" {
		t.Fatalf("feature = %+v", f)
	}
}

func TestCtlListMarksAndFiltersByFeature(t *testing.T) {
	m := featureModel(t)

	_, resp := ctlDo(t, m, ctl.Request{Op: ctl.OpList, Feature: "r/f"})
	if len(resp.Worktrees) != 2 {
		t.Fatalf("want the manager and a, got %+v", resp.Worktrees)
	}
	main, a := resp.Worktrees[0], resp.Worktrees[1]
	if main.Key != "r/main" || main.Feature != "r/f" || !main.Manager {
		t.Fatalf("main = %+v", main)
	}
	if a.Key != "r/a" || a.Feature != "r/f" || a.Manager {
		t.Fatalf("a = %+v", a)
	}

	if _, resp = ctlDo(t, m, ctl.Request{Op: ctl.OpList, Feature: "r/nope"}); resp.OK {
		t.Fatalf("an unknown feature should fail: %+v", resp)
	}
}

func TestCtlFeatureHidesRemovedWorktrees(t *testing.T) {
	m := featureModel(t)
	m.groups[0].Worktrees = m.groups[0].Worktrees[2:] // only b is left
	m.recomputeActive()

	_, resp := ctlDo(t, m, ctl.Request{Op: ctl.OpFeatureList})
	if f := resp.Features[0]; f.Manager != "" || len(f.Worktrees) != 0 {
		t.Fatalf("removed worktrees should not be listed: %+v", f)
	}
}

func TestCtlFeatureRefusals(t *testing.T) {
	m := featureModel(t)
	m.groups = append(m.groups, Group{
		Repo:      repo.Repo{Name: "other", Path: t.TempDir()},
		Worktrees: []WorktreeView{{WT: repo.Worktree{Repo: "other", Name: "x", Path: t.TempDir(), Branch: "x"}}},
	})
	m.recomputeActive()

	cases := []struct {
		name string
		req  ctl.Request
		want string
	}{
		{"name taken", ctl.Request{Op: ctl.OpFeatureNew, Feature: "r/f", Manager: "r/b"}, "already exists"},
		{"manager missing", ctl.Request{Op: ctl.OpFeatureNew, Feature: "r/g", Manager: "r/zzz"}, "no worktree"},
		{"manager in another repo", ctl.Request{Op: ctl.OpFeatureNew, Feature: "r/g", Manager: "other/x"}, "one repo"},
		{"repo missing", ctl.Request{Op: ctl.OpFeatureNew, Feature: "zzz/g", Manager: "r/b"}, "no repo"},
		{"bad name", ctl.Request{Op: ctl.OpFeatureNew, Feature: "r/..", Manager: "r/b"}, "invalid feature name"},
		{"member in another repo", ctl.Request{Op: ctl.OpFeatureAdd, Feature: "r/f", Worktree: "other/x"}, "one repo"},
		{"spawn into a missing feature", ctl.Request{Op: ctl.OpSpawn, Repo: "r", Name: "c", Feature: "g", Text: "x"}, "no feature r/g"},
		{"spawn into another repo's feature", ctl.Request{Op: ctl.OpSpawn, Repo: "other", Name: "c", Feature: "r/f", Text: "x"}, "one repo"},
		{"drop a missing feature", ctl.Request{Op: ctl.OpFeatureDrop, Feature: "r/g"}, "no feature"},
	}
	for _, tc := range cases {
		if _, resp := ctlDo(t, m, tc.req); resp.OK || !strings.Contains(resp.Error, tc.want) {
			t.Errorf("%s: resp %+v, want error with %q", tc.name, resp, tc.want)
		}
	}
	if len(m.creating) != 0 {
		t.Fatalf("a refused spawn must not start creating a worktree: %+v", m.creating)
	}
}

func TestCtlSpawnedWorktreeJoinsTheFeature(t *testing.T) {
	m := featureModel(t)
	m.ctrl = &Controller{cfg: config.Config{Editor: "cat", Agent: "cat", Shell: "sh", Keys: config.Default().Keys}}
	t.Cleanup(m.mgr.CloseAll)

	p, ch := pending(ctl.Request{Op: ctl.OpSpawn, Repo: "r", Name: "c", Feature: "f"})
	wt := repo.Worktree{Repo: "r", Name: "c", Path: t.TempDir(), Branch: "c"}
	mm, _ := m.handleCtlSpawned(ctlSpawnedMsg{p: p, repo: m.groups[0].Repo, name: "c", wt: wt, provider: m.defaultAgentProvider, feature: "r/f"})
	m = mm.(Model)

	if resp := <-ch; !resp.OK || !strings.Contains(resp.Message, "part of feature r/f") {
		t.Fatalf("resp %+v", resp)
	}
	if got, ok := m.state.FeatureOf("r/c"); !ok || got != "r/f" {
		t.Fatalf("c should be in feature r/f, got %q", got)
	}
}

func TestCtlFeatureDropKeepsWorktrees(t *testing.T) {
	m := featureModel(t)
	m, resp := ctlDo(t, m, ctl.Request{Op: ctl.OpFeatureDrop, Feature: "r/f"})
	if !resp.OK {
		t.Fatalf("drop: %+v", resp)
	}
	if _, resp = ctlDo(t, m, ctl.Request{Op: ctl.OpList}); len(resp.Worktrees) != 3 || resp.Worktrees[1].Feature != "" {
		t.Fatalf("worktrees should stay, without a feature: %+v", resp.Worktrees)
	}
}
