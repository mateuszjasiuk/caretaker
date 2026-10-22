package repo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(b)
}

func TestApplyWorktreeFiles(t *testing.T) {
	src := t.TempDir()
	dst := filepath.Join(src, ".worktrees", "feature")
	writeFile(t, filepath.Join(src, ".env"), "SECRET=1")
	writeFile(t, filepath.Join(src, ".env.local"), "LOCAL=1")
	writeFile(t, filepath.Join(src, "apps", "web", ".env"), "WEB=1")
	writeFile(t, filepath.Join(src, "config", "dev.json"), "{}")
	writeFile(t, filepath.Join(src, "node_modules", "pkg", "index.js"), "x")
	writeFile(t, filepath.Join(src, ".git", "HEAD"), "ref: refs/heads/main")
	// The checkout already has these: a tracked file and git's worktree pointer.
	writeFile(t, filepath.Join(dst, "README"), "tracked")
	writeFile(t, filepath.Join(dst, ".git"), "gitdir: x")
	writeFile(t, filepath.Join(src, "README"), "main copy")

	errs := ApplyWorktreeFiles(src, dst,
		[]string{".*", "apps/*/.env", "config", "README", "missing*"},
		[]string{"node_modules"})
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	for rel, want := range map[string]string{
		".env":                      "SECRET=1",
		".env.local":                "LOCAL=1",
		"apps/web/.env":             "WEB=1",
		"config/dev.json":           "{}",
		"README":                    "tracked",
		".git":                      "gitdir: x",
		"node_modules/pkg/index.js": "x",
	} {
		if got := readFile(t, filepath.Join(dst, rel)); got != want {
			t.Errorf("%s = %q, want %q", rel, got, want)
		}
	}

	// A copy is independent of the main checkout.
	if info, err := os.Lstat(filepath.Join(dst, ".env")); err != nil || info.Mode()&os.ModeSymlink != 0 {
		t.Errorf(".env should be a real file, got %v, %v", info, err)
	}
	writeFile(t, filepath.Join(dst, ".env"), "CHANGED")
	if got := readFile(t, filepath.Join(src, ".env")); got != "SECRET=1" {
		t.Errorf("editing the copy changed the source: %q", got)
	}

	// A link points back at the main checkout.
	target, err := os.Readlink(filepath.Join(dst, "node_modules"))
	if err != nil || target != filepath.Join(src, "node_modules") {
		t.Errorf("node_modules link = %q, %v", target, err)
	}

	// ".*" matches .worktrees, which holds dst: it must not be copied into itself.
	if _, err := os.Stat(filepath.Join(dst, ".worktrees")); !os.IsNotExist(err) {
		t.Errorf(".worktrees was copied into the worktree: %v", err)
	}
}

func TestApplyWorktreeFilesReportsFailures(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	writeFile(t, filepath.Join(src, ".env"), "x")
	writeFile(t, filepath.Join(src, "ok"), "x")
	// A file where a directory is needed makes the parent unusable.
	writeFile(t, filepath.Join(dst, "apps"), "not a dir")
	writeFile(t, filepath.Join(src, "apps", "web", ".env"), "x")

	errs := ApplyWorktreeFiles(src, dst, []string{"apps/*/.env", "ok"}, nil)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), filepath.Join("apps", "web", ".env")) {
		t.Fatalf("want one error naming apps/web/.env, got %v", errs)
	}
	if got := readFile(t, filepath.Join(dst, "ok")); got != "x" {
		t.Errorf("later entries should still be applied, ok = %q", got)
	}
}
