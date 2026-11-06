package state

import (
	"strings"
	"testing"
)

func TestFeaturesRoundTrip(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	s := Load()
	if err := s.NewFeature("r/sch-755", "r/coord", "~/notae/sch-755"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddToFeature("r/sch-755", "r/a"); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	f := Load().Features["r/sch-755"]
	if f == nil || f.Manager != "r/coord" || f.Notes != "~/notae/sch-755" || len(f.Worktrees) != 1 || f.Worktrees[0] != "r/a" {
		t.Fatalf("feature after reload = %+v", f)
	}
}

func TestFeatureRules(t *testing.T) {
	s := &State{}
	if err := s.NewFeature("r/one", "r/coord", ""); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		err  error
		want string
	}{
		{"same name twice", s.NewFeature("r/one", "r/other", ""), "already exists"},
		{"a manager leads one feature", s.NewFeature("r/two", "r/coord", ""), "already belongs to feature r/one"},
		{"no such feature", s.AddToFeature("r/nope", "r/a"), "no feature"},
		{"drop a missing feature", s.DropFeature("r/nope"), "no feature"},
	}
	for _, tc := range cases {
		if tc.err == nil || !strings.Contains(tc.err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", tc.name, tc.err, tc.want)
		}
	}

	if err := s.AddToFeature("r/one", "r/a"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddToFeature("r/one", "r/a"); err != nil || len(s.Features["r/one"].Worktrees) != 1 {
		t.Fatalf("adding a member again should change nothing: %v %+v", err, s.Features["r/one"])
	}
	if err := s.NewFeature("r/two", "r/coord2", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.AddToFeature("r/two", "r/a"); err == nil || !strings.Contains(err.Error(), "already belongs to feature r/one") {
		t.Fatalf("a worktree is in one feature at most: %v", err)
	}

	if err := s.DropFeature("r/one"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.FeatureOf("r/a"); ok {
		t.Fatal("dropping the feature should free its worktrees")
	}
}
