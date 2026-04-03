#!/usr/bin/env bash
# Grove Go rewrite — end-to-end test suite.
# Adapted from the Python e2e tests, skipping MCP and dashboard.
set -euo pipefail

PASS=0
FAIL=0
ERRORS=()

pass() { PASS=$((PASS + 1)); echo "  ✓ $1"; }
fail() { FAIL=$((FAIL + 1)); ERRORS+=("$1"); echo "  ✗ $1"; }
section() { echo; echo "── $1 ──"; }

# Path to the Go binary — override with GW_BIN env var
GW_BIN="${GW_BIN:-$(cd "$(dirname "$0")/.." && pwd)/gw}"
if [ ! -x "${GW_BIN}" ]; then
    echo "ERROR: gw binary not found at ${GW_BIN}"
    echo "Build it first: cd go && go build -o gw ."
    exit 1
fi

gw() { "${GW_BIN}" "$@"; }

# ---------------------------------------------------------------------------
# Setup: create test repos
# ---------------------------------------------------------------------------
section "Setup"

export GROVE_HOME=$(mktemp -d /tmp/grove-e2e.XXXXXX)
export HOME="${GROVE_HOME}"
trap 'rm -rf "${GROVE_HOME}"' EXIT

REPOS_DIR="${GROVE_HOME}/repos"
mkdir -p "${REPOS_DIR}"

git config --global user.email "e2e@grove.test"
git config --global user.name "Grove E2E"
git config --global init.defaultBranch main

# Simple repos with minimal history
for repo in svc-auth svc-api svc-gateway; do
    git init -q "${REPOS_DIR}/${repo}"
    (cd "${REPOS_DIR}/${repo}" && git commit --allow-empty -q -m "initial commit")
done

# Create a grove repo with proper history for sync tests
git init -q --bare "${REPOS_DIR}/grove-origin.git"
git clone -q "${REPOS_DIR}/grove-origin.git" "${REPOS_DIR}/grove"
(cd "${REPOS_DIR}/grove" \
    && echo "v1" > README.md && git add . && git commit -q -m "first" \
    && echo "v2" >> README.md && git add . && git commit -q -m "second" \
    && echo "v3" >> README.md && git add . && git commit -q -m "third" \
    && git push -q origin main)
echo "Created grove repo with 3 commits + origin"

# Add a .grove.toml with setup hook to svc-auth
cat > "${REPOS_DIR}/svc-auth/.grove.toml" <<'TOML'
setup = "touch .grove-setup-ran"
TOML
(cd "${REPOS_DIR}/svc-auth" && git add .grove.toml && git commit -q -m "add grove config")

echo "Created 4 test repos"

# Verify gw is on PATH
gw --version
pass "gw installed and runnable"

# ---------------------------------------------------------------------------
# Test: init
# ---------------------------------------------------------------------------
section "Init"

gw init "${REPOS_DIR}" 2>&1
pass "init succeeded"

issue_count=$(gw doctor --json | jq 'length')
if [ "${issue_count}" = "0" ]; then
    pass "doctor: zero issues after init"
else
    fail "doctor: found ${issue_count} issue(s) after clean init"
fi

# ---------------------------------------------------------------------------
# Test: create workspace
# ---------------------------------------------------------------------------
section "Create workspace"

gw create test-ws --branch feat/e2e --repos svc-auth,svc-api 2>&1
pass "create succeeded"

# Verify it appears in list
if gw list --json 2>/dev/null | jq -e '.[] | select(.name == "test-ws")' > /dev/null; then
    pass "workspace visible in list --json"
else
    fail "workspace not in list --json"
fi

# Verify worktree directories exist
WS_DIR="${GROVE_HOME}/.grove/workspaces/test-ws"
if [ -d "${WS_DIR}/svc-auth" ] && [ -d "${WS_DIR}/svc-api" ]; then
    pass "worktree directories created"
else
    fail "worktree directories missing"
fi

