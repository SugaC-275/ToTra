"""Typed dataclasses for ToTra SDK response objects."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any


@dataclass
class ChatMessage:
    role: str
    content: str


@dataclass
class ChatChoice:
    index: int
    message: ChatMessage
    finish_reason: str


@dataclass
class Usage:
    prompt_tokens: int
    completion_tokens: int
    total_tokens: int


@dataclass
class ChatCompletion:
    id: str
    model: str
    choices: list[ChatChoice]
    usage: Usage

    @classmethod
    def from_dict(cls, d: dict[str, Any]) -> "ChatCompletion":
        choices = [
            ChatChoice(
                index=c.get("index", 0),
                message=ChatMessage(
                    role=c["message"]["role"],
                    content=c["message"].get("content", ""),
                ),
                finish_reason=c.get("finish_reason", ""),
            )
            for c in d.get("choices", [])
        ]
        usage_raw = d.get("usage", {})
        usage = Usage(
            prompt_tokens=usage_raw.get("prompt_tokens", 0),
            completion_tokens=usage_raw.get("completion_tokens", 0),
            total_tokens=usage_raw.get("total_tokens", 0),
        )
        return cls(
            id=d.get("id", ""),
            model=d.get("model", ""),
            choices=choices,
            usage=usage,
        )
