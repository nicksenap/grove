"""CLI commands for Grove."""

from __future__ import annotations

import re
from pathlib import Path

import typer

from grove import __version__, config, discover, state, workspace
from grove.console import console, error, info, make_table, success


def _version_callback(value: bool) -> None:
    if value:
        print(f"gw {__version__}")
        raise typer.Exit()


app = typer.Typer(
    name="gw",
    help="Grove — Git Worktree Workspace Orchestrator",
    rich_markup_mode="rich",
)


@app.callback(invoke_without_command=True)
def main(
    ctx: typer.Context,
    version: bool = typer.Option(
        False,
        "--version",
        "-v",
        help="Show version and exit",
        callback=_version_callback,
        is_eager=True,
    ),
) -> None:
    """Grove — Git Worktree Workspace Orchestrator."""
    if ctx.invoked_subcommand is None and not version:
        # No subcommand and no --version: show help
        console.print(ctx.get_help())
        raise typer.Exit()


# ---------------------------------------------------------------------------
# Tab-completion callbacks
# ---------------------------------------------------------------------------


def complete_workspace_name(incomplete: str) -> list[str]:
    """Return workspace names matching the incomplete string."""
    try:
        return [ws.name for ws in state.load_workspaces() if ws.name.startswith(incomplete)]
    except Exception:
        return []


def complete_repo_name(incomplete: str) -> list[str]:
    """Return repo names matching the incomplete string."""
    try:
        cfg = config.load_config()
        if cfg is None:
            return []
        repos = discover.find_repos(cfg.repos_dir)
        return [name for name in repos if name.startswith(incomplete)]
    except Exception:
        return []


def complete_preset_name(incomplete: str) -> list[str]:
    """Return preset names matching the incomplete string."""
    try:
        cfg = config.load_config()
        if cfg is None:
            return []
        return [name for name in cfg.presets if name.startswith(incomplete)]
    except Exception:
        return []


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _sanitize_name(branch: str) -> str:
    """Derive a workspace name from a branch name.

    ``feat/login`` → ``feat-login``, strips leading/trailing dashes.
    """
    return re.sub(r"[/\s]+", "-", branch).strip("-")


def _pick_one(prompt_text: str, choices: list[str]) -> str:
    """Show a numbered list and let the user pick one."""
    from rich.prompt import Prompt

    console.print(f"\n[bold]{prompt_text}[/]")
    for i, choice in enumerate(choices, 1):
        console.print(f"  [cyan]{i}[/]) {choice}")

    while True:
        answer = Prompt.ask("Choice", console=console)
        # Accept the name directly
        if answer in choices:
            return answer
        # Accept a number
        try:
            idx = int(answer) - 1
            if 0 <= idx < len(choices):
                return choices[idx]
        except ValueError:
            pass
        console.print("[red]Invalid choice, try again[/]")


def _pick_many(prompt_text: str, choices: list[str]) -> list[str]:
    """Show a numbered list and let the user pick multiple (comma-separated)."""
    from rich.prompt import Prompt

    console.print(f"\n[bold]{prompt_text}[/]")
    for i, choice in enumerate(choices, 1):
        console.print(f"  [cyan]{i}[/]) {choice}")
    console.print("  [cyan]a[/]) all")

    while True:
        answer = Prompt.ask("Choices (comma-separated numbers, or 'a' for all)", console=console)
        if answer.strip().lower() == "a":
            return list(choices)
        parts = [p.strip() for p in answer.split(",")]
        selected: list[str] = []
        valid = True
        for p in parts:
            if p in choices:
                selected.append(p)
            else:
                try:
                    idx = int(p) - 1
                    if 0 <= idx < len(choices):
                        selected.append(choices[idx])
                    else:
                        valid = False
                        break
                except ValueError:
                    valid = False
                    break
        if valid and selected:
            return selected
        console.print("[red]Invalid selection, try again[/]")


# ---------------------------------------------------------------------------
# Commands
# ---------------------------------------------------------------------------


