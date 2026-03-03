"""CLI commands for Grove."""

from __future__ import annotations

import os
import re
from pathlib import Path

import typer

from grove import __version__, config, discover, state, workspace
from grove.console import console, error, info, make_table, success, warning
from grove.git import pr_status as git_pr_status
from grove.models import Workspace
from grove.update import get_newer_version


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
    # Non-blocking update check (reads cache, refreshes in background)
    newer = get_newer_version(__version__)
    if newer:
        warning(f"New version available: {__version__} → {newer} — run: brew upgrade grove")

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
    """Arrow-key single selection."""
    return choices[_pick_one_idx(prompt_text, choices)]


def _pick_one_idx(prompt_text: str, choices: list[str]) -> int:
    """Arrow-key single selection, returns the chosen index."""
    from simple_term_menu import TerminalMenu

    menu = TerminalMenu(
        choices,
        title=f"\n{prompt_text}",
        menu_cursor="❯ ",
        menu_cursor_style=("fg_cyan", "bold"),
        menu_highlight_style=("fg_cyan", "bold"),
    )
    idx = menu.show()
    if idx is None:
        raise typer.Abort()
    return idx


def _pick_many(prompt_text: str, choices: list[str]) -> list[str]:
    """Arrow-key + space multi-selection."""
    from simple_term_menu import TerminalMenu

    display = ["(all)", *choices]
    menu = TerminalMenu(
        display,
        title=f"\n{prompt_text}\n  ↑/↓ navigate · space select · enter confirm",
        multi_select=True,
        multi_select_select_on_accept=False,
        menu_cursor="❯ ",
        menu_cursor_style=("fg_cyan", "bold"),
        menu_highlight_style=("fg_cyan", "bold"),
    )
    result = menu.show()
    if result is None:
        raise typer.Abort()
    selected = menu.chosen_menu_entries
    if not selected:
        raise typer.Abort()
    if "(all)" in selected:
        return list(choices)
    return list(selected)


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
        # No flags at all — interactive picker
        if not available:
            error("No repos found. Run: gw init <repos-dir>")
            raise typer.Exit(1)

        # Offer presets when available
        if cfg.presets:
            preset_names = list(cfg.presets.keys())
            preset_choices = [
                f"{name}  ({', '.join(repos_list)})" for name, repos_list in cfg.presets.items()
            ]
            source_idx = _pick_one_idx(
                "Select repos from",
                [*preset_choices, "Pick manually…"],
            )
            if source_idx == len(preset_choices):
                repo_names = _pick_many("Select repos", sorted(available.keys()))
            else:
                repo_names = cfg.presets[preset_names[source_idx]]
        else:
            repo_names = _pick_many("Select repos", sorted(available.keys()))

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

    # --- Copy CLAUDE.md from repos dir if present ---
    claude_md = cfg.repos_dir / "CLAUDE.md"
    if claude_md.is_file():
        import shutil

        if typer.confirm("Copy CLAUDE.md into workspace?", default=True):
            shutil.copy2(claude_md, ws.path / "CLAUDE.md")
            success("CLAUDE.md copied")

    console.print()
    success(f"Workspace [bold]{name}[/] created at {ws.path}")

    # Signal the shell wrapper to cd into the new workspace
    cd_file = os.environ.get("GROVE_CD_FILE")
    if cd_file:
        Path(cd_file).write_text(str(ws.path))


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
    # Interactive fallback — multi-select
    if name is None:
        workspaces = state.load_workspaces()
        if not workspaces:
            error("No workspaces to delete")
            raise typer.Exit(1)
        names = _pick_many("Select workspace(s) to delete", [ws.name for ws in workspaces])
    else:
        names = [name]

    # Validate all names upfront
    for n in names:
        if state.get_workspace(n) is None:
            error(f"Workspace [bold]{n}[/] not found")
            raise typer.Exit(1)

    if not force:
        label = ", ".join(names)
        msg = f"Delete {len(names)} workspace(s) ({label}) and all their worktrees?"
        confirm = typer.confirm(msg)
        if not confirm:
            info("Cancelled")
            return

    failed = False
    for n in names:
        if workspace.delete_workspace(n):
            success(f"Workspace [bold]{n}[/] deleted")
        else:
            failed = True
    if failed:
        raise typer.Exit(1)


