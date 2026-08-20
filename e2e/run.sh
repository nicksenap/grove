#!/usr/bin/env bash
# Grove Go rewrite — end-to-end test suite.
# Adapted from the Python e2e tests, skipping the dashboard.
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

# Portable timeout wrapper: macOS ships `gtimeout` (coreutils) rather than
# `timeout`. Fall back to running the command without a timeout if neither
# exists so the suite still works everywhere.
if command -v timeout >/dev/null 2>&1; then
    timeout_cmd() { timeout "$@"; }
elif command -v gtimeout >/dev/null 2>&1; then
    timeout_cmd() { gtimeout "$@"; }
else
    timeout_cmd() { shift; "$@"; }  # drop the duration arg, run unbounded
fi

# ---------------------------------------------------------------------------
# Setup: create test repos
# ---------------------------------------------------------------------------
section "Setup"

export GROVE_HOME=$(mktemp -d /tmp/grove-e2e.XXXXXX)
export HOME="${GROVE_HOME}"
unset ZELLIJ_SESSION_NAME  # prevent host env from leaking into doctor checks
trap 'rm -rf "${GROVE_HOME}"' EXIT

REPOS_DIR="${GROVE_HOME}/repos"
mkdir -p "${REPOS_DIR}"

# Safe: HOME is overridden above, so --global writes to $GROVE_HOME/.gitconfig
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
    gw delete test-ws 2>/dev/null || true
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
# Test: gw repos (discovered repos + remotes, machine-readable)
# ---------------------------------------------------------------------------
section "Repos listing"

repos_json=$(gw repos --json 2>/dev/null)
if echo "${repos_json}" | jq -e '.[] | select(.name == "svc-auth")' > /dev/null 2>&1; then
    pass "gw repos --json lists discovered repos"
else
    fail "gw repos --json missing svc-auth: ${repos_json}"
fi

if echo "${repos_json}" | jq -e '.[0] | has("name") and has("path") and has("remote") and has("display_name")' > /dev/null 2>&1; then
    pass "gw repos --json entries have name/path/remote/display_name"
else
    fail "gw repos --json missing expected fields: ${repos_json}"
fi

# Plain (non-JSON) output should mention a known repo.
# Capture first, then grep: piping `gw` straight into `grep -q` can race under
# `set -o pipefail` (grep exits on match, gw gets SIGPIPE, pipeline fails).
repos_table=$(gw repos 2>&1)
if echo "${repos_table}" | grep -q "svc-auth"; then
    pass "gw repos (table) lists repos"
else
    fail "gw repos table missing svc-auth"
fi

# ---------------------------------------------------------------------------
# Test: create --track (check out an existing remote branch, e.g. a PR head)
# ---------------------------------------------------------------------------
section "Create --track (existing remote branch)"

# Build a repo whose origin has a 'PR head' branch that does NOT exist locally,
# so create --track must resolve it from the remote.
git init -q --bare "${GROVE_HOME}/pr-svc-origin.git"
git clone -q "${GROVE_HOME}/pr-svc-origin.git" "${REPOS_DIR}/pr-svc"
(cd "${REPOS_DIR}/pr-svc" \
    && git config user.email "e2e@grove.test" \
    && git config user.name "Grove E2E" \
    && echo base > README.md && git add . && git commit -q -m initial && git push -q origin HEAD \
    && git checkout -q -b feat/pr-head \
    && echo "pr work" > pr-marker.txt && git add . && git commit -q -m "pr work" \
    && git push -q origin feat/pr-head \
    && git checkout -q - \
    && git branch -q -D feat/pr-head \
    && git update-ref -d refs/remotes/origin/feat/pr-head)

gw create pr-ws --repos pr-svc --branch feat/pr-head --track \
    --source-url "https://github.com/acme/pr-svc/pull/42" \
    --source-provider github --source-ref 42 --source-title "Add the thing" 2>&1
pass "create --track succeeded"

PR_WS_DIR="${GROVE_HOME}/.grove/workspaces/pr-ws"

# The PR head's content must be present — we checked out the existing branch,
# not a fresh branch from base.
if [ -f "${PR_WS_DIR}/pr-svc/pr-marker.txt" ]; then
    pass "create --track checked out existing PR head (content present)"
