import os
import uuid
from dotenv import load_dotenv

from tszclient_py import TSZClient
from tszclient_py.client import TSZConfig

from attacks import ATTACKS
from utils import print_report

load_dotenv()

SENSITIVE_CONTEXT = """
User profile:
Name: Ayush Sharma
Email: ayush@example.com
Credit Card: 4111 1111 1111 1111
"""

def main():
    print("\n=== TSZ Data Exfiltration Red-Team Demo (Python) ===\n")

    config = TSZConfig(
        base_url=os.getenv("TSZ_BASE_URL", "http://localhost:8080"),
        timeout=30,
    )

    client = TSZClient(config)

    for attack in ATTACKS:
        rid = f"RID-EXFIL-{uuid.uuid4().hex[:8]}"
        print(f"[ATTACK] {attack['name']}")

        resp = client.detect_text(
            text=SENSITIVE_CONTEXT + "\n\nInstruction:\n" + attack["prompt"],
            rid=rid,
            guardrails=["PII"],
        )

        reasons = set()
        confidence = 0.0

        # ✅ Correct attribute access (NOT dict access)
        for d in resp.detections:
            reasons.add(d.type)
            confidence = max(confidence, float(d.confidence_score))

        print_report({
            "request_id": rid,
            "blocked": resp.blocked,
            "block_source": "DETECTION" if resp.blocked else None,
            "reasons": list(reasons),
            "confidence": confidence,
        })

if __name__ == "__main__":
    main()
