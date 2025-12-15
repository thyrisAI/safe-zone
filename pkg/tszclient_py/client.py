from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any, Dict, List, Mapping, Optional

import json
import urllib.parse

import requests


@dataclass
class TSZConfig:
    """Client configuration for talking to a TSZ gateway.

    Attributes
    ----------
    base_url: str
        Base URL of the TSZ HTTP endpoint, for example::

            http://localhost:8080
            https://tsz-gateway.your-company.com

    session: Optional[requests.Session]
        Optional custom :class:`requests.Session`. If ``None``, a
        short‑lived session is created per request.
    timeout: float
        Timeout in seconds for HTTP requests. Defaults to 60.0s.
    """

    base_url: str
    session: Optional[requests.Session] = None
    timeout: float = 60.0


class APIError(Exception):
    """HTTP/API level error returned by TSZ.

    Raised when TSZ responds with a non‑2xx HTTP status code.
    """

    def __init__(self, status_code: int, body: bytes):
        self.status_code = status_code
        self.body = body or b""
        super().__init__(self.__str__())

    def __str__(self) -> str:  # pragma: no cover - trivial
        if not self.body:
            return f"tsz api error: status={self.status_code}"
        try:
            body_str = self.body.decode("utf-8", errors="replace")
        except Exception:
            body_str = repr(self.body)
        return f"tsz api error: status={self.status_code} body={body_str}"


@dataclass
class DetectRequest:
    text: str
    rid: str | None = None
    expected_format: str | None = None
    guardrails: List[str] = field(default_factory=list)

    def to_payload(self) -> Dict[str, Any]:
        payload: Dict[str, Any] = {"text": self.text}
        if self.rid:
            payload["rid"] = self.rid
        if self.expected_format:
            payload["expected_format"] = self.expected_format
        if self.guardrails:
            payload["guardrails"] = list(self.guardrails)
        return payload


@dataclass
class DetectionResult:
    type: str
    value: str
    placeholder: str
    start: int
    end: int
    confidence_score: str
    confidence_explanation: Optional[Dict[str, Any]] = None


@dataclass
class ValidatorResult:
    name: str
    type: str
    passed: bool
    confidence_score: str


@dataclass
class DetectResponse:
    redacted_text: str | None
    detections: List[DetectionResult]
    validator_results: List[ValidatorResult]
    breakdown: Dict[str, int]
    blocked: bool
    contains_pii: bool
    overall_confidence: str
    message: str | None = None

    @staticmethod
    def from_dict(data: Mapping[str, Any]) -> "DetectResponse":
        detections = [
            DetectionResult(
                type=it.get("type", ""),
                value=it.get("value", ""),
                placeholder=it.get("placeholder", ""),
                start=int(it.get("start", 0)),
                end=int(it.get("end", 0)),
                confidence_score=str(it.get("confidence_score", "")),
                confidence_explanation=it.get("confidence_explanation"),
            )
            for it in data.get("detections", []) or []
        ]
        validators = [
            ValidatorResult(
                name=it.get("name", ""),
                type=it.get("type", ""),
                passed=bool(it.get("passed", False)),
                confidence_score=str(it.get("confidence_score", "")),
            )
            for it in data.get("validator_results", []) or []
        ]
        return DetectResponse(
            redacted_text=data.get("redacted_text"),
            detections=detections,
            validator_results=validators,
            breakdown={k: int(v) for k, v in (data.get("breakdown") or {}).items()},
            blocked=bool(data.get("blocked", False)),
            contains_pii=bool(data.get("contains_pii", False)),
            overall_confidence=str(data.get("overall_confidence", "")),
            message=data.get("message"),
        )


ChatCompletionResponse = Dict[str, Any]


@dataclass
class ChatCompletionRequest:
    model: str
    messages: List[Dict[str, Any]]
    stream: bool = False
    extra: Dict[str, Any] = field(default_factory=dict)

    def to_payload(self) -> Dict[str, Any]:
        payload: Dict[str, Any] = {
            "model": self.model,
            "messages": self.messages,
        }
        if self.stream:
            payload["stream"] = True
        # Merge any extra vendor‑specific fields
        payload.update(self.extra or {})
        return payload


