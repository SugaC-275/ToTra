"""Tests for the ToTra Python SDK using respx to mock HTTP calls."""

from __future__ import annotations

import json
import time

import httpx
import pytest
import respx

from totra import AsyncOpenAI, AsyncToTra, OpenAI, ToTra, ToTraConnectionError, ToTraError
from totra.types import ChatCompletion

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

PROMPTS_LIST_RESPONSE = [
    {"name": "greeting", "content": "Hello {{name}}!", "version": 1},
    {"name": "farewell", "content": "Goodbye {{name}}!", "version": 1},
]

PROMPT_RESPONSE = {"name": "greeting", "content": "Hello {{name}}!", "version": 1}

PROMPT_RENDER_RESPONSE = {"rendered": "Hello Alice!"}


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
# Synchronous tests — existing methods
# ---------------------------------------------------------------------------


@respx.mock
def test_chat_success(client: ToTra) -> None:
    """ToTra.chat.completions.create() returns the full OpenAI-compatible response dict."""
    respx.post(f"{BASE_URL}/v1/chat/completions").mock(
        return_value=httpx.Response(200, json=CHAT_RESPONSE)
    )

    result = client.chat.completions.create("gpt-4o", [{"role": "user", "content": "Hi"}])

    assert result["id"] == "chatcmpl-abc123"
    assert result["choices"][0]["message"]["content"] == "Hello, world!"


@respx.mock
def test_complete_success(client: ToTra) -> None:
    """ToTra.complete() returns the full response dict."""
    respx.post(f"{BASE_URL}/v1/chat/completions").mock(
        return_value=httpx.Response(200, json=CHAT_RESPONSE)
    )

    result = client.complete("gpt-4o", [{"role": "user", "content": "Hi"}])

    assert result["id"] == "chatcmpl-abc123"


