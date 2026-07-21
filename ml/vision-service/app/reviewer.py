"""Deterministic model-review stub.

This module intentionally does not import FastAPI or model libraries. It is a
contract double for the future Qwen/VLM implementation and can be unit-tested
with the Python standard library.
"""

from __future__ import annotations

from typing import Any


MODEL_NAME = "stub-heuristic-vlm"


def review_task(vod: dict[str, Any], task: dict[str, Any]) -> dict[str, Any]:
    task_id = str(task.get("id") or "task")
    window_id = str(task.get("window_id") or task_id)
    prompt_version = str(task.get("prompt_version") or "unknown")
    clip_path = str(task.get("clip_path") or "")
    kind = str(task.get("kind") or "model_review")
    severity = _severity(task)
    category = _category(kind)
    peak_seconds = _number(task.get("peak_seconds"))

    if not clip_path:
        return {
            "id": f"stub_{task_id}",
            "task_id": task_id,
            "window_id": window_id,
            "status": "failed",
            "model": MODEL_NAME,
            "prompt_version": prompt_version,
            "error": "clip_path is required for model review",
        }

    rank = str(vod.get("rank") or "unknown")
    verdict = _verdict(kind, rank, window_id)
    recommendation = _recommendation(kind)
    practice = _practice(kind)

    return {
        "id": f"stub_{task_id}",
        "task_id": task_id,
        "window_id": window_id,
        "status": "completed",
        "model": MODEL_NAME,
        "prompt_version": prompt_version,
        "verdict": verdict,
        "practice": practice,
        "needs_manual_review": True,
        "findings": [
            {
                "category": category,
                "severity": severity,
                "timestamp_seconds": peak_seconds,
                "evidence": f"Stub review for {kind} in {clip_path}. Replace this with Qwen/VLM-visible evidence.",
                "recommendation": recommendation,
                "confidence": 0.42,
            }
        ],
    }


def review_tasks(payload: dict[str, Any]) -> dict[str, Any]:
    vod = payload.get("vod") or {}
    tasks = payload.get("tasks") or []
    return {"runs": [review_task(vod, task) for task in tasks]}


def _category(kind: str) -> str:
    if kind == "combat_spike":
        return "crosshair"
    if kind == "rotation_spike":
        return "rotation"
    if kind == "low_activity":
        return "tempo"
    return "model_review"


def _severity(task: dict[str, Any]) -> str:
    severity = str(task.get("severity") or "medium")
    if severity in {"low", "medium", "high", "critical"}:
        return severity
    return "medium"


def _number(value: Any) -> float:
    try:
        return float(value)
    except (TypeError, ValueError):
        return 0.0


def _verdict(kind: str, rank: str, window_id: str) -> str:
    if kind == "combat_spike":
        return f"{window_id}: review first contact discipline for the {rank} VOD before trusting the heuristic label."
    if kind == "rotation_spike":
        return f"{window_id}: review whether the rotation follows visible minimap information and teammate spacing."
    if kind == "low_activity":
        return f"{window_id}: review whether the hold gained information or only lost tempo."
    return f"{window_id}: manual model review is required."


def _recommendation(kind: str) -> str:
    if kind == "combat_spike":
        return "Pause three seconds before contact and verify crosshair height, angle isolation, tradeability, and utility usage."
    if kind == "rotation_spike":
        return "Check the route against minimap state, teammate distance, sound discipline, and objective timing."
    if kind == "low_activity":
        return "Name the information that justifies waiting; if none exists, rotate, regroup, or take space earlier."
    return "Inspect the clip manually and replace this stub result with a Qwen/VLM-backed finding."


def _practice(kind: str) -> str:
    if kind == "combat_spike":
        return "Run a duel-review loop for three fight clips and write one correction before requeueing."
    if kind == "rotation_spike":
        return "Review two rotation clips and mark whether each route was timed with visible team info."
    if kind == "low_activity":
        return "Review two low-tempo clips and decide what information was gained or missed."
    return "Review the selected clip and add a manual note."
