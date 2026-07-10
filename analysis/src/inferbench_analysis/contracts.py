"""Pinned serving-contracts bundle access.

The analysis half validates everything it consumes (manifests, raw events,
SLO and cost-profile instances) and everything it emits (benchmark-result)
against the PINNED contracts bundle. inferbench owns no schema; the bundle
location is given explicitly (CLI --bundle or the CONTRACTS_BUNDLE env var),
never guessed.
"""

from __future__ import annotations

import json
import os
from functools import lru_cache
from pathlib import Path

import jsonschema

from .errors import LoaderError

ENV_BUNDLE = "CONTRACTS_BUNDLE"


class Bundle:
    """A serving-contracts bundle directory (schemas/*.schema.json)."""

    def __init__(self, root: str | os.PathLike[str]):
        self.root = Path(root)
        schemas = self.root / "schemas"
        if not schemas.is_dir():
            raise LoaderError(
                f"not a contracts bundle (no schemas/ directory): {self.root}"
            )

    @classmethod
    def from_env(cls) -> "Bundle":
        root = os.environ.get(ENV_BUNDLE)
        if not root:
            raise LoaderError(
                f"contracts bundle not configured: pass --bundle or set ${ENV_BUNDLE}"
            )
        return cls(root)

    @lru_cache(maxsize=None)
    def schema(self, name: str) -> dict:
        path = self.root / "schemas" / f"{name}.schema.json"
        if not path.is_file():
            raise LoaderError(f"schema '{name}' not in bundle: {path}")
        with open(path, encoding="utf-8") as f:
            return json.load(f)

    @lru_cache(maxsize=None)
    def validator(self, name: str) -> jsonschema.Draft202012Validator:
        schema = self.schema(name)
        jsonschema.Draft202012Validator.check_schema(schema)
        return jsonschema.Draft202012Validator(
            schema, format_checker=jsonschema.FormatChecker()
        )

    def validate(self, name: str, instance: object, *, context: str = "") -> None:
        """Raise LoaderError with the first violation, or return silently."""
        errors = sorted(
            self.validator(name).iter_errors(instance), key=lambda e: e.json_path
        )
        if errors:
            e = errors[0]
            where = f" at {e.json_path}" if e.json_path != "$" else ""
            ctx = f"{context}: " if context else ""
            raise LoaderError(
                f"{ctx}schema-invalid against '{name}'{where}: {e.message}"
            )