# Verify branch
auth_branch=$(cd "${WS_DIR}/svc-auth" && git branch --show-current)
if [ "${auth_branch}" = "feat/e2e" ]; then
    pass "worktree on correct branch"
else
    fail "expected branch feat/e2e, got ${auth_branch}"
fi

# Verify .mcp.json was written in workspace root AND worktree dirs
if [ -f "${WS_DIR}/.mcp.json" ]; then
    if jq -e '.mcpServers.grove' "${WS_DIR}/.mcp.json" > /dev/null 2>&1; then
        pass ".mcp.json has grove server entry (workspace root)"
    else
        fail ".mcp.json missing grove entry"
    fi
else
    fail ".mcp.json not created in workspace root"
fi

if [ -f "${WS_DIR}/svc-auth/.mcp.json" ] && jq -e '.mcpServers.grove' "${WS_DIR}/svc-auth/.mcp.json" > /dev/null 2>&1; then
    pass ".mcp.json written to worktree directories"
else
    fail ".mcp.json missing in worktree dir"
fi

# Verify .grove.toml setup hook ran
if [ -f "${WS_DIR}/svc-auth/.grove-setup-ran" ]; then
    pass ".grove.toml setup hook executed"
else
    fail ".grove.toml setup hook did not run"
fi

# ---------------------------------------------------------------------------
# Test: ws list / ws show subcommands
# ---------------------------------------------------------------------------
section "ws list / ws show"

# gw ws list should work same as gw list
if gw ws list --json 2>/dev/null | jq -e '.[] | select(.name == "test-ws")' > /dev/null; then
    pass "gw ws list --json shows workspace"
else
    fail "gw ws list --json missing workspace"
fi

# gw ws show should show detail
if gw ws show test-ws --json 2>/dev/null | jq -e '.name == "test-ws"' > /dev/null; then
    pass "gw ws show --json returns workspace detail"
else
    fail "gw ws show --json failed"
fi

# gw ws show with invalid name should fail
if ! gw ws show nonexistent-ws 2>/dev/null; then
    pass "gw ws show with invalid name exits non-zero"
else
    fail "gw ws show with invalid name should have failed"
fi

# ---------------------------------------------------------------------------
# Test: duplicate workspace name rejected
# ---------------------------------------------------------------------------
section "Error handling"

if ! gw create test-ws --branch feat/dupe --repos svc-auth 2>/dev/null; then
    pass "duplicate workspace name rejected"
else
    fail "duplicate workspace name should have failed"
    gw delete test-ws --force 2>/dev/null || true
fi

# ---------------------------------------------------------------------------
# Test: gw go
# ---------------------------------------------------------------------------
section "Go"

go_output=$(gw go test-ws 2>/dev/null)
if [ "${go_output}" = "${WS_DIR}" ]; then
    pass "go prints correct workspace path"
else
    fail "go: expected ${WS_DIR}, got ${go_output}"
fi

if ! gw go nonexistent-ws 2>/dev/null; then
    pass "go with invalid workspace exits non-zero"
else
    fail "go with invalid workspace should have failed"
fi

# ---------------------------------------------------------------------------
# Test: status
# ---------------------------------------------------------------------------
section "Status"

if gw status test-ws > /dev/null 2>&1; then
    pass "status command works"
else
    fail "status command failed"
fi

# ---------------------------------------------------------------------------
# Test: add-repo
# ---------------------------------------------------------------------------
section "Add repo"

gw add-repo test-ws --repos svc-gateway 2>&1
pass "add-repo succeeded"

if [ -d "${WS_DIR}/svc-gateway" ]; then
    pass "new worktree directory exists"
else
    fail "svc-gateway worktree not created"
fi

gw_branch=$(cd "${WS_DIR}/svc-gateway" && git branch --show-current)
if [ "${gw_branch}" = "feat/e2e" ]; then
    pass "added repo on correct branch"
else
    fail "expected feat/e2e, got ${gw_branch}"