class TSZClient:
    """Lightweight TSZ API client.

    Small, dependency‑light client for TSZ `/detect` and LLM gateway calls.
    """

    def __init__(self, config: TSZConfig):
        if not config.base_url:
            raise ValueError("base_url is required")
        self._base_url = _normalize_base_url(config.base_url)
        self._session = config.session
        self._timeout = config.timeout or 60.0

    # --- /detect ---------------------------------------------------------

    def detect(self, req: DetectRequest, *, headers: Optional[Mapping[str, str]] = None) -> DetectResponse:
        """Call the `/detect` endpoint.

        Parameters
        ----------
        req:
            :class:`DetectRequest` instance.
        headers:
            Optional extra headers to send (e.g. ``{"X-TSZ-RID": "RID-123"}``).
        """

        data = _post_json(
            base_url=self._base_url,
            path="/detect",
            body=req.to_payload(),
            headers=headers,
            session=self._session,
            timeout=self._timeout,
        )
        return DetectResponse.from_dict(data)

    # Convenience wrapper around :meth:`detect` for simple text use‑cases
    def detect_text(
        self,
        text: str,
        *,
        rid: Optional[str] = None,
        expected_format: Optional[str] = None,
        guardrails: Optional[List[str]] = None,
        headers: Optional[Mapping[str, str]] = None,
    ) -> DetectResponse:
        req = DetectRequest(
            text=text,
            rid=rid,
            expected_format=expected_format,
            guardrails=list(guardrails or []),
        )
        return self.detect(req, headers=headers)

    # --- LLM Gateway: /v1/chat/completions ------------------------------

    def chat_completions(
        self,
        req: ChatCompletionRequest,
        headers: Optional[Mapping[str, str]] = None,
    ) -> ChatCompletionResponse:
        """Call the OpenAI‑compatible LLM gateway (`/v1/chat/completions`)."""

        data = _post_json(
            base_url=self._base_url,
            path="/v1/chat/completions",
            body=req.to_payload(),
            headers=headers,
            session=self._session,
            timeout=self._timeout,
        )
        return data


# --- Low‑level HTTP helpers ----------------------------------------------


def _normalize_base_url(base_url: str) -> str:
    parsed = urllib.parse.urlparse(base_url)
    if not parsed.scheme:
        # Default to http if no scheme provided
        base_url = "http://" + base_url
        parsed = urllib.parse.urlparse(base_url)
    # Strip any trailing slash for consistency
    return base_url.rstrip("/")


def _post_json(
    *,
    base_url: str,
    path: str,
    body: Mapping[str, Any],
    headers: Optional[Mapping[str, str]],
    session: Optional[requests.Session],
    timeout: float,
) -> Dict[str, Any]:
    url = f"{base_url}{path}"

    payload = json.dumps(body).encode("utf-8")
    all_headers: Dict[str, str] = {"Content-Type": "application/json"}
    if headers:
        all_headers.update(headers)

    sess = session or requests.Session()
    try:
        resp = sess.post(url, data=payload, headers=all_headers, timeout=timeout)
    except requests.RequestException as exc:  # pragma: no cover - passthrough
        raise RuntimeError(f"http request failed: {exc}") from exc
    finally:
        if session is None:
            sess.close()

    if resp.status_code < 200 or resp.status_code >= 300:
        # Keep raw bytes so callers can inspect raw TSZ error JSON
        raise APIError(resp.status_code, resp.content)

    try:
        data = resp.json()
    except ValueError as exc:  # pragma: no cover - passthrough
        raise RuntimeError("failed to decode response body") from exc

    if not isinstance(data, dict):  # pragma: no cover - defensive
        raise RuntimeError("expected JSON object in response body")

    return data
