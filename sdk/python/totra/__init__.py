"""ToTra Python SDK."""

from .client import AsyncToTra, ToTra, ToTraConnectionError, ToTraError
from .types import ChatCompletion, ChatChoice, ChatMessage, Usage

# OpenAI drop-in aliases — swap `from openai import OpenAI` for `from totra import OpenAI`
OpenAI = ToTra
AsyncOpenAI = AsyncToTra

__all__ = [
    "ToTra",
    "AsyncToTra",
    "ToTraError",
    "ToTraConnectionError",
    "OpenAI",
    "AsyncOpenAI",
    "ChatCompletion",
    "ChatChoice",
    "ChatMessage",
    "Usage",
]
__version__ = "0.1.0"
