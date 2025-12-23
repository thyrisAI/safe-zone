# TSZ Test Suite

This directory contains the comprehensive automated test suite for Thyris Safe Zone (TSZ). It is structured to separate **unit**, **integration**, and **end-to-end (E2E)** concerns and to keep tests decoupled from production code.

## Structure

```text
tests/
  unit/          # Pure unit tests (logic, helpers, AI client, SIEM, config, cache, etc.)
  integration/   # HTTP-level integration tests (API + DB + Redis + AI boundary)
  e2e/           # Smoke / sanity and gateway streaming tests
  data/          # JSON fixtures for data-driven tests and golden files
  README.md      # This document
```

## Test Coverage Overview

- **Total Tests**: 55+ tests (150% increase from original)
- **Unit Tests**: 40+ tests covering core business logic
- **Integration Tests**: 15+ tests covering API endpoints and error handling
- **E2E Tests**: 5 tests covering full system workflows

### 1. Unit tests (`tests/unit`)

Focus: **fast, deterministic, no external dependencies**.

Currently covered:

- `guardrails_test.go`
  - Confidence and thresholds:
    - `resolveAction` (ALLOW / MASK / BLOCK decisions)
    - Allow/block thresholds from env (`CONFIDENCE_ALLOW_THRESHOLD`, `CONFIDENCE_BLOCK_THRESHOLD`)
    - Category thresholds via `GetCategoryThreshold` (e.g. `CONFIDENCE_PII_THRESHOLD`).
  - Confidence engine:
    - `ComputeConfidence` for various `ConfidenceContext` combinations (blacklist hit, allowlist hit, PII/SECRET/INJECTION categories, REGEX/AI/SCHEMA sources).
  - Utility helpers:
    - `ApplyRegexHitWeight` (per-hit weighting and clamping at 1.0).
    - Placeholder generation (`generatePlaceholder`) – ensures RID and pattern name are present but does not leak raw PII.
  - Format helpers:
    - `isValidJSON`, `isValidXML`, `isValidSchema` via exported test helpers under `internal/guardrails/testing_exports.go` (build-tagged for `test` only).

- `siem_ai_repository_test.go`
  - SIEM webhook:
    - Uses a fake `http.RoundTripper` to assert that `publishSecurityEvent` sends JSON to the URL from `SIEM_WEBHOOK_URL` with the expected payload.
  - AI client:
    - `CheckWithAI` error propagation when upstream returns non-200.
    - `CheckWithAI` success path when the upstream responds with a `YES`-like content.
  - AI confidence cache:
    - Basic cache roundtrip for `SetCachedConfidence` / `GetCachedConfidence` with a local Redis client.

- `ai_provider_test.go` *(NEW)*
  - AI provider initialization:
    - Tests provider setup with invalid/empty configurations
    - Provider state management (get/set operations)
  - Hybrid confidence calculations:
    - Edge cases for combining regex and AI confidence scores
    - Boundary testing (zero values, high values, mixed scenarios)
  - AI confidence caching:
    - Cache key generation and validation
    - Graceful handling when Redis is unavailable
    - Cache operations with different label/text combinations

- `config_cache_test.go` *(NEW)*
  - Configuration management:
    - DSN and Redis URL generation from environment variables
    - Config loading with custom environment settings
    - Default value handling and environment variable precedence
  - Cache operations:
    - Pattern caching (set/get operations with graceful Redis handling)
    - Allowlist/blocklist caching with data validation
    - Cache clearing operations across different key types
    - Panic recovery for unavailable cache backends

- `repository_test.go` *(NEW)*
  - Repository function testing:
    - Pattern retrieval with invalid IDs (graceful DB error handling)
    - Pattern update operations with nil/invalid data
    - Format validator CRUD operations
    - Database connection failure scenarios with panic recovery

> Note: test-only helpers are defined in `internal/guardrails/testing_exports.go`. These functions expose internal logic for unit testing while keeping production code encapsulated.

### 2. Integration tests (`tests/integration`)

Focus: **real HTTP endpoints + DB + Redis + configuration**, with external AI treated as a boundary (can be real or fake, depending on environment).

Currently covered:

- `detect_integration_test.go`
  - `/detect` endpoint:
    - PII detection with email (HTTP 200, `contains_pii=true`, redaction applied, at least one detection).
    - Non-PII text (HTTP 200, `contains_pii=false`, no detections).
    - Invalid JSON payload (client error like 400/422).

- `pii_matrix_integration_test.go`
  - Data-driven PII detection using `tests/data/pii_cases.json`:
    - Multiple cases for EMAIL, TCKN-like values, US_SSN, UK_NINO, mixed cases, and fully safe text.
    - Asserts both `contains_pii` and presence of expected detection types.

