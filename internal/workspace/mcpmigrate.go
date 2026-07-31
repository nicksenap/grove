package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/nicksenap/grove/internal/logging"
)

// Grove used to run a built-in MCP server (`gw mcp-serve`) and wrote a `grove`
// entry into each workspace's `.mcp.json` at creation time. The server is gone,
// so those entries now point at a command that no longer exists.
//
// This file is a one-way migration shim: it detects and removes the stale entry
// from workspaces created by older Grove versions. It never writes `.mcp.json`
// and never touches entries belonging to other tools. Once existing workspaces
// have been recycled it can be deleted.

// mcpConfigFile is the per-workspace MCP client config Grove used to write into.
const mcpConfigFile = ".mcp.json"

// StaleMCPEntry reports whether the workspace at wsPath still carries Grove's
// legacy `grove` MCP server entry. Anything unreadable, unparseable, or owned by
// another tool reports false — the migration only claims what Grove wrote.
func StaleMCPEntry(wsPath string) bool {
	servers, _, err := readMCPServers(filepath.Join(wsPath, mcpConfigFile))
	if err != nil {
		return false
	}
	return isGroveEntry(servers["grove"])
}

// CleanStaleMCPEntry removes Grove's legacy `grove` entry from the workspace's
// `.mcp.json`, preserving every other server. The file is deleted when Grove's
// entry was the only one left. It reports whether anything changed.
func CleanStaleMCPEntry(wsPath string) bool {
	path := filepath.Join(wsPath, mcpConfigFile)
	servers, root, err := readMCPServers(path)
	if err != nil || !isGroveEntry(servers["grove"]) {
		return false
	}

	delete(servers, "grove")

	if len(servers) == 0 && len(root) == 1 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			logging.Warn("could not remove %s: %s", path, err)
			return false
		}
		return true
	}

	root["mcpServers"] = servers
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		logging.Warn("could not marshal %s: %s", path, err)
		return false
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		logging.Warn("could not update %s: %s", path, err)
		return false
	}
	return true
}

// readMCPServers parses a `.mcp.json` and returns its mcpServers map plus the
// full decoded document, so callers can rewrite it without losing sibling keys.
func readMCPServers(path string) (servers map[string]any, root map[string]any, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, nil, err
	}
	servers, ok := root["mcpServers"].(map[string]any)
	if !ok {
		return map[string]any{}, root, nil
	}
	return servers, root, nil
}

// isGroveEntry reports whether an mcpServers entry is the one Grove used to
// write: `gw mcp-serve ...`. A user-authored `grove` entry pointing somewhere
// else (e.g. an external adapter) is left alone.
func isGroveEntry(entry any) bool {
	m, ok := entry.(map[string]any)
	if !ok {
		return false
	}
	if cmd, _ := m["command"].(string); cmd != "gw" && !strings.HasSuffix(cmd, "/gw") {
		return false
	}
	args, _ := m["args"].([]any)
	for _, a := range args {
		if s, _ := a.(string); s == "mcp-serve" {
			return true
		}
	}
	return false
}
