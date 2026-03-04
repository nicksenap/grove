"""Tests for grove.config — TOML save/load with presets."""

from __future__ import annotations

import tomllib

import pytest

from grove import config
from grove.models import Config


class TestSaveLoad:
    def test_save_and_load_basic(self, tmp_grove):
        cfg = Config(
            repos_dir=tmp_grove["repos_dir"],
            workspace_dir=tmp_grove["workspace_dir"],
        )
        config.save_config(cfg)
        loaded = config.load_config()
        assert loaded is not None
        assert loaded.repos_dir == cfg.repos_dir
        assert loaded.workspace_dir == cfg.workspace_dir
        assert loaded.presets == {}

    def test_save_and_load_with_presets(self, tmp_grove):
        cfg = Config(
            repos_dir=tmp_grove["repos_dir"],
            workspace_dir=tmp_grove["workspace_dir"],
            presets={
                "backend": ["svc-auth", "svc-api"],
                "frontend": ["web-app", "design-system"],
            },
        )
        config.save_config(cfg)
        loaded = config.load_config()
        assert loaded is not None
        assert loaded.presets == {
            "backend": ["svc-auth", "svc-api"],
            "frontend": ["web-app", "design-system"],
        }

    def test_saved_toml_is_valid(self, tmp_grove):
        cfg = Config(
            repos_dir=tmp_grove["repos_dir"],
            workspace_dir=tmp_grove["workspace_dir"],
            presets={"test": ["a", "b"]},
        )
        config.save_config(cfg)
        # Verify the file is valid TOML
        with open(tmp_grove["config_path"], "rb") as f:
            data = tomllib.load(f)
        assert data["presets"]["test"]["repos"] == ["a", "b"]

    def test_load_returns_none_when_missing(self, tmp_grove):
        tmp_grove["config_path"].unlink()
        assert config.load_config() is None


class TestPresetNameValidation:
    def test_valid_names(self):
        for name in ("backend", "front-end", "my_preset", "v2", "A-Z_0-9"):
            config.validate_preset_name(name)  # should not raise

    def test_dots_rejected(self):
        with pytest.raises(ValueError, match="Invalid preset name"):
            config.validate_preset_name("backend.v2")

    def test_spaces_rejected(self):
        with pytest.raises(ValueError, match="Invalid preset name"):
            config.validate_preset_name("my preset")

    def test_brackets_rejected(self):
        with pytest.raises(ValueError, match="Invalid preset name"):
            config.validate_preset_name("bad]name")

    def test_save_rejects_bad_preset_name(self, tmp_grove):
        cfg = Config(
            repos_dir=tmp_grove["repos_dir"],
            workspace_dir=tmp_grove["workspace_dir"],
            presets={"backend.v2": ["svc-auth"]},
        )
        with pytest.raises(ValueError, match="Invalid preset name"):
            config.save_config(cfg)


class TestRequireConfig:
    def test_raises_when_not_initialized(self, tmp_grove):
        tmp_grove["config_path"].unlink()
        with pytest.raises(SystemExit, match="not initialized"):
            config.require_config()

    def test_returns_config_when_exists(self, tmp_grove):
        cfg = config.require_config()
        assert cfg is not None
        assert cfg.repos_dir == tmp_grove["repos_dir"]