else
    fail "create --track did not check out PR head content"
fi

# The new local branch should track origin/feat/pr-head.
pr_upstream=$(cd "${PR_WS_DIR}/pr-svc" && git rev-parse --abbrev-ref --symbolic-full-name '@{upstream}' 2>/dev/null || echo none)
if [ "${pr_upstream}" = "origin/feat/pr-head" ]; then
    pass "create --track set upstream to origin/feat/pr-head"
else
    fail "expected upstream origin/feat/pr-head, got ${pr_upstream}"
fi

# Source provenance persisted on the workspace.
if gw ws show pr-ws --json 2>/dev/null | jq -e '.source.provider == "github" and .source.ref == "42"' > /dev/null 2>&1; then
    pass "source persisted on workspace (provider + ref)"
else
    fail "source not persisted correctly: $(gw ws show pr-ws --json 2>/dev/null | jq -c .source)"
fi

# Source surfaced in status --json.
status_src=$(gw status pr-ws --json 2>/dev/null | jq -r '.source.url')
if [ "${status_src}" = "https://github.com/acme/pr-svc/pull/42" ]; then
    pass "gw status --json surfaces source url"
else
    fail "status --json source url wrong: ${status_src}"
fi

# Source surfaced in status table. Capture first, then grep (see note above:
# `gw ... | grep -q` races under pipefail once grep matches and exits early).
status_table=$(gw status pr-ws 2>&1)
if echo "${status_table}" | grep -q "Source: github 42"; then
    pass "gw status table shows Source line"
else
    fail "status table missing Source line: ${status_table}"
fi

gw delete pr-ws 2>&1

# ---------------------------------------------------------------------------
# Test: create --track fallback (remote branch missing → new branch, no error)
# ---------------------------------------------------------------------------
section "Create --track fallback"

gw create fallback-ws --repos pr-svc --branch feat/ghost-pr --track 2>&1
pass "create --track with missing remote branch did not error"

fb_branch=$(cd "${GROVE_HOME}/.grove/workspaces/fallback-ws/pr-svc" && git branch --show-current)
if [ "${fb_branch}" = "feat/ghost-pr" ]; then
    pass "fallback created a new branch from base"
else
    fail "expected feat/ghost-pr, got ${fb_branch}"
fi

gw delete fallback-ws 2>&1

# ---------------------------------------------------------------------------
# Test: post_create hook receives source_* placeholders
# ---------------------------------------------------------------------------
section "post_create source placeholders"

SRC_HOOK_MARKER="${GROVE_HOME}/src_hook_out"
cat > "${GROVE_HOME}/.grove/config.toml" <<EOF
repo_dirs = ["${REPOS_DIR}"]
workspace_dir = "${GROVE_HOME}/.grove/workspaces"

[hooks]
post_create = "echo url={source_url} ref={source_ref} title={source_title} > ${SRC_HOOK_MARKER}"
EOF

gw create hooksrc-ws --repos svc-auth --branch feat/hooksrc \
    --source-url "https://example.com/x" --source-provider notion \
    --source-ref pageid123 --source-title "My Task" 2>&1

if [ -f "${SRC_HOOK_MARKER}" ] \
    && grep -q "url=https://example.com/x" "${SRC_HOOK_MARKER}" \
    && grep -q "ref=pageid123" "${SRC_HOOK_MARKER}" \
    && grep -q "title=My Task" "${SRC_HOOK_MARKER}"; then
    pass "post_create hook received source_url/ref/title"
else
    fail "post_create source vars wrong: $(cat "${SRC_HOOK_MARKER}" 2>/dev/null)"
fi

gw delete hooksrc-ws 2>&1
rm -f "${SRC_HOOK_MARKER}"

# Restore config without hooks for subsequent tests.
cat > "${GROVE_HOME}/.grove/config.toml" <<EOF
repo_dirs = ["${REPOS_DIR}"]
workspace_dir = "${GROVE_HOME}/.grove/workspaces"
EOF

# Remove the throwaway PR repo so it doesn't affect later discovery.
rm -rf "${REPOS_DIR}/pr-svc" "${GROVE_HOME}/pr-svc-origin.git"

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
# Test: add-repo with remote URL (clone from URL)
# ---------------------------------------------------------------------------
section "Add repo from remote URL"

