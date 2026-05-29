"""ToTra Python SDK — synchronous and asynchronous clients."""

from __future__ import annotations

import json
import time
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
_PROMPTS_PATH = "/v1/prompts"
_BUDGET_PATH = "/v1/key/budget"

_RETRYABLE_STATUSES = {429, 502, 503}


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
# OpenAI compat sub-objects
# ---------------------------------------------------------------------------


class _Completions:
    """Mimics openai.chat.completions with a .create() method."""

    def __init__(self, client: "ToTra") -> None:
        self._c = client

    def create(
        self,
        model: str,
        messages: list[dict[str, Any]],
        *,
        stream: bool = False,
        **kwargs: Any,
    ) -> Any:
        """Create a chat completion. Mirrors openai.chat.completions.create()."""
        if stream:
            return self._c.stream(model, messages, **kwargs)
        return self._c.complete(model, messages, **kwargs)


class _Chat:
    """Mimics the openai.chat namespace."""

    def __init__(self, client: "ToTra") -> None:
        self.completions = _Completions(client)


# ---------------------------------------------------------------------------
# Prompts clients
# ---------------------------------------------------------------------------


class _PromptsClient:
    """Manage prompt templates stored in the ToTra gateway."""

    def __init__(self, client: "ToTra") -> None:
        self._c = client

    def list(self) -> list[dict[str, Any]]:
        """List all saved prompt templates. GET /v1/prompts"""
        return self._c._get(_PROMPTS_PATH)  # type: ignore[return-value]

    def get(self, name: str) -> dict[str, Any]:
        """Fetch a single prompt template by name. GET /v1/prompts/{name}"""
        return self._c._get(f"{_PROMPTS_PATH}/{name}")  # type: ignore[return-value]

    def save(self, name: str, content: str) -> dict[str, Any]:
        """Save or update a prompt template. POST /v1/prompts"""
        return self._c._post(_PROMPTS_PATH, {"name": name, "content": content})  # type: ignore[return-value]

    def render(self, name: str, vars: dict[str, str]) -> str:  # noqa: A002
        """Render a prompt template with variables. POST /v1/prompts/{name}/render"""
        result: dict[str, Any] = self._c._post(  # type: ignore[assignment]
            f"{_PROMPTS_PATH}/{name}/render", {"variables": vars}
        )
        return result.get("rendered", "")


class _AsyncPromptsClient:
    """Async version of _PromptsClient."""

    def __init__(self, client: "AsyncToTra") -> None:
        self._c = client

    async def list(self) -> list[dict[str, Any]]:
        return await self._c._get(_PROMPTS_PATH)  # type: ignore[return-value]

    async def get(self, name: str) -> dict[str, Any]:
        return await self._c._get(f"{_PROMPTS_PATH}/{name}")  # type: ignore[return-value]

    async def save(self, name: str, content: str) -> dict[str, Any]:
        return await self._c._post(_PROMPTS_PATH, {"name": name, "content": content})  # type: ignore[return-value]

    async def render(self, name: str, vars: dict[str, str]) -> str:  # noqa: A002
        result: dict[str, Any] = await self._c._post(  # type: ignore[assignment]
            f"{_PROMPTS_PATH}/{name}/render", {"variables": vars}
        )
        return result.get("rendered", "")


# ---------------------------------------------------------------------------
# Evals clients
# ---------------------------------------------------------------------------


