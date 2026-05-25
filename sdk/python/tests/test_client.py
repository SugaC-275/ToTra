"""Tests for the ToTra Python SDK using respx to mock HTTP calls."""

from __future__ import annotations

import json

import httpx
import pytest
import respx

from totra import AsyncToTra, ToTra, ToTraConnectionError, ToTraError

BASE_URL = "https://gateway.example.com"
API_KEY = "test-key-abc"

# ---------------------------------------------------------------------------
# Fixtures
# ---------------------------------------------------------------------------


@pytest.fixture
def client() -> ToTra:
    return ToTra(api_key=API_KEY, base_url=BASE_URL)


@pytest.fixture
def async_client() -> AsyncToTra:
    return AsyncToTra(api_key=API_KEY, base_url=BASE_URL)


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

CHAT_RESPONSE = {
    "id": "chatcmpl-abc123",
    "object": "chat.completion",
    "model": "gpt-4o",
    "choices": [
        {
            "index": 0,
            "message": {"role": "assistant", "content": "Hello, world!"},
            "finish_reason": "stop",
        }
    ],
    "usage": {"prompt_tokens": 10, "completion_tokens": 3, "total_tokens": 13},
}

EMBED_RESPONSE = {
    "object": "list",
    "model": "text-embedding-3-small",
    "data": [
        {"index": 0, "object": "embedding", "embedding": [0.1, 0.2, 0.3]},
        {"index": 1, "object": "embedding", "embedding": [0.4, 0.5, 0.6]},
    ],
    "usage": {"prompt_tokens": 4, "total_tokens": 4},
}


def _sse_lines(*contents: str, done: bool = True) -> str:
    """Build a fake SSE response body from content delta strings."""
    parts: list[str] = []
    for i, content in enumerate(contents):
        chunk = {
            "id": f"chatcmpl-{i}",
            "object": "chat.completion.chunk",
            "choices": [{"index": 0, "delta": {"content": content}}],
        }
        parts.append(f"data: {json.dumps(chunk)}\n\n")
    if done:
        parts.append("data: [DONE]\n\n")
    return "".join(parts)


# ---------------------------------------------------------------------------
# Synchronous tests
# ---------------------------------------------------------------------------


@respx.mock
def test_chat_success(client: ToTra) -> None:
    """ToTra.chat() returns the full OpenAI-compatible response dict."""
    respx.post(f"{BASE_URL}/v1/chat/completions").mock(
        return_value=httpx.Response(200, json=CHAT_RESPONSE)
    )

    result = client.chat("gpt-4o", [{"role": "user", "content": "Hi"}])

    assert result["id"] == "chatcmpl-abc123"
    assert result["choices"][0]["message"]["content"] == "Hello, world!"


@respx.mock
def test_chat_system_prepend(client: ToTra) -> None:
    """system= parameter is prepended as a system message before user messages."""
    route = respx.post(f"{BASE_URL}/v1/chat/completions").mock(
        return_value=httpx.Response(200, json=CHAT_RESPONSE)
    )

    client.chat(
        "gpt-4o",
        [{"role": "user", "content": "Hello"}],
        system="You are a helpful assistant.",
    )

    sent = json.loads(route.calls[0].request.content)
    assert sent["messages"][0] == {"role": "system", "content": "You are a helpful assistant."}
    assert sent["messages"][1] == {"role": "user", "content": "Hello"}


@respx.mock
def test_stream_yields_delta_content(client: ToTra) -> None:
    """ToTra.stream() yields individual content delta strings."""
    respx.post(f"{BASE_URL}/v1/chat/completions").mock(
        return_value=httpx.Response(
            200,
            text=_sse_lines("Hello", ", ", "world!"),
            headers={"content-type": "text/event-stream"},
        )
    )

    chunks = list(client.stream("gpt-4o", [{"role": "user", "content": "Hi"}]))

    assert chunks == ["Hello", ", ", "world!"]


@respx.mock
def test_embed_returns_vectors(client: ToTra) -> None:
    """ToTra.embed() returns a list of float vectors in input order."""
    respx.post(f"{BASE_URL}/v1/embeddings").mock(
        return_value=httpx.Response(200, json=EMBED_RESPONSE)
    )

    vectors = client.embed("text-embedding-3-small", ["foo", "bar"])

    assert len(vectors) == 2
    assert vectors[0] == pytest.approx([0.1, 0.2, 0.3])
    assert vectors[1] == pytest.approx([0.4, 0.5, 0.6])


@respx.mock
def test_chat_4xx_raises_totra_error(client: ToTra) -> None:
    """HTTP 4xx responses raise ToTraError with correct status_code."""
    respx.post(f"{BASE_URL}/v1/chat/completions").mock(
        return_value=httpx.Response(
            401,
            json={"error": {"message": "Invalid API key"}},
        )
    )

    with pytest.raises(ToTraError) as exc_info:
        client.chat("gpt-4o", [{"role": "user", "content": "Hi"}])

    assert exc_info.value.status_code == 401
    assert "401" in str(exc_info.value)


@respx.mock
def test_chat_5xx_raises_totra_error(client: ToTra) -> None:
    """HTTP 5xx responses raise ToTraError."""
    respx.post(f"{BASE_URL}/v1/chat/completions").mock(
        return_value=httpx.Response(503, text="Service Unavailable")
    )

    with pytest.raises(ToTraError) as exc_info:
        client.chat("gpt-4o", [{"role": "user", "content": "Hi"}])

    assert exc_info.value.status_code == 503


def test_connection_error_raises_totra_connection_error(client: ToTra) -> None:
    """Network-level failures raise ToTraConnectionError."""
    with respx.mock:
        respx.post(f"{BASE_URL}/v1/chat/completions").mock(
            side_effect=httpx.ConnectError("Connection refused")
        )

        with pytest.raises(ToTraConnectionError):
            client.chat("gpt-4o", [{"role": "user", "content": "Hi"}])


# ---------------------------------------------------------------------------
# Asynchronous tests
# ---------------------------------------------------------------------------


@respx.mock
async def test_async_chat_success(async_client: AsyncToTra) -> None:
    """AsyncToTra.chat() returns the full response dict."""
    respx.post(f"{BASE_URL}/v1/chat/completions").mock(
        return_value=httpx.Response(200, json=CHAT_RESPONSE)
    )

    result = await async_client.chat("gpt-4o", [{"role": "user", "content": "Hi"}])

    assert result["choices"][0]["message"]["content"] == "Hello, world!"


@respx.mock
async def test_async_chat_system_prepend(async_client: AsyncToTra) -> None:
    """AsyncToTra.chat() also prepends the system message correctly."""
    route = respx.post(f"{BASE_URL}/v1/chat/completions").mock(
        return_value=httpx.Response(200, json=CHAT_RESPONSE)
    )

    await async_client.chat(
        "gpt-4o",
        [{"role": "user", "content": "Hello"}],
        system="Be concise.",
    )

    sent = json.loads(route.calls[0].request.content)
    assert sent["messages"][0]["role"] == "system"
    assert sent["messages"][0]["content"] == "Be concise."


@respx.mock
async def test_async_embed_returns_vectors(async_client: AsyncToTra) -> None:
    """AsyncToTra.embed() returns vectors in input order."""
    respx.post(f"{BASE_URL}/v1/embeddings").mock(
        return_value=httpx.Response(200, json=EMBED_RESPONSE)
    )

    vectors = await async_client.embed("text-embedding-3-small", ["foo", "bar"])

    assert len(vectors) == 2
    assert vectors[0] == pytest.approx([0.1, 0.2, 0.3])
