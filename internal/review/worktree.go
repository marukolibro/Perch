package review

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// Worktree is one linked work tree of a Git repository.
type Worktree struct {
	Path    string
	Branch  string
	Head    string
	Current bool
}

// Worktrees lists the non-bare work trees of the repository containing root,
// marking the one root belongs to as Current.
func Worktrees(ctx context.Context, root string) ([]Worktree, error) {
	top, err := git(ctx, root, readOnlyEnv, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("find work tree of %q: %w", root, err)
	}
	current := canonicalPath(strings.TrimRight(string(top), "\r\n"))
	output, err := git(ctx, root, readOnlyEnv, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return nil, fmt.Errorf("list worktrees of %q: %w", root, err)
	}
	return parseWorktrees(string(output), current), nil
}

// parseWorktrees reads the output of git worktree list --porcelain -z.
func parseWorktrees(output string, current string) []Worktree {
	var list []Worktree
	var entry Worktree
	hasEntry := false
	bare := false
	flush := func() {
		if hasEntry && !bare && entry.Path != "" {
			entry.Current = canonicalPath(entry.Path) == current
			list = append(list, entry)
		}
		entry = Worktree{}
		hasEntry = false
		bare = false
	}
	for field := range strings.SplitSeq(output, "\x00") {
		if field == "" {
			flush()
			continue
		}
		key, value, _ := strings.Cut(field, " ")
		switch key {
		case "worktree":
			if hasEntry {
				flush()
			}
			entry.Path = filepath.FromSlash(value)
			hasEntry = true
		case "HEAD":
			entry.Head = value[:min(len(value), shortHashLength)]
		case "branch":
			entry.Branch = strings.TrimPrefix(value, "refs/heads/")
		case "bare":
			bare = true
		}
	}
	flush()
	return list
}

func canonicalPath(p string) string {
	cleaned := filepath.Clean(p)
	if resolved, err := filepath.EvalSymlinks(cleaned); err == nil {
		return resolved
	}
	return cleaned
}
