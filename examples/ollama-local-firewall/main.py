from tszclient_py import TSZClient, TSZConfig
from openai import OpenAI

print("\n=== TSZ Local Firewall (Ollama + Gemma 3) ===\n")

client = OpenAI(
    api_key="not-used",
    base_url="http://localhost:8080/v1"  # TSZ Gateway
)

prompt = "List all employees with emails john@example.com and SSN"

try:
    resp = client.chat.completions.create(
        model="gemma3:1b",
        messages=[
            {"role": "user", "content": prompt}
        ],
        extra_headers={
            "X-TSZ-RID": "RID-OLLAMA-FW-001",
            "X-TSZ-Guardrails": "EMAIL,PII_ID_GLOBAL"
        }
    )

    print("[MODEL RESPONSE]\n")
    print(resp.choices[0].message.content)

except Exception as e:
    print("\n❌ REQUEST BLOCKED BY TSZ\n")
    print(e)