@app.command()
def init(
    repos_dir: str = typer.Argument(help="Directory containing your git repos"),
) -> None:
    """Initialize Grove with a repos directory."""
    repos_path = Path(repos_dir).expanduser().resolve()

    if not repos_path.is_dir():
        error(f"Directory does not exist: {repos_path}")
        raise typer.Exit(1)

    repos = discover.find_repos(repos_path)
    cfg = config.Config(
        repos_dir=repos_path,
        workspace_dir=config.DEFAULT_WORKSPACE_DIR,
    )
    config.save_config(cfg)
    config.DEFAULT_WORKSPACE_DIR.mkdir(parents=True, exist_ok=True)

    success(f"Initialized Grove with repos dir: {repos_path}")
    if repos:
        info(f"Found {len(repos)} repos: {', '.join(repos.keys())}")
    else:
        info("No git repos found in that directory yet")


@app.command()
def create(
    name: str | None = typer.Argument(
        None, help="Workspace name (auto-derived from branch if omitted)"
    ),
    repos: str | None = typer.Option(
        None,
        "--repos",
        "-r",
        help="Comma-separated repo names",
        autocompletion=complete_repo_name,
    ),
    branch: str | None = typer.Option(None, "--branch", "-b", help="Branch name"),
    preset: str | None = typer.Option(
        None,
        "--preset",
        "-p",
        help="Named preset from config",
        autocompletion=complete_preset_name,
    ),
    all_repos: bool = typer.Option(False, "--all", help="Use all discovered repos"),
    go: bool = typer.Option(False, "--go", help="Print cd sentinel after creation"),
) -> None:
    """Create a new workspace with worktrees from selected repos."""
    cfg = config.require_config()
    available = discover.find_repos(cfg.repos_dir)

    # --- Interactive fallback when branch is missing ---
    if branch is None:
        from rich.prompt import Prompt

        branch = Prompt.ask("[bold]Branch name[/]", console=console)
        if not branch:
            error("Branch name is required")
            raise typer.Exit(1)

    # --- Resolve repos: -r > -p > --all / default all ---
    if repos is not None:
        # Explicit repo list
        repo_names = [r.strip() for r in repos.split(",")]
    elif preset is not None:
        if preset not in cfg.presets:
            error(f"Preset [bold]{preset}[/] not found in config")
            available_presets = ", ".join(cfg.presets.keys()) if cfg.presets else "(none)"
            info(f"Available presets: {available_presets}")
            raise typer.Exit(1)
        repo_names = cfg.presets[preset]
    elif all_repos:
        repo_names = list(available.keys())
    else:
        # No flags at all — interactive if tty, else default to all
        if not available:
            error("No repos found. Run: gw init <repos-dir>")
            raise typer.Exit(1)
        repo_names = _pick_many(
            "Select repos",
            sorted(available.keys()),
        )

        # Offer to save as preset if none exist
        if (
            not cfg.presets
            and len(repo_names) < len(available)
            and typer.confirm("Save this selection as a preset?", default=False)
        ):
                from rich.prompt import Prompt

                preset_name = Prompt.ask("[bold]Preset name[/]", console=console)
                if preset_name:
                    cfg.presets[preset_name] = repo_names
                    config.save_config(cfg)
                    success(f"Preset [bold]{preset_name}[/] saved")

    # Validate selected repos
    selected: dict[str, Path] = {}
    for rn in repo_names:
        if rn not in available:
            error(f"Repo [bold]{rn}[/] not found in {cfg.repos_dir}")
            info(f"Available: {', '.join(available.keys())}")
            raise typer.Exit(1)
        selected[rn] = available[rn]

    # --- Resolve name: explicit > auto-derive from branch ---
    if name is None:
        name = _sanitize_name(branch)

    ws = workspace.create_workspace(name, selected, branch, cfg)
    if ws is None:
        raise typer.Exit(1)

    console.print()
    success(f"Workspace [bold]{name}[/] created at {ws.path}")

    if go:
        # Sentinel for shell function to intercept and cd
        print(f"__grove_cd:{ws.path}")


