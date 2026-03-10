"""Tests for the hook installer."""

from __future__ import annotations

import json
from pathlib import Path

import pytest

from grove.dash.installer import (
    install_hooks,
    is_installed,
    uninstall_hooks,
)


@pytest.fixture
def claude_settings(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> Path:
    """Override CLAUDE_SETTINGS to use a temp file."""
    import grove.dash.installer as installer_mod

    settings_path = tmp_path / ".claude" / "settings.json"
    monkeypatch.setattr(installer_mod, "CLAUDE_SETTINGS", settings_path)
    return settings_path


class TestInstallHooks:
    def test_install_creates_settings(self, claude_settings: Path) -> None:
        actions = install_hooks()
        assert claude_settings.exists()

        settings = json.loads(claude_settings.read_text())
        assert "hooks" in settings
        assert "PreToolUse" in settings["hooks"]
        assert "SessionStart" in settings["hooks"]

        # All events should be "added"
        for acts in actions.values():
            assert "added" in acts

    def test_install_preserves_existing_hooks(self, claude_settings: Path) -> None:
        claude_settings.parent.mkdir(parents=True, exist_ok=True)
        existing = {
            "hooks": {
                "PreToolUse": [
                    {"matcher": "", "hooks": [{"type": "command", "command": "my-custom-hook"}]}
                ]
            },
            "other_setting": True,
        }
        claude_settings.write_text(json.dumps(existing))

        install_hooks()
        settings = json.loads(claude_settings.read_text())

        # Custom hook rule should still be there
        pre_rules = settings["hooks"]["PreToolUse"]
        assert len(pre_rules) == 2
        all_cmds = [h["command"] for rule in pre_rules for h in rule.get("hooks", [])]
        assert "my-custom-hook" in all_cmds
        assert any("grove.dash" in c for c in all_cmds)

        # Other settings preserved
        assert settings["other_setting"] is True

    def test_install_updates_existing_grove_hook(self, claude_settings: Path) -> None:
        install_hooks()
        # Install again — should update, not duplicate
        actions = install_hooks()

        settings = json.loads(claude_settings.read_text())
        pre_rules = settings["hooks"]["PreToolUse"]
        grove_rules = [
            rule
            for rule in pre_rules
            if any("grove.dash" in h.get("command", "") for h in rule.get("hooks", []))
        ]
        assert len(grove_rules) == 1

        for acts in actions.values():
            assert "updated" in acts

    def test_install_creates_backup(self, claude_settings: Path) -> None:
        claude_settings.parent.mkdir(parents=True, exist_ok=True)
        claude_settings.write_text("{}")

        install_hooks()
        backups = list(claude_settings.parent.glob("settings.bak.*"))
        assert len(backups) == 1

    def test_dry_run_no_changes(self, claude_settings: Path) -> None:
        actions = install_hooks(dry_run=True)
        assert not claude_settings.exists()
        assert len(actions) > 0


class TestUninstallHooks:
    def test_uninstall_removes_grove_hooks(self, claude_settings: Path) -> None:
        install_hooks()
        assert is_installed()

        removed = uninstall_hooks()
        assert removed > 0
        assert not is_installed()

    def test_uninstall_preserves_other_hooks(self, claude_settings: Path) -> None:
        claude_settings.parent.mkdir(parents=True, exist_ok=True)
        settings = {
            "hooks": {
                "PreToolUse": [
                    {"matcher": "", "hooks": [{"type": "command", "command": "my-custom-hook"}]},
                    {"matcher": "", "hooks": [{"type": "command", "command": "grove.dash x"}]},
                ]
            }
        }
        claude_settings.write_text(json.dumps(settings))

        uninstall_hooks()
        result = json.loads(claude_settings.read_text())
        pre_rules = result["hooks"]["PreToolUse"]
        assert len(pre_rules) == 1
        assert pre_rules[0]["hooks"][0]["command"] == "my-custom-hook"

    def test_uninstall_no_settings(self, claude_settings: Path) -> None:
        assert uninstall_hooks() == 0


class TestIsInstalled:
    def test_not_installed(self, claude_settings: Path) -> None:
        assert not is_installed()

    def test_installed(self, claude_settings: Path) -> None:
        install_hooks()
        assert is_installed()
