"""
Minimal LLM Safe Prompt Example using TSZ

Flow:
User Input → TSZ Detect & Redact → Safe Prompt → LLM
"""

import os
from tszclient_py import TSZClient, TSZConfig


def main() -> None:
    base_url = os.getenv("TSZ_BASE_URL", "http://localhost:8080")
    client = TSZClient(TSZConfig(base_url=base_url))

    user_prompt = """
    Hi, my name is Ayush Sharma.
    Email: ayush@example.com
    Credit Card: 4111 1111 1111 1111
    Please summarize this.
    """

    resp = client.detect_text(
        user_prompt,
        rid="RID-LLM-SAFE-001",
    )

    if resp.blocked:
        print("Prompt blocked by TSZ:")
        print(resp.message)
        return

    print("Original prompt:\n", user_prompt)
    print("\nRedacted prompt (safe for LLM):\n")
    print(resp.redacted_text)

    # This is the prompt you should send to your LLM
    safe_prompt = resp.redacted_text


if __name__ == "__main__":
    main()
