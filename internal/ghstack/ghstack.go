// Package ghstack reads the local state of the gh stack extension
// (github/gh-stack), where each branch is one PR in a stack. gh stack keeps this
// state in one file shared by all of a repo's worktrees, so reading it needs no
// subprocess and no network. Nothing here changes a stack.
//
// This is a different model from package stack, which drives `ct stack`: there,
// each commit on one branch is a PR.
package ghstack

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// schemaVersion is the state file format this package understands. gh stack is
// young, so any other version reads as no stacks rather than as wrong ones.
const schemaVersion = 1

// Position is where a branch sits in its stack.
type Position struct {
	// Stack is the stack's number on GitHub, 0 until it has been submitted.
	Stack int
	// Index is the branch's place, counted from 1 at the bottom of the stack.
	Index int
	Total int
	// PR is the branch's pull request number, 0 until it has one.
	PR int
	// Bottom is the stack's first branch. It tells stacks apart before they have
	// a number.
	Bottom string
}

// Name is how the stack is shown: "#1369", or its first branch before it has
// been submitted.
func (p Position) Name() string {
	if p.Stack == 0 {
		return p.Bottom
	}
	return fmt.Sprintf("#%d", p.Stack)
}

// Label is the position as the bar shows it: "4/5 #1366", or "4/5" without a PR.
func (p Position) Label() string {
	if p.PR == 0 {
		return fmt.Sprintf("%d/%d", p.Index, p.Total)
	}
	return fmt.Sprintf("%d/%d #%d", p.Index, p.Total, p.PR)
}

// Stacks maps a branch name to its position.
type Stacks map[string]Position

// ModTime is when the repo's state file last changed, zero when there is none.
func ModTime(repoPath string) time.Time {
	info, err := os.Stat(statePath(repoPath))
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// Read loads the repo's stacks. A missing, broken or newer state file reads as
// no stacks: showing nothing is the right result for all three.
func Read(repoPath string) Stacks {
	data, err := os.ReadFile(statePath(repoPath))
	if err != nil {
		return nil
	}

	stacks, err := parse(data)
	if err != nil {
		return nil
	}
	return stacks
}

func statePath(repoPath string) string {
	return filepath.Join(repoPath, ".git", "gh-stack")
}

func parse(data []byte) (Stacks, error) {
	var file struct {
		SchemaVersion int `json:"schemaVersion"`
		Stacks        []struct {
			Number   int `json:"number"`
			Branches []struct {
				Branch      string `json:"branch"`
				PullRequest *struct {
					Number int `json:"number"`
				} `json:"pullRequest"`
			} `json:"branches"`
		} `json:"stacks"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, err
	}
	if file.SchemaVersion != schemaVersion {
		return nil, fmt.Errorf("unknown gh-stack schema version %d", file.SchemaVersion)
	}

	stacks := Stacks{}
	for _, s := range file.Stacks {
		for i, b := range s.Branches {
			pos := Position{Stack: s.Number, Index: i + 1, Total: len(s.Branches), Bottom: s.Branches[0].Branch}
			if b.PullRequest != nil {
				pos.PR = b.PullRequest.Number
			}
			stacks[b.Branch] = pos
		}
	}
	return stacks, nil
}
