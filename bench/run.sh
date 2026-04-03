#!/usr/bin/env bash
# Grove performance benchmark — Python vs Go.
# Sets up an isolated environment, runs common commands N times, reports timings.
set -euo pipefail

ITERATIONS="${ITERATIONS:-20}"
BENCH_DIR="$(cd "$(dirname "$0")/.." && pwd)"

# ---------------------------------------------------------------------------
# Resolve binaries
# ---------------------------------------------------------------------------

# Python: always use uv run to get the actual Python implementation
run_py() { uv run --project "${BENCH_DIR}" gw "$@"; }

# Go: prebuilt binary
GW_GO="${GW_GO:-${BENCH_DIR}/go/gw}"
if [ ! -x "${GW_GO}" ]; then
    echo "ERROR: Go binary not found at ${GW_GO}"
    echo "Build it first: cd go && go build -o gw ."
    exit 1
fi
run_go() { "${GW_GO}" "$@"; }

py_version=$(run_py --version 2>&1 | head -1)
go_version=$(run_go --version 2>&1 | head -1)
echo "Python: uv run gw  (${py_version})"
echo "Go:     ${GW_GO}  (${go_version})"
echo "Iterations: ${ITERATIONS}"

if echo "${py_version}" | grep -q '\-go'; then
    echo
    echo "WARNING: Python gw reports a Go version — you may be benchmarking Go vs Go."
    echo "         Make sure 'uv run gw' invokes the Python entry point."
fi

# ---------------------------------------------------------------------------
# Setup: isolated environment with test repos
# ---------------------------------------------------------------------------

export GROVE_HOME=$(mktemp -d /tmp/grove-bench.XXXXXX)
export HOME="${GROVE_HOME}"
trap 'rm -rf "${GROVE_HOME}"' EXIT

REPOS_DIR="${GROVE_HOME}/repos"
mkdir -p "${REPOS_DIR}"

git config --global user.email "bench@grove.test"
git config --global user.name "Grove Bench"
git config --global init.defaultBranch main

for repo in svc-auth svc-api svc-gateway svc-catalog svc-payments; do
    git init -q "${REPOS_DIR}/${repo}"
    (cd "${REPOS_DIR}/${repo}" && git commit --allow-empty -q -m "initial commit")
done

# Init with Go (faster), state format is shared
run_go init "${REPOS_DIR}" > /dev/null 2>&1
run_go create bench-ws --branch feat/bench --repos svc-auth,svc-api,svc-gateway > /dev/null 2>&1

cat >> "${GROVE_HOME}/.grove/config.toml" <<EOF

[presets.backend]
repos = ["svc-auth", "svc-api", "svc-gateway"]
EOF

echo "Setup complete: 5 repos, 1 workspace, 1 preset"

# ---------------------------------------------------------------------------
# Timing helper — uses date +%s%N on Linux, python3 fallback on macOS
# ---------------------------------------------------------------------------

if date +%s%N 2>/dev/null | grep -qE '^[0-9]{19}$'; then
    now_ns() { date +%s%N; }
else
    now_ns() { python3 -c 'import time; print(time.monotonic_ns())'; }
fi

# ---------------------------------------------------------------------------
# Benchmark harness
# ---------------------------------------------------------------------------

# bench <label> <args...>
# Same args for both Python and Go.
bench() {
    local label="$1"; shift
    local args=("$@")

    # Warmup
    run_py "${args[@]}" > /dev/null 2>&1 || true
    run_go "${args[@]}" > /dev/null 2>&1 || true

    # Time Python
    local start end py_total=0
    for ((i = 0; i < ITERATIONS; i++)); do
        start=$(now_ns)
        run_py "${args[@]}" > /dev/null 2>&1 || true
        end=$(now_ns)
        py_total=$((py_total + end - start))
    done

    # Time Go
    local go_total=0
    for ((i = 0; i < ITERATIONS; i++)); do
        start=$(now_ns)
        run_go "${args[@]}" > /dev/null 2>&1 || true
        end=$(now_ns)
        go_total=$((go_total + end - start))
    done

    local py_avg=$((py_total / ITERATIONS / 1000000))
    local go_avg=$((go_total / ITERATIONS / 1000000))

    if [ "${go_avg}" -gt 0 ]; then
        local speedup
        speedup=$(python3 -c "print(f'{${py_avg}/${go_avg}:.1f}')")
        printf "  %-30s  %6s ms  %6s ms  %5sx\n" "${label}" "${py_avg}" "${go_avg}" "${speedup}"
    else
        printf "  %-30s  %6s ms    <1 ms      —\n" "${label}" "${py_avg}"
    fi
}

# bench_cycle: create + delete (heavier, fewer iterations)
bench_cycle() {
    local label="create+delete cycle"
    local n=$((ITERATIONS / 4))
    [ "${n}" -lt 3 ] && n=3

    run_go delete cycle-ws --force > /dev/null 2>&1 || true

    local start end py_total=0
    for ((i = 0; i < n; i++)); do
        start=$(now_ns)
        run_py create cycle-ws --branch "feat/c${i}" --repos svc-auth > /dev/null 2>&1
        run_py delete cycle-ws --force > /dev/null 2>&1
        end=$(now_ns)
        py_total=$((py_total + end - start))
    done

    local go_total=0
    for ((i = 0; i < n; i++)); do
        start=$(now_ns)
        run_go create cycle-ws --branch "feat/c${i}" --repos svc-auth > /dev/null 2>&1
        run_go delete cycle-ws --force > /dev/null 2>&1
        end=$(now_ns)
        go_total=$((go_total + end - start))
    done

    local py_avg=$((py_total / n / 1000000))
    local go_avg=$((go_total / n / 1000000))

    if [ "${go_avg}" -gt 0 ]; then
        local speedup
        speedup=$(python3 -c "print(f'{${py_avg}/${go_avg}:.1f}')")
        printf "  %-30s  %6s ms  %6s ms  %5sx\n" "${label} (n=${n})" "${py_avg}" "${go_avg}" "${speedup}"
    else
        printf "  %-30s  %6s ms    <1 ms      —\n" "${label} (n=${n})" "${py_avg}"
    fi
}

# ---------------------------------------------------------------------------
# Run benchmarks
# ---------------------------------------------------------------------------

printf "\n  %-30s  %8s  %8s  %5s\n" "COMMAND" "PYTHON" "GO" "RATIO"
printf "  %-30s  %8s  %8s  %5s\n" "───────" "──────" "──────" "─────"

bench "--version"              --version
bench "list"                   list
bench "list --json"            list --json
bench "ws list"                ws list
bench "ws show --json"         ws show bench-ws --json
bench "status"                 status bench-ws
bench "doctor --json"          doctor --json
bench "preset list"            preset list
bench_cycle

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
echo
echo "Done. Wall-clock averages over ${ITERATIONS} runs (create/delete uses fewer)."
echo "Lower is better. Ratio = Python/Go (higher means Go is faster)."