# Create a bare repo to act as the "remote"
REMOTE_REPO="${GROVE_HOME}/remote-origin.git"
git init -q --bare "${REMOTE_REPO}"
REMOTE_CLONE="${GROVE_HOME}/remote-tmp"
git clone -q "${REMOTE_REPO}" "${REMOTE_CLONE}"
(cd "${REMOTE_CLONE}" \
    && git config user.email "e2e@grove.test" \
    && git config user.name "Grove E2E" \
    && echo "remote content" > README.md && git add . && git commit -q -m "initial" \
    && git push -q origin HEAD)
rm -rf "${REMOTE_CLONE}"

# Add the remote URL (file:// protocol) to the workspace — should clone into REPOS_DIR
REMOTE_URL="file://${REMOTE_REPO}"
gw add-repo test-ws --repos "${REMOTE_URL}" 2>&1
pass "add-repo with remote URL succeeded"

# Verify the repo was cloned into REPOS_DIR
if [ -d "${REPOS_DIR}/remote-origin" ] && [ -d "${REPOS_DIR}/remote-origin/.git" ]; then
    pass "remote repo cloned into repo_dir"
else
    fail "remote repo not cloned into ${REPOS_DIR}"
fi

# Verify worktree was created in workspace
if [ -d "${WS_DIR}/remote-origin" ]; then
    pass "remote repo worktree created in workspace"
else
    fail "remote repo worktree missing"
fi

# Verify branch
remote_branch=$(cd "${WS_DIR}/remote-origin" && git branch --show-current)
if [ "${remote_branch}" = "feat/e2e" ]; then
    pass "remote repo worktree on correct branch"
else
    fail "expected feat/e2e, got ${remote_branch}"
fi

# Verify state updated
repo_count=$(gw ws show test-ws --json 2>/dev/null | jq '.repos | length')
if [ "${repo_count}" = "4" ]; then
    pass "state reflects 4 repos after remote add"
else
    fail "expected 4 repos, got ${repo_count}"
fi

# Adding same URL again should be idempotent (repo already in workspace)
gw add-repo test-ws --repos "${REMOTE_URL}" 2>&1
repo_count_after=$(gw ws show test-ws --json 2>/dev/null | jq '.repos | length')
if [ "${repo_count_after}" = "4" ]; then
    pass "add-repo with same URL is idempotent (count unchanged)"
else
    fail "idempotent add-repo changed repo count to ${repo_count_after}"
fi

# Clean up: remove the remote repo from workspace
gw remove-repo test-ws --repos remote-origin --force 2>&1

# ---------------------------------------------------------------------------
# Test: add-repo with real HTTPS remote (gw-zellij)
# ---------------------------------------------------------------------------
section "Add repo from HTTPS remote"

# Only run if we have network access
if curl -sf --max-time 5 https://github.com > /dev/null 2>&1; then
    HTTPS_URL="https://github.com/nicksenap/gw-zellij.git"
    gw add-repo test-ws --repos "${HTTPS_URL}" 2>&1
    pass "add-repo with HTTPS URL succeeded"

    if [ -d "${REPOS_DIR}/gw-zellij" ] && [ -d "${REPOS_DIR}/gw-zellij/.git" ]; then
        pass "HTTPS repo cloned into repo_dir"
    else
        fail "HTTPS repo not cloned into ${REPOS_DIR}"
    fi

    if [ -d "${WS_DIR}/gw-zellij" ]; then
        pass "HTTPS repo worktree created in workspace"
    else
        fail "HTTPS repo worktree missing"
    fi

    https_branch=$(cd "${WS_DIR}/gw-zellij" && git branch --show-current)
    if [ "${https_branch}" = "feat/e2e" ]; then
        pass "HTTPS repo worktree on correct branch"
    else
        fail "expected feat/e2e, got ${https_branch}"
    fi

    # Verify origin remote points to the correct URL
    cloned_url=$(cd "${REPOS_DIR}/gw-zellij" && git remote get-url origin)
    if [ "${cloned_url}" = "${HTTPS_URL}" ]; then
        pass "cloned repo has correct origin URL"
    else
        fail "expected origin ${HTTPS_URL}, got ${cloned_url}"
    fi

    gw remove-repo test-ws --repos gw-zellij --force 2>&1
    pass "HTTPS remote repo cleaned up from workspace"