class _EvalsClient:
    """Manage evaluation suites and runs."""

    def __init__(self, client: "ToTra") -> None:
        self._c = client

    def create_suite(self, name: str, prompt_name: str) -> dict[str, Any]:
        """Create an eval suite. POST /v1/evals/suites"""
        return self._c._post("/v1/evals/suites", {"name": name, "prompt_name": prompt_name})  # type: ignore[return-value]

    def list_suites(self) -> list[dict[str, Any]]:
        """List all eval suites. GET /v1/evals/suites"""
        return self._c._get("/v1/evals/suites")  # type: ignore[return-value]

    def add_case(
        self,
        suite_id: str,
        input_vars: dict[str, Any],
        expected: str = "",
        contains: list[str] | None = None,
        method: str = "contains",
    ) -> dict[str, Any]:
        """Add a test case to a suite. POST /v1/evals/suites/{suite_id}/cases"""
        body: dict[str, Any] = {
            "input_vars": input_vars,
            "expected": expected,
            "method": method,
        }
        if contains is not None:
            body["contains"] = contains
        return self._c._post(f"/v1/evals/suites/{suite_id}/cases", body)  # type: ignore[return-value]

    def run(
        self,
        suite_id: str,
        model: str,
        prompt_version: int | None = None,
    ) -> dict[str, Any]:
        """Trigger an eval run. POST /v1/evals/suites/{suite_id}/runs"""
        body: dict[str, Any] = {"model": model}
        if prompt_version is not None:
            body["prompt_version"] = prompt_version
        return self._c._post(f"/v1/evals/suites/{suite_id}/runs", body)  # type: ignore[return-value]

    def get_run(self, run_id: str) -> dict[str, Any]:
        """Fetch a run by ID. GET /v1/evals/runs/{run_id}"""
        return self._c._get(f"/v1/evals/runs/{run_id}")  # type: ignore[return-value]

    def wait_for_run(
        self,
        run_id: str,
        poll_interval: float = 2.0,
        timeout: float = 300.0,
    ) -> dict[str, Any]:
        """Poll until run.status is 'completed' or 'failed'. Returns final run dict."""
        deadline = time.monotonic() + timeout
        while True:
            run = self.get_run(run_id)
            status = run.get("status", "")
            if status in ("completed", "failed"):
                return run
            if time.monotonic() >= deadline:
                raise TimeoutError(f"Eval run {run_id} did not finish within {timeout}s")
            time.sleep(poll_interval)


class _AsyncEvalsClient:
    """Async version of _EvalsClient."""

    def __init__(self, client: "AsyncToTra") -> None:
        self._c = client

    async def create_suite(self, name: str, prompt_name: str) -> dict[str, Any]:
        return await self._c._post("/v1/evals/suites", {"name": name, "prompt_name": prompt_name})  # type: ignore[return-value]

    async def list_suites(self) -> list[dict[str, Any]]:
        return await self._c._get("/v1/evals/suites")  # type: ignore[return-value]

    async def add_case(
        self,
        suite_id: str,
        input_vars: dict[str, Any],
        expected: str = "",
        contains: list[str] | None = None,
        method: str = "contains",
    ) -> dict[str, Any]:
        body: dict[str, Any] = {
            "input_vars": input_vars,
            "expected": expected,
            "method": method,
        }
        if contains is not None:
            body["contains"] = contains
        return await self._c._post(f"/v1/evals/suites/{suite_id}/cases", body)  # type: ignore[return-value]

    async def run(
        self,
        suite_id: str,
        model: str,
        prompt_version: int | None = None,
    ) -> dict[str, Any]:
        body: dict[str, Any] = {"model": model}
        if prompt_version is not None:
            body["prompt_version"] = prompt_version
        return await self._c._post(f"/v1/evals/suites/{suite_id}/runs", body)  # type: ignore[return-value]

    async def get_run(self, run_id: str) -> dict[str, Any]:
        return await self._c._get(f"/v1/evals/runs/{run_id}")  # type: ignore[return-value]

    async def wait_for_run(
        self,
        run_id: str,
        poll_interval: float = 2.0,
        timeout: float = 300.0,
    ) -> dict[str, Any]:
        """Async poll until run.status is 'completed' or 'failed'."""
        import asyncio  # noqa: PLC0415
        deadline = time.monotonic() + timeout
        while True:
            run = await self.get_run(run_id)
            status = run.get("status", "")
            if status in ("completed", "failed"):
                return run
            if time.monotonic() >= deadline:
                raise TimeoutError(f"Eval run {run_id} did not finish within {timeout}s")
            await asyncio.sleep(poll_interval)


# ---------------------------------------------------------------------------
# Budget clients
# ---------------------------------------------------------------------------