@respx.mock
def test_chat_system_prepend(client: ToTra) -> None:
    """system= parameter is prepended as a system message before user messages."""
    route = respx.post(f"{BASE_URL}/v1/chat/completions").mock(
        return_value=httpx.Response(200, json=CHAT_RESPONSE)
    )

    client.complete(
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
        client.complete("gpt-4o", [{"role": "user", "content": "Hi"}])

    assert exc_info.value.status_code == 401
    assert "401" in str(exc_info.value)


@respx.mock
def test_chat_5xx_raises_totra_error(client: ToTra) -> None:
    """HTTP 5xx responses raise ToTraError after retries are exhausted."""
    respx.post(f"{BASE_URL}/v1/chat/completions").mock(
        return_value=httpx.Response(503, text="Service Unavailable")
    )

    # Use retries=0 to avoid slow test
    no_retry_client = ToTra(api_key=API_KEY, base_url=BASE_URL, retries=0)
    with pytest.raises(ToTraError) as exc_info:
        no_retry_client.complete("gpt-4o", [{"role": "user", "content": "Hi"}])

    assert exc_info.value.status_code == 503


def test_connection_error_raises_totra_connection_error(client: ToTra) -> None:
    """Network-level failures raise ToTraConnectionError."""
    with respx.mock:
        respx.post(f"{BASE_URL}/v1/chat/completions").mock(
            side_effect=httpx.ConnectError("Connection refused")
        )

        with pytest.raises(ToTraConnectionError):
            client.complete("gpt-4o", [{"role": "user", "content": "Hi"}])


# ---------------------------------------------------------------------------
# OpenAI drop-in alias tests
# ---------------------------------------------------------------------------


def test_openai_alias_is_totra() -> None:
    """OpenAI is an alias for ToTra."""
    assert OpenAI is ToTra
    assert AsyncOpenAI is AsyncToTra


@respx.mock
def test_openai_compat_completions_create() -> None:
    """client.chat.completions.create() works as OpenAI drop-in."""
    respx.post(f"{BASE_URL}/v1/chat/completions").mock(
        return_value=httpx.Response(200, json=CHAT_RESPONSE)
    )

    client = OpenAI(api_key=API_KEY, base_url=BASE_URL)
    result = client.chat.completions.create(
        "gpt-4o", [{"role": "user", "content": "Hi"}]
    )

    assert result["choices"][0]["message"]["content"] == "Hello, world!"


@respx.mock
def test_openai_compat_completions_create_stream() -> None:
    """client.chat.completions.create(stream=True) returns an iterator."""
    respx.post(f"{BASE_URL}/v1/chat/completions").mock(
        return_value=httpx.Response(
            200,
            text=_sse_lines("Hi", " there"),
            headers={"content-type": "text/event-stream"},
        )
    )

    client = OpenAI(api_key=API_KEY, base_url=BASE_URL)
    gen = client.chat.completions.create(
        "gpt-4o", [{"role": "user", "content": "Hi"}], stream=True
    )
    chunks = list(gen)

    assert chunks == ["Hi", " there"]


# ---------------------------------------------------------------------------
# Retry and fallback tests
# ---------------------------------------------------------------------------


@respx.mock
def test_retry_on_503(monkeypatch: pytest.MonkeyPatch) -> None:
    """complete() retries on 503 and succeeds on the second attempt."""
    monkeypatch.setattr(time, "sleep", lambda _: None)

    call_count = 0

    def _handler(request: httpx.Request) -> httpx.Response:
        nonlocal call_count
        call_count += 1
        if call_count < 2:
            return httpx.Response(503, text="Unavailable")
        return httpx.Response(200, json=CHAT_RESPONSE)

    respx.post(f"{BASE_URL}/v1/chat/completions").mock(side_effect=_handler)

    client = ToTra(api_key=API_KEY, base_url=BASE_URL, retries=2)
    result = client.complete("gpt-4o", [{"role": "user", "content": "Hi"}])

    assert call_count == 2
    assert result["id"] == "chatcmpl-abc123"


@respx.mock
def test_fallback_model_used_after_retries_exhausted(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """Falls back to the next model when primary exhausts all retries."""
    monkeypatch.setattr(time, "sleep", lambda _: None)

    fallback_response = {**CHAT_RESPONSE, "model": "gpt-3.5-turbo"}

    def _handler(request: httpx.Request) -> httpx.Response:
        body = json.loads(request.content)
        if body["model"] == "gpt-4o":
            return httpx.Response(503, text="Unavailable")
        return httpx.Response(200, json=fallback_response)

    respx.post(f"{BASE_URL}/v1/chat/completions").mock(side_effect=_handler)

    client = ToTra(
        api_key=API_KEY,
        base_url=BASE_URL,
        retries=1,
        fallback_models=["gpt-3.5-turbo"],
    )
    result = client.complete("gpt-4o", [{"role": "user", "content": "Hi"}])

    assert result["model"] == "gpt-3.5-turbo"


@respx.mock
def test_non_retryable_error_raises_immediately(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """A 401 error is not retried — it raises immediately."""
    monkeypatch.setattr(time, "sleep", lambda _: None)

    call_count = 0

    def _handler(request: httpx.Request) -> httpx.Response:
        nonlocal call_count
        call_count += 1
        return httpx.Response(401, json={"error": "Unauthorized"})

    respx.post(f"{BASE_URL}/v1/chat/completions").mock(side_effect=_handler)

    client = ToTra(api_key=API_KEY, base_url=BASE_URL, retries=3)
    with pytest.raises(ToTraError) as exc_info:
        client.complete("gpt-4o", [{"role": "user", "content": "Hi"}])

    assert call_count == 1
    assert exc_info.value.status_code == 401


# ---------------------------------------------------------------------------
# Prompts client tests
# ---------------------------------------------------------------------------


@respx.mock
def test_prompts_list(client: ToTra) -> None:
    """prompts.list() returns a list of prompt dicts."""
    respx.get(f"{BASE_URL}/v1/prompts").mock(
        return_value=httpx.Response(200, json=PROMPTS_LIST_RESPONSE)
    )

    result = client.prompts.list()

    assert len(result) == 2
    assert result[0]["name"] == "greeting"


@respx.mock
def test_prompts_get(client: ToTra) -> None:
    """prompts.get(name) returns the named prompt dict."""
    respx.get(f"{BASE_URL}/v1/prompts/greeting").mock(
        return_value=httpx.Response(200, json=PROMPT_RESPONSE)
    )

    result = client.prompts.get("greeting")

    assert result["name"] == "greeting"
    assert result["content"] == "Hello {{name}}!"


@respx.mock
def test_prompts_save(client: ToTra) -> None:
    """prompts.save() POSTs name and content."""
    route = respx.post(f"{BASE_URL}/v1/prompts").mock(
        return_value=httpx.Response(200, json={"name": "hello", "content": "Hi!", "version": 1})
    )

    client.prompts.save("hello", "Hi!")

    sent = json.loads(route.calls[0].request.content)
    assert sent["name"] == "hello"
    assert sent["content"] == "Hi!"


@respx.mock
def test_prompts_render(client: ToTra) -> None:
    """prompts.render() returns the rendered string."""
    respx.post(f"{BASE_URL}/v1/prompts/greeting/render").mock(
        return_value=httpx.Response(200, json=PROMPT_RENDER_RESPONSE)
    )

    result = client.prompts.render("greeting", {"name": "Alice"})

    assert result == "Hello Alice!"


# ---------------------------------------------------------------------------
# Budget client tests
# ---------------------------------------------------------------------------


@respx.mock
def test_budget_get(client: ToTra) -> None:
    """budget.get() fetches budget info for a user."""
    budget_data = {"user_id": "u123", "budget_usd": 10.0, "used_usd": 2.5}
    respx.get(f"{BASE_URL}/v1/key/budget?user_id=u123").mock(
        return_value=httpx.Response(200, json=budget_data)
    )

    result = client.budget.get("u123")

    assert result["user_id"] == "u123"
    assert result["budget_usd"] == 10.0


@respx.mock
def test_budget_set(client: ToTra) -> None:
    """budget.set() sends a PUT with the correct body."""
    route = respx.put(f"{BASE_URL}/v1/key/budget").mock(
        return_value=httpx.Response(200, json={"ok": True})
    )

    client.budget.set("u123", budget_usd=20.0, period="monthly", rpm_limit=100)

    sent = json.loads(route.calls[0].request.content)
    assert sent["user_id"] == "u123"
    assert sent["budget_usd"] == 20.0
    assert sent["period"] == "monthly"
    assert sent["rpm_limit"] == 100


# ---------------------------------------------------------------------------
# types.py tests
# ---------------------------------------------------------------------------


def test_chat_completion_from_dict() -> None:
    """ChatCompletion.from_dict() correctly parses a response dict."""
    result = ChatCompletion.from_dict(CHAT_RESPONSE)

    assert result.id == "chatcmpl-abc123"
    assert result.model == "gpt-4o"
    assert len(result.choices) == 1
    assert result.choices[0].message.role == "assistant"
    assert result.choices[0].message.content == "Hello, world!"
    assert result.choices[0].finish_reason == "stop"
    assert result.usage.prompt_tokens == 10
    assert result.usage.completion_tokens == 3
    assert result.usage.total_tokens == 13


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


@respx.mock
async def test_async_prompts_list(async_client: AsyncToTra) -> None:
    """AsyncToTra.prompts.list() returns the prompt list."""
    respx.get(f"{BASE_URL}/v1/prompts").mock(
        return_value=httpx.Response(200, json=PROMPTS_LIST_RESPONSE)
    )

    result = await async_client.prompts.list()

    assert len(result) == 2
    assert result[1]["name"] == "farewell"


@respx.mock
async def test_async_prompts_render(async_client: AsyncToTra) -> None:
    """AsyncToTra.prompts.render() returns the rendered string."""
    respx.post(f"{BASE_URL}/v1/prompts/greeting/render").mock(
        return_value=httpx.Response(200, json=PROMPT_RENDER_RESPONSE)
    )

    result = await async_client.prompts.render("greeting", {"name": "Alice"})

    assert result == "Hello Alice!"
