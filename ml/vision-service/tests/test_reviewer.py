import unittest

from app.reviewer import review_task, review_tasks


class ReviewerTest(unittest.TestCase):
    def test_review_task_returns_completed_run(self) -> None:
        result = review_task(
            {"label": "vod_01", "rank": "diamond"},
            {
                "id": "vlm_combat_001",
                "window_id": "combat_001",
                "kind": "combat_spike",
                "severity": "high",
                "prompt_version": "vlm-review-v1",
                "clip_path": "clips/combat_001.mp4",
                "peak_seconds": 42.5,
            },
        )

        self.assertEqual(result["status"], "completed")
        self.assertEqual(result["task_id"], "vlm_combat_001")
        self.assertEqual(result["findings"][0]["category"], "crosshair")
        self.assertEqual(result["findings"][0]["severity"], "high")
        self.assertEqual(result["findings"][0]["timestamp_seconds"], 42.5)

    def test_review_task_requires_clip(self) -> None:
        result = review_task(
            {"label": "vod_01"},
            {"id": "vlm_window_001", "window_id": "window_001", "prompt_version": "vlm-review-v1"},
        )

        self.assertEqual(result["status"], "failed")
        self.assertIn("clip_path", result["error"])

    def test_review_tasks_batches_payload(self) -> None:
        result = review_tasks(
            {
                "vod": {"label": "vod_01"},
                "tasks": [
                    {"id": "task_01", "window_id": "window_01", "clip_path": "clips/window_01.mp4"},
                    {"id": "task_02", "window_id": "window_02", "clip_path": "clips/window_02.mp4"},
                ],
            }
        )

        self.assertEqual(len(result["runs"]), 2)


if __name__ == "__main__":
    unittest.main()