class _BudgetClient:
    """Manage per-user budgets and rate limits."""

    def __init__(self, client: "ToTra") -> None:
        self._c = client

    def get(self, user_id: str) -> dict[str, Any]:
        """Fetch budget info for a user. GET /v1/key/budget?user_id="""
        return self._c._get(f"{_BUDGET_PATH}?user_id={user_id}")  # type: ignore[return-value]

    def set(
        self,
        user_id: str,
        budget_usd: float | None = None,
        period: str = "monthly",
        rpm_limit: int | None = None,
    ) -> dict[str, Any]:
        """Set budget and/or rate limits for a user. PUT /v1/key/budget"""
        body: dict[str, Any] = {"user_id": user_id, "period": period}
        if budget_usd is not None:
            body["budget_usd"] = budget_usd
        if rpm_limit is not None:
            body["rpm_limit"] = rpm_limit
        return self._c._put(_BUDGET_PATH, body)  # type: ignore[return-value]


class _AsyncBudgetClient:
    """Async version of _BudgetClient."""

    def __init__(self, client: "AsyncToTra") -> None:
        self._c = client

    async def get(self, user_id: str) -> dict[str, Any]:
        return await self._c._get(f"{_BUDGET_PATH}?user_id={user_id}")  # type: ignore[return-value]

    async def set(
        self,
        user_id: str,
        budget_usd: float | None = None,
        period: str = "monthly",
        rpm_limit: int | None = None,
    ) -> dict[str, Any]:
        body: dict[str, Any] = {"user_id": user_id, "period": period}
        if budget_usd is not None:
            body["budget_usd"] = budget_usd
        if rpm_limit is not None:
            body["rpm_limit"] = rpm_limit
        return await self._c._put(_BUDGET_PATH, body)  # type: ignore[return-value]


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
    retries:
        Number of retry attempts on 429/502/503 responses (default 2).
    fallback_models:
        Ordered list of fallback model identifiers to try if all retries
        on the primary model are exhausted.
    """

    def __init__(
        self,
        api_key: str,
        base_url: str,
        timeout: float = 120.0,
        retries: int = 2,
        fallback_models: list[str] | None = None,
    ) -> None:
        self._base_url = base_url.rstrip("/")
        self._client = httpx.Client(
            headers={
                "Authorization": f"Bearer {api_key}",
                "Content-Type": "application/json",
            },
            timeout=timeout,
        )
        self._retries = retries
        self._fallback_models = fallback_models or []

        # OpenAI-compat namespace: client.chat.completions.create(...)
        self.chat = _Chat(self)

        # Sub-clients
        self.prompts = _PromptsClient(self)
        self.evals = _EvalsClient(self)
        self.budget = _BudgetClient(self)

    # ------------------------------------------------------------------
    # Private HTTP helpers
    # ------------------------------------------------------------------

    def _get(self, path: str) -> Any:
        """Issue a GET request and return the parsed JSON body."""
        try:
            response = self._client.get(f"{self._base_url}{path}")
        except httpx.ConnectError as exc:
            raise ToTraConnectionError(str(exc)) from exc
        _check_response(response)
        return response.json()

    def _post(self, path: str, body: dict[str, Any]) -> Any:
        """Issue a POST request and return the parsed JSON body."""
        try:
            response = self._client.post(f"{self._base_url}{path}", json=body)
        except httpx.ConnectError as exc:
            raise ToTraConnectionError(str(exc)) from exc
        _check_response(response)
        return response.json()

    def _put(self, path: str, body: dict[str, Any]) -> Any:
        """Issue a PUT request and return the parsed JSON body."""
        try:
            response = self._client.put(f"{self._base_url}{path}", json=body)
        except httpx.ConnectError as exc:
            raise ToTraConnectionError(str(exc)) from exc
        _check_response(response)
        return response.json()

    def _chat_raw(
        self,
        model: str,
        messages: list[dict[str, Any]],
        *,
        max_tokens: int | None = None,
        temperature: float | None = None,
        system: str | None = None,
        **kwargs: Any,
    ) -> dict[str, Any]:
        """Execute a single non-streaming chat-completions POST (no retry logic)."""
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

    # ------------------------------------------------------------------
    # Public API
    # ------------------------------------------------------------------

    def complete(
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

        Retries on 429/502/503 up to ``self._retries`` times with exponential
        backoff. Falls back to ``self._fallback_models`` if all retries fail.

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
            System prompt; prepended as a ``{"role": "system"}`` message.
        **kwargs:
            Any additional OpenAI-compatible parameters forwarded verbatim.

        Returns
        -------
        dict
            Full OpenAI-compatible response object.

        Raises
        ------
        ToTraError
            On HTTP 4xx / 5xx responses after all retries and fallbacks.
        ToTraConnectionError
            When the gateway cannot be reached.
        """
        call_kwargs = dict(
            max_tokens=max_tokens,
            temperature=temperature,
            system=system,
            **kwargs,
        )
        models_to_try = [model] + list(self._fallback_models)
        last_exc: ToTraError | None = None

        for candidate in models_to_try:
            for attempt in range(self._retries + 1):
                try:
                    return self._chat_raw(candidate, messages, **call_kwargs)
                except ToTraError as exc:
                    last_exc = exc
                    if exc.status_code not in _RETRYABLE_STATUSES:
                        raise
                    if attempt < self._retries:
                        time.sleep(0.5 * (2 ** attempt))
            # All retries for this candidate exhausted; try next fallback

        assert last_exc is not None
        raise last_exc

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
        input: str | list[str],  # noqa: A002
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
    retries:
        Number of retry attempts on 429/502/503 responses (default 2).
    fallback_models:
        Ordered list of fallback model identifiers to try if all retries
        on the primary model are exhausted.
    """

    def __init__(
        self,
        api_key: str,
        base_url: str,
        timeout: float = 120.0,
        retries: int = 2,
        fallback_models: list[str] | None = None,
    ) -> None:
        self._base_url = base_url.rstrip("/")
        self._client = httpx.AsyncClient(
            headers={
                "Authorization": f"Bearer {api_key}",
                "Content-Type": "application/json",
            },
            timeout=timeout,
        )
        self._retries = retries
        self._fallback_models = fallback_models or []

        # Sub-clients
        self.prompts = _AsyncPromptsClient(self)
        self.evals = _AsyncEvalsClient(self)
        self.budget = _AsyncBudgetClient(self)

    # ------------------------------------------------------------------
    # Private HTTP helpers
    # ------------------------------------------------------------------

    async def _get(self, path: str) -> Any:
        try:
            response = await self._client.get(f"{self._base_url}{path}")
        except httpx.ConnectError as exc:
            raise ToTraConnectionError(str(exc)) from exc
        _check_response(response)
        return response.json()

    async def _post(self, path: str, body: dict[str, Any]) -> Any:
        try:
            response = await self._client.post(f"{self._base_url}{path}", json=body)
        except httpx.ConnectError as exc:
            raise ToTraConnectionError(str(exc)) from exc
        _check_response(response)
        return response.json()

    async def _put(self, path: str, body: dict[str, Any]) -> Any:
        try:
            response = await self._client.put(f"{self._base_url}{path}", json=body)
        except httpx.ConnectError as exc:
            raise ToTraConnectionError(str(exc)) from exc
        _check_response(response)
        return response.json()

    async def _chat_raw(
        self,
        model: str,
        messages: list[dict[str, Any]],
        *,
        max_tokens: int | None = None,
        temperature: float | None = None,
        system: str | None = None,
        **kwargs: Any,
    ) -> dict[str, Any]:
        """Execute a single non-streaming chat-completions POST (no retry logic)."""
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
        """Async chat-completions with retry and fallback logic.

        Retries on 429/502/503 up to ``self._retries`` times with exponential
        backoff. Falls back to ``self._fallback_models`` if all retries fail.
        """
        import asyncio  # noqa: PLC0415

        call_kwargs = dict(
            max_tokens=max_tokens,
            temperature=temperature,
            system=system,
            **kwargs,
        )
        models_to_try = [model] + list(self._fallback_models)
        last_exc: ToTraError | None = None

        for candidate in models_to_try:
            for attempt in range(self._retries + 1):
                try:
                    return await self._chat_raw(candidate, messages, **call_kwargs)
                except ToTraError as exc:
                    last_exc = exc
                    if exc.status_code not in _RETRYABLE_STATUSES:
                        raise
                    if attempt < self._retries:
                        await asyncio.sleep(0.5 * (2 ** attempt))

        assert last_exc is not None
        raise last_exc

    # Alias for OpenAI parity on the async client
    complete = chat

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
        """Async streaming version of chat.

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
        input: str | list[str],  # noqa: A002
        **kwargs: Any,
    ) -> list[list[float]]:
        """Async version of embed."""
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
