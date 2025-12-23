"""Python SDK demo – using the `tszclient_py` Python package.

This example uses the `tszclient_py` Python client, which is installable
from this GitHub repository via pip.

It demonstrates both:

- Calling the core `/detect` API for PII detection and guardrails
- Calling the OpenAI‑compatible LLM gateway (`/v1/chat/completions`)

Prerequisites:
- TSZ is running locally and accessible at http://localhost:8080
- The upstream LLM is configured and reachable (see main README/QUICK_START)
- `AI_MODEL` (or `TSZ_MODEL`) is set in your TSZ environment – the same
  model name is used here for the demo call.
- The Python client is installed, for example:

    pip install "tszclient-py @ git+https://github.com/thyrisAI/safe-zone.git@main"

Run from repo root (after installing the package):

    python -m examples.python-sdk-demo.main
"""

from __future__ import annotations

import os

from tszclient_py import (
    TSZClient,
    TSZConfig,
    ChatCompletionRequest,
)


def main() -> None:
    base_url = os.getenv("TSZ_BASE_URL", "http://localhost:8080")

    client = TSZClient(TSZConfig(base_url=base_url))

    # --- /detect example -------------------------------------------------
    print("[DETECT] Calling /detect via Python client...")
    detect_resp = client.detect_text(
        "Contact me at john@example.com",
        rid="RID-PY-001",
        guardrails=["TOXIC_LANGUAGE"],
    )

    if detect_resp.blocked:
        print(f"Request blocked by TSZ: {detect_resp.message}")
    else:
        print("Redacted text:")
        print(detect_resp.redacted_text)

    # --- LLM gateway example --------------------------------------------
    print("\n[LLM] Calling /v1/chat/completions via Python client...")

    # Model resolution logic mirrors the Go gateway test helper:
    #   1) AI_MODEL
    #   2) TSZ_MODEL
    #   3) fallback "llama3.1:8b"
    model = os.getenv("AI_MODEL") or os.getenv("TSZ_MODEL") or "llama3.1:8b"

    chat_req = ChatCompletionRequest(
        model=model,
        messages=[{"role": "user", "content": "Hello via TSZ gateway (Python)"}],
        stream=False,
        extra={},
    )

    resp = client.chat_completions(
        chat_req,
        headers={
            "X-TSZ-RID": "RID-GW-PY-001",
            "X-TSZ-Guardrails": "TOXIC_LANGUAGE",
        },
    )

    choices = resp.get("choices") or []
    if not choices:
        print("No choices in response")
        return

    first = choices[0] or {}
    msg = first.get("message") or {}
    content = msg.get("content") or "<no content>"

    print("LLM response via TSZ:")
    print(content)


if __name__ == "__main__":  # pragma: no cover - manual demo
    main()
