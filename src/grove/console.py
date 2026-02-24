"""Rich console output helpers."""

from __future__ import annotations

from rich.console import Console
from rich.table import Table

console = Console()


def error(msg: str) -> None:
    console.print(f"[bold red]error:[/] {msg}")


def success(msg: str) -> None:
    console.print(f"[bold green]ok:[/] {msg}")


def info(msg: str) -> None:
    console.print(f"[dim]{msg}[/]")


def warning(msg: str) -> None:
    console.print(f"[bold yellow]warn:[/] {msg}")


def make_table(*columns: str) -> Table:
    """Create a styled table with the given column names."""
    table = Table(show_header=True, header_style="bold cyan", border_style="dim")
    for col in columns:
        table.add_column(col)
    return table
