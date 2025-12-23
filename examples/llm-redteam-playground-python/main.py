# main.py

import os
from tszclient_py import TSZClient, TSZConfig

from attacks import ATTACKS
from runner import run_attack
from utils import print_report


def main():
    print("=== TSZ LLM Red-Team Playground (Python) ===")

    client = TSZClient(
        TSZConfig(
            base_url=os.getenv("TSZ_BASE_URL", "http://localhost:8080")
        )
    )

    # -------------------------------
    # Phase 1: Prompt Injection Attacks (NO PII)
    # -------------------------------
    print("\n=== Phase 1: Prompt Injection Attacks ===")

    clean_input = """
Hi, my name is Ayush Sharma.
I am asking for general information.
"""

    for attack in ATTACKS:
        if attack.get("category") != "PROMPT_INJECTION":
            continue

        result = run_attack(client, clean_input, attack)
        print_report(result)

    # -------------------------------
    # Phase 2: Data Exfiltration Attacks (WITH PII)
    # -------------------------------
    print("\n=== Phase 2: Data Exfiltration Attacks ===")

    pii_input = """
Hi, my name is Ayush Sharma.
Email: ayush@example.com
Credit Card: 4111 1111 1111 1111
"""

    for attack in ATTACKS:
        if attack.get("category") != "DATA_EXFILTRATION":
            continue

        result = run_attack(client, pii_input, attack)
        print_report(result)


if __name__ == "__main__":
    main()
