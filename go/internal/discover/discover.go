package discover

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Repo represents a discovered git repository.
type Repo struct {
	Name string
	Path string
}

// FindRepos scans directories for git repositories (depth 1).
func FindRepos(dirs []string) []Repo {
	seen := make(map[string]bool)
	var repos []Repo

	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			name := entry.Name()
			if strings.HasPrefix(name, ".") {
				continue
			}
			repoPath := filepath.Join(dir, name)
			gitDir := filepath.Join(repoPath, ".git")
			if info, err := os.Stat(gitDir); err == nil && info.IsDir() && !seen[name] {
				seen[name] = true
				repos = append(repos, Repo{Name: name, Path: repoPath})
			}
		}
	}

	sort.Slice(repos, func(i, j int) bool {
		return repos[i].Name < repos[j].Name
	})
	return repos
}

// FindAllRepos scans all configured directories and returns repos.
func FindAllRepos(dirs []string) []Repo {
	return FindRepos(dirs)
}

// RepoMap returns a name→path mapping.
func RepoMap(repos []Repo) map[string]string {
	m := make(map[string]string, len(repos))
	for _, r := range repos {
		m[r.Name] = r.Path
	}
	return m
}
