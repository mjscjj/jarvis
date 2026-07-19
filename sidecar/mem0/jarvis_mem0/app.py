from __future__ import annotations

import os
from typing import Any

from fastapi import FastAPI, HTTPException
from pydantic import BaseModel, ConfigDict, Field

from .backend import Backend, build_backend
from .settings import Settings


class AddRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    messages: str = Field(min_length=1)
    metadata: dict[str, Any] | None = None
    infer: bool = True


class SearchRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    query: str = Field(min_length=1)
    filters: dict[str, Any] | None = None
    top_k: int = Field(default=20, gt=0)
    threshold: float = Field(default=0.1, ge=0, le=1)
    rerank: bool = False


def create_app(backend: Backend | None = None, settings: Settings | None = None) -> FastAPI:
    if backend is None:
        config_path = os.environ.get("JARVIS_CONFIG", "conf/config.yaml")
        settings = settings or Settings.from_file(config_path)
        backend = build_backend(settings)

    app = FastAPI(title="Jarvis mem0 sidecar", version="0.1.0")

    @app.post("/memories")
    def add_memories(request: AddRequest) -> dict[str, Any]:
        try:
            results = backend.add(request.messages, request.metadata, request.infer)
        except Exception as exc:
            raise HTTPException(status_code=502, detail=str(exc)) from exc
        return {"results": results}

    @app.post("/memories/search")
    def search_memories(request: SearchRequest) -> dict[str, Any]:
        try:
            results = backend.search(
                request.query,
                request.filters,
                request.top_k,
                request.threshold,
                request.rerank,
            )
        except Exception as exc:
            raise HTTPException(status_code=502, detail=str(exc)) from exc
        return {"results": results}

    @app.delete("/memories/{memory_id}")
    def delete_memory(memory_id: str) -> dict[str, str]:
        try:
            backend.delete(memory_id)
        except Exception as exc:
            raise HTTPException(status_code=502, detail=str(exc)) from exc
        return {"deleted": memory_id}

    @app.post("/memories/delete_all")
    def delete_all_memories() -> dict[str, bool]:
        try:
            backend.delete_all()
        except Exception as exc:
            raise HTTPException(status_code=502, detail=str(exc)) from exc
        return {"deleted": True}

    @app.get("/health")
    def health() -> dict[str, str]:
        try:
            backend.health()
        except Exception as exc:
            raise HTTPException(status_code=503, detail=str(exc)) from exc
        return {"status": "ok"}

    return app
