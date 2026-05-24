"""Tests for the parser's /health and /parse endpoints."""
import main


def test_health(client):
    resp = client.get("/health")
    assert resp.status_code == 200
    assert resp.json() == {"status": "ok"}


# ---------------------------------------------------------------------------
# Internal-secret auth tests
# ---------------------------------------------------------------------------

def test_health_requires_no_auth(client, monkeypatch):
    """Health endpoint must remain open even when INTERNAL_SECRET is set."""
    monkeypatch.setattr(main, "INTERNAL_SECRET", "super-secret")
    resp = client.get("/health")
    assert resp.status_code == 200


def test_parse_missing_secret_returns_401(client, monkeypatch, pdf_bytes):
    monkeypatch.setattr(main, "INTERNAL_SECRET", "super-secret")
    resp = client.post(
        "/parse", files={"file": ("doc.pdf", pdf_bytes, "application/pdf")}
    )
    assert resp.status_code == 401


def test_parse_wrong_secret_returns_401(client, monkeypatch, pdf_bytes):
    monkeypatch.setattr(main, "INTERNAL_SECRET", "super-secret")
    resp = client.post(
        "/parse",
        files={"file": ("doc.pdf", pdf_bytes, "application/pdf")},
        headers={"X-Internal-Secret": "wrong-secret"},
    )
    assert resp.status_code == 401


def test_parse_correct_secret_passes(client, monkeypatch, pdf_bytes):
    monkeypatch.setattr(main, "INTERNAL_SECRET", "super-secret")
    resp = client.post(
        "/parse",
        files={"file": ("doc.pdf", pdf_bytes, "application/pdf")},
        headers={"X-Internal-Secret": "super-secret"},
    )
    assert resp.status_code == 200
    assert "hello pdf" in resp.json()["text"]


def test_parse_no_secret_env_skips_check(client, monkeypatch, pdf_bytes):
    """When INTERNAL_SECRET is empty the header check is bypassed (local dev)."""
    monkeypatch.setattr(main, "INTERNAL_SECRET", "")
    resp = client.post(
        "/parse", files={"file": ("doc.pdf", pdf_bytes, "application/pdf")}
    )
    assert resp.status_code == 200


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
