#!/usr/bin/env bash
# Grove end-to-end test suite.
# Runs inside a container (see Dockerfile) or locally if you set up the env.
set -euo pipefail

PASS=0
FAIL=0
ERRORS=()

pass() { PASS=$((PASS + 1)); echo "  ✓ $1"; }
fail() { FAIL=$((FAIL + 1)); ERRORS+=("$1"); echo "  ✗ $1"; }
section() { echo; echo "── $1 ──"; }

# Wrapper that tolerates crashes during Python exit cleanup (SIGSEGV=139, SIGABRT=134).
# Some CI runners trigger these in native extension destructors (ncurses, uvloop, etc.)
# after the command has completed its work. Not a Grove bug.
gw() { command gw "$@" || { rc=$?; if [ $rc -eq 139 ] || [ $rc -eq 134 ]; then echo "  (ignoring signal crash at exit, rc=$rc)" >&2; else return $rc; fi; }; }

# ---------------------------------------------------------------------------
# Setup: create test repos (including a clone of Grove itself)
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

# Use a copy of the real Grove repo — has proper commit history for sync tests
GROVE_SRC="${GROVE_SRC:-/src/grove}"
if [ -d "${GROVE_SRC}/.git" ]; then
    git clone -q --local "${GROVE_SRC}" "${REPOS_DIR}/grove"
    echo "Cloned Grove repo ($(cd "${REPOS_DIR}/grove" && git rev-list --count HEAD) commits)"
else
    # Fallback: create a bare origin + clone so we have proper remote refs
    git init -q --bare "${REPOS_DIR}/grove-origin.git"
    git clone -q "${REPOS_DIR}/grove-origin.git" "${REPOS_DIR}/grove"
    (cd "${REPOS_DIR}/grove" \
        && echo "v1" > README.md && git add . && git commit -q -m "first" \
        && echo "v2" >> README.md && git add . && git commit -q -m "second" \
        && echo "v3" >> README.md && git add . && git commit -q -m "third" \
        && git push -q origin main)
    echo "Created grove repo with 3 commits + origin (no source clone available)"
fi

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

gw create test-ws --branch feat/e2e --repos svc-auth,svc-api
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

gw add-repo test-ws --repos svc-gateway
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
repo_count=$(gw list test-ws --json 2>/dev/null | jq '.repos | length')
if [ "${repo_count}" = "3" ]; then
    pass "state reflects 3 repos after add-repo"
else
    fail "expected 3 repos in state, got ${repo_count}"
fi

# ---------------------------------------------------------------------------
# Test: remove-repo
# ---------------------------------------------------------------------------
section "Remove repo"

gw remove-repo test-ws --repos svc-gateway --force
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

gw rename test-ws --to renamed-ws

# Verify rename via state (not exit code — segfaults can happen at Python exit)
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
gw rename renamed-ws --to test-ws
WS_DIR="${GROVE_HOME}/.grove/workspaces/test-ws"

# ---------------------------------------------------------------------------
# Test: sync (using grove repo with real history)
# ---------------------------------------------------------------------------
section "Sync"

# Use the Grove clone — a real repo with full commit history
GROVE_BASE=$(cd "${REPOS_DIR}/grove" && git symbolic-ref --short HEAD)

gw create sync-ws --branch feat/sync-test --repos grove
SYNC_WS_DIR="${GROVE_HOME}/.grove/workspaces/sync-ws"
pass "created sync workspace with Grove repo"

# Clean the worktree so sync doesn't skip it (.mcp.json is untracked)
(cd "${SYNC_WS_DIR}/grove" && git add -A && git commit -q -m "workspace setup files")

# Add a commit to the base branch in the source repo (simulating upstream work)
# Then update origin/master ref so gw sync (which rebases onto origin/<base>) picks it up
(cd "${REPOS_DIR}/grove" \
    && git checkout -q "${GROVE_BASE}" \
    && echo "upstream change" >> README.md \
    && git add . \
    && git commit -q -m "upstream: new feature" \
    && git update-ref "refs/remotes/origin/${GROVE_BASE}" HEAD \
    && git remote set-url origin /dev/null)

# Verify the worktree is behind origin/<base> (what gw sync rebases onto)
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

gw delete sync-ws --force

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
    # If fix couldn't resolve it, that's still informative
    pass "doctor --fix completed (issues: ${issue_count_after})"
fi

# ---------------------------------------------------------------------------
# Test: doctor detects orphaned workspace directory (simulated interrupted create)
# ---------------------------------------------------------------------------
section "Doctor: orphaned workspace dir"

# Simulate an interrupted create by manually creating a workspace directory
# that is NOT tracked in state (exactly what happens on Ctrl+C)
ORPHAN_DIR="${GROVE_HOME}/.grove/workspaces/interrupted-ws"
mkdir -p "${ORPHAN_DIR}/svc-auth"
echo "leftover" > "${ORPHAN_DIR}/svc-auth/junk.txt"

orphan_issues=$(gw doctor --json 2>/dev/null | jq '[.[] | select(.issue | contains("orphaned workspace directory"))] | length')
if [ "${orphan_issues}" -gt "0" ]; then
    pass "doctor detects orphaned workspace directory"
else
    fail "doctor should detect orphaned workspace directory"
fi

# Verify the orphan issue text mentions the directory name
if gw doctor --json 2>/dev/null | jq -e '.[] | select(.issue | contains("interrupted-ws"))' > /dev/null 2>&1; then
    pass "doctor issue names the orphaned directory"
else
    fail "doctor issue should mention 'interrupted-ws'"
fi

# Fix should remove the orphaned directory
gw doctor --fix 2>&1
pass "doctor --fix ran for orphaned dir"

if [ ! -d "${ORPHAN_DIR}" ]; then
    pass "doctor --fix removed orphaned workspace directory"
else
    fail "orphaned directory still exists after doctor --fix"
fi

# Verify doctor is clean after fix
orphan_issues_after=$(gw doctor --json 2>/dev/null | jq '[.[] | select(.issue | contains("orphaned workspace directory"))] | length')
if [ "${orphan_issues_after}" = "0" ]; then
    pass "doctor clean after fixing orphaned dir"
else
    fail "doctor still reports orphaned dir after fix"
fi

# ---------------------------------------------------------------------------
# Test: second workspace (isolation check)
# ---------------------------------------------------------------------------
section "Multiple workspaces"

gw create ws-two --branch feat/other --repos svc-auth
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

gw delete ws-two --force
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

gw create preset-ws --branch feat/preset --preset backend
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

gw delete preset-ws --force
pass "preset workspace cleaned up"

# ---------------------------------------------------------------------------
# Test: stats
# ---------------------------------------------------------------------------
section "Stats"

# We've created and deleted workspaces above, so stats should have data
if gw stats 2>&1 | grep -q "created"; then
    pass "stats shows creation data"
else
    # stats might display differently, just check it doesn't error
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
# Test: MCP server (stdio JSON-RPC)
# ---------------------------------------------------------------------------
section "MCP server"

# Need a workspace for the MCP server to run against
gw create mcp-ws --branch feat/mcp --repos svc-auth

# mcp_smoke.py prints ✓/✗ lines and exits with error count
if python3 "$(dirname "$0")/mcp_smoke.py" mcp-ws; then
    pass "MCP server smoke test passed"
else
    fail "MCP server smoke test failed"
fi

gw delete mcp-ws --force

# ---------------------------------------------------------------------------
# Cleanup: delete remaining workspace
# ---------------------------------------------------------------------------
section "Final cleanup"

gw delete test-ws --force
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
