package discover

import (
	"os"
	"path/filepath"
	"strings"
)

// RepoVisitFunc is called for each git repo found during a walk.
// path is the repo directory, rootDir is the configured source dir it was found under,
// depth is the directory nesting level (0 = direct child of rootDir),
// insideRepo is true if this repo is nested inside another repo.
// Return true to descend into the repo looking for further nested repos.
type RepoVisitFunc func(path, rootDir string, depth int, insideRepo bool) (descend bool)

// WalkRepos walks configured directories up to maxDepth looking for git repos.
// For each repo found, fn is called. Symlink loops are detected and skipped.
func WalkRepos(dirs []string, maxDepth int, fn RepoVisitFunc) {
	seen := make(map[string]bool)
	for _, dir := range dirs {
		walkReposRec(dir, dir, 0, maxDepth, false, seen, fn)
	}
}

func walkReposRec(rootDir, currentDir string, depth, maxDepth int, insideRepo bool, seen map[string]bool, fn RepoVisitFunc) {
	if depth > maxDepth {
		return
	}

	resolved, err := filepath.EvalSymlinks(currentDir)
	if err != nil {
		resolved = currentDir
	}
	if seen[resolved] {
		return
	}
	seen[resolved] = true

	entries, err := os.ReadDir(currentDir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") || name == "node_modules" || name == "__pycache__" {
			continue
		}

		path := filepath.Join(currentDir, name)
		gitDir := filepath.Join(path, ".git")
		if info, err := os.Stat(gitDir); err == nil && info.IsDir() {
			if descend := fn(path, rootDir, depth, insideRepo); descend {
				walkReposRec(rootDir, path, depth+1, maxDepth, true, seen, fn)
			}
		} else {
			walkReposRec(rootDir, path, depth+1, maxDepth, insideRepo, seen, fn)
		}
	}
}
