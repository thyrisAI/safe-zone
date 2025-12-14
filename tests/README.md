# TSZ Test Suite

This directory contains the automated test suite for Thyris Safe Zone (TSZ). It is structured to separate **unit**, **integration**, and **end-to-end (E2E)** concerns and to keep tests decoupled from production code.

## Structure

```text
tests/
  unit/          # Pure unit tests (logic, helpers, AI client, SIEM, etc.)
  integration/   # HTTP-level integration tests (API + DB + Redis + AI boundary)
  e2e/           # Smoke / sanity and gateway streaming tests
  data/          # JSON fixtures for data-driven tests and golden files
  README.md      # This document
```

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

> Note: test-only helpers are defined in `internal/guardrails/testing_exports.go` and are build-tagged (`//go:build test`). They do not ship in production binaries.

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

> Note: Some gateway tests depend on an upstream LLM being configured via `THYRIS_AI_MODEL_URL` / `THYRIS_AI_MODEL`. If the upstream is not reachable, tests are designed to **skip** instead of hard-fail, to keep the suite robust across different environments.

---
## Relation to `test-scripts/`

The older `test-scripts/` directory contains Go programs intended as **manual test harnesses** and load/stress helpers:

- `test-scripts/main.go`: a manual end-to-end sanity suite and basic load test.
- `test-scripts/gateway-test/main.go`: a manual gateway streaming test harness.

The `tests/` tree covers most of the functional scenarios in a standard `go test` format. The scripts can remain as optional tools for:

- Manual debugging and exploration,
- Ad-hoc load testing in non-CI environments.

In future iterations, heavy load tests can be moved into `tests/perf/` and wired into a separate CI workflow (e.g., nightly runs) if needed.
