package discover

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ExploredRepo is a repo found during deep scan.
type ExploredRepo struct {
	Name     string
	Path     string
	Nested   bool
	ParentDir string // the configured source dir it was found under
}

// Explore does a deep scan (up to depth 3) of configured directories.
func Explore(dirs []string) []ExploredRepo {
	var results []ExploredRepo

	for _, dir := range dirs {
		repos := deepScan(dir, dir, 0, 3)
		results = append(results, repos...)
	}

	return results
}

func deepScan(rootDir, currentDir string, depth, maxDepth int) []ExploredRepo {
	if depth > maxDepth {
		return nil
	}

	entries, err := os.ReadDir(currentDir)
	if err != nil {
		return nil
	}

	var results []ExploredRepo

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == "." || name == ".." || name == ".git" || name == "node_modules" || name == "__pycache__" {
			continue
		}

		path := filepath.Join(currentDir, name)

		// Check if this is a git repo
		gitDir := filepath.Join(path, ".git")
		if info, err := os.Stat(gitDir); err == nil && info.IsDir() {
			nested := depth > 0
			results = append(results, ExploredRepo{
				Name:      name,
				Path:      path,
				Nested:    nested,
				ParentDir: rootDir,
			})
			// Still scan inside for nested repos
			nested_repos := deepScan(rootDir, path, depth+1, maxDepth)
			for i := range nested_repos {
				nested_repos[i].Nested = true
			}
			results = append(results, nested_repos...)
			continue
		}

		// Not a repo — recurse
		results = append(results, deepScan(rootDir, path, depth+1, maxDepth)...)
	}

	return results
}

// PrintExploreResults prints grouped explore output.
func PrintExploreResults(repos []ExploredRepo) {
	if len(repos) == 0 {
		fmt.Fprintf(os.Stderr, "No repos found.\n")
		return
	}

	// Group by parent dir
	groups := make(map[string][]ExploredRepo)
	var dirs []string
	for _, r := range repos {
		if _, ok := groups[r.ParentDir]; !ok {
			dirs = append(dirs, r.ParentDir)
		}
		groups[r.ParentDir] = append(groups[r.ParentDir], r)
	}
	sort.Strings(dirs)

	total := 0
	nested := 0

	for _, dir := range dirs {
		fmt.Fprintf(os.Stderr, "\033[1m%s\033[0m\n", dir)

		dirRepos := groups[dir]
		sort.Slice(dirRepos, func(i, j int) bool {
			return dirRepos[i].Path < dirRepos[j].Path
		})

		for _, r := range dirRepos {
			total++
			relPath := r.Path
			if rel, err := filepath.Rel(dir, r.Path); err == nil {
				relPath = rel
			}

			if r.Nested {
				nested++
				fmt.Fprintf(os.Stderr, "  ★ %-30s %s  (nested)\n", r.Name, relPath)
			} else {
				fmt.Fprintf(os.Stderr, "    %-30s %s\n", r.Name, relPath)
			}
		}
		fmt.Fprintln(os.Stderr)
	}

	suffix := ""
	if nested > 0 {
		suffix = fmt.Sprintf(" (%d nested)", nested)
	}
	fmt.Fprintf(os.Stderr, "%d repos found%s\n", total, suffix)
}

// RepoNameFromPath extracts a display name from a path.
func RepoNameFromPath(path string) string {
	return filepath.Base(strings.TrimRight(path, "/"))
}