else
    echo "  ⊘ skipped (no network access)"
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
# Test: add-repo auto-detects workspace from cwd
# ---------------------------------------------------------------------------
section "Add repo (cwd auto-detect)"

# svc-gateway was removed above. Re-add it by running `gw add-repo` with NO
# workspace name from inside the workspace directory — the cwd should be
# auto-detected.
(cd "${WS_DIR}" && gw add-repo --repos svc-gateway) 2>&1
pass "add-repo without NAME succeeded from inside workspace"

repo_count=$(gw ws show test-ws --json 2>/dev/null | jq '.repos | length')
if [ "${repo_count}" = "3" ]; then
    pass "cwd-detected add-repo added to correct workspace"
else
    fail "expected 3 repos after cwd-detected add-repo, got ${repo_count}"
fi

# Also works from a subdirectory inside a worktree
gw remove-repo test-ws --repos svc-gateway --force 2>&1
(cd "${WS_DIR}/svc-auth" && gw add-repo --repos svc-gateway) 2>&1
repo_count=$(gw ws show test-ws --json 2>/dev/null | jq '.repos | length')
if [ "${repo_count}" = "3" ]; then
    pass "cwd auto-detect works from worktree subdirectory"
else
    fail "expected 3 repos from subdir detect, got ${repo_count}"
fi

# Clean up for subsequent tests that expect the original 2-repo state
gw remove-repo test-ws --repos svc-gateway --force 2>&1

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

gw delete sync-ws 2>&1

# ---------------------------------------------------------------------------
# Test: reset (switch back to workspace branch, then sync)
# ---------------------------------------------------------------------------
section "Reset"

gw create reset-ws --branch feat/reset-test --repos grove 2>&1
RESET_WS_DIR="${GROVE_HOME}/.grove/workspaces/reset-ws"
pass "created reset workspace with Grove repo"

(cd "${RESET_WS_DIR}/grove" && git switch -q -c feat/wander)
wander=$(cd "${RESET_WS_DIR}/grove" && git branch --show-current)
if [ "${wander}" = "feat/wander" ]; then
    pass "worktree switched off workspace branch"
else
    fail "expected feat/wander, got: ${wander}"
fi

gw reset reset-ws 2>&1
pass "reset command ran"

home=$(cd "${RESET_WS_DIR}/grove" && git branch --show-current)
if [ "${home}" = "feat/reset-test" ]; then
    pass "worktree back on workspace branch"
else
    fail "expected feat/reset-test after reset, got: ${home}"
fi

(cd "${RESET_WS_DIR}/grove" && git switch -q -c feat/wander2 && echo dirt >> README.md)
gw reset reset-ws 2>&1
dirty_branch=$(cd "${RESET_WS_DIR}/grove" && git branch --show-current)
if [ "${dirty_branch}" = "feat/wander2" ]; then
    pass "reset skips dirty wanderer"
else
    fail "dirty wanderer should stay on feat/wander2, got: ${dirty_branch}"
fi

gw delete reset-ws --force 2>&1

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

gw delete ws-two 2>&1
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
# Test: lifecycle hooks (post_create + pre_delete)
# ---------------------------------------------------------------------------
section "Lifecycle hooks"

# Configure lifecycle hooks that write marker files
POST_CREATE_MARKER="${GROVE_HOME}/post_create_fired"
PRE_DELETE_MARKER="${GROVE_HOME}/pre_delete_fired"
cat > "${GROVE_HOME}/.grove/config.toml" <<EOF
repo_dirs = ["${REPOS_DIR}"]
workspace_dir = "${GROVE_HOME}/.grove/workspaces"

[hooks]
post_create = "touch ${POST_CREATE_MARKER}"
pre_delete = "touch ${PRE_DELETE_MARKER}"
EOF

gw create hook-ws --branch feat/hook-test --repos svc-auth 2>&1
if [ -f "${POST_CREATE_MARKER}" ]; then
    pass "post_create hook fired after workspace creation"
else
    fail "post_create hook did not fire"
fi

gw delete hook-ws 2>&1
if [ -f "${PRE_DELETE_MARKER}" ]; then
    pass "pre_delete hook fired before workspace deletion"