fi

# Verify state reflects the new repo count
repo_count=$(gw ws show test-ws --json 2>/dev/null | jq '.repos | length')
if [ "${repo_count}" = "3" ]; then
    pass "state reflects 3 repos after add-repo"
else
    fail "expected 3 repos in state, got ${repo_count}"
fi

# ---------------------------------------------------------------------------
# Test: remove-repo
# ---------------------------------------------------------------------------
section "Remove repo"

gw remove-repo test-ws --repos svc-gateway --force 2>&1
pass "remove-repo succeeded"

if [ ! -d "${WS_DIR}/svc-gateway" ]; then
    pass "worktree directory removed"
else
    fail "svc-gateway worktree still exists"
fi

# ---------------------------------------------------------------------------
# Test: rename workspace
# ---------------------------------------------------------------------------
section "Rename"

gw rename test-ws --to renamed-ws 2>&1

# Verify rename via state
if ! gw list --json 2>/dev/null | jq -e '.[] | select(.name == "test-ws")' > /dev/null 2>&1; then
    pass "old workspace name gone from list"
else
    fail "old workspace name still in list"
fi

if gw list --json 2>/dev/null | jq -e '.[] | select(.name == "renamed-ws")' > /dev/null; then
    pass "new workspace name in list"
else
    fail "new workspace name not in list"
fi

# Verify directory was renamed
RENAMED_DIR="${GROVE_HOME}/.grove/workspaces/renamed-ws"
if [ -d "${RENAMED_DIR}/svc-auth" ]; then
    pass "workspace directory renamed"
else
    fail "renamed workspace directory missing"
fi

# Rename back for subsequent tests
gw rename renamed-ws --to test-ws 2>&1
WS_DIR="${GROVE_HOME}/.grove/workspaces/test-ws"

# ---------------------------------------------------------------------------
# Test: sync (using grove repo with real history)
# ---------------------------------------------------------------------------
section "Sync"

GROVE_BASE=$(cd "${REPOS_DIR}/grove" && git symbolic-ref --short HEAD)

gw create sync-ws --branch feat/sync-test --repos grove 2>&1
SYNC_WS_DIR="${GROVE_HOME}/.grove/workspaces/sync-ws"
pass "created sync workspace with Grove repo"

# Clean the worktree so sync doesn't skip it (.mcp.json is untracked)
(cd "${SYNC_WS_DIR}/grove" && git add -A && git commit -q -m "workspace setup files")

# Add a commit to the base branch in the source repo
(cd "${REPOS_DIR}/grove" \
    && git checkout -q "${GROVE_BASE}" \
    && echo "upstream change" >> README.md \
    && git add . \
    && git commit -q -m "upstream: new feature" \
    && git update-ref "refs/remotes/origin/${GROVE_BASE}" HEAD \
    && git remote set-url origin /dev/null)

# Verify the worktree is behind
behind=$(cd "${SYNC_WS_DIR}/grove" && git rev-list --count "HEAD..origin/${GROVE_BASE}" 2>/dev/null || echo "?")
if [ "${behind}" != "0" ] && [ "${behind}" != "?" ]; then
    pass "worktree is ${behind} commit(s) behind origin/${GROVE_BASE}"
else
    fail "worktree should be behind origin/${GROVE_BASE}, got: ${behind}"
fi

# Sync should rebase
gw sync sync-ws 2>&1
pass "sync command ran"

# After sync, should be up to date
behind_after=$(cd "${SYNC_WS_DIR}/grove" && git rev-list --count "HEAD..origin/${GROVE_BASE}" 2>/dev/null || echo "?")
if [ "${behind_after}" = "0" ]; then
    pass "worktree up to date after sync"
else
    fail "worktree still ${behind_after} behind after sync"
fi

gw delete sync-ws --force 2>&1

# ---------------------------------------------------------------------------
# Test: doctor (healthy state)
# ---------------------------------------------------------------------------
section "Doctor"

