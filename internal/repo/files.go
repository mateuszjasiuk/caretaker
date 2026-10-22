package repo

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ApplyWorktreeFiles brings files from a repo's main checkout (src) into a new
// worktree (dst). Each entry in copies and links is a filepath.Glob pattern
// relative to src; a pattern that matches nothing is not an error. A path that
// already exists in dst, such as a tracked file, is left alone. It keeps going
// after a failure and returns one error per path that could not be brought over.
func ApplyWorktreeFiles(src, dst string, copies, links []string) []error {
	var errs []error
	apply := func(patterns []string, do func(from, to string) error) {
		for _, pattern := range patterns {
			matches, err := filepath.Glob(filepath.Join(src, pattern))
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", pattern, err))
				continue
			}
			for _, from := range matches {
				rel, err := filepath.Rel(src, from)
				if err != nil {
					errs = append(errs, err)
					continue
				}
				if skipWorktreeFile(rel, from, dst) {
					continue
				}
				to := filepath.Join(dst, rel)
				if _, err := os.Lstat(to); err == nil {
					continue
				}
				if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
					errs = append(errs, fmt.Errorf("%s: %w", rel, err))
					continue
				}
				if err := do(from, to); err != nil {
					errs = append(errs, fmt.Errorf("%s: %w", rel, err))
				}
			}
		}
	}
	apply(copies, copyPath)
	apply(links, os.Symlink)
	return errs
}

// skipWorktreeFile reports matches a broad pattern like ".*" can reach but
// that must never be copied: git's own metadata, and any folder that holds
// the new worktree (".worktrees"), which would otherwise be copied into itself.
func skipWorktreeFile(rel, from, dst string) bool {
	if rel == ".git" || strings.HasPrefix(rel, ".git"+string(filepath.Separator)) {
		return true
	}
	inside, err := filepath.Rel(from, dst)
	return err == nil && inside != ".." && !strings.HasPrefix(inside, ".."+string(filepath.Separator))
}

// copyPath copies a file or a whole directory. On macOS it first tries an APFS
// clone (cp -c), which is near-instant even for node_modules and uses no extra
// disk space until a file changes; other file systems fall back to a full copy.
func copyPath(from, to string) error {
	if runtime.GOOS == "darwin" {
		if err := runCp("-c", "-pR", from, to); err == nil {
			return nil
		}
		// A failed clone can leave a partial copy behind.
		os.RemoveAll(to)
	}
	return runCp("-pR", from, to)
}

func runCp(args ...string) error {
	out, err := exec.Command("cp", args...).CombinedOutput()
	if err != nil {
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return fmt.Errorf("%s", msg)
		}
		return err
	}
	return nil
}