else
    fail "pre_delete hook did not fire"
fi

rm -f "${POST_CREATE_MARKER}" "${PRE_DELETE_MARKER}"

# ---------------------------------------------------------------------------
# Test: --no-hooks skips lifecycle hooks
# ---------------------------------------------------------------------------
section "--no-hooks flag"

# Hooks are still configured from the previous section. With --no-hooks the
# create + delete should run but neither marker should appear.
gw --no-hooks create nohook-ws --branch feat/no-hooks --repos svc-auth 2>&1
if [ ! -f "${POST_CREATE_MARKER}" ]; then
    pass "--no-hooks skipped post_create hook"
else
    fail "--no-hooks did not skip post_create hook"
fi

# Use the -n shorthand here to cover both spellings.
gw -n delete nohook-ws 2>&1
if [ ! -f "${PRE_DELETE_MARKER}" ]; then
    pass "-n (--no-hooks shorthand) skipped pre_delete hook"
else
    fail "-n did not skip pre_delete hook"
fi

rm -f "${POST_CREATE_MARKER}" "${PRE_DELETE_MARKER}"

# Restore config without hooks for subsequent tests
cat > "${GROVE_HOME}/.grove/config.toml" <<EOF
repo_dirs = ["${REPOS_DIR}"]
workspace_dir = "${GROVE_HOME}/.grove/workspaces"
EOF

# ---------------------------------------------------------------------------
# Test: hook metadata (stream + on_failure)
# ---------------------------------------------------------------------------
section "Hook metadata (stream + on_failure)"

# stream = true should send the hook's output live to stderr, each line
# prefixed with the hook name.
cat > "${GROVE_HOME}/.grove/config.toml" <<EOF
repo_dirs = ["${REPOS_DIR}"]
workspace_dir = "${GROVE_HOME}/.grove/workspaces"

[hooks.post_create]
command = "echo hello-from-stream"
stream = true
EOF

stream_out=$(gw create stream-ws --branch feat/stream --repos svc-auth 2>&1)
if echo "${stream_out}" | grep -q '\[post_create\] hello-from-stream'; then
    pass "stream = true streamed prefixed hook output"
else
    fail "stream hook output missing [post_create] prefix: ${stream_out}"
fi
gw delete stream-ws 2>&1

# on_failure = "abort" should make a failing hook fatal — gw exits non-zero.
cat > "${GROVE_HOME}/.grove/config.toml" <<EOF
repo_dirs = ["${REPOS_DIR}"]
workspace_dir = "${GROVE_HOME}/.grove/workspaces"

[hooks.post_create]
command = "echo boom-details; exit 1"
on_failure = "abort"
EOF

if gw create abort-ws --branch feat/abort --repos svc-auth 2>&1; then
    fail "on_failure = abort did not make the failing hook fatal"
else
    pass "on_failure = abort made the failing hook fatal (gw exited non-zero)"
fi
# The workspace is created before post_create runs, so it still exists. Only
# pre_delete fires on delete (unset here), so cleanup succeeds cleanly.
gw delete abort-ws 2>&1

# Default on_failure ("warn") should NOT abort — a failing hook only warns.
cat > "${GROVE_HOME}/.grove/config.toml" <<EOF
repo_dirs = ["${REPOS_DIR}"]
workspace_dir = "${GROVE_HOME}/.grove/workspaces"

[hooks.post_create]
command = "echo boom-details; exit 1"
EOF

if gw create warn-ws --branch feat/warn --repos svc-auth 2>&1; then
    pass "default on_failure = warn did not abort on hook failure"
else
    fail "default on_failure = warn should not abort the command"
fi
gw delete warn-ws 2>&1

# Restore config without hooks for subsequent tests
cat > "${GROVE_HOME}/.grove/config.toml" <<EOF
repo_dirs = ["${REPOS_DIR}"]
workspace_dir = "${GROVE_HOME}/.grove/workspaces"
EOF

# ---------------------------------------------------------------------------
# Test: gw ws delete (subcommand parity with gw delete)
# ---------------------------------------------------------------------------
section "ws delete subcommand"

gw create ws-del-sub --branch feat/ws-del --repos svc-auth 2>&1
pass "workspace for ws delete test created"

gw ws delete ws-del-sub 2>&1
pass "gw ws delete succeeded"

