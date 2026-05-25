# totra-sdk

Python SDK for [ToTra](https://github.com/yourorg/totra) — AI Spend Management & LLM Gateway.

ToTra exposes an OpenAI-compatible API, so this SDK is a thin, dependency-light wrapper around `httpx` that provides a clean interface without requiring the `openai` package.

## Installation

```bash
pip install totra-sdk
```

For development (testing):

```bash
pip install "totra-sdk[dev]"
```

## Quick Start

### Synchronous

```python
from totra import ToTra

client = ToTra(
    api_key="your-totra-api-key",
    base_url="https://gateway.your-domain.com",
)

# Non-streaming chat
response = client.chat(
    model="gpt-4o",
    messages=[{"role": "user", "content": "Explain HTTP in one sentence."}],
    system="You are a concise technical writer.",
    max_tokens=100,
    temperature=0.3,
)
print(response["choices"][0]["message"]["content"])

# Streaming chat
for chunk in client.stream(
    model="gpt-4o",
    messages=[{"role": "user", "content": "Count to 5."}],
):
    print(chunk, end="", flush=True)
print()

# Embeddings
vectors = client.embed(
    model="text-embedding-3-small",
    input=["Hello world", "Goodbye world"],
)
print(len(vectors), "vectors of dimension", len(vectors[0]))
```

### Asynchronous

```python
import asyncio
from totra import AsyncToTra

async def main():
    client = AsyncToTra(
        api_key="your-totra-api-key",
        base_url="https://gateway.your-domain.com",
    )

    # Non-streaming chat
    response = await client.chat(
        model="gpt-4o",
        messages=[{"role": "user", "content": "Hello!"}],
    )
    print(response["choices"][0]["message"]["content"])

    # Streaming chat
    async for chunk in client.stream(
        model="gpt-4o",
        messages=[{"role": "user", "content": "Tell me a joke."}],
    ):
        print(chunk, end="", flush=True)
    print()

    # Embeddings
    vectors = await client.embed(
        model="text-embedding-3-small",
        input="Single string also works",
    )
    print(vectors[0][:5], "...")  # first 5 dimensions

    await client.close()

asyncio.run(main())
```

### Context Manager

Both clients support the context-manager protocol so the underlying HTTP connection pool is always closed:

```python
# Synchronous
with ToTra(api_key="...", base_url="...") as client:
    response = client.chat("gpt-4o", [{"role": "user", "content": "Hi"}])

# Asynchronous
async with AsyncToTra(api_key="...", base_url="...") as client:
    response = await client.chat("gpt-4o", [{"role": "user", "content": "Hi"}])
```

## Error Handling

```python
from totra import ToTra, ToTraError, ToTraConnectionError

client = ToTra(api_key="...", base_url="...")

try:
    response = client.chat("gpt-4o", [{"role": "user", "content": "Hi"}])
except ToTraError as e:
    print(f"API error {e.status_code}: {e.response_body}")
except ToTraConnectionError as e:
    print(f"Could not reach gateway: {e}")
```

| Exception | When raised |
|-----------|-------------|
| `ToTraError` | HTTP 4xx or 5xx from the gateway. Has `.status_code` (int) and `.response_body` (str). |
| `ToTraConnectionError` | Network failure — gateway unreachable. |

## API Reference

### `ToTra(api_key, base_url, timeout=120.0)`

Synchronous client.

| Method | Signature | Returns |
|--------|-----------|---------|
| `chat` | `(model, messages, *, max_tokens=None, temperature=None, system=None, **kwargs)` | `dict` |
| `stream` | `(model, messages, *, max_tokens=None, temperature=None, system=None, **kwargs)` | `Iterator[str]` |
| `embed` | `(model, input, **kwargs)` | `list[list[float]]` |
| `close` | `()` | `None` |

### `AsyncToTra(api_key, base_url, timeout=120.0)`

Asynchronous client — same methods as `ToTra` but all coroutines. `stream` returns `AsyncIterator[str]`.

### Common Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `model` | `str` | Model identifier (e.g. `"gpt-4o"`) |
| `messages` | `list[dict]` | OpenAI-format message list |
| `system` | `str \| None` | System prompt — prepended as `{"role": "system"}` automatically |
| `max_tokens` | `int \| None` | Token limit for the response |
| `temperature` | `float \| None` | Sampling temperature |
| `input` (embed) | `str \| list[str]` | Text(s) to embed |
| `**kwargs` | any | Extra OpenAI-compatible parameters forwarded verbatim |

## Running Tests

```bash
cd sdk/python
pip install -e ".[dev]"
pytest tests/ -v
```