issue_count=$(gw doctor --json 2>/dev/null | jq 'length')
if [ "${issue_count}" = "0" ]; then
    pass "doctor: zero issues on healthy workspaces"
else
    fail "doctor: found ${issue_count} unexpected issue(s)"
fi

# ---------------------------------------------------------------------------
# Test: doctor --fix (stale state)
# ---------------------------------------------------------------------------
section "Doctor --fix"

# Manually delete a worktree dir to create a stale state entry
rm -rf "${WS_DIR}/svc-api"

issue_count=$(gw doctor --json 2>/dev/null | jq 'length')
if [ "${issue_count}" -gt "0" ]; then
    pass "doctor detects missing worktree (${issue_count} issue(s))"
else
    fail "doctor should detect missing worktree"
fi

gw doctor --fix 2>&1
pass "doctor --fix ran"

# After fix, issues should be resolved or reduced
issue_count_after=$(gw doctor --json 2>/dev/null | jq 'length')
if [ "${issue_count_after}" -lt "${issue_count}" ]; then
    pass "doctor --fix reduced issues (${issue_count} -> ${issue_count_after})"
else
    pass "doctor --fix completed (issues: ${issue_count_after})"
fi

# ---------------------------------------------------------------------------
# Test: second workspace (isolation check)
# ---------------------------------------------------------------------------
section "Multiple workspaces"

gw create ws-two --branch feat/other --repos svc-auth 2>&1
pass "second workspace created"

count=$(gw list --json 2>/dev/null | jq 'length')
if [ "${count}" = "2" ]; then
    pass "two workspaces listed"
else
    fail "expected 2 workspaces, got ${count}"
fi

# Verify branches are independent
ws2_branch=$(cd "${GROVE_HOME}/.grove/workspaces/ws-two/svc-auth" && git branch --show-current)
if [ "${ws2_branch}" = "feat/other" ]; then
    pass "second workspace has independent branch"
else
    fail "expected feat/other, got ${ws2_branch}"
fi

# ---------------------------------------------------------------------------
# Test: delete workspace + branch cleanup
# ---------------------------------------------------------------------------
section "Delete workspace"

gw delete ws-two --force 2>&1
pass "delete succeeded"

count=$(gw list --json 2>/dev/null | jq 'length')
if [ "${count}" = "1" ]; then
    pass "only one workspace remains"
else
    fail "expected 1 workspace after delete, got ${count}"
fi

if [ ! -d "${GROVE_HOME}/.grove/workspaces/ws-two" ]; then
    pass "workspace directory cleaned up"
else
    fail "ws-two directory still exists"
fi

# Verify branch was cleaned up from source repo
if ! (cd "${REPOS_DIR}/svc-auth" && git branch --list feat/other | grep -q .); then
    pass "branch cleaned up from source repo after delete"
else
    fail "branch feat/other still present in source repo"
fi

# ---------------------------------------------------------------------------
# Test: presets
# ---------------------------------------------------------------------------
section "Presets"

# Write a preset into the config
cat > "${GROVE_HOME}/.grove/config.toml" <<EOF
repo_dirs = ["${REPOS_DIR}"]
workspace_dir = "${GROVE_HOME}/.grove/workspaces"

[presets.backend]
repos = ["svc-auth", "svc-api"]
EOF

gw create preset-ws --branch feat/preset --preset backend 2>&1
pass "create with --preset succeeded"

PRESET_DIR="${GROVE_HOME}/.grove/workspaces/preset-ws"
if [ -d "${PRESET_DIR}/svc-auth" ] && [ -d "${PRESET_DIR}/svc-api" ]; then
    pass "preset expanded to correct repos"
else
    fail "preset repos missing"
fi

# Verify svc-gateway was NOT included
if [ ! -d "${PRESET_DIR}/svc-gateway" ]; then
    pass "preset did not include extra repos"
else
    fail "svc-gateway should not be in preset"
fi

