# utils.py

def print_report(result):
    print(f"\n[ATTACK] {result['attack_name']}")
    print(f"[REQUEST_ID] {result['request_id']}")

    if result["blocked"]:
        print("[STATUS] BLOCKED")
        print(f"[BLOCK_SOURCE] {result['block_source']}")

        if result["reasons"]:
            print(f"[REASONS] {', '.join(result['reasons'])}")
        else:
            print("[REASONS] NONE")

        print(f"[CONFIDENCE] {result['confidence']}")
        print("[LLM] ❌ Not executed (blocked by TSZ)")
    else:
        print("[STATUS] ALLOWED")
        print("[LLM] ✅ Would be executed safely")

    print("-" * 50)
