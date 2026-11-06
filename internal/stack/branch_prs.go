package stack

import (
	"errors"
	"os/exec"
)

// BranchPR is the newest pull request whose head is a given branch, as the gh
// stack popup shows it.
type BranchPR struct {
	Number int
	URL    string
	State  string // OPEN, CLOSED, MERGED
	Draft  bool
	Review string // reviewDecision: APPROVED, CHANGES_REQUESTED, REVIEW_REQUIRED or ""
	Checks Checks
}

// BranchPRs fetches the newest PR of each branch in one batched GraphQL query
// (more only past ghRefsPerQuery branches). A branch without a PR is left out of
// the map. It runs gh, so it must stay off the UI goroutine.
func BranchPRs(dir string, branches []string) (map[string]BranchPR, error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return nil, errors.New("gh CLI not found on PATH")
	}
	slug, err := resolveGHRepo(dir)
	if err != nil {
		return nil, err
	}

	refs := make([]ghRef, len(branches))
	for i, b := range branches {
		refs[i] = ghRef{Branch: b, Tracked: true}
	}
	prs, err := queryStackPRs(dir, slug, refs)
	if err != nil {
		return nil, errors.New(describeGHFailure(err))
	}
	return newestPerBranch(prs), nil
}

// newestPerBranch keeps the first PR of each head branch; queryStackPRs returns
// them newest first.
func newestPerBranch(prs []ghPR) map[string]BranchPR {
	out := map[string]BranchPR{}
	for _, p := range prs {
		if _, seen := out[p.HeadRefName]; seen {
			continue
		}
		out[p.HeadRefName] = BranchPR{
			Number: p.Number, URL: p.URL, State: p.State, Draft: p.IsDraft,
			Review: p.ReviewDecision, Checks: summarizeChecks(p.checks()),
		}
	}
	return out
}
