package mcp

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/nicksenap/grove/internal/state"
)

// JSONRPCRequest is an incoming JSON-RPC 2.0 message.
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	ID      json.RawMessage `json:"id,omitempty"` // absent for notifications, present (even if null) for requests
	Params  json.RawMessage `json:"params,omitempty"`
	hasID   bool
}

// UnmarshalJSONRPC parses a JSON-RPC message, distinguishing notifications (no "id" key)
// from requests ("id" key present, even if value is null).
func unmarshalRequest(data []byte) (JSONRPCRequest, bool) {
	var req JSONRPCRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return req, false
	}

	// Check if "id" key is present in the raw JSON.
	// Notifications have no "id" field at all.
	// Requests have "id" (could be number, string, or null).
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err == nil {
		_, req.hasID = raw["id"]
	}

	return req, true
}

func (r *JSONRPCRequest) isNotification() bool {
	return !r.hasID
}

// JSONRPCResponse is an outgoing JSON-RPC 2.0 message.
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ToolDef describes an MCP tool.
type ToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// ContentItem is a text content block returned by tools.
type ContentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

var tools = []ToolDef{
	{
		Name:        "announce",
		Description: "Publish an announcement visible to other workspaces working on the same repo",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"repo_url": map[string]any{"type": "string", "description": "Git remote URL (SSH or HTTPS)"},
				"category": map[string]any{"type": "string", "enum": []string{"breaking_change", "status", "warning", "info"}},
				"message":  map[string]any{"type": "string", "description": "Announcement text"},
			},
			"required": []string{"repo_url", "category", "message"},
		},
	},
	{
		Name:        "get_announcements",
		Description: "Get recent announcements from other workspaces for a repo",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"repo_url": map[string]any{"type": "string", "description": "Git remote URL"},
				"since":    map[string]any{"type": "string", "description": "ISO 8601 datetime filter (optional)"},
			},
			"required": []string{"repo_url"},
		},
	},
	{
		Name:        "list_workspaces",
		Description: "List all Grove workspaces",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	},
}

// RunServer starts the MCP stdio server for the given workspace.
func RunServer(workspaceID string) error {
	if workspaceID == "" {
		return fmt.Errorf("workspace ID is required")
	}

	db, err := OpenDB()
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		req, ok := unmarshalRequest([]byte(line))
		if !ok {
			continue // skip malformed JSON
		}

		// Notifications (no "id" key) — no response expected
		if req.isNotification() {
			continue
		}

		resp := handleRequest(req, workspaceID, db)
		if resp != nil {
			data, _ := json.Marshal(resp)
			fmt.Fprintf(os.Stdout, "%s\n", data)
			os.Stdout.Sync()
		}
	}

	return nil
}

func handleRequest(req JSONRPCRequest, workspaceID string, db *sql.DB) *JSONRPCResponse {
	switch req.Method {
	case "initialize":
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities": map[string]any{
					"tools": map[string]any{},
				},
				"serverInfo": map[string]any{
					"name":    "grove",
					"version": "0.13.0-go",
				},
			},
		}

	case "ping":
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  map[string]any{},
		}

	case "tools/list":
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  map[string]any{"tools": tools},
		}

	case "tools/call":
		return handleToolCall(req, workspaceID, db)

	default:
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &RPCError{Code: -32601, Message: "Method not found: " + req.Method},
		}
	}
}

func handleToolCall(req JSONRPCRequest, workspaceID string, db *sql.DB) *JSONRPCResponse {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &RPCError{Code: -32602, Message: "Invalid params"},
		}
	}

	var content []ContentItem

	switch params.Name {
	case "announce":
		var args struct {
			RepoURL  string `json:"repo_url"`
			Category string `json:"category"`
			Message  string `json:"message"`
		}
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			content = []ContentItem{{Type: "text", Text: "Error: invalid arguments"}}
			break
		}

		id, err := InsertAnnouncement(db, workspaceID, args.RepoURL, args.Category, args.Message)
		if err != nil {
			content = []ContentItem{{Type: "text", Text: "Error: " + err.Error()}}
		} else {
			content = []ContentItem{{Type: "text", Text: fmt.Sprintf("Announcement #%d published (%s)", id, args.Category)}}
		}

	case "get_announcements":
		var args struct {
			RepoURL string `json:"repo_url"`
			Since   string `json:"since"`
		}
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			content = []ContentItem{{Type: "text", Text: "Error: invalid arguments"}}
			break
		}

		announcements, err := QueryAnnouncements(db, args.RepoURL, workspaceID, args.Since)
		if err != nil {
			content = []ContentItem{{Type: "text", Text: "Error: " + err.Error()}}
		} else {
			data, _ := json.Marshal(announcements)
			content = []ContentItem{{Type: "text", Text: string(data)}}
		}

	case "list_workspaces":
		workspaces, err := state.Load()
		if err != nil {
			content = []ContentItem{{Type: "text", Text: "Error: " + err.Error()}}
		} else {
			type wsInfo struct {
				Name   string `json:"name"`
				Branch string `json:"branch"`
				Path   string `json:"path"`
				Repos  []struct {
					RepoName   string `json:"repo_name"`
					Branch     string `json:"branch"`
					SourceRepo string `json:"source_repo"`
				} `json:"repos"`
			}
			var result []wsInfo
			for _, ws := range workspaces {
				info := wsInfo{
					Name:   ws.Name,
					Branch: ws.Branch,
					Path:   ws.Path,
				}
				for _, r := range ws.Repos {
					info.Repos = append(info.Repos, struct {
						RepoName   string `json:"repo_name"`
						Branch     string `json:"branch"`
						SourceRepo string `json:"source_repo"`
					}{
						RepoName:   r.RepoName,
						Branch:     r.Branch,
						SourceRepo: r.SourceRepo,
					})
				}
				result = append(result, info)
			}
			data, _ := json.Marshal(result)
			content = []ContentItem{{Type: "text", Text: string(data)}}
		}

	default:
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &RPCError{Code: -32602, Message: "Unknown tool: " + params.Name},
		}
	}

	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  map[string]any{"content": content},
	}
}
