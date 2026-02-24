"""Tests for grove.cli — Typer CliRunner for commands."""

from __future__ import annotations

from unittest.mock import patch

from typer.testing import CliRunner

from grove.cli import _sanitize_name, app
from grove.models import Config, Workspace

runner = CliRunner()


class TestVersion:
    def test_version_flag(self):
        from grove import __version__

        result = runner.invoke(app, ["--version"])
        assert result.exit_code == 0
        assert f"gw {__version__}" in result.output


class TestSanitizeName:
    def test_slash_to_dash(self):
        assert _sanitize_name("feat/login") == "feat-login"

    def test_multiple_slashes(self):
        assert _sanitize_name("feat/auth/login") == "feat-auth-login"

    def test_spaces_to_dash(self):
        assert _sanitize_name("my feature") == "my-feature"

    def test_mixed(self):
        assert _sanitize_name("feat/my feature") == "feat-my-feature"

    def test_plain(self):
        assert _sanitize_name("bugfix") == "bugfix"

    def test_trailing_slash(self):
        assert _sanitize_name("feat/") == "feat"

    def test_leading_slash(self):
        assert _sanitize_name("/feat") == "feat"


class TestInit:
    def test_success(self, tmp_grove):
        repos_dir = tmp_grove["repos_dir"]
        result = runner.invoke(app, ["init", str(repos_dir)])
        assert result.exit_code == 0
        assert "Initialized" in result.output

    def test_nonexistent_dir(self, tmp_grove):
        result = runner.invoke(app, ["init", "/nonexistent/path"])
        assert result.exit_code == 1
        assert "does not exist" in result.output


class TestCreate:
    def _make_config(self, tmp_grove):
        return Config(
            repos_dir=tmp_grove["repos_dir"],
            workspace_dir=tmp_grove["workspace_dir"],
        )

    def test_with_all_args(self, tmp_grove, fake_repos):
        ws_path = tmp_grove["workspace_dir"] / "my-ws"
        mock_ws = Workspace(
            name="my-ws",
            path=ws_path,
            branch="feat/test",
            repos=[],
        )

        with (
            patch("grove.cli.config.require_config") as mock_cfg,
            patch("grove.cli.discover.find_repos") as mock_find,
            patch("grove.cli.workspace.create_workspace", return_value=mock_ws),
        ):
            mock_cfg.return_value = self._make_config(tmp_grove)
            mock_find.return_value = fake_repos
            result = runner.invoke(
                app, ["create", "my-ws", "-r", "svc-auth,svc-api", "-b", "feat/test"]
            )
            assert result.exit_code == 0
            assert "my-ws" in result.output

    def test_auto_name_from_branch(self, tmp_grove, fake_repos):
        ws_path = tmp_grove["workspace_dir"] / "feat-login"
        mock_ws = Workspace(name="feat-login", path=ws_path, branch="feat/login", repos=[])

        with (
            patch("grove.cli.config.require_config") as mock_cfg,
            patch("grove.cli.discover.find_repos") as mock_find,
            patch("grove.cli.workspace.create_workspace", return_value=mock_ws) as mock_create,
        ):
            mock_cfg.return_value = self._make_config(tmp_grove)
            mock_find.return_value = fake_repos
            result = runner.invoke(app, ["create", "-r", "svc-auth", "-b", "feat/login"])
            assert result.exit_code == 0
            # Name should be auto-derived
            mock_create.assert_called_once()
            assert mock_create.call_args[0][0] == "feat-login"

    def test_preset_flag(self, tmp_grove, fake_repos):
        cfg = self._make_config(tmp_grove)
        cfg.presets = {"backend": ["svc-auth", "svc-api"]}

        ws_path = tmp_grove["workspace_dir"] / "feat-x"
        mock_ws = Workspace(name="feat-x", path=ws_path, branch="feat/x", repos=[])

        with (
            patch("grove.cli.config.require_config", return_value=cfg),
            patch("grove.cli.discover.find_repos", return_value=fake_repos),
            patch("grove.cli.workspace.create_workspace", return_value=mock_ws) as mock_create,
        ):
            result = runner.invoke(app, ["create", "-p", "backend", "-b", "feat/x"])
            assert result.exit_code == 0
            # Should select preset repos
            selected = mock_create.call_args[0][1]
            assert set(selected.keys()) == {"svc-auth", "svc-api"}

    def test_invalid_preset(self, tmp_grove, fake_repos):
        cfg = self._make_config(tmp_grove)
        cfg.presets = {}

        with (
            patch("grove.cli.config.require_config", return_value=cfg),
            patch("grove.cli.discover.find_repos", return_value=fake_repos),
        ):
            result = runner.invoke(app, ["create", "-p", "nope", "-b", "feat/x"])
            assert result.exit_code == 1
            assert "not found" in result.output

    def test_go_flag_prints_sentinel(self, tmp_grove, fake_repos):
        ws_path = tmp_grove["workspace_dir"] / "feat-go"
        mock_ws = Workspace(name="feat-go", path=ws_path, branch="feat/go", repos=[])

        with (
            patch("grove.cli.config.require_config") as mock_cfg,
            patch("grove.cli.discover.find_repos") as mock_find,
            patch("grove.cli.workspace.create_workspace", return_value=mock_ws),
        ):
            mock_cfg.return_value = self._make_config(tmp_grove)
            mock_find.return_value = fake_repos
            result = runner.invoke(app, ["create", "-r", "svc-auth", "-b", "feat/go", "--go"])
            assert result.exit_code == 0
            assert f"__grove_cd:{ws_path}" in result.output

    def test_create_failure(self, tmp_grove, fake_repos):
        with (
            patch("grove.cli.config.require_config") as mock_cfg,
            patch("grove.cli.discover.find_repos") as mock_find,
            patch("grove.cli.workspace.create_workspace", return_value=None),
        ):
            mock_cfg.return_value = self._make_config(tmp_grove)
            mock_find.return_value = fake_repos
            result = runner.invoke(app, ["create", "x", "-r", "svc-auth", "-b", "feat/fail"])
            assert result.exit_code == 1

    def test_invalid_repo(self, tmp_grove, fake_repos):
        with (
            patch("grove.cli.config.require_config") as mock_cfg,
            patch("grove.cli.discover.find_repos") as mock_find,
        ):
            mock_cfg.return_value = self._make_config(tmp_grove)
            mock_find.return_value = fake_repos
            result = runner.invoke(app, ["create", "x", "-r", "nonexistent", "-b", "feat/x"])
            assert result.exit_code == 1
            assert "not found" in result.output


