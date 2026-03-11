"""CLI commands for Grove."""

from __future__ import annotations

import os
import re
import subprocess
import sys
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
        repos = discover.find_all_repos(cfg.repo_dirs)
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
    Raises ``typer.BadParameter`` if the result is empty.
    """
    name = re.sub(r"[/\s]+", "-", branch).strip("-")
    if not name:
        raise typer.BadParameter(f"Branch name {branch!r} produces an empty workspace name")
    return name


def _validate_repo_names(repo_names: list[str], available: dict[str, Path]) -> dict[str, Path]:
    """Validate repo names against available repos, exit on unknown names."""
    selected: dict[str, Path] = {}
    for rn in repo_names:
        if rn not in available:
            error(f"Repo [bold]{rn}[/] not found")
            info(f"Available: {', '.join(available.keys())}")
            raise typer.Exit(1)
        selected[rn] = available[rn]
    return selected


def _prompt(label: str) -> str:
    """Prompt the user for text input, handling encoding errors gracefully."""
    from rich.prompt import Prompt

    try:
        return Prompt.ask(f"[bold]{label}[/]", console=console)
    except UnicodeDecodeError:
        error("Invalid input — check your terminal encoding")
        raise typer.Exit(1) from None


def _pick_one(prompt_text: str, choices: list[str]) -> str:
    """Arrow-key single selection."""
    return choices[_pick_one_idx(prompt_text, choices)]


def _make_menu(
    choices: list[str],
    title: str,
    *,
    multi_select: bool = False,
    type_to_search: bool = True,
):
    from simple_term_menu import TerminalMenu

    kwargs: dict = {
        "menu_cursor": "❯ ",
        "menu_cursor_style": ("fg_cyan", "bold"),
        "menu_highlight_style": ("fg_cyan", "bold"),
        "search_highlight_style": ("fg_yellow", "bold"),
    }
    if multi_select:
        kwargs.update(
            multi_select=True,
            multi_select_select_on_accept=False,
            multi_select_keys=("tab",),
        )
    if type_to_search:
        kwargs["search_key"] = None
    return TerminalMenu(choices, title=title, **kwargs)


def _show_menu(menu, choices: list[str], fallback_title: str, **kw):
    """Show a menu, falling back to /-triggered search if the library crashes."""
    try:
        return menu.show()
    except (ValueError, OSError):
        # simple_term_menu can crash when search text exceeds terminal width;
        # recover by retrying without live search
        fallback = _make_menu(choices, fallback_title, type_to_search=False, **kw)
        return fallback.show()


def _pick_one_idx(prompt_text: str, choices: list[str]) -> int:
    """Arrow-key single selection with type-to-search, returns the chosen index."""
    if not sys.stdin.isatty():
        error("Interactive selection requires a terminal. Provide explicit flags instead.")
        raise typer.Exit(1)
    title = f"\n{prompt_text}\n  ↑/↓ navigate · type to search · enter confirm"
    fallback_title = f"\n{prompt_text}\n  ↑/↓ navigate · / to search · enter confirm"
    menu = _make_menu(choices, title)
    idx = _show_menu(menu, choices, fallback_title)
    if idx is None:
        raise typer.Abort()
    return idx


def _pick_many(prompt_text: str, choices: list[str]) -> list[str]:
    """Type-to-search + tab multi-selection."""
    if not sys.stdin.isatty():
        error("Interactive selection requires a terminal. Provide explicit flags instead.")
        raise typer.Exit(1)
    display = ["(all)", *choices]
    title = f"\n{prompt_text}\n  ↑/↓ navigate · tab select · type to search · enter confirm"
    fallback_title = f"\n{prompt_text}\n  ↑/↓ navigate · tab select · / to search · enter confirm"
    menu = _make_menu(display, title, multi_select=True)
    result = _show_menu(menu, display, fallback_title, multi_select=True)
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
    dirs: list[str] | None = typer.Argument(  # noqa: B008
        None, help="Directories containing your git repos (optional, can add later)"
    ),
) -> None:
    """Initialize Grove, optionally with repo directories."""
    repo_dirs: list[Path] = []
    if dirs:
        for d in dirs:
            p = Path(d).expanduser().resolve()
            if not p.is_dir():
                error(f"Directory does not exist: {p}")
                raise typer.Exit(1)
            repo_dirs.append(p)

    # Merge with existing config if re-running init
    existing = config.load_config()
    if existing:
        # Add new dirs to existing, avoiding duplicates
        existing_set = {p.resolve() for p in existing.repo_dirs}
        for p in repo_dirs:
            if p.resolve() not in existing_set:
                existing.repo_dirs.append(p)
        config.save_config(existing)
        cfg = existing
    else:
        cfg = config.Config(
            repo_dirs=repo_dirs,
            workspace_dir=config.DEFAULT_WORKSPACE_DIR,
        )
        config.save_config(cfg)
        config.DEFAULT_WORKSPACE_DIR.mkdir(parents=True, exist_ok=True)

    success("Initialized Grove")
    if cfg.repo_dirs:
        repos = discover.find_all_repos(cfg.repo_dirs)
        dirs_label = ", ".join(str(d) for d in cfg.repo_dirs)
        info(f"Repo dirs: {dirs_label}")
        if repos:
            info(f"Found {len(repos)} repos: {', '.join(repos.keys())}")
        else:
            info("No git repos found in configured directories yet")
    else:
        info("No repo dirs configured. Add one with: gw add-dir <path>")


@app.command("add-dir")
def add_dir(
    path: str = typer.Argument(help="Directory containing git repos"),
) -> None:
    """Add a repo source directory."""
    cfg = config.require_config()
    dir_path = Path(path).expanduser().resolve()

    if not dir_path.is_dir():
        error(f"Directory does not exist: {dir_path}")
        raise typer.Exit(1)

    if dir_path in {p.resolve() for p in cfg.repo_dirs}:
        info(f"Directory already configured: {dir_path}")
        return

    cfg.repo_dirs.append(dir_path)
    config.save_config(cfg)

    repos = discover.find_repos(dir_path)
    success(f"Added repo dir: {dir_path}")
    if repos:
        info(f"Found {len(repos)} repos: {', '.join(repos.keys())}")
    else:
        info("No git repos found in that directory")


@app.command("remove-dir")
def remove_dir(
    path: str | None = typer.Argument(None, help="Directory to remove"),
) -> None:
    """Remove a repo source directory."""
    cfg = config.require_config()

    if not cfg.repo_dirs:
        error("No repo dirs configured")
        raise typer.Exit(1)

    if path is None:
        path = _pick_one("Select directory to remove", [str(d) for d in cfg.repo_dirs])

    dir_path = Path(path).expanduser().resolve()
    resolved_dirs = {p.resolve(): p for p in cfg.repo_dirs}
    if dir_path not in resolved_dirs:
        error(f"Directory not configured: {dir_path}")
        raise typer.Exit(1)

    cfg.repo_dirs = [p for p in cfg.repo_dirs if p.resolve() != dir_path]
    config.save_config(cfg)
    success(f"Removed repo dir: {dir_path}")


@app.command()
def explore() -> None:
    """Scan configured directories for git repos (deep search)."""
    cfg = config.require_config()

    if not cfg.repo_dirs:
        error("No repo dirs configured. Run: gw add-dir <path>")
        raise typer.Exit(1)

    discovered = discover.discover_repos(cfg.repo_dirs)

    if not discovered:
        info("No git repos found in configured directories")
        return

    # Group by parent dir for display
    from collections import defaultdict

    by_dir: dict[Path, list[discover.RepoInfo]] = defaultdict(list)
    for r in discovered:
        # Find which repo_dir this belongs to
        parent = r.path.parent
        for d in cfg.repo_dirs:
            resolved_d = d.resolve()
            try:
                r.path.resolve().relative_to(resolved_d)
                parent = d
                break
            except ValueError:
                continue
        by_dir[parent].append(r)

    nested_count = 0
    for source_dir in sorted(by_dir):
        console.print(f"\n[bold]{source_dir}[/]")
        for r in sorted(by_dir[source_dir], key=lambda x: x.display_name):
            is_nested = r.path.parent.resolve() != source_dir.resolve()
            if is_nested:
                nested_count += 1
                console.print(
                    f"  [green]★[/] {r.display_name}  [dim]{r.path}[/]  [green](nested)[/]"
                )
            else:
                console.print(f"    {r.display_name}  [dim]{r.path}[/]")

    console.print()
    info(f"{len(discovered)} repos found" + (f" ({nested_count} nested)" if nested_count else ""))


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
    copy_claude_md: bool | None = typer.Option(
        None, "--copy-claude-md/--no-copy-claude-md", help="Copy CLAUDE.md into workspace"
    ),
) -> None:
    """Create a new workspace with worktrees from selected repos."""
    cfg = config.require_config()

    # --- Interactive fallback when branch is missing ---
    if branch is None:
        if not sys.stdin.isatty():
            error("--branch is required in non-interactive mode")
            raise typer.Exit(1)
        branch = _prompt("Branch name")
        if not branch:
            error("Branch name is required")
            raise typer.Exit(1)

    # --- Resolve repos: -r > -p > --all / deep-scan picker ---
    selected: dict[str, Path] | None = None

    if repos is not None:
        repo_names = [r.strip() for r in repos.split(",")]
    elif preset is not None:
        if preset not in cfg.presets:
            error(f"Preset [bold]{preset}[/] not found in config")
            available_presets = ", ".join(cfg.presets.keys()) if cfg.presets else "(none)"
            info(f"Available presets: {available_presets}")
            raise typer.Exit(1)
        repo_names = cfg.presets[preset]
    elif all_repos:
        available = discover.find_all_repos(cfg.repo_dirs)
        repo_names = list(available.keys())
    else:
        # Interactive picker — deep scan with remote identity
        discovered = discover.discover_repos(cfg.repo_dirs)
        if not discovered:
            error("No repos found. Run: gw add-dir <path>")
            raise typer.Exit(1)

        # Build picker: display_name -> RepoInfo
        repo_by_display = {r.display_name: r for r in discovered}

        # Offer presets when available
        if cfg.presets:
            available = discover.find_all_repos(cfg.repo_dirs)
            preset_names = list(cfg.presets.keys())
            preset_choices = [
                f"{name}  ({', '.join(repos_list)})" for name, repos_list in cfg.presets.items()
            ]
            source_idx = _pick_one_idx(
                "Select repos from",
                [*preset_choices, "Pick manually…"],
            )
            if source_idx == len(preset_choices):
                picked = _pick_many("Select repos", sorted(repo_by_display.keys()))
                selected = {repo_by_display[p].name: repo_by_display[p].path for p in picked}
            else:
                repo_names = cfg.presets[preset_names[source_idx]]
                selected = _validate_repo_names(repo_names, available)
        else:
            picked = _pick_many("Select repos", sorted(repo_by_display.keys()))
            selected = {repo_by_display[p].name: repo_by_display[p].path for p in picked}

        # Offer to save as preset if none exist
        if (
            not cfg.presets
            and selected
            and len(selected) < len(repo_by_display)
            and typer.confirm("Save this selection as a preset?", default=False)
        ):
            preset_name = _prompt("Preset name")
            if preset_name:
                cfg.presets[preset_name] = list(selected.keys())
                config.save_config(cfg)
                success(f"Preset [bold]{preset_name}[/] saved")

    # Validate selected repos (non-interactive paths use folder names)
    if selected is None:
        available = discover.find_all_repos(cfg.repo_dirs)
        selected = _validate_repo_names(repo_names, available)

    # --- Resolve name: explicit > auto-derive from branch ---
    if name is None:
        name = _sanitize_name(branch)

    ws = workspace.create_workspace(name, selected, branch, cfg)
    if ws is None:
        raise typer.Exit(1)

    # --- Copy CLAUDE.md from repos dir if present ---
    claude_md = next((d / "CLAUDE.md" for d in cfg.repo_dirs if (d / "CLAUDE.md").is_file()), None)
    if claude_md is not None:
        should_copy = copy_claude_md
        if should_copy is None:
            should_copy = typer.confirm("Copy CLAUDE.md into workspace?", default=True)
        if should_copy:
            import shutil

            shutil.copy2(claude_md, ws.path / "CLAUDE.md")
            success("CLAUDE.md copied")

    console.print()
    success(f"Workspace [bold]{name}[/] created at {ws.path}")

    # Signal the shell wrapper to cd into the new workspace
    cd_file = os.environ.get("GROVE_CD_FILE")
    if cd_file:
        Path(cd_file).write_text(str(ws.path))


@app.command("list")
def list_workspaces(
    show_status: bool = typer.Option(False, "--status", "-s", help="Include git status summary"),
) -> None:
    """List all workspaces."""
    if show_status:
        summaries = workspace.all_workspaces_summary()
        if not summaries:
            info("No workspaces. Create one with: gw create <name> -r repo1,repo2 -b branch")
            return
        table = make_table("Name", "Branch", "Repos", "Status", "Path")
        for s in summaries:
            table.add_row(s["name"], s["branch"], s["repos"], s["status"], s["path"])
        console.print(table)
        return

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

    if repos is not None:
        available = discover.find_all_repos(cfg.repo_dirs)
        repo_names = [r.strip() for r in repos.split(",")]
        for rn in repo_names:
            if rn not in available:
                error(f"Repo [bold]{rn}[/] not found")
                raise typer.Exit(1)
        selected = {rn: available[rn] for rn in repo_names}
    else:
        # Interactive — deep scan with remote identity
        discovered = discover.discover_repos(cfg.repo_dirs)
        addable = {r.display_name: r for r in discovered if r.name not in existing_names}
        if not addable:
            info("All repos are already in this workspace")
            return
        picked = _pick_many("Select repos to add", sorted(addable.keys()))
        selected = {addable[p].name: addable[p].path for p in picked}
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
        warning("--all is deprecated, use: gw list -s")
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
    """Run dev processes across workspace repos (TUI with sidebar)."""
    ws = _resolve_workspace(name)

    runnable = workspace.get_runnable(ws)
    if not runnable:
        info("No repos have a [bold]run[/] hook in .grove.toml")
        return

    # Pre-run hooks
    workspace.run_pre_hooks(runnable)

    # Build TUI entries: (repo_name, command, cwd)
    entries = [(wt.repo_name, " && ".join(cmds), str(wt.worktree_path)) for wt, cmds in runnable]

    # Launch TUI
    from grove.tui import RunApp

    app = RunApp(entries)
    app.run()

    # Post-run hooks
    workspace.run_post_hooks(runnable)


_BACK_TO_REPOS = "← back to repos dir"


def _resolve_back_path(ws: Workspace) -> Path:
    """Resolve the 'back' destination for a workspace.

    1. Single repo → parent directory of source_repo
    2. Multi-repo, all in same parent dir → that shared parent dir
    3. Multi-repo, different parent dirs → interactive picker
    """
    source_dirs: list[Path] = []
    seen: set[str] = set()
    for repo_wt in ws.repos:
        parent = repo_wt.source_repo.parent.resolve()
        key = str(parent)
        if key not in seen:
            seen.add(key)
            source_dirs.append(parent)

    if not source_dirs:
        error("Workspace has no repos — cannot determine a source directory")
        raise typer.Exit(1)

    if len(source_dirs) == 1:
        return source_dirs[0]

    picked = _pick_one("Select repo directory", [str(d) for d in source_dirs])
    return Path(picked)


def _do_cleanup(ws_name: str) -> None:
    """Spawn a detached subprocess to delete a workspace.

    The subprocess runs independently so the shell function can cd
    immediately without waiting for worktree removal to finish.
    If cleanup fails, ``gw doctor`` will catch the stale state.
    """
    import shutil

    gw_path = shutil.which("gw")
    if gw_path:
        cmd = [gw_path, "delete", "--force", ws_name]
    else:
        cmd = [sys.executable, "-m", "grove", "delete", "--force", ws_name]
    subprocess.Popen(
        cmd,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        start_new_session=True,
    )


@app.command()
def go(
    name: str | None = typer.Argument(
        None,
        help="Workspace name",
        autocompletion=complete_workspace_name,
    ),
    back: bool = typer.Option(False, "--back", "-b", help="Go back to the source repo directory"),
    delete: bool = typer.Option(
        False, "--delete", "-d", help="Delete current workspace after navigating away"
    ),
) -> None:
    """Print workspace path (use with shell function for cd)."""
    if back and name is not None:
        error("--back cannot be combined with a workspace name")
        raise typer.Exit(1)

    current_ws = state.find_workspace_by_path(Path.cwd())

    # --back: go to source repo dir of current workspace
    if back:
        if current_ws is None:
            error("Not inside a workspace")
            raise typer.Exit(1)
        dest = _resolve_back_path(current_ws)
        if delete:
            _do_cleanup(current_ws.name)
        print(dest)
        return

    # Interactive fallback
    if name is None:
        workspaces = state.load_workspaces()
        if not workspaces:
            error("No workspaces. Create one first: gw create ...")
            raise typer.Exit(1)

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
            if len(cfg.repo_dirs) == 1:
                print(cfg.repo_dirs[0])
            elif cfg.repo_dirs:
                picked_dir = _pick_one("Select repo directory", [str(d) for d in cfg.repo_dirs])
                print(picked_dir)
            else:
                error("No repo dirs configured. Run: gw add-dir <path>")
            return

        # Strip the "(current)" suffix if present
        name = picked.split("  (current)")[0]

    ws = state.get_workspace(name)
    if ws is None:
        error(f"Workspace [bold]{name}[/] not found")
        raise typer.Exit(1)

    # --delete: delete current workspace before navigating to target
    if delete:
        if current_ws is None:
            warning("--delete has no effect: not inside a workspace")
        elif current_ws.name == ws.name:
            warning(f"--delete skipped: already in workspace [bold]{ws.name}[/]")
        else:
            _do_cleanup(current_ws.name)

    # Print raw path for shell function to consume
    print(ws.path)


# ---------------------------------------------------------------------------
# Dashboard — agent monitoring
# ---------------------------------------------------------------------------

dash_app = typer.Typer(help="Agent dashboard and hooks.")
app.add_typer(dash_app, name="dash")


@dash_app.callback(invoke_without_command=True)
def dash_main(ctx: typer.Context) -> None:
    """Launch agent dashboard or manage hooks."""
    if ctx.invoked_subcommand is None:
        from grove.dash.app import run_dashboard

        run_dashboard()


@app.command("_hook", hidden=True)
def _hook_entrypoint(
    event: str = typer.Option(..., "--event", help="Hook event type"),
) -> None:
    """Internal hook handler invoked by Claude Code. Not for direct use."""
    import json as _json

    from grove.dash.hook import handle_event

    try:
        input_data = _json.load(sys.stdin)
    except (ValueError, _json.JSONDecodeError):
        return
    handle_event(event, input_data)


@dash_app.command("install")
def dash_install(
    dry_run: bool = typer.Option(False, "--dry-run", help="Show what would change"),
) -> None:
    """Install Claude Code hooks for agent monitoring."""
    from grove.dash.installer import install_hooks

    actions = install_hooks(dry_run=dry_run)
    if dry_run:
        info("Dry run — no changes made")
    else:
        success("Hooks installed into ~/.claude/settings.json")
    for event, acts in actions.items():
        info(f"  {event}: {', '.join(acts)}")


@dash_app.command("uninstall")
def dash_uninstall() -> None:
    """Remove Grove hooks from Claude Code settings."""
    from grove.dash.installer import uninstall_hooks

    removed = uninstall_hooks()
    if removed:
        success(f"Removed {removed} hook(s) from ~/.claude/settings.json")
    else:
        info("No Grove hooks found")


@dash_app.command("status")
def dash_status() -> None:
    """Show a one-line summary of active agents."""
    from grove.dash import manager

    agents, summary = manager.scan()
    if not agents:
        info("No active agents")
        return
    console.print(f"{summary.status_line} | total:{summary.total}")


@dash_app.command("list")
def dash_list() -> None:
    """List all active agents."""
    from grove.dash import manager

    agents, summary = manager.scan()
    if not agents:
        info("No active agents")
        return

    table = make_table("Status", "Project", "Branch", "Tool", "Tools", "Uptime")
    for a in agents:
        from grove.dash.constants import STATUS_STYLES

        style, label = STATUS_STYLES.get(a.status, ("dim", "?"))
        table.add_row(
            f"[{style}]{label}[/]",
            a.display_name or a.session_id[:12],
            a.git_branch or "[dim]—[/]",
            a.last_tool or "[dim]—[/]",
            str(a.tool_count),
            a.uptime or "[dim]—[/]",
        )
    console.print(table)


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
    available = discover.find_all_repos(cfg.repo_dirs)

    if not available:
        error("No repos found. Run: gw add-dir <path>")
        raise typer.Exit(1)

    # Interactive: prompt for name
    if name is None:
        name = _prompt("Preset name")
        if not name:
            error("Preset name is required")
            raise typer.Exit(1)

    # Interactive: pick repos
    if repos is not None:
        repo_names = [r.strip() for r in repos.split(",")]
        for rn in repo_names:
            if rn not in available:
                error(f"Repo [bold]{rn}[/] not found")
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