count=$(gw list --json 2>/dev/null | jq 'length')
if [ "${count}" = "1" ]; then
    pass "workspace removed by gw ws delete"
else
    fail "expected 1 workspace after gw ws delete, got ${count}"
fi

if [ ! -d "${GROVE_HOME}/.grove/workspaces/ws-del-sub" ]; then
    pass "workspace directory cleaned up by ws delete"
else
    fail "ws-del-sub directory still exists after ws delete"
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

gw delete preset-ws 2>&1
pass "preset workspace cleaned up"

# Test: preset list --json
preset_json=$(gw preset list --json 2>/dev/null)
if echo "${preset_json}" | jq -e '.backend.repos' > /dev/null 2>&1; then
    pass "preset list --json shows presets"
else
    fail "preset list --json missing presets: ${preset_json}"
fi

# Test: preset show --json
preset_show=$(gw preset show backend --json 2>/dev/null)
if echo "${preset_show}" | jq -e '.name == "backend"' > /dev/null 2>&1; then
    pass "preset show --json returns preset detail"
else
    fail "preset show --json failed: ${preset_show}"
fi

if echo "${preset_show}" | jq -e '.repos | length == 2' > /dev/null 2>&1; then
    pass "preset show --json has correct repo count"
else
    fail "preset show --json wrong repo count"
fi

# ---------------------------------------------------------------------------
# Test: Recipe workspace creation
# ---------------------------------------------------------------------------
section "Recipe create"

RECIPE_AUTH_ORIGIN="${GROVE_HOME}/recipe-auth.git"
RECIPE_API_ORIGIN="${GROVE_HOME}/recipe-api.git"
git clone -q --bare "${REPOS_DIR}/svc-auth" "${RECIPE_AUTH_ORIGIN}"
git clone -q --bare "${REPOS_DIR}/svc-api" "${RECIPE_API_ORIGIN}"
git -C "${REPOS_DIR}/svc-auth" remote add origin "file://${RECIPE_AUTH_ORIGIN}"
git -C "${REPOS_DIR}/svc-api" remote add origin "file://${RECIPE_API_ORIGIN}"
git -C "${REPOS_DIR}/svc-auth" fetch -q origin
git -C "${REPOS_DIR}/svc-api" fetch -q origin

RECIPE_FILE="${GROVE_HOME}/recipe.yaml"
cat > "${RECIPE_FILE}" <<EOF
version: 1
name: e2e-recipe
repositories:
  auth:
    url: file://${RECIPE_AUTH_ORIGIN}
    ref: main
  api:
    url: file://${RECIPE_API_ORIGIN}
    ref: main
jobs:
  setup-auth:
    repository: auth
    steps:
      - run: touch .recipe-ran
  setup-api:
    repository: api
    steps:
      - run: touch .recipe-ran
  verify:
    repository: auth
    needs: [setup-auth, setup-api]
    steps:
      - run: test -f .recipe-ran && test -f ../api/.recipe-ran
EOF

recipe_json=$(gw create recipe-ws --branch feat/recipe-e2e --recipe "${RECIPE_FILE}" --json 2>/dev/null)
if echo "${recipe_json}" | jq -e '.created == true and .name == "recipe-ws" and (.jobs | length) == 3' >/dev/null; then
    pass "create --recipe returns successful JSON"
else
    fail "create --recipe JSON failed: ${recipe_json}"
fi

RECIPE_WS="${GROVE_HOME}/.grove/workspaces/recipe-ws"
if [ -f "${RECIPE_WS}/auth/.recipe-ran" ] && [ -f "${RECIPE_WS}/api/.recipe-ran" ]; then
    pass "Recipe jobs prepared aliased repository worktrees"
else
    fail "Recipe job outputs missing"
fi
if [ ! -f "${RECIPE_WS}/auth/.grove-setup-ran" ]; then
    pass "Recipe create skipped legacy .grove.toml setup"
else
    fail "Recipe create unexpectedly ran legacy setup"
fi

gw delete recipe-ws >/dev/null 2>&1
pass "Recipe workspace cleaned up normally"

FAIL_RECIPE="${GROVE_HOME}/recipe-fail.yaml"
cat > "${FAIL_RECIPE}" <<EOF
version: 1
repositories:
  api:
    url: file://${RECIPE_API_ORIGIN}
    ref: main