@app.command()
def doctor(
    fix: bool = typer.Option(False, "--fix", help="Auto-fix stale state entries"),
) -> None:
    """Diagnose workspace health issues."""
    cfg = config.require_config()
    issues = workspace.diagnose_workspaces(cfg)

    if not issues:
        success("All workspaces healthy")
        return

    table = make_table("Workspace", "Repo", "Issue", "Suggested Action")
    for issue in issues:
        table.add_row(
            issue.workspace_name,
            issue.repo_name or "[dim]—[/]",
            issue.issue,
            issue.suggested_action,
        )
    console.print(table)

    if fix:
        fixed = workspace.fix_workspace_issues(issues)
        if fixed:
            success(f"Fixed {fixed} issue(s)")
        else:
            info("No auto-fixable issues found")
    else:
        info(f"Found {len(issues)} issue(s). Run [bold]gw doctor --fix[/] to auto-fix")


@app.command()
def rename(
    name: str | None = typer.Argument(
        None,
        help="Current workspace name",
        autocompletion=complete_workspace_name,
    ),
    to: str = typer.Option(..., "--to", help="New workspace name"),
) -> None:
    """Rename a workspace."""
    cfg = config.require_config()

    if name is None:
        workspaces = state.load_workspaces()
        if not workspaces:
            error("No workspaces exist")
            raise typer.Exit(1)
        name = _pick_one("Select workspace to rename", [w.name for w in workspaces])

    if workspace.rename_workspace(name, to, cfg):
        success(f"Renamed [bold]{name}[/] → [bold]{to}[/]")
    else:
        raise typer.Exit(1)


@app.command("add-repo")
def add_repo(
    name: str | None = typer.Argument(
        None,
        help="Workspace name",
        autocompletion=complete_workspace_name,
    ),
    repos: str | None = typer.Option(
        None,
        "--repos",
        "-r",
        help="Comma-separated repo names to add",
        autocompletion=complete_repo_name,
    ),
) -> None:
    """Add repos to an existing workspace."""
    cfg = config.require_config()
    available = discover.find_repos(cfg.repos_dir)

    # Resolve workspace
    if name is None:
        workspaces = state.load_workspaces()
        if not workspaces:
            error("No workspaces exist")
            raise typer.Exit(1)
        name = _pick_one("Select workspace", [w.name for w in workspaces])

    ws = state.get_workspace(name)
    if ws is None:
        error(f"Workspace [bold]{name}[/] not found")
        raise typer.Exit(1)

    existing_names = {r.repo_name for r in ws.repos}
    addable = {n: p for n, p in available.items() if n not in existing_names}

    if not addable:
        info("All repos are already in this workspace")
        return

    if repos is not None:
        repo_names = [r.strip() for r in repos.split(",")]
        for rn in repo_names:
            if rn not in available:
                error(f"Repo [bold]{rn}[/] not found in {cfg.repos_dir}")
                raise typer.Exit(1)
    else:
        repo_names = _pick_many("Select repos to add", sorted(addable.keys()))

    selected = {rn: available[rn] for rn in repo_names}
    # Filter out already-present repos before calling workspace function
    actually_new = {rn: p for rn, p in selected.items() if rn not in existing_names}
    if not actually_new:
        info("All selected repos are already in the workspace")
        return
    added = workspace.add_repo_to_workspace(ws, actually_new, cfg)
    if added is None:
        raise typer.Exit(1)
    success(f"Added {len(added)} repo(s) to [bold]{name}[/]")


