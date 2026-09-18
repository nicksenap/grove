package plugin

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// registryJSON is the curated list of known plugins shipped with gw. Adding a
// plugin is a PR to this file. The schema is intentionally small so the same
// file can later be served remotely.
//
//go:embed registry.json
var registryJSON []byte

// RegistryEntry describes one known plugin.
type RegistryEntry struct {
	Name        string `json:"name"`
	Repo        string `json:"repo"`
	Description string `json:"description"`
}

// Registry returns the embedded plugin registry sorted by name.
func Registry() ([]RegistryEntry, error) {
	return parseRegistry(registryJSON)
}

func parseRegistry(data []byte) ([]RegistryEntry, error) {
	var entries []RegistryEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parsing plugin registry: %w", err)
	}
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if err := validatePluginName(entry.Name); err != nil {
			return nil, fmt.Errorf("plugin registry: %w", err)
		}
		if _, _, err := parseRepo(entry.Repo); err != nil {
			return nil, fmt.Errorf("plugin registry entry %q: %w", entry.Name, err)
		}
		if seen[entry.Name] {
			return nil, fmt.Errorf("plugin registry: duplicate entry %q", entry.Name)
		}
		seen[entry.Name] = true
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, nil
}

// Search returns registry entries whose name, repo, or description contains
// term (case-insensitive). An empty term returns everything.
func Search(term string) ([]RegistryEntry, error) {
	entries, err := Registry()
	if err != nil {
		return nil, err
	}
	term = strings.ToLower(strings.TrimSpace(term))
	if term == "" {
		return entries, nil
	}
	var matches []RegistryEntry
	for _, entry := range entries {
		haystack := strings.ToLower(entry.Name + " " + entry.Repo + " " + entry.Description)
		if strings.Contains(haystack, term) {
			matches = append(matches, entry)
		}
	}
	return matches, nil
}

// ResolveRepo maps an install argument to owner/repo. Anything containing a
// slash is already a repo reference; a bare name is looked up in the registry.
func ResolveRepo(arg string) (string, error) {
	if strings.Contains(arg, "/") {
		return arg, nil
	}
	entries, err := Registry()
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry.Name == arg {
			return entry.Repo, nil
		}
	}
	return "", fmt.Errorf("unknown plugin %q; use owner/repo, or see: gw plugin search", arg)
}