@app.command("list")
def list_workspaces() -> None:
    """List all workspaces."""
    workspaces = state.load_workspaces()
    if not workspaces:
        info("No workspaces. Create one with: gw create <name> -r repo1,repo2 -b branch")
        return

    table = make_table("Name", "Branch", "Repos", "Path", "Created")
    for ws in workspaces:
        repo_names = ", ".join(r.repo_name for r in ws.repos)
        table.add_row(ws.name, ws.branch, repo_names, str(ws.path), ws.created_at[:10])
    console.print(table)


@app.command()
def delete(
    name: str | None = typer.Argument(
        None,
        help="Workspace name to delete",
        autocompletion=complete_workspace_name,
    ),
    force: bool = typer.Option(False, "--force", "-f", help="Skip confirmation"),
) -> None:
    """Delete a workspace and its worktrees."""
    # Interactive fallback
    if name is None:
        workspaces = state.load_workspaces()
        if not workspaces:
            error("No workspaces to delete")
            raise typer.Exit(1)
        name = _pick_one("Select workspace to delete", [ws.name for ws in workspaces])

    ws = state.get_workspace(name)
    if ws is None:
        error(f"Workspace [bold]{name}[/] not found")
        raise typer.Exit(1)

    if not force:
        confirm = typer.confirm(f"Delete workspace '{name}' and all its worktrees?")
        if not confirm:
            info("Cancelled")
            return

    if workspace.delete_workspace(name):
        success(f"Workspace [bold]{name}[/] deleted")
    else:
        raise typer.Exit(1)


@app.command()
def status(
    name: str | None = typer.Argument(
        None,
        help="Workspace name (auto-detects from cwd)",
        autocompletion=complete_workspace_name,
    ),
    verbose: bool = typer.Option(False, "--verbose", "-V", help="Show full git status output"),
) -> None:
    """Show git status across a workspace's repos."""
    if name is None:
        ws = state.find_workspace_by_path(Path.cwd())
        if ws is None:
            # Interactive fallback
            workspaces = state.load_workspaces()
            if not workspaces:
                error("Not inside a workspace and no workspaces exist")
                raise typer.Exit(1)
            name = _pick_one("Select workspace", [w.name for w in workspaces])
            ws = state.get_workspace(name)
            if ws is None:
                error(f"Workspace [bold]{name}[/] not found")
                raise typer.Exit(1)
    else:
        ws = state.get_workspace(name)
        if ws is None:
            error(f"Workspace [bold]{name}[/] not found")
            raise typer.Exit(1)

    console.print(f"[bold]Workspace:[/] {ws.name}  [dim]({ws.path})[/]")
    console.print()

    results = workspace.workspace_status(ws)
    table = make_table("Repo", "Branch", "Status")
    for r in results:
        raw_status = r["status"]
        if raw_status == "clean":
            display = "[green]clean[/]"
        elif raw_status.startswith("error:"):
            display = f"[red]{raw_status}[/]"
        else:
            # Count changed files
            changed_count = len(raw_status.splitlines())
            display = f"[yellow]{changed_count} changed[/]"

        table.add_row(r["repo"], r["branch"], display)
    console.print(table)

    # Show full status when verbose
    if verbose:
        for r in results:
            if r["status"] not in ("clean", "") and not r["status"].startswith("error:"):
                console.print(f"\n[bold cyan]{r['repo']}[/]")
                console.print(r["status"])


@app.command()
def go(
    name: str | None = typer.Argument(
        None,
        help="Workspace name",
        autocompletion=complete_workspace_name,
    ),
) -> None:
    """Print workspace path (use with shell function for cd)."""
    # Interactive fallback
    if name is None:
        workspaces = state.load_workspaces()
        if not workspaces:
            error("No workspaces. Create one first: gw create ...")
            raise typer.Exit(1)
        name = _pick_one("Select workspace", [ws.name for ws in workspaces])

    ws = state.get_workspace(name)
    if ws is None:
        error(f"Workspace [bold]{name}[/] not found")
        raise typer.Exit(1)

    # Print raw path for shell function to consume
    print(ws.path)


