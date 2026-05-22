# Parser Service

A small FastAPI service that extracts plain text from uploaded documents. The
gateway calls it to turn file uploads into prompt text.

- **Port:** 8090
- **Entry point:** `main.py`

## Endpoints

| Method | Path | Purpose |
|--------|------|---------|
| GET  | `/health` | Liveness probe → `{"status": "ok"}` |
| POST | `/parse`  | Extract text from an uploaded file |

### `POST /parse`

Multipart upload with a single `file` field. Supported formats: **PDF**, **DOCX**, **PPTX**.

Success (`200`):

```json
{ "text": "extracted text…", "page_count": 3 }
```

> `page_count` is always `1` for DOCX — a Word document has no reliable page
> count without rendering it.

Errors are returned as `{"detail": "<reason>"}`:

| Status | When |
|--------|------|
| 400 | Empty file, or unsupported format |
| 413 | File exceeds the size limit |
| 422 | File is corrupted / cannot be parsed, or no `file` field was sent |

## Configuration

| Var | Default | Purpose |
|-----|---------|---------|
| `MAX_UPLOAD_BYTES` | `10485760` (10 MiB) | Maximum accepted upload size |

## Run

```bash
pip install -r requirements.txt
uvicorn main:app --host 0.0.0.0 --port 8090
```

## Test

```bash
pip install -r requirements.txt -r requirements-dev.txt
pytest
```
