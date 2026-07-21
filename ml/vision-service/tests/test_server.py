from __future__ import annotations

import json
import unittest

from app.server import build_response


class ServerTest(unittest.TestCase):
    def test_health_response(self) -> None:
        status, payload = build_response("GET", "/health", b"")

        self.assertEqual(status, 200)
        self.assertEqual(payload["status"], "ok")
        self.assertEqual(payload["runtime"], "stdlib-http")

    def test_model_review_response(self) -> None:
        raw = json.dumps(
            {
                "vod": {"label": "iron_spudbud_01", "rank": "iron"},
                "tasks": [
                    {
                        "id": "vlm_01",
                        "window_id": "window_01",
                        "kind": "combat_spike",
                        "severity": "high",
                        "prompt_version": "vlm-review-v1",
                        "clip_path": "clips/window_01.mp4",
                        "peak_seconds": 42,
                    }
                ],
            }
        ).encode("utf-8")

        status, payload = build_response("POST", "/v1/model-review", raw)

        self.assertEqual(status, 200)
        self.assertEqual(len(payload["runs"]), 1)
        self.assertEqual(payload["runs"][0]["status"], "completed")

    def test_invalid_json(self) -> None:
        status, payload = build_response("POST", "/v1/model-review", b"{")

        self.assertEqual(status, 400)
        self.assertIn("invalid json", payload["error"])


if __name__ == "__main__":
    unittest.main()