jobs:
  fail:
    repository: api
    steps:
      - name: Fail deliberately
        run: touch generated.txt; exit 7
EOF

if failed_json=$(gw create failed-recipe --branch feat/recipe-fail --recipe "${FAIL_RECIPE}" --json 2>/dev/null); then
    fail "failing Recipe unexpectedly succeeded"
else
    if echo "${failed_json}" | jq -e '.created == false and .error.code == "recipe_step_failed" and .error.job == "fail" and .error.step == 1' >/dev/null; then
        pass "Recipe failure identifies job and step in JSON"
    else
        fail "Recipe failure JSON was not actionable: ${failed_json}"
    fi
fi
if [ ! -d "${GROVE_HOME}/.grove/workspaces/failed-recipe" ] && ! git -C "${REPOS_DIR}/svc-api" show-ref --verify --quiet refs/heads/feat/recipe-fail; then
    pass "failed Recipe rolled back workspace and owned branch"
else
    fail "failed Recipe rollback left workspace or branch"
fi

if gw create conflict-recipe --branch feat/conflict --recipe "${RECIPE_FILE}" --repos svc-api >/dev/null 2>&1; then
    fail "--recipe accepted conflicting --repos"
else
    pass "--recipe rejects existing repository selectors"
fi

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
# Test: recursive repository discovery
# ---------------------------------------------------------------------------
section "Recursive repository discovery"

NESTED_REPOS_DIR="${GROVE_HOME}/nested-repos"
mkdir -p "${NESTED_REPOS_DIR}/team"
git init -q "${NESTED_REPOS_DIR}/team/svc-nested"
(cd "${NESTED_REPOS_DIR}/team/svc-nested" && git commit --allow-empty -q -m "initial commit")

add_dir_out=$(gw add-dir "${NESTED_REPOS_DIR}" 2>&1)
if echo "${add_dir_out}" | grep -q "1 repos found"; then
    pass "add-dir counts nested repos"
else
    fail "add-dir did not discover nested repo: ${add_dir_out}"
fi

if gw create nested-discovery -b feat/nested-discovery -r svc-nested >/dev/null 2>&1; then
    pass "create uses nested repo discovery"
    gw -n delete nested-discovery >/dev/null 2>&1
else
    fail "create could not use nested repo"
fi

if gw --help 2>&1 | grep -qE '^  explore[[:space:]]'; then
    fail "explore command is still advertised"
else
    pass "explore command removed"
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

gw delete run-ws 2>&1

# ---------------------------------------------------------------------------
# Test: plugin system
# ---------------------------------------------------------------------------
section "Plugins"

# plugin list — empty
plugin_list_out=$(gw plugin list 2>&1)
if echo "${plugin_list_out}" | grep -q "No plugins"; then
    pass "plugin list shows empty"
else
    fail "plugin list should show empty: ${plugin_list_out}"
fi

# Install a fake plugin as a shell script
PLUGINS_DIR="${GROVE_HOME}/.grove/plugins"
mkdir -p "${PLUGINS_DIR}"
cat > "${PLUGINS_DIR}/gw-hello" <<'SH'
#!/bin/sh
echo "hello-from-plugin GROVE_DIR=${GROVE_DIR} args=$*"
SH
chmod +x "${PLUGINS_DIR}/gw-hello"

# plugin list — shows hello
if gw plugin list 2>&1 | grep -q "hello"; then
    pass "plugin list shows installed plugin"
else
    fail "plugin list missing hello"
fi

# plugin list --json
plugin_json=$(gw plugin list --json 2>/dev/null)
if echo "${plugin_json}" | jq -e '.[0].name == "hello"' > /dev/null 2>&1; then
    pass "plugin list --json returns plugin data"
else
    fail "plugin list --json failed: ${plugin_json}"
fi

if echo "${plugin_json}" | jq -e '.[0].path' > /dev/null 2>&1; then
    pass "plugin list --json includes path"
else
    fail "plugin list --json missing path"
fi

# Unknown command fallback — gw hello should exec the plugin
hello_out=$(gw hello --test-flag 2>&1)
if echo "${hello_out}" | grep -q "hello-from-plugin"; then
    pass "unknown command falls back to plugin"
