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
	Name      string
	Path      string
	Nested    bool
	ParentDir string // the configured source dir it was found under
}

// Explore does a deep scan (up to depth 3) of configured directories.
func Explore(dirs []string) []ExploredRepo {
	var results []ExploredRepo

	WalkRepos(dirs, 3, func(path, rootDir string, depth int, insideRepo bool) bool {
		results = append(results, ExploredRepo{
			Name:      filepath.Base(path),
			Path:      path,
			Nested:    depth > 0 || insideRepo,
			ParentDir: rootDir,
		})
		return true // descend into repos to find nested repos
	})

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
