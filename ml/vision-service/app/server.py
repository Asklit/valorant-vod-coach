from __future__ import annotations

import argparse
import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any

from app.reviewer import MODEL_NAME, review_tasks


def build_response(method: str, path: str, body: bytes) -> tuple[int, dict[str, Any]]:
    route = path.split("?", 1)[0]
    if method == "GET" and route == "/health":
        return 200, {
            "status": "ok",
            "model": MODEL_NAME,
            "mode": "stub",
            "runtime": "stdlib-http",
        }

    if method == "POST" and route == "/v1/model-review":
        payload, error = _decode_json_object(body)
        if error:
            return 400, {"error": error}
        return 200, review_tasks(payload)

    return 404, {"error": "not found"}


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Run local Valorant VOD Coach vision-service stub.")
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--port", default=8091, type=int)
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> None:
    args = parse_args(argv)
    server = ThreadingHTTPServer((args.host, args.port), VisionHTTPRequestHandler)
    print(f"vision-service stub listening on http://{args.host}:{args.port}", flush=True)
    server.serve_forever()


class VisionHTTPRequestHandler(BaseHTTPRequestHandler):
    server_version = "ValorantVODCoachVisionStub/0.1"

    def do_GET(self) -> None:
        self._write(*build_response("GET", self.path, b""))

    def do_POST(self) -> None:
        length = int(self.headers.get("Content-Length", "0") or "0")
        body = self.rfile.read(length)
        self._write(*build_response("POST", self.path, body))

    def do_OPTIONS(self) -> None:
        self.send_response(204)
        self._write_headers()
        self.end_headers()

    def log_message(self, format: str, *args: Any) -> None:
        return

    def _write(self, status: int, payload: dict[str, Any]) -> None:
        raw = json.dumps(payload, ensure_ascii=False, indent=2).encode("utf-8") + b"\n"
        self.send_response(status)
        self._write_headers()
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def _write_headers(self) -> None:
        self.send_header("Content-Type", "application/json")
        self.send_header("Access-Control-Allow-Origin", "*")
        self.send_header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
        self.send_header("Access-Control-Allow-Headers", "Content-Type")


def _decode_json_object(body: bytes) -> tuple[dict[str, Any], str]:
    if not body:
        return {}, ""
    try:
        payload = json.loads(body.decode("utf-8"))
    except UnicodeDecodeError:
        return {}, "request body must be utf-8 json"
    except json.JSONDecodeError as exc:
        return {}, f"invalid json: {exc.msg}"

    if not isinstance(payload, dict):
        return {}, "request body must be a json object"
    return payload, ""


if __name__ == "__main__":
    main()
