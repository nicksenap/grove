package discover

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/gitops"
)

// RepoInfo represents a discovered repository with identity and location.
type RepoInfo struct {
	Name        string // folder name
	Path        string // absolute path
	Remote      string // origin URL (may be empty)
	DisplayName string // "owner/repo" from remote, or folder name
}

// RemoteCachePath returns the path to the on-disk remote URL cache.
func RemoteCachePath() string {
	return filepath.Join(config.GroveDir, "cache", "remotes.json")
}

// DiscoverReposWithCache is the production entry point — loads cache from disk,
// uses gitops.RemoteURL for cache misses, saves cache after resolution.
func DiscoverReposWithCache(dirs []string) []RepoInfo {
	cachePath := RemoteCachePath()
	cache := LoadRemoteCache(cachePath)

	fetcher := func(path string) string {
		return gitops.RemoteURL(path, "origin")
	}

	results := DiscoverReposWithRemoteCache(dirs, cache, fetcher)

	// Save updated cache (best-effort)
	_ = SaveRemoteCache(cachePath, cache)

	return results
}

// repoCandidate is a repo path paired with the configured dir it was found under.
type repoCandidate struct {
	path string
	root string
}

// DiscoverRepos deep-scans directories for git repos, deduped by remote URL.
// The fetcher function resolves a repo path to its remote URL (or "" if none).
// When multiple paths share the same remote, direct children of configured dirs win.
// Results are sorted by display name.
// Pass nil for cache to skip caching; pass a non-nil map to use/populate it.
func DiscoverRepos(dirs []string, fetcher func(string) string) []RepoInfo {
	return DiscoverReposWithRemoteCache(dirs, nil, fetcher)
}

// DiscoverReposWithRemoteCache is like DiscoverRepos but uses the provided cache.
func DiscoverReposWithRemoteCache(dirs []string, cache map[string]CacheEntry, fetcher func(string) string) []RepoInfo {
	// Phase 1: filesystem scan
	var candidates []repoCandidate
	seen := make(map[string]bool)

	for _, dir := range dirs {
		collectRepoPaths(dir, dir, 0, 3, seen, &candidates)
	}

	if len(candidates) == 0 {
		return nil
	}

	// Phase 2: batch-resolve remote URLs (cached + parallel)
	paths := make([]string, len(candidates))
	for i, c := range candidates {
		paths[i] = c.path
	}

	if cache == nil {
		cache = map[string]CacheEntry{}
	}
	results := BatchResolveRemotes(paths, cache, fetcher, defaultNow, GitConfigMtime)

	// Phase 3: dedup and build RepoInfo
	type dedupEntry struct {
		info RepoInfo
		root string
	}
	seenRemotes := make(map[string]dedupEntry)

	for _, c := range candidates {
		url := results[c.path]
		name := filepath.Base(c.path)
		display := displayNameFromURL(url, name)

		info := RepoInfo{
			Name:        name,
			Path:        c.path,
			Remote:      url,
			DisplayName: display,
		}

		if url != "" {
			if existing, ok := seenRemotes[url]; ok {
				// Direct child of configured dir wins over nested
				if existing.root != filepath.Dir(existing.info.Path) &&
					c.root == filepath.Dir(c.path) {
					seenRemotes[url] = dedupEntry{info: info, root: c.root}
				}
			} else {
				seenRemotes[url] = dedupEntry{info: info, root: c.root}
			}
		} else {
			// No remote — use resolved path as dedup key
			resolved, err := filepath.EvalSymlinks(c.path)
			if err != nil {
				resolved = c.path
			}
			key := resolved
			if _, ok := seenRemotes[key]; !ok {
				seenRemotes[key] = dedupEntry{info: info, root: c.root}
			}
		}
	}

	infos := make([]RepoInfo, 0, len(seenRemotes))
	for _, e := range seenRemotes {
		infos = append(infos, e.info)
	}
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].DisplayName < infos[j].DisplayName
	})
	return infos
}

func collectRepoPaths(rootDir, currentDir string, depth, maxDepth int, seen map[string]bool, out *[]repoCandidate) {
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
			resolvedPath, err := filepath.EvalSymlinks(path)
			if err != nil {
				resolvedPath = path
			}
			if !seen[resolvedPath] {
				seen[resolvedPath] = true
				*out = append(*out, repoCandidate{path: path, root: rootDir})
			}
			// Don't descend into repos
		} else {
			collectRepoPaths(rootDir, path, depth+1, maxDepth, seen, out)
		}
	}
}

// displayNameFromURL derives "owner/repo" from a remote URL, falling back to the given name.
func displayNameFromURL(url, fallback string) string {
	if url != "" {
		parsed := gitops.ParseRemoteName(url)
		if parsed != "" {
			return parsed
		}
	}
	return fallback
}
