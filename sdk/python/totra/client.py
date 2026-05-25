"""ToTra Python SDK — synchronous and asynchronous clients."""

from __future__ import annotations

import json
from collections.abc import AsyncIterator, Iterator
from typing import Any

import httpx


# ---------------------------------------------------------------------------
# Exceptions
# ---------------------------------------------------------------------------


class ToTraError(Exception):
    """Raised for HTTP 4xx / 5xx responses from the ToTra gateway."""

    def __init__(self, message: str, status_code: int, response_body: str) -> None:
        super().__init__(message)
        self.status_code = status_code
        self.response_body = response_body


class ToTraConnectionError(Exception):
    """Raised when the SDK cannot reach the ToTra gateway."""


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

_CHAT_PATH = "/v1/chat/completions"
_EMBED_PATH = "/v1/embeddings"


def _build_chat_payload(
    model: str,
    messages: list[dict[str, Any]],
    *,
    max_tokens: int | None,
    temperature: float | None,
    system: str | None,
    stream: bool,
    kwargs: dict[str, Any],
) -> dict[str, Any]:
    """Assemble the JSON body for a chat-completions request."""
    all_messages: list[dict[str, Any]] = []
    if system is not None:
        all_messages.append({"role": "system", "content": system})
    all_messages.extend(messages)

    payload: dict[str, Any] = {
        "model": model,
        "messages": all_messages,
        "stream": stream,
    }
    if max_tokens is not None:
        payload["max_tokens"] = max_tokens
    if temperature is not None:
        payload["temperature"] = temperature
    payload.update(kwargs)
    return payload


def _check_response(response: httpx.Response) -> None:
    """Raise ToTraError for non-2xx HTTP responses."""
    if response.status_code >= 400:
        raise ToTraError(
            f"ToTra API error {response.status_code}",
            status_code=response.status_code,
            response_body=response.text,
        )


def _iter_sse_content(lines: Iterator[str]) -> Iterator[str]:
    """Parse server-sent events and yield delta content strings."""
    for line in lines:
        line = line.strip()
        if not line or not line.startswith("data:"):
            continue
        data = line[len("data:"):].strip()
        if data == "[DONE]":
            break
        try:
            chunk = json.loads(data)
        except json.JSONDecodeError:
            continue
        content = (
            chunk.get("choices", [{}])[0]
            .get("delta", {})
            .get("content")
        )
        if content:
            yield content


async def _aiter_sse_content(lines: AsyncIterator[str]) -> AsyncIterator[str]:
    """Async version of SSE content parser."""
    async for line in lines:
        line = line.strip()
        if not line or not line.startswith("data:"):
            continue
        data = line[len("data:"):].strip()
        if data == "[DONE]":
            break
        try:
            chunk = json.loads(data)
        except json.JSONDecodeError:
            continue
        content = (
            chunk.get("choices", [{}])[0]
            .get("delta", {})
            .get("content")
        )
        if content:
            yield content


def _extract_embeddings(body: dict[str, Any]) -> list[list[float]]:
    """Extract the embedding vectors from an /v1/embeddings response."""
    data = body.get("data", [])
    # Sort by index to preserve input order
    data.sort(key=lambda d: d.get("index", 0))
    return [item["embedding"] for item in data]


# ---------------------------------------------------------------------------
# Synchronous client
# ---------------------------------------------------------------------------


class ToTra:
    """Synchronous ToTra gateway client.

    Parameters
    ----------
    api_key:
        The API key issued by ToTra (or any key accepted by your gateway).
    base_url:
        Root URL of your ToTra gateway, e.g. ``https://gateway.example.com``.
    timeout:
        Request timeout in seconds (default 120).
    """

    def __init__(
        self,
        api_key: str,
        base_url: str,
        timeout: float = 120.0,
    ) -> None:
        self._base_url = base_url.rstrip("/")
        self._client = httpx.Client(
            headers={
                "Authorization": f"Bearer {api_key}",
                "Content-Type": "application/json",
            },
            timeout=timeout,
        )

    # ------------------------------------------------------------------
    # Public API
    # ------------------------------------------------------------------

    def chat(
        self,
        model: str,
        messages: list[dict[str, Any]],
        *,
        max_tokens: int | None = None,
        temperature: float | None = None,
        system: str | None = None,
        **kwargs: Any,
    ) -> dict[str, Any]:
        """Send a chat-completions request and return the full response dict.

        Parameters
        ----------
        model:
            The model identifier to use (e.g. ``"gpt-4o"``).
        messages:
            List of message dicts following the OpenAI format.
        max_tokens:
            Maximum tokens to generate.
        temperature:
            Sampling temperature.
        system:
            System prompt; prepended automatically as a ``{"role": "system"}``
            message before the provided ``messages``.
        **kwargs:
            Any additional OpenAI-compatible parameters forwarded verbatim.

        Returns
        -------
        dict
            Full OpenAI-compatible response object.

        Raises
        ------
        ToTraError
            On HTTP 4xx / 5xx responses.
        ToTraConnectionError
            When the gateway cannot be reached.
        """
        payload = _build_chat_payload(
            model, messages,
            max_tokens=max_tokens,
            temperature=temperature,
            system=system,
            stream=False,
            kwargs=kwargs,
        )
        try:
            response = self._client.post(
                f"{self._base_url}{_CHAT_PATH}", json=payload
            )
        except httpx.ConnectError as exc:
            raise ToTraConnectionError(str(exc)) from exc
        _check_response(response)
        return response.json()

    def stream(
        self,
        model: str,
        messages: list[dict[str, Any]],
        *,
        max_tokens: int | None = None,
        temperature: float | None = None,
        system: str | None = None,
        **kwargs: Any,
    ) -> Iterator[str]:
        """Stream a chat-completions request, yielding delta content strings.

        Parameters
        ----------
        model:
            The model identifier to use.
        messages:
            List of message dicts.
        max_tokens:
            Maximum tokens to generate.
        temperature:
            Sampling temperature.
        system:
            System prompt prepended as a system message.
        **kwargs:
            Additional OpenAI-compatible parameters.

        Yields
        ------
        str
            Individual content delta strings as they arrive.

        Raises
        ------
        ToTraError
            On HTTP 4xx / 5xx responses.
        ToTraConnectionError
            When the gateway cannot be reached.
        """
        payload = _build_chat_payload(
            model, messages,
            max_tokens=max_tokens,
            temperature=temperature,
            system=system,
            stream=True,
            kwargs=kwargs,
        )
        try:
            with self._client.stream(
                "POST",
                f"{self._base_url}{_CHAT_PATH}",
                json=payload,
            ) as response:
                _check_response(response)
                yield from _iter_sse_content(response.iter_lines())
        except httpx.ConnectError as exc:
            raise ToTraConnectionError(str(exc)) from exc

    def embed(
        self,
        model: str,
        input: str | list[str],
        **kwargs: Any,
    ) -> list[list[float]]:
        """Request text embeddings from the gateway.

        Parameters
        ----------
        model:
            The embedding model identifier.
        input:
            A single string or list of strings to embed.
        **kwargs:
            Additional OpenAI-compatible parameters.

        Returns
        -------
        list[list[float]]
            One embedding vector per input string, in input order.

        Raises
        ------
        ToTraError
            On HTTP 4xx / 5xx responses.
        ToTraConnectionError
            When the gateway cannot be reached.
        """
        payload: dict[str, Any] = {"model": model, "input": input, **kwargs}
        try:
            response = self._client.post(
                f"{self._base_url}{_EMBED_PATH}", json=payload
            )
        except httpx.ConnectError as exc:
            raise ToTraConnectionError(str(exc)) from exc
        _check_response(response)
        return _extract_embeddings(response.json())

    def close(self) -> None:
        """Close the underlying HTTP client."""
        self._client.close()

    def __enter__(self) -> "ToTra":
        return self

    def __exit__(self, *_: object) -> None:
        self.close()