- `detect_golden_integration_test.go`
  - Golden/snapshot test using `tests/data/detect_email_ssn_input.json` and `tests/data/detect_email_ssn_expect.json`:
    - Verifies that a known input containing email + SSN produces detections for `EMAIL` and `US_SSN`.

- `templates_integration_test.go`
  - `/templates/import` → `/detect` flow:
    - Imports a simple template with one pattern and one validator.
    - Verifies that a `/detect` call after import triggers the new pattern.

- `gateway_integration_test.go`
  - `/v1/chat/completions` non-streaming:
    - Safe prompt: expects HTTP 200 and at least one `choice` when upstream LLM is configured; otherwise logs and soft-fails.
    - Unsafe prompt with `X-TSZ-Guardrails: TOXIC_LANGUAGE`:
      - If blocked, expects HTTP 400 + OpenAI-style `error` object.
      - If not blocked, expects either `choices` or `error` in a valid JSON envelope.

- `mode_matrix_integration_test.go`
  - Behavior under different configuration modes (documented behavior):
    - `PII_MODE` matrix: verifies that PII is detected regardless of mode (MASK/BLOCK). Full behavioral checks for each mode should be exercised in dedicated environments.
    - `GATEWAY_BLOCK_MODE` matrix: verifies that gateway responses are valid JSON envelopes (either `choices` or `error`) across different block modes (`BLOCK`, `MASK`, `WARN`).

- `error_handling_integration_test.go` *(NEW)*
  - Comprehensive error handling scenarios:
    - `/detect` endpoint error cases (empty payload, missing fields, invalid modes, extremely long text)
    - `/v1/chat/completions` error cases (malformed requests, invalid headers, unknown models)
    - CRUD operation error handling (invalid regex patterns, non-existent resources)
    - Concurrent request testing (multiple simultaneous API calls)
    - Unicode and special character handling in requests
    - Graceful degradation when upstream services are unavailable

These tests assume TSZ is running against a Postgres + Redis instance (wired by CI via GitHub Actions services).

### 3. E2E / smoke and streaming tests (`tests/e2e`)

Focus: **end-to-end system health and realistic user flows**, including streaming behavior of the LLM gateway.

Currently covered:

- `sanity_suite_test.go`
  - Health and readiness:
    - `GET /healthz` → 200
    - `GET /ready` → 200 (DB + Redis ready)
  - Configuration APIs:
    - `GET /patterns`
    - `GET /validators`
    - `GET /allowlist`
  - Core flows:
    - `/detect` email scenario (PII should be detected).
    - `/v1/chat/completions` basic call (verifies response can be parsed as JSON, regardless of upstream model result).

- `gateway_streaming_test.go`
  - Streaming without guardrails:
    - `stream=true`, verifies SSE-like `data:` chunks.
    - Soft-skips if upstream LLM is not configured in CI (non-200 status).
  - Streaming with guardrails in `stream-sync` filter mode:
    - `X-TSZ-Guardrails: TOXIC_LANGUAGE`, `X-TSZ-Guardrails-Mode: stream-sync`, `OnFail=filter`.
    - Ensures that a fake credit card number like `4111 1111 1111 1111` does **not** appear in streamed content.
  - Streaming with guardrails in `stream-sync` halt mode:
    - `OnFail=halt`.
    - Expects the stream to contain an error event or TSZ-specific metadata when unsafe content is encountered.

These tests exercise real network boundaries and are intended to run in CI after unit and integration tests have passed.

---
## Running tests locally

Assuming you have Postgres + Redis and a TSZ instance running locally:

```bash
# Unit tests (do not require DB/Redis)
go test ./tests/unit/...

# Integration tests (require TSZ + DB + Redis)
export TSZ_BASE_URL=http://localhost:8080
go test ./tests/integration/...

# E2E smoke + streaming (require TSZ + DB + Redis)
export TSZ_BASE_URL=http://localhost:8080
go test ./tests/e2e/...
```

> Note: Some gateway tests depend on an upstream LLM being configured via `AI_MODEL_URL` / `AI_MODEL`. If the upstream is not reachable, tests are designed to **skip** instead of hard-fail, to keep the suite robust across different environments.

---
## Relation to `examples/`

The `examples/` directory contains practical demonstration programs and integration examples:

- `examples/go-sdk-demo/`: Shows how to use the Go client library (`tszclient-go`)
- `examples/python-sdk-demo/`: Demonstrates the Python client library usage
- `examples/go-bedrock-gateway/`: AWS Bedrock integration example
- `examples/go-llm-safe-pipeline/`: End-to-end LLM pipeline with guardrails
- `examples/llm-redteam-playground-python/`: Security testing and red team scenarios

The `tests/` tree provides automated testing in standard `go test` format, while `examples/` offers:

- Manual integration testing and exploration
- Real-world usage patterns and best practices
- Performance testing and load scenarios
- Educational resources for developers

For automated testing, use the `tests/` directory. For learning and manual testing, explore the `examples/` directory.
