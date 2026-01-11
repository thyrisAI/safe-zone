import os
from tszclient_py import TSZClient, TSZConfig, DetectRequest
from retriever import retrieve_docs

print("\n=== TSZ Secured RAG Pipeline ===\n")

# TSZ config
config = TSZConfig(
    base_url=os.getenv("TSZ_BASE_URL", "http://localhost:8080"),
    timeout=30
)

client = TSZClient(config)

query = "Summarize employee records"

# Step 1
print("[USER QUERY]")
print(query)

# Step 2
docs = retrieve_docs(query)

print("\n[RETRIEVED DOCS]")
print(docs)

# Step 3
combined_input = f"""
User question:
{query}

Retrieved documents:
{docs}
"""

# Step 4
req = DetectRequest(
    text=combined_input,
    guardrails=["PII", "PROMPT_INJECTION"],
    rid="RID-RAG-001"
)

# Step 5
resp = client.detect(req)

print("\n[TSZ DECISION]")
print("Status:", "BLOCKED" if resp.blocked else "ALLOWED")
print("Message:", resp.message)
print("Overall confidence:", resp.overall_confidence)

# Step 6 – Detailed reasons
if resp.detections:
    print("\n[DETECTIONS]")
    for d in resp.detections:
        print(
            f"- {d.type} → {d.value} "
            f"(confidence={d.confidence_score})"
        )

# Step 7
if resp.blocked:
    print("\n[LLM] ❌ Blocked by TSZ")
else:
    print("\n[LLM] ✅ Would be executed safely")
