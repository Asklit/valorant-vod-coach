# Vision Service

Python boundary for OCR/CV/VLM work.

Current mode is a deterministic stub. It validates the `model_review_tasks` contract and returns structured placeholder findings. Replace `app/reviewer.py` with Qwen/VLM inference while keeping the HTTP contract stable.

## Run

Dependency-free local MVP server:

```sh
cd ../..
./scripts/run_vision_service.sh
```

Or run the module directly:

```sh
PYTHONPATH=ml/vision-service python3 -m app.server --host 127.0.0.1 --port 8091
```

FastAPI entrypoint for the future production-style service:

```sh
cd ml/vision-service
python3 -m venv .venv
. .venv/bin/activate
pip install -e .
uvicorn app.main:app --host 127.0.0.1 --port 8091
```

Then run the Go API with:

```sh
VISION_SERVICE_URL=http://127.0.0.1:8091 go run ./cmd/vod-web
```

Or run the CLI directly:

```sh
go run ./cmd/vodctl analyze run --vod iron_spudbud_01 --model-review --vision-url http://127.0.0.1:8091 --force
```

## Contract

- `GET /health`
- `POST /v1/model-review`

Request body:

```json
{
  "run_id": "run_01",
  "vod": {"label": "iron_spudbud_01", "rank": "iron"},
  "tasks": []
}
```

Response body:

```json
{
  "runs": []
}
```