class TestList:
    def test_empty(self, tmp_grove):
        result = runner.invoke(app, ["list"])
        assert result.exit_code == 0
        assert "No workspaces" in result.output

    def test_with_workspaces(self, tmp_grove, sample_workspace):
        from grove import state

        state.add_workspace(sample_workspace)
        result = runner.invoke(app, ["list"])
        assert result.exit_code == 0
        assert "test-ws" in result.output


class TestDelete:
    def test_success(self, tmp_grove, sample_workspace):
        from grove import state

        state.add_workspace(sample_workspace)
        with patch("grove.cli.workspace.delete_workspace", return_value=True):
            result = runner.invoke(app, ["delete", "test-ws", "--force"])
        assert result.exit_code == 0
        assert "deleted" in result.output

    def test_not_found(self, tmp_grove):
        result = runner.invoke(app, ["delete", "nope", "--force"])
        assert result.exit_code == 1
        assert "not found" in result.output


class TestStatus:
    def test_with_name(self, tmp_grove, sample_workspace):
        from grove import state

        state.add_workspace(sample_workspace)
        with (
            patch("grove.cli.workspace.workspace_status") as mock_status,
        ):
            mock_status.return_value = [
                {"repo": "svc-auth", "branch": "feat/test", "status": "clean"},
            ]
            result = runner.invoke(app, ["status", "test-ws"])
        assert result.exit_code == 0
        assert "clean" in result.output

    def test_verbose_shows_details(self, tmp_grove, sample_workspace):
        from grove import state

        state.add_workspace(sample_workspace)
        with (
            patch("grove.cli.workspace.workspace_status") as mock_status,
        ):
            mock_status.return_value = [
                {"repo": "svc-auth", "branch": "feat/test", "status": " M file.py\n?? new.txt"},
            ]
            result = runner.invoke(app, ["status", "test-ws", "-V"])
        assert result.exit_code == 0
        assert "2 changed" in result.output
        assert "file.py" in result.output

    def test_not_found(self, tmp_grove):
        result = runner.invoke(app, ["status", "nope"])
        assert result.exit_code == 1
        assert "not found" in result.output


