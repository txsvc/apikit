"""Tests for project configuration and package structure (TS-11-1 through TS-11-4).

These tests verify the pyproject.toml configuration, package layout,
public API exports, and absence of PyPI publishing configuration.
"""

from __future__ import annotations

import tomllib
from pathlib import Path

# Root of the sdk-python package (two levels up from tests/)
SDK_ROOT = Path(__file__).resolve().parent.parent
PYPROJECT_PATH = SDK_ROOT / "pyproject.toml"


class TestPyprojectToml:
    """TS-11-1: Verify pyproject.toml declares correct metadata and dependencies."""

    def test_pyproject_exists(self) -> None:
        """pyproject.toml must exist at packages/sdk-python/pyproject.toml."""
        assert PYPROJECT_PATH.exists(), f"pyproject.toml not found at {PYPROJECT_PATH}"

    def test_pyproject_is_valid_toml(self) -> None:
        """pyproject.toml must be parseable as valid TOML."""
        with open(PYPROJECT_PATH, "rb") as f:
            data = tomllib.load(f)
        assert "project" in data

    def test_package_name_is_apikit(self) -> None:
        """project.name must be 'apikit'."""
        with open(PYPROJECT_PATH, "rb") as f:
            data = tomllib.load(f)
        assert data["project"]["name"] == "apikit"

    def test_requires_python_312(self) -> None:
        """requires-python must be '>=3.12'."""
        with open(PYPROJECT_PATH, "rb") as f:
            data = tomllib.load(f)
        assert data["project"]["requires-python"] == ">=3.12"

    def test_httpx_runtime_dependency(self) -> None:
        """httpx>=0.27 must be in project.dependencies."""
        with open(PYPROJECT_PATH, "rb") as f:
            data = tomllib.load(f)
        deps = data["project"]["dependencies"]
        assert any("httpx" in d for d in deps), "httpx not found in dependencies"

    def test_dev_dependency_respx(self) -> None:
        """respx>=0.21 must be in optional-dependencies.dev."""
        with open(PYPROJECT_PATH, "rb") as f:
            data = tomllib.load(f)
        dev_deps = data["project"]["optional-dependencies"]["dev"]
        assert any("respx" in d for d in dev_deps), "respx not found in dev deps"

    def test_dev_dependency_mypy(self) -> None:
        """mypy>=1.10 must be in optional-dependencies.dev."""
        with open(PYPROJECT_PATH, "rb") as f:
            data = tomllib.load(f)
        dev_deps = data["project"]["optional-dependencies"]["dev"]
        assert any("mypy" in d for d in dev_deps), "mypy not found in dev deps"

    def test_dev_dependency_ruff(self) -> None:
        """ruff>=0.4 must be in optional-dependencies.dev."""
        with open(PYPROJECT_PATH, "rb") as f:
            data = tomllib.load(f)
        dev_deps = data["project"]["optional-dependencies"]["dev"]
        assert any("ruff" in d for d in dev_deps), "ruff not found in dev deps"

    def test_dev_dependency_pytest(self) -> None:
        """pytest must be in optional-dependencies.dev."""
        with open(PYPROJECT_PATH, "rb") as f:
            data = tomllib.load(f)
        dev_deps = data["project"]["optional-dependencies"]["dev"]
        assert any("pytest" in d for d in dev_deps), "pytest not found in dev deps"


class TestPackageLayout:
    """TS-11-2: Verify required source and test files exist at correct paths."""

    def test_init_py_exists(self) -> None:
        assert (SDK_ROOT / "src" / "apikit" / "__init__.py").exists()

    def test_client_py_exists(self) -> None:
        assert (SDK_ROOT / "src" / "apikit" / "client.py").exists()

    def test_models_py_exists(self) -> None:
        assert (SDK_ROOT / "src" / "apikit" / "models.py").exists()

    def test_exceptions_py_exists(self) -> None:
        assert (SDK_ROOT / "src" / "apikit" / "exceptions.py").exists()

    def test_tests_init_exists(self) -> None:
        assert (SDK_ROOT / "tests" / "__init__.py").exists()

    def test_tests_test_client_exists(self) -> None:
        assert (SDK_ROOT / "tests" / "test_client.py").exists()

    def test_tests_test_models_exists(self) -> None:
        assert (SDK_ROOT / "tests" / "test_models.py").exists()

    def test_tests_test_exceptions_exists(self) -> None:
        assert (SDK_ROOT / "tests" / "test_exceptions.py").exists()


class TestPublicAPIExports:
    """TS-11-3: Verify that the public API surface is exported from __init__.py."""

    def test_import_client(self) -> None:
        from apikit import Client  # noqa: F401

        assert isinstance(Client, type)

    def test_import_api_error(self) -> None:
        from apikit import APIError  # noqa: F401

        assert isinstance(APIError, type)

    def test_import_user(self) -> None:
        from apikit import User  # noqa: F401

        assert isinstance(User, type)

    def test_import_organization(self) -> None:
        from apikit import Organization  # noqa: F401

        assert isinstance(Organization, type)


class TestNoPublishingConfig:
    """TS-11-4: Verify pyproject.toml contains no PyPI publishing configuration."""

    def test_no_publish_url(self) -> None:
        """No 'publish' key in project.urls."""
        with open(PYPROJECT_PATH, "rb") as f:
            data = tomllib.load(f)
        urls = data.get("project", {}).get("urls", {})
        assert "publish" not in urls, "project.urls should not contain 'publish'"

    def test_no_flit_config(self) -> None:
        """No [tool.flit] section."""
        with open(PYPROJECT_PATH, "rb") as f:
            data = tomllib.load(f)
        assert "flit" not in data.get("tool", {}), "tool.flit should not be present"

    def test_no_twine_config(self) -> None:
        """No [tool.twine] section."""
        with open(PYPROJECT_PATH, "rb") as f:
            data = tomllib.load(f)
        assert "twine" not in data.get("tool", {}), "tool.twine should not be present"
