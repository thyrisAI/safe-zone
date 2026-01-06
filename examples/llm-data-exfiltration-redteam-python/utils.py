def print_report(result):
    print(f"[REQUEST_ID] {result['request_id']}")
    print(f"[STATUS] {'BLOCKED' if result['blocked'] else 'ALLOWED'}")

    if result.get("block_source"):
        print(f"[BLOCK_SOURCE] {result['block_source']}")

    if result.get("reasons"):
        print(f"[REASONS] {', '.join(result['reasons'])}")

    if result.get("confidence") is not None:
        print(f"[CONFIDENCE] {result['confidence']:.2f}")

    if result["blocked"]:
        print("[LLM] ❌ Not executed (blocked by TSZ)")
    else:
        print("[LLM] ✅ Would be executed safely")

    print("-" * 50)
