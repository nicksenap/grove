#!/usr/bin/env bash
# Clean up what Grove's removed MCP server left behind.
#
# Grove ≤ 1.1.11 shipped a built-in MCP server. Two artifacts outlive it:
#
#   1. a "grove" entry in each workspace's .mcp.json, pointing at the removed
#      `gw mcp-serve` command;
#   2. an announcements SQLite database at ~/.grove/messages.db (plus its -wal and
#      -shm sidecars), which nothing can read now that the driver is gone.
#
# `gw doctor --fix` handles both for workspaces Grove still tracks. This script
# exists for the cases it cannot reach: directories left behind by workspaces that
# were removed from state, checkouts outside the configured workspace directory,
# and machines where you would rather not upgrade first.
#
# It reports what it would do and changes nothing unless you pass --apply.
#
# Usage:
#   scripts/cleanup-mcp-migration.sh [--apply] [--grove-dir DIR] [SEARCH_DIR...]
#
#   --apply           make the changes (default is a dry run)
#   --grove-dir DIR   Grove home (default: $GROVE_HOME or ~/.grove)
#   SEARCH_DIR...     extra directories to scan for .mcp.json
#                     (default: the configured workspace dir, else <grove-dir>/workspaces)
#
# Examples:
#   scripts/cleanup-mcp-migration.sh                    # show what would change
#   scripts/cleanup-mcp-migration.sh --apply            # do it
#   scripts/cleanup-mcp-migration.sh --apply ~/projects # also scan another tree

set -euo pipefail

APPLY=0
GROVE_DIR="${GROVE_HOME:-${HOME}/.grove}"
SEARCH_DIRS=()

while [ $# -gt 0 ]; do
    case "$1" in
        --apply) APPLY=1; shift ;;
        --grove-dir) GROVE_DIR="${2:?--grove-dir needs a path}"; shift 2 ;;
        -h|--help) sed -n '2,32p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
        -*) echo "unknown option: $1" >&2; exit 2 ;;
        *) SEARCH_DIRS+=("$1"); shift ;;
    esac
done

CHANGED=0
WOULD_CHANGE=0
SKIPPED=0

note()  { printf '  %s\n' "$1"; }
acted() { CHANGED=$((CHANGED + 1)); printf '  removed  %s\n' "$1"; }
would() { WOULD_CHANGE=$((WOULD_CHANGE + 1)); printf '  would remove  %s\n' "$1"; }

# ---------------------------------------------------------------------------
# Where to look
# ---------------------------------------------------------------------------

if [ ${#SEARCH_DIRS[@]} -eq 0 ]; then
    # Prefer the configured workspace_dir, since it may not be under the Grove dir.
    configured=""
    if [ -f "${GROVE_DIR}/config.toml" ]; then
        configured=$(sed -n 's/^[[:space:]]*workspace_dir[[:space:]]*=[[:space:]]*"\(.*\)"[[:space:]]*$/\1/p' \
            "${GROVE_DIR}/config.toml" | head -1)
        # Expand a leading ~ the way the config loader does.
        case "${configured}" in "~"/*) configured="${HOME}/${configured#\~/}" ;; esac
    fi
    if [ -n "${configured}" ] && [ -d "${configured}" ]; then
        SEARCH_DIRS=("${configured}")
    else
        SEARCH_DIRS=("${GROVE_DIR}/workspaces")
    fi
fi

echo "Grove MCP cleanup"
echo "  grove dir:   ${GROVE_DIR}"
echo "  scanning:    ${SEARCH_DIRS[*]}"
if [ "${APPLY}" -eq 0 ]; then
    echo "  mode:        dry run (pass --apply to make changes)"
else
    echo "  mode:        applying changes"
fi

# ---------------------------------------------------------------------------
# 1. Stale "grove" entries in .mcp.json
# ---------------------------------------------------------------------------

echo
echo "── .mcp.json entries ──"

have_jq=1
command -v jq > /dev/null 2>&1 || have_jq=0

if [ "${have_jq}" -eq 0 ]; then
    note "jq not found — cannot safely edit .mcp.json files."
    note "Install jq, or run: gw doctor --fix"
else
    # A "grove" entry is only ours if it launches gw mcp-serve. Anything else with
    # that name belongs to the user (for example an external MCP adapter), and is
    # not this script's to touch.
    is_ours='(.mcpServers.grove.command // "") as $c
             | (($c == "gw") or ($c | endswith("/gw")))
               and ((.mcpServers.grove.args // []) | index("mcp-serve") != null)'

    found_any=0
    while IFS= read -r cfg; do
        [ -n "${cfg}" ] || continue
        found_any=1

        if ! jq -e . "${cfg}" > /dev/null 2>&1; then
            SKIPPED=$((SKIPPED + 1))
            note "skipped (not valid JSON): ${cfg}"
            continue
        fi
        if ! jq -e "${is_ours}" "${cfg}" > /dev/null 2>&1; then
            continue
        fi

        remaining=$(jq '(.mcpServers | del(.grove)) | length' "${cfg}")
        other_keys=$(jq '. | del(.mcpServers) | length' "${cfg}")

        if [ "${remaining}" -eq 0 ] && [ "${other_keys}" -eq 0 ]; then
            # Grove was the only thing in the file.
            if [ "${APPLY}" -eq 1 ]; then
                rm -f "${cfg}" && acted "${cfg} (file: grove was its only server)"
            else
                would "${cfg} (file: grove was its only server)"
            fi
            continue
        fi

        if [ "${APPLY}" -eq 1 ]; then
            tmp="${cfg}.grove-cleanup.$$"
            if jq 'del(.mcpServers.grove)' "${cfg}" > "${tmp}" && mv "${tmp}" "${cfg}"; then
                acted "${cfg} (grove entry; ${remaining} other server(s) kept)"
            else
                rm -f "${tmp}"
                SKIPPED=$((SKIPPED + 1))
                note "failed to rewrite: ${cfg}"
            fi
        else
            would "${cfg} (grove entry; ${remaining} other server(s) would be kept)"
        fi
    done <<EOF
$(for dir in "${SEARCH_DIRS[@]}"; do
    [ -d "${dir}" ] && find "${dir}" -name '.mcp.json' -type f 2>/dev/null
done)
EOF

    if [ "${found_any}" -eq 0 ]; then
        note "no .mcp.json files found"
    fi
fi

# ---------------------------------------------------------------------------
# 2. The orphaned announcements database
# ---------------------------------------------------------------------------

echo
echo "── announcements database ──"

db="${GROVE_DIR}/messages.db"
db_found=0
for f in "${db}" "${db}-wal" "${db}-shm" "${db}-journal"; do
    [ -e "${f}" ] || continue
    db_found=1
    if [ "${APPLY}" -eq 1 ]; then
        rm -f "${f}" && acted "${f}"
    else
        would "${f}"
    fi
done

if [ "${db_found}" -eq 0 ]; then
    note "no legacy database found"
elif [ "${APPLY}" -eq 0 ]; then
    note "cross-workspace notes now live in ${GROVE_DIR}/announcements/ (one JSON file per note)"
    note "the old database is unreadable by current Grove, so nothing is migrated out of it"
fi

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------

echo
if [ "${APPLY}" -eq 1 ]; then
    echo "Removed ${CHANGED} item(s)."
else
    echo "Would remove ${WOULD_CHANGE} item(s). Re-run with --apply to do it."
fi
[ "${SKIPPED}" -gt 0 ] && echo "Skipped ${SKIPPED} file(s) — see above."
exit 0