# ---------------------------------------------------------------------------
# Preset management
# ---------------------------------------------------------------------------

preset_app = typer.Typer(help="Manage repo presets.")
app.add_typer(preset_app, name="preset")


@preset_app.command("add")
def preset_add(
    name: str | None = typer.Argument(None, help="Preset name"),
    repos: str | None = typer.Option(
        None,
        "--repos",
        "-r",
        help="Comma-separated repo names",
        autocompletion=complete_repo_name,
    ),
) -> None:
    """Create or update a named preset."""
    cfg = config.require_config()
    available = discover.find_repos(cfg.repos_dir)

    if not available:
        error("No repos found. Run: gw init <repos-dir>")
        raise typer.Exit(1)

    # Interactive: prompt for name
    if name is None:
        from rich.prompt import Prompt

        name = Prompt.ask("[bold]Preset name[/]", console=console)
        if not name:
            error("Preset name is required")
            raise typer.Exit(1)

    # Interactive: pick repos
    if repos is not None:
        repo_names = [r.strip() for r in repos.split(",")]
        for rn in repo_names:
            if rn not in available:
                error(f"Repo [bold]{rn}[/] not found in {cfg.repos_dir}")
                info(f"Available: {', '.join(available.keys())}")
                raise typer.Exit(1)
    else:
        repo_names = _pick_many("Select repos for preset", sorted(available.keys()))

    cfg.presets[name] = repo_names
    config.save_config(cfg)
    success(f"Preset [bold]{name}[/] saved: {', '.join(repo_names)}")


@preset_app.command("list")
def preset_list() -> None:
    """List all presets."""
    cfg = config.require_config()
    if not cfg.presets:
        info("No presets configured. Add one with: gw preset add")
        return

    table = make_table("Preset", "Repos")
    for name, repos in cfg.presets.items():
        table.add_row(name, ", ".join(repos))
    console.print(table)


@preset_app.command("remove")
def preset_remove(
    name: str | None = typer.Argument(
        None,
        help="Preset name to remove",
        autocompletion=complete_preset_name,
    ),
) -> None:
    """Remove a preset."""
    cfg = config.require_config()
    if not cfg.presets:
        error("No presets to remove")
        raise typer.Exit(1)

    # Interactive: pick preset
    if name is None:
        name = _pick_one("Select preset to remove", list(cfg.presets.keys()))

    if name not in cfg.presets:
        error(f"Preset [bold]{name}[/] not found")
        raise typer.Exit(1)

    del cfg.presets[name]
    config.save_config(cfg)
    success(f"Preset [bold]{name}[/] removed")


@app.command("shell-init")
def shell_init() -> None:
    """Print shell function for gw go navigation. Add to .zshrc:

    eval "$(gw shell-init)"
    """
    # Read the shell script from the package's shell directory
    shell_script = Path(__file__).parent.parent.parent / "shell" / "grove.sh"
    if shell_script.exists():
        print(shell_script.read_text())
    else:
        # Inline fallback
        print(_SHELL_FUNCTION)


_SHELL_FUNCTION = """\
gw() {
    if [ "$1" = "go" ]; then
        local output
        output="$(command gw "$@" 2>&1)"
        local rc=$?
        if [ $rc -eq 0 ] && [ -n "$output" ] && [ -d "$output" ]; then
            cd "$output" || return 1
        else
            echo "$output"
        fi
        return $rc
    fi

    local has_go=false
    for arg in "$@"; do
        [ "$arg" = "--go" ] && has_go=true
    done

    if [ "$has_go" = true ]; then
        local tmpfile
        tmpfile="$(mktemp)"
        command gw "$@" | tee "$tmpfile"
        local rc=${PIPESTATUS[0]}
        local cd_line
        cd_line="$(grep '^__grove_cd:' "$tmpfile")"
        rm -f "$tmpfile"
        if [ -n "$cd_line" ]; then
            local dir="${cd_line#__grove_cd:}"
            [ -d "$dir" ] && cd "$dir" || return 1
        fi
        return $rc
    fi

    command gw "$@"
}
"""