# ---------------------------------------------------------------------------
# Asynchronous client
# ---------------------------------------------------------------------------


class AsyncToTra:
    """Asynchronous ToTra gateway client.

    Parameters
    ----------
    api_key:
        The API key issued by ToTra.
    base_url:
        Root URL of your ToTra gateway.
    timeout:
        Request timeout in seconds (default 120).
    """

    def __init__(
        self,
        api_key: str,
        base_url: str,
        timeout: float = 120.0,
    ) -> None:
        self._base_url = base_url.rstrip("/")
        self._client = httpx.AsyncClient(
            headers={
                "Authorization": f"Bearer {api_key}",
                "Content-Type": "application/json",
            },
            timeout=timeout,
        )

    # ------------------------------------------------------------------
    # Public API
    # ------------------------------------------------------------------

    async def chat(
        self,
        model: str,
        messages: list[dict[str, Any]],
        *,
        max_tokens: int | None = None,
        temperature: float | None = None,
        system: str | None = None,
        **kwargs: Any,
    ) -> dict[str, Any]:
        """Async version of :meth:`ToTra.chat`."""
        payload = _build_chat_payload(
            model, messages,
            max_tokens=max_tokens,
            temperature=temperature,
            system=system,
            stream=False,
            kwargs=kwargs,
        )
        try:
            response = await self._client.post(
                f"{self._base_url}{_CHAT_PATH}", json=payload
            )
        except httpx.ConnectError as exc:
            raise ToTraConnectionError(str(exc)) from exc
        _check_response(response)
        return response.json()

    async def stream(
        self,
        model: str,
        messages: list[dict[str, Any]],
        *,
        max_tokens: int | None = None,
        temperature: float | None = None,
        system: str | None = None,
        **kwargs: Any,
    ) -> AsyncIterator[str]:
        """Async streaming version of :meth:`ToTra.stream`.

        Must be used with ``async for``:

        .. code-block:: python

            async for chunk in client.stream("gpt-4o", messages):
                print(chunk, end="", flush=True)
        """
        payload = _build_chat_payload(
            model, messages,
            max_tokens=max_tokens,
            temperature=temperature,
            system=system,
            stream=True,
            kwargs=kwargs,
        )
        try:
            async with self._client.stream(
                "POST",
                f"{self._base_url}{_CHAT_PATH}",
                json=payload,
            ) as response:
                _check_response(response)
                async for chunk in _aiter_sse_content(response.aiter_lines()):
                    yield chunk
        except httpx.ConnectError as exc:
            raise ToTraConnectionError(str(exc)) from exc

    async def embed(
        self,
        model: str,
        input: str | list[str],
        **kwargs: Any,
    ) -> list[list[float]]:
        """Async version of :meth:`ToTra.embed`."""
        payload: dict[str, Any] = {"model": model, "input": input, **kwargs}
        try:
            response = await self._client.post(
                f"{self._base_url}{_EMBED_PATH}", json=payload
            )
        except httpx.ConnectError as exc:
            raise ToTraConnectionError(str(exc)) from exc
        _check_response(response)
        return _extract_embeddings(response.json())

    async def close(self) -> None:
        """Close the underlying async HTTP client."""
        await self._client.aclose()

    async def __aenter__(self) -> "AsyncToTra":
        return self

    async def __aexit__(self, *_: object) -> None:
        await self.close()
