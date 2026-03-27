"""MCP stdio smoke test — sends JSON-RPC messages to gw mcp-serve."""

from __future__ import annotations

import json
import queue
import subprocess
import sys
import threading


def read_responses(
    proc: subprocess.Popen, q: queue.Queue, stop: threading.Event
) -> None:
    """Background reader: collect newline-delimited JSON responses."""
    while not stop.is_set():
        line = proc.stdout.readline()
        if not line:
            break
        line = line.strip()
        if not line:
            continue
        try:
            q.put(json.loads(line))
        except json.JSONDecodeError:
            pass


def send(proc: subprocess.Popen, msg: dict) -> None:
    """Send a JSON-RPC message as newline-delimited JSON."""
    proc.stdin.write(json.dumps(msg) + "\n")
    proc.stdin.flush()


def wait_for_response(
    q: queue.Queue, seen: list, id: int, timeout: float = 10.0
) -> dict | None:
    """Wait for a response with the given id."""
    import time

    deadline = time.monotonic() + timeout
    # Check already-seen responses first
    for r in seen:
        if r.get("id") == id:
            return r
    while time.monotonic() < deadline:
        try:
            msg = q.get(timeout=0.1)
            seen.append(msg)
            if msg.get("id") == id:
                return msg
        except queue.Empty:
            pass
    return None


def main() -> int:
    workspace = sys.argv[1]
    errors = []
    q: queue.Queue[dict] = queue.Queue()
    seen: list[dict] = []

    proc = subprocess.Popen(
        ["gw", "mcp-serve", "--workspace", workspace],
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )

    stop = threading.Event()
    reader = threading.Thread(target=read_responses, args=(proc, q, stop), daemon=True)
    reader.start()

    try:
        # 1. Initialize
        send(proc, {
            "jsonrpc": "2.0",
            "method": "initialize",
            "id": 1,
            "params": {
                "protocolVersion": "2024-11-05",
                "capabilities": {},
                "clientInfo": {"name": "e2e-test", "version": "0.1"},
            },
        })

        resp = wait_for_response(q, seen,1)
        if resp and "result" in resp:
            print("  ✓ MCP initialize")
        else:
            errors.append(f"initialize failed: {resp}")
            print(f"  ✗ MCP initialize: {resp}")

        # 2. Send initialized notification
        send(proc, {"jsonrpc": "2.0", "method": "notifications/initialized"})

        # 3. List tools
        send(proc, {"jsonrpc": "2.0", "method": "tools/list", "id": 2})
        resp = wait_for_response(q, seen,2)
        if resp and "result" in resp:
            tool_names = sorted(t["name"] for t in resp["result"]["tools"])
            expected = ["announce", "get_announcements", "list_workspaces"]
            if tool_names == expected:
                print("  ✓ MCP tools/list returns all 3 tools")
            else:
                errors.append(f"expected tools {expected}, got {tool_names}")
                print(f"  ✗ MCP tools/list: got {tool_names}")
        else:
            errors.append(f"tools/list failed: {resp}")
            print(f"  ✗ MCP tools/list: {resp}")

        # 4. Call announce
        send(proc, {
            "jsonrpc": "2.0",
            "method": "tools/call",
            "id": 3,
            "params": {
                "name": "announce",
                "arguments": {
                    "repo_url": "git@github.com:org/repo.git",
                    "category": "info",
                    "message": "e2e test announcement",
                },
            },
        })
        resp = wait_for_response(q, seen,3)
        if resp and "result" in resp:
            text = resp["result"]["content"][0]["text"]
            if "published" in text:
                print("  ✓ MCP announce tool works")
            else:
                errors.append(f"unexpected announce result: {text}")
                print(f"  ✗ MCP announce: {text}")
        else:
            errors.append(f"announce failed: {resp}")
            print(f"  ✗ MCP announce: {resp}")

        # 5. Call get_announcements (should be empty — same workspace excluded)
        send(proc, {
            "jsonrpc": "2.0",
            "method": "tools/call",
            "id": 4,
            "params": {
                "name": "get_announcements",
                "arguments": {"repo_url": "git@github.com:org/repo.git"},
            },
        })
        resp = wait_for_response(q, seen,4)
        if resp and "result" in resp:
            content = resp["result"].get("content", [])
            if not content:
                # Empty list returned as empty content
                print("  ✓ MCP get_announcements excludes own workspace")
            else:
                text = content[0]["text"]
                announcements = json.loads(text)
                if announcements == []:
                    print("  ✓ MCP get_announcements excludes own workspace")
                else:
                    errors.append(f"expected empty announcements, got: {text}")
                    print(f"  ✗ MCP get_announcements: {text}")
        else:
            errors.append(f"get_announcements failed: {resp}")
            print(f"  ✗ MCP get_announcements: {resp}")

        # 6. Call list_workspaces
        send(proc, {
            "jsonrpc": "2.0",
            "method": "tools/call",
            "id": 5,
            "params": {
                "name": "list_workspaces",
                "arguments": {},
            },
        })
        resp = wait_for_response(q, seen,5)
        if resp and "result" in resp:
            content = resp["result"].get("content", [])
            # list_workspaces returns a list — MCP serializes each item or the whole list as text
            found = False
            for item in content:
                if workspace in item.get("text", ""):
                    found = True
                    break
            if found:
                print("  ✓ MCP list_workspaces returns current workspace")
            else:
                errors.append(f"workspace {workspace} not found in list_workspaces content")
                print(f"  ✗ MCP list_workspaces: {content}")
        else:
            errors.append(f"list_workspaces failed: {resp}")
            print(f"  ✗ MCP list_workspaces: {resp}")

    finally:
        stop.set()
        proc.stdin.close()
        proc.terminate()
        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait()

    if errors:
        for e in errors:
            print(f"  ERROR: {e}")
    return len(errors)


if __name__ == "__main__":
    sys.exit(main())
