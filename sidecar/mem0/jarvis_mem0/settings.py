from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
from typing import Any

import yaml


@dataclass(frozen=True)
class Settings:
    owner_id: str
    qdrant_host: str
    qdrant_port: int
    collection: str
    state_dir: Path
    model_base_url: str
    model_api_key: str
    model_name: str
    model_is_reasoning: bool
    embedding_model: str
    embedding_dims: int

    @classmethod
    def from_file(cls, path: str | Path) -> "Settings":
        config_path = Path(path)
        try:
            raw = yaml.safe_load(config_path.read_text(encoding="utf-8"))
        except OSError as exc:
            raise ValueError(f"read config {config_path}: {exc}") from exc
        except yaml.YAMLError as exc:
            raise ValueError(f"parse config {config_path}: {exc}") from exc
        if not isinstance(raw, dict):
            raise ValueError(f"config {config_path} must be a mapping")

        mem0 = _mapping(raw, "mem0")
        model = _mapping(raw, "model")
        state_dir = Path(_string(mem0, "state_dir", "mem0"))
        if not state_dir.is_absolute():
            state_dir = config_path.resolve().parent.parent / state_dir

        return cls(
            owner_id=_string(mem0, "owner_id", "mem0"),
            qdrant_host=_string(mem0, "qdrant_host", "mem0"),
            qdrant_port=_positive_int(mem0, "qdrant_port", "mem0"),
            collection=_string(mem0, "collection", "mem0"),
            state_dir=state_dir.resolve(),
            model_base_url=_string(model, "base_url", "model"),
            model_api_key=_string(model, "api_key", "model"),
            model_name=_string(model, "model", "model"),
            model_is_reasoning=_bool(model, "is_reasoning_model", "model"),
            embedding_model=_string(mem0, "embedding_model", "mem0"),
            embedding_dims=_positive_int(mem0, "embedding_dims", "mem0"),
        )


def _mapping(parent: dict[str, Any], key: str) -> dict[str, Any]:
    value = parent.get(key)
    if not isinstance(value, dict):
        raise ValueError(f"config section {key} must be a mapping")
    return value


def _string(parent: dict[str, Any], key: str, section: str) -> str:
    value = parent.get(key)
    if not isinstance(value, str) or not value.strip():
        raise ValueError(f"config value {section}.{key} must be a non-empty string")
    return value.strip()


def _positive_int(parent: dict[str, Any], key: str, section: str) -> int:
    value = parent.get(key)
    if isinstance(value, bool) or not isinstance(value, int) or value <= 0:
        raise ValueError(f"config value {section}.{key} must be a positive integer")
    return value


def _bool(parent: dict[str, Any], key: str, section: str) -> bool:
    value = parent.get(key)
    if not isinstance(value, bool):
        raise ValueError(f"config value {section}.{key} must be a boolean")
    return value
