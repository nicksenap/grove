// Package discover handles repository discovery across configured directories.
package discover

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/git"
	"github.com/nicksenap/grove/internal/models"
)

const (
	remoteCacheTTL = 24 * time.Hour
	maxWorkers     = 16
)

// remoteCacheEntry is a single cached remote URL result.
type remoteCacheEntry struct {
	Remote string `json:"remote"`
	Mtime  int64  `json:"mtime"`
}

// remoteCacheFile is the on-disk format for ~/.grove/cache/remotes.json.
type remoteCacheFile struct {
	Entries   map[string]remoteCacheEntry `json:"entries"`
	UpdatedAt int64                       `json:"updated_at"`
}

func remoteCachePath() string {
	return filepath.Join(config.GroveDir(), "cache", "remotes.json")
}

func loadRemoteCache() *remoteCacheFile {
	data, err := os.ReadFile(remoteCachePath())
	if err != nil {
		return &remoteCacheFile{Entries: make(map[string]remoteCacheEntry)}
	}
	var cache remoteCacheFile
	if err := json.Unmarshal(data, &cache); err != nil {
		return &remoteCacheFile{Entries: make(map[string]remoteCacheEntry)}
	}
	if cache.Entries == nil {
		cache.Entries = make(map[string]remoteCacheEntry)
	}

	// Check TTL
	if time.Since(time.Unix(cache.UpdatedAt, 0)) > remoteCacheTTL {
		return &remoteCacheFile{Entries: make(map[string]remoteCacheEntry)}
	}
	return &cache
}

func saveRemoteCache(cache *remoteCacheFile) {
	cache.UpdatedAt = time.Now().Unix()
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return
	}
	dir := filepath.Dir(remoteCachePath())
	os.MkdirAll(dir, 0o755)
	os.WriteFile(remoteCachePath(), data, 0o644)
}

// FindRepos discovers repos one level deep in a directory.
func FindRepos(repoDir string) map[string]string {
	repos := make(map[string]string)
	entries, err := os.ReadDir(repoDir)
	if err != nil {
		return repos
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		full := filepath.Join(repoDir, entry.Name())
		if git.IsGitRepo(full) {
			repos[entry.Name()] = full
		}
	}
	return repos
}

// FindAllRepos discovers repos across all configured directories, deduplicating by name.
func FindAllRepos(repoDirs []string) map[string]string {
	repos := make(map[string]string)
	for _, dir := range repoDirs {
		for name, path := range FindRepos(dir) {
			if _, exists := repos[name]; !exists {
				repos[name] = path
			}
		}
	}
	return repos
}

// DiscoverRepos performs a deep scan with deduplication by remote URL.
func DiscoverRepos(repoDirs []string, maxDepth int) []models.RepoInfo {
	if maxDepth <= 0 {
		maxDepth = 3
	}

	// Phase 1: Fast filesystem scan
	var candidates []candidate
	for _, dir := range repoDirs {
		collectRepoPaths(dir, 0, maxDepth, &candidates)
	}

	// Phase 2: Resolve remotes (cached + parallel)
	cache := loadRemoteCache()
	type resolvedRepo struct {
		name    string
		path    string
		remote  string
		display string
		depth   int
	}

	var (
		resolved []resolvedRepo
		mu       sync.Mutex
		wg       sync.WaitGroup
		sem      = make(chan struct{}, maxWorkers)
	)

	for _, c := range candidates {
		wg.Add(1)
		go func(c candidate) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			remote := ""
			display := c.name

			// Check cache by .git/config mtime
			gitConfig := filepath.Join(c.path, ".git", "config")
			if info, err := os.Stat(gitConfig); err == nil {
				mtime := info.ModTime().Unix()
				if cached, ok := cache.Entries[c.path]; ok && cached.Mtime == mtime {
					remote = cached.Remote
				} else {
					if url, err := git.RemoteURL(c.path, "origin"); err == nil {
						remote = url
					}
					mu.Lock()
					cache.Entries[c.path] = remoteCacheEntry{Remote: remote, Mtime: mtime}
					mu.Unlock()
				}
			}

			if remote != "" {
				if parsed := git.ParseRemoteName(remote); parsed != "" {
					display = parsed
				}
			}

			// Calculate depth from any repo_dir
			depth := 999
			for _, dir := range repoDirs {
				rel, err := filepath.Rel(dir, c.path)
				if err == nil {
					d := strings.Count(rel, string(filepath.Separator))
					if d < depth {
						depth = d
					}
				}
			}

			mu.Lock()
			resolved = append(resolved, resolvedRepo{
				name:    c.name,
				path:    c.path,
				remote:  remote,
				display: display,
				depth:   depth,
			})
			mu.Unlock()
		}(c)
	}
	wg.Wait()
	saveRemoteCache(cache)

	// Phase 3: Dedup by remote, prefer shallower paths
	sort.Slice(resolved, func(i, j int) bool {
		return resolved[i].depth < resolved[j].depth
	})

	seen := make(map[string]bool)
	var result []models.RepoInfo
	for _, r := range resolved {
		key := r.remote
		if key == "" {
			key = r.path // No remote → use path as unique key
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, models.RepoInfo{
			Name:        r.name,
			Path:        r.path,
			Remote:      r.remote,
			DisplayName: r.display,
		})
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].DisplayName < result[j].DisplayName
	})
	return result
}

// ExploreRepos groups discovered repos by source directory.
func ExploreRepos(repoDirs []string, maxDepth int) map[string]map[string]string {
	if maxDepth <= 0 {
		maxDepth = 3
	}
	grouped := make(map[string]map[string]string)
	for _, dir := range repoDirs {
		repos := make(map[string]string)
		walkRepos(dir, 0, maxDepth, repos)
		if len(repos) > 0 {
			grouped[dir] = repos
		}
	}
	return grouped
}

func walkRepos(dir string, depth, maxDepth int, repos map[string]string) {
	if depth >= maxDepth {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		full := filepath.Join(dir, entry.Name())
		if git.IsGitRepo(full) {
			repos[entry.Name()] = full
		} else {
			walkRepos(full, depth+1, maxDepth, repos)
		}
	}
}

type candidate = struct {
	name string
	path string
}

func collectRepoPaths(dir string, depth, maxDepth int, out *[]candidate) {
	if depth >= maxDepth {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		full := filepath.Join(dir, entry.Name())
		if git.IsGitRepo(full) {
			if out != nil {
				*out = append(*out, candidate{name: entry.Name(), path: full})
			}
		} else {
			collectRepoPaths(full, depth+1, maxDepth, out)
		}
	}
}