@app.command("remove-repo")
def remove_repo(
    name: str | None = typer.Argument(
        None,
        help="Workspace name",
        autocompletion=complete_workspace_name,
    ),
    repos: str | None = typer.Option(
        None,
        "--repos",
        "-r",
        help="Comma-separated repo names to remove",
    ),
    force: bool = typer.Option(False, "--force", "-f", help="Skip confirmation"),
) -> None:
    """Remove repos from an existing workspace."""
    # Resolve workspace
    if name is None:
        workspaces = state.load_workspaces()
        if not workspaces:
            error("No workspaces exist")
            raise typer.Exit(1)
        name = _pick_one("Select workspace", [w.name for w in workspaces])

    ws = state.get_workspace(name)
    if ws is None:
        error(f"Workspace [bold]{name}[/] not found")
        raise typer.Exit(1)

    if not ws.repos:
        error(f"Workspace [bold]{name}[/] has no repos")
        raise typer.Exit(1)

    if repos is not None:
        repo_names = [r.strip() for r in repos.split(",")]
    else:
        repo_names = _pick_many("Select repos to remove", [r.repo_name for r in ws.repos])

    if not force:
        label = ", ".join(repo_names)
        if not typer.confirm(f"Remove {label} from workspace {name}?"):
            info("Cancelled")
            return

    ok = workspace.remove_repo_from_workspace(ws, repo_names, force=force)
    if not ok:
        raise typer.Exit(1)


def _resolve_workspace(name: str | None) -> Workspace:
    """Resolve a workspace by name, cwd auto-detection, or interactive picker."""
    if name is None:
        ws = state.find_workspace_by_path(Path.cwd())
        if ws is None:
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
    return ws


def _format_drift(ahead: str, behind: str) -> str:
    """Format ahead/behind counts for table display."""
    if ahead == "-" or behind == "-":
        return "[dim]-[/]"
    return f"[green]{ahead}↑[/] [yellow]{behind}↓[/]"


def _format_pr(pr_data: dict | None) -> str:
    """Format PR info for table display."""
    if pr_data is None:
        return "[dim]—[/]"
    number = pr_data.get("number", "?")
    pr_state = pr_data.get("state", "OPEN")
    review = pr_data.get("reviewDecision", "")

    if pr_state == "MERGED":
        return f"[magenta]#{number} (merged)[/]"
    if pr_state == "CLOSED":
        return f"[dim]#{number} (closed)[/]"

    review_map = {
        "APPROVED": "[green]approved[/]",
        "CHANGES_REQUESTED": "[yellow]changes requested[/]",
        "REVIEW_REQUIRED": "[dim]review required[/]",
    }
    review_text = review_map.get(review, "[dim]open[/]")
    return f"#{number} ({review_text})"


@app.command()
def status(
    name: str | None = typer.Argument(
        None,
        help="Workspace name (auto-detects from cwd)",
        autocompletion=complete_workspace_name,
    ),
    verbose: bool = typer.Option(False, "--verbose", "-V", help="Show full git status output"),
    show_pr: bool = typer.Option(False, "--pr", "-P", help="Show GitHub PR status (requires gh)"),
    show_all: bool = typer.Option(False, "--all", "-a", help="Show summary of all workspaces"),
) -> None:
    """Show git status across a workspace's repos."""
    if show_all:
        if name is not None:
            error("Cannot combine workspace name with --all")
            raise typer.Exit(1)
        summaries = workspace.all_workspaces_summary()
        if not summaries:
            info("No workspaces. Create one with: gw create <name> -r repo1,repo2 -b branch")
            return
        table = make_table("Workspace", "Branch", "Repos", "Status")
        for s in summaries:
            table.add_row(s["name"], s["branch"], s["repos"], s["status"])
        console.print(table)
        return

    ws = _resolve_workspace(name)

    console.print(f"[bold]Workspace:[/] {ws.name}  [dim]({ws.path})[/]")
    console.print()

    results = workspace.workspace_status(ws)

    columns = ["Repo", "Branch", "↑↓", "Status"]
    if show_pr:
        columns.append("PR")
    table = make_table(*columns)

    for r in results:
        raw_status = r["status"]
        if raw_status == "clean":
            display = "[green]clean[/]"
        elif raw_status.startswith("error:"):
            display = f"[red]{raw_status}[/]"
        else:
            changed_count = len(raw_status.splitlines())
            display = f"[yellow]{changed_count} changed[/]"

        drift = _format_drift(r.get("ahead", "-"), r.get("behind", "-"))

        row = [r["repo"], r["branch"], drift, display]
        if show_pr:
            pr_info = git_pr_status(ws.path / r["repo"])
            row.append(_format_pr(pr_info))

        table.add_row(*row)
    console.print(table)

    # Show full status when verbose
    if verbose:
        for r in results:
            if r["status"] not in ("clean", "") and not r["status"].startswith("error:"):
                console.print(f"\n[bold cyan]{r['repo']}[/]")
                console.print(r["status"])


