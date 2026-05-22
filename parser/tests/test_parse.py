"""Tests for the parser's /health and /parse endpoints."""
import main


def test_health(client):
    resp = client.get("/health")
    assert resp.status_code == 200
    assert resp.json() == {"status": "ok"}


def test_parse_pdf(client, pdf_bytes):
    resp = client.post(
        "/parse", files={"file": ("doc.pdf", pdf_bytes, "application/pdf")}
    )
    assert resp.status_code == 200
    body = resp.json()
    assert "hello pdf" in body["text"]
    assert body["page_count"] == 1


def test_parse_docx(client, docx_bytes):
    resp = client.post(
        "/parse", files={"file": ("doc.docx", docx_bytes, "application/octet-stream")}
    )
    assert resp.status_code == 200
    body = resp.json()
    assert "hello docx" in body["text"]
    assert body["page_count"] == 1


def test_parse_pptx(client, pptx_bytes):
    resp = client.post(
        "/parse", files={"file": ("deck.pptx", pptx_bytes, "application/octet-stream")}
    )
    assert resp.status_code == 200
    body = resp.json()
    assert "hello pptx" in body["text"]
    assert body["page_count"] >= 1


def test_unsupported_format(client):
    resp = client.post(
        "/parse", files={"file": ("notes.txt", b"plain text", "text/plain")}
    )
    assert resp.status_code == 400


def test_empty_file(client):
    resp = client.post(
        "/parse", files={"file": ("empty.pdf", b"", "application/pdf")}
    )
    assert resp.status_code == 400


def test_corrupted_pdf(client):
    resp = client.post(
        "/parse",
        files={"file": ("broken.pdf", b"this is not a real pdf", "application/pdf")},
    )
    assert resp.status_code == 422


def test_oversized_file(client, monkeypatch):
    monkeypatch.setattr(main, "MAX_UPLOAD_BYTES", 8)
    resp = client.post(
        "/parse",
        files={"file": ("big.pdf", b"way more than eight bytes", "application/pdf")},
    )
    assert resp.status_code == 413


def test_missing_file_field(client):
    resp = client.post("/parse")
    assert resp.status_code == 422
