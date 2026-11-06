package stack

import "testing"

func TestNewestPerBranchKeepsTheFirstPROfEachBranch(t *testing.T) {
	prs := []ghPR{
		{Number: 12, HeadRefName: "b", State: "OPEN", IsDraft: true, ReviewDecision: "CHANGES_REQUESTED"},
		{Number: 11, HeadRefName: "a", State: "MERGED", ReviewDecision: "APPROVED"},
		{Number: 5, HeadRefName: "b", State: "CLOSED"},
	}

	got := newestPerBranch(prs)

	if len(got) != 2 {
		t.Fatalf("want one PR per branch, got %+v", got)
	}
	if b := got["b"]; b.Number != 12 || !b.Draft || b.Review != "CHANGES_REQUESTED" || b.Checks.Summary != "none" {
		t.Fatalf("b should be the newest PR, #12: %+v", b)
	}
	if a := got["a"]; a.Number != 11 || a.State != "MERGED" {
		t.Fatalf("a = %+v", a)
	}
}
