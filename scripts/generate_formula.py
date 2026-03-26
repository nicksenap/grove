#!/usr/bin/env python3
"""Generate a Homebrew formula for grove with all Python resource blocks.

Usage:
    pip install .
    python3 scripts/generate_formula.py <tarball_url> <tarball_sha256>

Reads installed packages from the current environment, fetches artifact
URLs and SHA256s from PyPI (preferring wheels over sdists), and writes
a complete Formula/grove.rb to stdout.
"""

from __future__ import annotations

import json
import subprocess
import sys
import urllib.request

SKIP = {"grove", "gw-cli", "pip", "setuptools", "wheel", "build", "pyproject-hooks"}

TEMPLATE = """\
class Grove < Formula
  include Language::Python::Virtualenv

  desc "Git Worktree Workspace Orchestrator"
  homepage "https://github.com/nicksenap/grove"
  url "{tarball_url}"
  sha256 "{tarball_sha}"
  license "MIT"

  depends_on "python@3.12"

{resources}

  def install
    virtualenv_install_with_resources
  end

  def caveats
    <<~EOS
      Add shell integration to your shell profile for `gw go` to work:
        eval "$(gw shell-init)"
    EOS
  end

  test do
    assert_match "Usage", shell_output("#{{bin}}/gw --help")
  end
end
"""


def _get_installed_packages() -> list[dict[str, str]]:
    result = subprocess.run(
        [sys.executable, "-m", "pip", "list", "--format=json"],
        capture_output=True,
        text=True,
        check=True,
    )
    return json.loads(result.stdout)


def _pypi_artifact(name: str, version: str) -> tuple[str, str]:
    """Return (url, sha256) for a PyPI artifact of *name*==*version*.

    Prefers pure-Python wheels (no build deps needed) over sdists.
    """
    api_url = f"https://pypi.org/pypi/{name}/{version}/json"
    with urllib.request.urlopen(api_url) as resp:
        data = json.loads(resp.read())

    # Prefer a pure-Python wheel (py3-none-any) — no build deps needed
    for entry in data["urls"]:
        if entry["packagetype"] == "bdist_wheel" and entry["filename"].endswith("-py3-none-any.whl"):
            return entry["url"], entry["digests"]["sha256"]

    # Fall back to sdist
    for entry in data["urls"]:
        if entry["packagetype"] == "sdist":
            return entry["url"], entry["digests"]["sha256"]

    raise RuntimeError(f"No artifact found for {name}=={version}")


def main() -> None:
    if len(sys.argv) != 3:
        print(f"Usage: {sys.argv[0]} <tarball_url> <tarball_sha256>", file=sys.stderr)
        sys.exit(1)

    tarball_url, tarball_sha = sys.argv[1], sys.argv[2]

    packages = _get_installed_packages()
    blocks: list[str] = []
    for pkg in sorted(packages, key=lambda p: p["name"].lower()):
        name, ver = pkg["name"], pkg["version"]
        if name.lower() in SKIP:
            continue
        url, sha = _pypi_artifact(name, ver)
        blocks.append(f'  resource "{name}" do\n    url "{url}"\n    sha256 "{sha}"\n  end')

    print(
        TEMPLATE.format(
            tarball_url=tarball_url,
            tarball_sha=tarball_sha,
            resources="\n\n".join(blocks),
        ),
        end="",
    )


if __name__ == "__main__":
    main()
