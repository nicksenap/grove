"""CLI commands for Grove."""

from __future__ import annotations

from pathlib import Path

import typer

from grove import config, discover, state, workspace
from grove.console import console, error, info, make_table, success

app = typer.Typer(
    name="gw",
    help="Grove — Git Worktree Workspace Orchestrator",
    no_args_is_help=True,
    rich_markup_mode="rich",
)


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
    name: str = typer.Argument(help="Workspace name"),
    repos: str = typer.Option(..., "--repos", "-r", help="Comma-separated repo names"),
    branch: str = typer.Option(..., "--branch", "-b", help="Branch name"),
) -> None:
    """Create a new workspace with worktrees from selected repos."""
    cfg = config.require_config()
    available = discover.find_repos(cfg.repos_dir)

    repo_names = [r.strip() for r in repos.split(",")]
    selected: dict[str, Path] = {}
    for rn in repo_names:
        if rn not in available:
            error(f"Repo [bold]{rn}[/] not found in {cfg.repos_dir}")
            info(f"Available: {', '.join(available.keys())}")
            raise typer.Exit(1)
        selected[rn] = available[rn]

    ws = workspace.create_workspace(name, selected, branch, cfg)
    if ws is None:
        raise typer.Exit(1)

    console.print()
    success(f"Workspace [bold]{name}[/] created at {ws.path}")


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
    name: str = typer.Argument(help="Workspace name to delete"),
    force: bool = typer.Option(False, "--force", "-f", help="Skip confirmation"),
) -> None:
    """Delete a workspace and its worktrees."""
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
    name: str | None = typer.Argument(None, help="Workspace name (auto-detects from cwd)"),
) -> None:
    """Show git status across a workspace's repos."""
    if name is None:
        ws = state.find_workspace_by_path(Path.cwd())
        if ws is None:
            error("Not inside a workspace. Specify a name: gw status <name>")
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
        status_style = "[green]" if r["status"] == "clean" else "[yellow]"
        table.add_row(r["repo"], r["branch"], f"{status_style}{r['status']}[/]")
    console.print(table)


@app.command()
def go(
    name: str = typer.Argument(help="Workspace name"),
) -> None:
    """Print workspace path (use with shell function for cd)."""
    ws = state.get_workspace(name)
    if ws is None:
        error(f"Workspace [bold]{name}[/] not found")
        raise typer.Exit(1)

    # Print raw path for shell function to consume
    print(ws.path)


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
    if [ "$1" = "go" ] && [ -n "$2" ]; then
        local dir
        dir="$(command gw go "$2" 2>/dev/null)"
        if [ -n "$dir" ] && [ -d "$dir" ]; then
            cd "$dir" || return 1
        else
            command gw go "$2"
        fi
    else
        command gw "$@"
    fi
}
"""
