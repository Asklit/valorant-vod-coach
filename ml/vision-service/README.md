# Experimental Vision Service

Optional Python boundary for evaluated model-review experiments. The default product performs CPU CV/OCR through Go adapters and does not invoke this service.

Current mode is a deterministic contract double. It validates `model_review_tasks` and returns synthetic responses for adapter tests; those responses are not product coaching findings. Add a real implementation only after it passes reviewed fixtures and an inference budget is explicitly approved.

## Run

Dependency-free contract server:

```sh
cd ../..
./scripts/run_vision_service.sh
```

Or run the module directly:

```sh
PYTHONPATH=ml/vision-service python3 -m app.server --host 127.0.0.1 --port 8091
```

Optional FastAPI entrypoint for contract development:

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

Or explicitly test the model-review adapter through the CLI:

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
