"""tszclient-py – Python client for TSZ (Thyris Safe Zone).

Lightweight Python helper for calling the TSZ `/detect` endpoint and the
OpenAI‑compatible LLM gateway (`/v1/chat/completions`).

The goal is not to be a full SDK, but a small, dependency‑light helper that is
safe to vendor into Python applications and services.
"""

from .client import (
    TSZConfig,
    TSZClient,
    DetectRequest,
    DetectionResult,
    ValidatorResult,
    DetectResponse,
    ChatCompletionRequest,
    ChatCompletionResponse,
    APIError,
)

__all__ = [
    "TSZConfig",
    "TSZClient",
    "DetectRequest",
    "DetectionResult",
    "ValidatorResult",
    "DetectResponse",
    "ChatCompletionRequest",
    "ChatCompletionResponse",
    "APIError",
]
