from __future__ import annotations

from typing import Any

from fastapi import FastAPI

from app.reviewer import MODEL_NAME, review_tasks


app = FastAPI(title="Valorant VOD Coach Vision Service", version="0.1.0")


@app.get("/health")
def health() -> dict[str, Any]:
    return {
        "status": "ok",
        "model": MODEL_NAME,
        "mode": "stub",
    }


@app.post("/v1/model-review")
def model_review(payload: dict[str, Any]) -> dict[str, Any]:
    return review_tasks(payload)
