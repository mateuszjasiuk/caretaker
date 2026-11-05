package main

import (
	"os"
	"strings"
	"testing"

	"github.com/KCaverly/caretaker/internal/ctl"
)

func TestParseCtlArgs(t *testing.T) {
	cases := []struct {
		args    []string
		want    ctl.Request
		json    bool
		wantErr bool
	}{
		{args: []string{"list", "--json"}, want: ctl.Request{Op: ctl.OpList}, json: true},
		{args: []string{"summary", "r/a", "-n", "5"}, want: ctl.Request{Op: ctl.OpSummary, Worktree: "r/a", Lines: 5}},
		{args: []string{"summary", "a"}, wantErr: true},
		{args: []string{"spawn", "r", "fix-it", "--prompt", "do it", "--base", "main", "--provider", "codex"},
			want: ctl.Request{Op: ctl.OpSpawn, Repo: "r", Name: "fix-it", Text: "do it", Base: "main", Provider: "codex"}},
		{args: []string{"spawn", "r", "fix-it", "do", "it"}, want: ctl.Request{Op: ctl.OpSpawn, Repo: "r", Name: "fix-it", Text: "do it"}},
		{args: []string{"spawn", "r"}, wantErr: true},
		{args: []string{"send", "r/a", "run", "the", "tests"}, want: ctl.Request{Op: ctl.OpSend, Worktree: "r/a", Text: "run the tests"}},
		{args: []string{"send", "r/a"}, wantErr: true},
		{args: []string{"stop", "r/a", "--force"}, want: ctl.Request{Op: ctl.OpStop, Worktree: "r/a", Force: true}},
		{args: []string{"open", "r/a"}, want: ctl.Request{Op: ctl.OpOpen, Worktree: "r/a"}},
		{args: []string{"reopen", "r/a", "--force"}, want: ctl.Request{Op: ctl.OpReopen, Worktree: "r/a", Force: true}},
		{args: []string{"interrupt", "r/a"}, want: ctl.Request{Op: ctl.OpInterrupt, Worktree: "r/a"}},
		{args: []string{"remove", "r/a"}, want: ctl.Request{Op: ctl.OpRemove, Worktree: "r/a"}},
		{args: []string{"remove", "r/a", "r/b"}, wantErr: true},
		{args: []string{"list", "--bogus"}, wantErr: true},
		{args: []string{"dance"}, wantErr: true},
	}
	for _, tc := range cases {
		got, asJSON, err := parseCtlArgs(tc.args)
		if tc.wantErr {
			if err == nil {
				t.Errorf("%v: expected an error, got %+v", tc.args, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("%v: %v", tc.args, err)
			continue
		}
		if got != tc.want || asJSON != tc.json {
			t.Errorf("%v: got %+v json=%v, want %+v json=%v", tc.args, got, asJSON, tc.want, tc.json)
		}
	}
}

// TestParseCtlArgsStdin checks that both "-" and "--prompt -" read the text
// from stdin, not take "-" as the text.
func TestParseCtlArgsStdin(t *testing.T) {
	for _, args := range [][]string{
		{"spawn", "r", "fix-it", "--prompt", "-"},
		{"spawn", "r", "fix-it", "-"},
	} {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.WriteString("review the diff\n")
		w.Close()
		old := os.Stdin
		os.Stdin = r
		got, _, err := parseCtlArgs(args)
		os.Stdin = old
		r.Close()
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if got.Text != "review the diff" {
			t.Errorf("%v: text = %q, want %q", args, got.Text, "review the diff")
		}
	}
}

func TestFormatWorktree(t *testing.T) {
	wt := ctl.WorktreeInfo{
		Key: "r/a", Name: "a", Branch: "a", Open: true, Base: "main", Ahead: 2, Dirty: true, Added: 3, Deleted: 1,
		Agents: []ctl.AgentInfo{{Provider: "claude", Label: "calm-fox", Status: "waiting", WaitingFor: "permission prompt"}},
		Queued: 1, Subject: "Add thing",
	}
	got := formatWorktree(wt)
	want := `r/a  open  claude:calm-fox=waiting(permission prompt)  queued=1  ahead=2 behind=0 (main)  dirty(+3/-1 lines)  "Add thing"`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestFormatWorktreeShowsGhStack(t *testing.T) {
	got := formatWorktree(ctl.WorktreeInfo{Key: "r/a", Stack: 7, StackIndex: 4, StackTotal: 5, PR: 11})
	if !strings.Contains(got, "stack=#7 4/5  pr=#11") {
		t.Errorf("submitted: %q", got)
	}

	got = formatWorktree(ctl.WorktreeInfo{Key: "r/a", StackIndex: 4, StackTotal: 5})
	if !strings.Contains(got, "stack=4/5") || strings.Contains(got, "pr=") {
		t.Errorf("not submitted: %q", got)
	}

	got = formatWorktree(ctl.WorktreeInfo{Key: "r/a"})
	if strings.Contains(got, "stack=") {
		t.Errorf("outside a stack: %q", got)
	}
}