class TestGo:
    def test_success(self, tmp_grove, sample_workspace):
        from grove import state

        state.add_workspace(sample_workspace)
        result = runner.invoke(app, ["go", "test-ws"])
        assert result.exit_code == 0
        assert str(sample_workspace.path) in result.output

    def test_not_found(self, tmp_grove):
        result = runner.invoke(app, ["go", "nope"])
        assert result.exit_code == 1
        assert "not found" in result.output


class TestPreset:
    def _make_config(self, tmp_grove):
        return Config(
            repos_dir=tmp_grove["repos_dir"],
            workspace_dir=tmp_grove["workspace_dir"],
        )

    def test_add_with_flags(self, tmp_grove, fake_repos):
        with (
            patch("grove.cli.config.require_config") as mock_cfg,
            patch("grove.cli.discover.find_repos", return_value=fake_repos),
            patch("grove.cli.config.save_config") as mock_save,
        ):
            cfg = self._make_config(tmp_grove)
            mock_cfg.return_value = cfg
            result = runner.invoke(
                app, ["preset", "add", "backend", "-r", "svc-auth,svc-api"]
            )
            assert result.exit_code == 0
            assert "backend" in result.output
            mock_save.assert_called_once()
            assert cfg.presets["backend"] == ["svc-auth", "svc-api"]

    def test_add_invalid_repo(self, tmp_grove, fake_repos):
        with (
            patch("grove.cli.config.require_config") as mock_cfg,
            patch("grove.cli.discover.find_repos", return_value=fake_repos),
        ):
            mock_cfg.return_value = self._make_config(tmp_grove)
            result = runner.invoke(
                app, ["preset", "add", "bad", "-r", "nonexistent"]
            )
            assert result.exit_code == 1
            assert "not found" in result.output

    def test_list_empty(self, tmp_grove):
        with patch("grove.cli.config.require_config") as mock_cfg:
            mock_cfg.return_value = self._make_config(tmp_grove)
            result = runner.invoke(app, ["preset", "list"])
            assert result.exit_code == 0
            assert "No presets" in result.output

    def test_list_with_presets(self, tmp_grove):
        with patch("grove.cli.config.require_config") as mock_cfg:
            cfg = self._make_config(tmp_grove)
            cfg.presets = {"backend": ["svc-auth", "svc-api"]}
            mock_cfg.return_value = cfg
            result = runner.invoke(app, ["preset", "list"])
            assert result.exit_code == 0
            assert "backend" in result.output
            assert "svc-auth" in result.output

    def test_remove(self, tmp_grove):
        with (
            patch("grove.cli.config.require_config") as mock_cfg,
            patch("grove.cli.config.save_config") as mock_save,
        ):
            cfg = self._make_config(tmp_grove)
            cfg.presets = {"backend": ["svc-auth"], "frontend": ["web-app"]}
            mock_cfg.return_value = cfg
            result = runner.invoke(app, ["preset", "remove", "backend"])
            assert result.exit_code == 0
            assert "removed" in result.output
            assert "backend" not in cfg.presets
            mock_save.assert_called_once()

    def test_remove_not_found(self, tmp_grove):
        with patch("grove.cli.config.require_config") as mock_cfg:
            cfg = self._make_config(tmp_grove)
            cfg.presets = {"backend": ["svc-auth"]}
            mock_cfg.return_value = cfg
            result = runner.invoke(app, ["preset", "remove", "nope"])
            assert result.exit_code == 1
            assert "not found" in result.output


class TestShellInit:
    def test_prints_function(self, tmp_grove):
        result = runner.invoke(app, ["shell-init"])
        assert result.exit_code == 0
        assert "gw()" in result.output
        assert "__grove_cd:" in result.output