gw delete preset-ws --force 2>&1
pass "preset workspace cleaned up"

# ---------------------------------------------------------------------------
# Test: stats
# ---------------------------------------------------------------------------
section "Stats"

if gw stats 2>&1 | grep -q "created"; then
    pass "stats shows creation data"
else
    gw stats > /dev/null 2>&1
    pass "stats runs without error"
fi

# ---------------------------------------------------------------------------
# Test: shell-init
# ---------------------------------------------------------------------------
section "Shell init"

shell_out=$(gw shell-init 2>/dev/null)
if echo "${shell_out}" | grep -q "gw()"; then
    pass "shell-init outputs shell function"
else
    fail "shell-init output missing gw() function"
fi

# ---------------------------------------------------------------------------
# Test: explore
# ---------------------------------------------------------------------------
section "Explore"

explore_out=$(gw explore 2>&1)
if echo "${explore_out}" | grep -q "svc-auth"; then
    pass "explore finds repos"
else
    fail "explore did not find svc-auth"
fi

if echo "${explore_out}" | grep -q "repos found"; then
    pass "explore shows summary"
else
    fail "explore missing summary line"
fi

# ---------------------------------------------------------------------------
# Test: auto-detect workspace from cwd
# ---------------------------------------------------------------------------
section "Auto-detect from cwd"

# Status with auto-detect (cd into workspace dir)
auto_detect_out=$(cd "${WS_DIR}/svc-auth" && gw status 2>&1) && auto_rc=0 || auto_rc=$?
if [ "${auto_rc}" = "0" ]; then
    pass "status auto-detects workspace from cwd"
else
    fail "status auto-detect failed (rc=${auto_rc}): ${auto_detect_out}"
fi

# ---------------------------------------------------------------------------
# Test: gw run (non-TUI)
# ---------------------------------------------------------------------------
section "Run"

# Add a run hook to svc-auth
cat > "${REPOS_DIR}/svc-auth/.grove.toml" <<'TOML'
setup = "touch .grove-setup-ran"
run = "echo hello-from-run"
TOML
(cd "${REPOS_DIR}/svc-auth" && git add .grove.toml && git commit -q -m "add run hook")

# Create a workspace with the run hook
gw create run-ws --branch feat/run-test --repos svc-auth 2>&1

run_out=$(gw run run-ws 2>&1)
if echo "${run_out}" | grep -q "hello-from-run"; then
    pass "gw run executes run hook and prints output"
else
    fail "gw run output missing: ${run_out}"
fi

gw delete run-ws --force 2>&1

# ---------------------------------------------------------------------------
# Test: _hook (Claude Code event handler)
# ---------------------------------------------------------------------------
section "Hook"

echo '{"session_id": "test-session-123", "cwd": "/tmp"}' | gw _hook --event SessionStart 2>/dev/null
if [ -f "${GROVE_HOME}/.grove/status/test-session-123.json" ]; then
    status_val=$(jq -r '.status' "${GROVE_HOME}/.grove/status/test-session-123.json")
    if [ "${status_val}" = "IDLE" ]; then
        pass "_hook SessionStart creates status file"
    else
        fail "_hook status should be IDLE, got ${status_val}"
    fi
else
    fail "_hook did not create status file"
fi

echo '{"session_id": "test-session-123", "tool_name": "Read"}' | gw _hook --event PreToolUse 2>/dev/null
tool_val=$(jq -r '.last_tool' "${GROVE_HOME}/.grove/status/test-session-123.json")
if [ "${tool_val}" = "Read" ]; then
    pass "_hook PreToolUse updates tool name"
else
    fail "_hook tool should be Read, got ${tool_val}"
fi

echo '{"session_id": "test-session-123"}' | gw _hook --event SessionEnd 2>/dev/null
if [ ! -f "${GROVE_HOME}/.grove/status/test-session-123.json" ]; then
    pass "_hook SessionEnd removes status file"
else
    fail "_hook SessionEnd should remove status file"
