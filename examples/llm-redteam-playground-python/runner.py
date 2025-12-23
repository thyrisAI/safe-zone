# runner.py

import uuid

def run_attack(client, base_input: str, attack: dict):
    rid = f"RID-REDTEAM-{uuid.uuid4().hex[:8]}"

    combined_prompt = f"""
User input:
{base_input}

Attack instruction:
{attack['prompt']}
"""

    guardrails = []

    if attack["category"] == "PROMPT_INJECTION":
        guardrails.append("PROMPT_INJECTION_SIMPLE")

    if attack["category"] == "DATA_EXFILTRATION":
        guardrails.append("PII")

    detect_resp = client.detect_text(
        combined_prompt,
        rid=rid,
        guardrails=guardrails,
    )

    # Determine block source
    if detect_resp.detections:
        reasons = [d.type for d in detect_resp.detections]
        block_source = "DETECTION"
    elif detect_resp.blocked:
        reasons = guardrails
        block_source = "POLICY_VALIDATOR"
    else:
        reasons = []
        block_source = "NONE"

    result = {
        "attack_id": attack["id"],
        "attack_name": attack["name"],
        "category": attack["category"],
        "request_id": rid,
        "blocked": detect_resp.blocked,
        "confidence": detect_resp.overall_confidence,
        "reasons": reasons,
        "block_source": block_source,
        "redacted_text": detect_resp.redacted_text,
    }

    return result