@app.command()
def sync(
    name: str | None = typer.Argument(
        None,
        help="Workspace name (auto-detects from cwd)",
        autocompletion=complete_workspace_name,
    ),
) -> None:
    """Sync workspace repos by rebasing onto their base branches."""
    ws = _resolve_workspace(name)

    console.print(f"[bold]Syncing:[/] {ws.name}")
    console.print()

    results = workspace.sync_workspace(ws)
    for r in results:
        result_text = r["result"]
        base = r.get("base", "?")
        if result_text == "up to date":
            console.print(f"  [green]✓[/] {r['repo']}  [dim]already up to date[/]")
        elif result_text.startswith("rebased"):
            success(f"{r['repo']}  {result_text}")
        elif result_text == "conflict":
            error(
                f"{r['repo']}  conflict — rebase aborted. "
                f"To retry: cd {ws.path / r['repo']} && git rebase {base}"
            )
        elif result_text.startswith("skipped"):
            warning(f"{r['repo']}  {result_text}")
        else:
            error(f"{r['repo']}  {result_text}")


@app.command()
def run(
    name: str | None = typer.Argument(
        None,
        help="Workspace name (auto-detects from cwd)",
        autocompletion=complete_workspace_name,
    ),
) -> None:
    """Run dev processes across workspace repos (Ctrl+C to stop)."""
    ws = _resolve_workspace(name)

    console.print(f"[bold]Running:[/] {ws.name}")
    console.print()

    count = workspace.run_workspace(ws)
    if count == 0:
        info("No repos have a [bold]run[/] hook in .grove.toml")


_BACK_TO_REPOS = "← back to repos dir"


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

        current_ws = state.find_workspace_by_path(Path.cwd())
        choices = [
            f"{ws.name}  (current)" if current_ws and ws.name == current_ws.name else ws.name
            for ws in workspaces
        ]

        # Offer "back to repos dir" when inside a workspace
        if current_ws:
            choices.append(_BACK_TO_REPOS)

        picked = _pick_one("Select workspace", choices)

        if picked == _BACK_TO_REPOS:
            cfg = config.require_config()
            print(cfg.repos_dir)
            return

        # Strip the "(current)" suffix if present
        name = picked.split("  (current)")[0]

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
        output="$(command gw "$@")"
        local rc=$?
        if [ $rc -eq 0 ] && [ -n "$output" ] && [ -d "$output" ]; then
            cd "$output" || return 1
        else
            echo "$output"
        fi
        return $rc
    fi

    if [ "$1" = "create" ]; then
        local cdfile
        cdfile="$(mktemp "${TMPDIR:-/tmp}/.grove_cd.XXXXXX")"
        GROVE_CD_FILE="$cdfile" command gw "$@"
        local rc=$?
        if [ $rc -eq 0 ] && [ -s "$cdfile" ]; then
            local dir
            dir="$(cat "$cdfile")"
            [ -d "$dir" ] && cd "$dir"
        fi
        rm -f "$cdfile"
        return $rc
    fi

    command gw "$@"
}
"""
