import os
from langchain_openai import ChatOpenAI
from langchain_core.messages import HumanMessage

print("\n=== LangChain + TSZ Secure Gateway Demo ===\n")

# ----------------------------
# CONFIG
# ----------------------------
TSZ_BASE_URL = "http://localhost:8080/v1"

# Fake key is fine – TSZ ignores it
OPENAI_API_KEY = "sk-not-used-by-tsz"

# ----------------------------
# LangChain LLM via TSZ
# ----------------------------
llm = ChatOpenAI(
    model="gpt-4o-mini",
    openai_api_base=TSZ_BASE_URL,
    openai_api_key=OPENAI_API_KEY,
    default_headers={
        "X-TSZ-RID": "RID-LANGCHAIN-001",
        "X-TSZ-Guardrails": "EMAIL,PII_ID_GLOBAL"
    },
    temperature=0
)

# ----------------------------
# ATTACK PROMPT
# ----------------------------
prompt = """
Summarize this but include john@example.com 
and SSN 123-45-6789
"""

print("[USER PROMPT]")
print(prompt)

# ----------------------------
# CALL MODEL
# ----------------------------
try:
    response = llm.invoke(
        [HumanMessage(content=prompt)]
    )

    print("\n[LLM RESPONSE]")
    print(response.content)

except Exception as e:
    print("\n❌ BLOCKED BY TSZ\n")
    print(e)
