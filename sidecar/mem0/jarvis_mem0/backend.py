from __future__ import annotations

import os
import threading
from typing import Any, Protocol

from .settings import Settings


class Backend(Protocol):
    def add(self, messages: str, metadata: dict[str, Any] | None, infer: bool) -> list[Any]: ...

    def search(
        self,
        query: str,
        filters: dict[str, Any] | None,
        top_k: int,
        threshold: float,
        rerank: bool,
    ) -> list[Any]: ...

    def delete(self, memory_id: str) -> None: ...

    def delete_all(self) -> None: ...

    def health(self) -> None: ...


class Mem0Backend:
    def __init__(self, memory: Any, owner_id: str):
        self._memory = memory
        self._owner_id = owner_id
        self._lock = threading.Lock()

    def add(self, messages: str, metadata: dict[str, Any] | None, infer: bool) -> list[Any]:
        with self._lock:
            response = self._memory.add(
                messages,
                user_id=self._owner_id,
                metadata=metadata,
                infer=infer,
            )
        return _results(response, "add")

    def search(
        self,
        query: str,
        filters: dict[str, Any] | None,
        top_k: int,
        threshold: float,
        rerank: bool,
    ) -> list[Any]:
        scoped_filters = {**(filters or {}), "user_id": self._owner_id}
        with self._lock:
            response = self._memory.search(
                query,
                filters=scoped_filters,
                top_k=top_k,
                threshold=threshold,
                rerank=rerank,
            )
        return _results(response, "search")

    def delete(self, memory_id: str) -> None:
        with self._lock:
            self._memory.delete(memory_id)

    def delete_all(self) -> None:
        with self._lock:
            self._memory.delete_all(user_id=self._owner_id)

    def health(self) -> None:
        with self._lock:
            self._memory.vector_store.client.get_collections()


def build_backend(settings: Settings) -> Mem0Backend:
    settings.state_dir.mkdir(parents=True, exist_ok=True)
    os.environ["MEM0_DIR"] = str(settings.state_dir)
    os.environ["MEM0_TELEMETRY"] = "false"

    # Import only after MEM0_DIR is fixed. mem0 creates its state directory at
    # import time, so a top-level import would violate the configured boundary.
    from mem0 import Memory

    return Mem0Backend(Memory.from_config(build_mem0_config(settings)), settings.owner_id)


def build_mem0_config(settings: Settings) -> dict[str, Any]:
    return {
        "vector_store": {
            "provider": "qdrant",
            "config": {
                "collection_name": settings.collection,
                "embedding_model_dims": settings.embedding_dims,
                "host": settings.qdrant_host,
                "port": settings.qdrant_port,
                "on_disk": True,
            },
        },
        "llm": {
            "provider": "openai",
            "config": {
                "model": settings.model_name,
                "api_key": settings.model_api_key,
                "openai_base_url": settings.model_base_url,
                "temperature": 0.1,
            },
        },
        "embedder": {
            "provider": "openai",
            "config": {
                "model": settings.embedding_model,
                "api_key": settings.model_api_key,
                "openai_base_url": settings.model_base_url,
                "embedding_dims": settings.embedding_dims,
            },
        },
        "history_db_path": str(settings.state_dir / "history.db"),
        "custom_instructions": (
            "只提取长期有价值的工作事实：明确交办、行动线索、项目决策、截止时间、职责关系、"
            "阻塞点和稳定偏好。忽略寒暄、表情、验证码、自动告警和无上下文的临时状态。"
        ),
    }


def _results(response: Any, operation: str) -> list[Any]:
    if isinstance(response, dict):
        response = response.get("results")
    if not isinstance(response, list):
        raise ValueError(f"mem0 {operation} response must contain a results list")
    return response