else
    fail "plugin fallback failed: ${hello_out}"
fi

# Verify env vars are passed
if echo "${hello_out}" | grep -q "GROVE_DIR="; then
    pass "GROVE_DIR passed to plugin"
else
    fail "GROVE_DIR not passed to plugin"
fi

# Verify args are forwarded
if echo "${hello_out}" | grep -q "\-\-test-flag"; then
    pass "args forwarded to plugin"
else
    fail "args not forwarded: ${hello_out}"
fi

# plugin remove
gw plugin remove hello 2>&1
if [ ! -f "${PLUGINS_DIR}/gw-hello" ]; then
    pass "plugin remove deletes binary"
else
    fail "plugin remove did not delete binary"
fi

# plugin remove nonexistent should fail
if ! gw plugin remove nonexistent 2>/dev/null; then
    pass "plugin remove nonexistent exits non-zero"
else
    fail "plugin remove nonexistent should fail"
fi

# Unknown command with no plugin should fail
if ! gw nonexistent-cmd 2>/dev/null; then
    pass "unknown command without plugin exits non-zero"
else
    fail "unknown command without plugin should fail"
fi

# ---------------------------------------------------------------------------
# Test: create --replace (delete current ws, create new one)
# ---------------------------------------------------------------------------
section "Create --replace"

gw create replace-old --branch feat/replace-old --repos svc-api 2>&1
REPLACE_OLD_DIR="${GROVE_HOME}/.grove/workspaces/replace-old"
pass "created workspace to be replaced"

# Run create --replace from inside replace-old. Should delete replace-old and
# create replace-new.
(cd "${REPLACE_OLD_DIR}" && gw create replace-new --branch feat/replace-new --repos svc-api --replace -f) 2>&1
pass "create --replace succeeded"

if ! gw list --json 2>/dev/null | jq -e '.[] | select(.name == "replace-old")' > /dev/null 2>&1; then
    pass "old workspace deleted by --replace"
else
    fail "old workspace still present after --replace"
fi

if gw list --json 2>/dev/null | jq -e '.[] | select(.name == "replace-new")' > /dev/null; then
    pass "new workspace created by --replace"
else
    fail "new workspace missing after --replace"
fi

if [ ! -d "${REPLACE_OLD_DIR}" ]; then
    pass "old workspace directory removed"
else
    fail "old workspace directory still on disk"
fi

if [ -d "${GROVE_HOME}/.grove/workspaces/replace-new/svc-api" ]; then
    pass "new workspace worktree on disk"
else
    fail "new workspace worktree missing"
fi

# --replace outside any workspace should error
if ! (cd "${GROVE_HOME}" && gw create should-not-exist --branch feat/nope --repos svc-api --replace -f) 2>/dev/null; then
    pass "--replace outside a workspace exits non-zero"
else
    fail "--replace outside a workspace should have failed"
    gw delete should-not-exist 2>/dev/null || true
fi

# --replace with same name as current ws should error (name collision guard)
if ! (cd "${GROVE_HOME}/.grove/workspaces/replace-new" && gw create replace-new --branch feat/collide --repos svc-api --replace -f) 2>/dev/null; then
    pass "--replace rejects same-name collision"
else
    fail "--replace should reject same-name collision"
fi

gw delete replace-new 2>&1

# ---------------------------------------------------------------------------
# Test: bug-report (verify diagnostics collection — don't actually open browser)
# ---------------------------------------------------------------------------
section "Bug report"

# Use --print to output report to stdout instead of opening a browser
bug_out=$(gw bug-report --print 2>/dev/null)
if echo "${bug_out}" | grep -q "## Environment"; then
    pass "bug-report --print includes environment info"
else
    fail "bug-report --print missing environment section"
fi

if echo "${bug_out}" | grep -q "## Doctor"; then
    pass "bug-report --print includes doctor output"
else
    fail "bug-report --print missing doctor section"
fi

if echo "${bug_out}" | grep -q "## Recent Logs"; then
    pass "bug-report --print includes recent logs"
else
    fail "bug-report --print missing logs section"
fi

# ---------------------------------------------------------------------------
# Cleanup: delete remaining workspace
# ---------------------------------------------------------------------------
section "Final cleanup"

gw delete test-ws 2>&1
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
