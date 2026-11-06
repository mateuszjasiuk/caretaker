package ghstack

import (
	"os"
	"path/filepath"
	"testing"
)

// writeState stores content as a repo's gh stack state file.
func writeState(t *testing.T, content string) string {
	t.Helper()
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath(repo), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return repo
}

// The shape comes from a real state file written by gh-stack 0.3.0.
const realShape = `{
  "schemaVersion": 1,
  "repository": "owner/repo",
  "stacks": [{
    "id": "2977688",
    "number": 1369,
    "trunk": {"branch": "main", "head": "051e"},
    "branches": [
      {"branch": "a", "head": "dda8", "base": "051e", "pullRequest": {"number": 1171, "id": "PR_1", "url": "https://x/1171"}},
      {"branch": "b", "head": "3063", "base": "dda8"}
    ]
  }]
}`

func TestReadPositions(t *testing.T) {
	stacks := Read(writeState(t, realShape))

	if got := stacks["a"]; got != (Position{Stack: 1369, Index: 1, Total: 2, PR: 1171, Bottom: "a"}) {
		t.Errorf("a = %+v", got)
	}
	if got := stacks["b"]; got != (Position{Stack: 1369, Index: 2, Total: 2, Bottom: "a"}) {
		t.Errorf("b = %+v", got)
	}
	if _, ok := stacks["main"]; ok {
		t.Error("the trunk is not part of the stack")
	}
}

func TestReadTreatsBadStateAsNoStacks(t *testing.T) {
	cases := map[string]string{
		"broken JSON":   `{"schemaVersion": 1, "stacks": [`,
		"newer version": `{"schemaVersion": 2, "stacks": [{"number": 1, "branches": [{"branch": "a"}]}]}`,
		"no version":    `{"stacks": [{"number": 1, "branches": [{"branch": "a"}]}]}`,
	}
	for name, content := range cases {
		if got := Read(writeState(t, content)); len(got) != 0 {
			t.Errorf("%s: got %+v", name, got)
		}
	}

	if got := Read(t.TempDir()); len(got) != 0 {
		t.Errorf("no file: got %+v", got)
	}
	if !ModTime(t.TempDir()).IsZero() {
		t.Error("no file should have a zero ModTime")
	}
}

func TestLabel(t *testing.T) {
	if got := (Position{Index: 4, Total: 5, PR: 1366}).Label(); got != "4/5 #1366" {
		t.Errorf("with a PR: %q", got)
	}
	if got := (Position{Index: 4, Total: 5}).Label(); got != "4/5" {
		t.Errorf("without a PR: %q", got)
	}
}