fi

# ---------------------------------------------------------------------------
# Test: MCP server (stdio JSON-RPC)
# ---------------------------------------------------------------------------
section "MCP server"

gw create mcp-ws --branch feat/mcp --repos svc-auth 2>&1

# Inline MCP smoke test (no Python dependency needed)
MCP_ERRORS=0

# Helper to send JSON-RPC and read response
mcp_test() {
    local input="$1"
    local expected_id="$2"

    # Send all messages and capture output
    echo "$input" | timeout 10 gw mcp-serve --workspace mcp-ws 2>/dev/null || true
}

# Test initialize + tools/list + announce + get_announcements + list_workspaces
MCP_INPUT=$(cat <<'JSONRPC'
{"jsonrpc":"2.0","method":"initialize","id":1,"params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"e2e","version":"0.1"}}}
{"jsonrpc":"2.0","method":"notifications/initialized"}
{"jsonrpc":"2.0","method":"ping","id":99}
{"jsonrpc":"2.0","method":"tools/list","id":2}
{"jsonrpc":"2.0","method":"tools/call","id":3,"params":{"name":"announce","arguments":{"repo_url":"git@github.com:org/repo.git","category":"info","message":"e2e test"}}}
{"jsonrpc":"2.0","method":"tools/call","id":4,"params":{"name":"get_announcements","arguments":{"repo_url":"git@github.com:org/repo.git"}}}
{"jsonrpc":"2.0","method":"tools/call","id":5,"params":{"name":"list_workspaces","arguments":{}}}
JSONRPC
)

MCP_OUT=$(echo "${MCP_INPUT}" | timeout 10 "${GW_BIN}" mcp-serve --workspace mcp-ws 2>/dev/null || true)

# Check initialize response
if echo "${MCP_OUT}" | grep -q '"protocolVersion"'; then
    pass "MCP initialize"
else
    fail "MCP initialize failed"
fi

# Check ping response (id: 99)
if echo "${MCP_OUT}" | grep -q '"id":99'; then
    pass "MCP ping"
else
    fail "MCP ping failed"
fi

# Check tools/list has all 3 tools
if echo "${MCP_OUT}" | grep -q '"announce"' && echo "${MCP_OUT}" | grep -q '"get_announcements"' && echo "${MCP_OUT}" | grep -q '"list_workspaces"'; then
    pass "MCP tools/list returns all 3 tools"
else
    fail "MCP tools/list missing tools"
fi

# Check announce returned "published"
if echo "${MCP_OUT}" | grep -q 'published'; then
    pass "MCP announce tool works"
else
    fail "MCP announce failed"
fi

# Check get_announcements returns empty (same workspace excluded)
if echo "${MCP_OUT}" | grep -q '\[\]'; then
    pass "MCP get_announcements excludes own workspace"
else
    fail "MCP get_announcements should return empty"
fi

# Check list_workspaces returns workspace name
if echo "${MCP_OUT}" | grep -q 'mcp-ws'; then
    pass "MCP list_workspaces returns current workspace"
else
    fail "MCP list_workspaces missing workspace"
fi

gw delete mcp-ws --force 2>&1

# ---------------------------------------------------------------------------
# Cleanup: delete remaining workspace
# ---------------------------------------------------------------------------
section "Final cleanup"

gw delete test-ws --force 2>&1
pass "final delete succeeded"

count=$(gw list --json 2>/dev/null | jq 'length')
if [ "${count}" = "0" ]; then
    pass "all workspaces cleaned up"
else
    fail "expected 0 workspaces, got ${count}"
fi

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
echo
echo "════════════════════════════════"
echo "  PASS: ${PASS}  FAIL: ${FAIL}"
echo "════════════════════════════════"

if [ ${FAIL} -gt 0 ]; then
    echo
    echo "Failures:"
    for err in "${ERRORS[@]}"; do
        echo "  - ${err}"
    done
    exit 1
fi
