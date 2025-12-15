# tszclient-go – Go client for TSZ (Thyris Safe Zone)

`tszclient-go` is a lightweight Go client for interacting with a TSZ (Thyris Safe Zone) deployment.

It provides:

- A typed interface for the **/detect** endpoint (PII detection & guardrails)
- A helper for the **OpenAI-compatible LLM gateway** (`/v1/chat/completions`)
- Simple configuration via `Config` and a single `Client` type

> Note: This package currently lives inside the `thyris-sz` repository. When/if it is published as a separate module, import paths will be adjusted accordingly.

---

## Installation

From within this repository, you can import it as:

```go
import "thyris-sz/pkg/tszclient-go"
```

If you want to consume it via the GitHub module path (for example from an
external project), you can import it as:

```go
import tszclient "github.com/thyrisAI/safe-zone/pkg/tszclient-go"
```

See `examples/go-sdk-demo` in this repository for a complete, runnable
example that:

- Uses the GitHub import path
- Has its own `go.mod` demonstrating how to depend on `github.com/thyrisAI/safe-zone`
- Calls both `/detect` and the `/v1/chat/completions` gateway from a single program

---

## Configuration

```go
type Config struct {
    BaseURL    string
    HTTPClient *http.Client
}

client, err := tszclient.New(tszclient.Config{
    BaseURL: "http://localhost:8080", // TSZ gateway URL
})
if err != nil {
    log.Fatalf("failed to create tsz client: %v", err)
}
```

- `BaseURL` should point to your TSZ instance (gateway or direct).
- `HTTPClient` is optional; if nil, a default client with 60 second timeout is used.

---

## Using /detect via the client

### Contexts, timeouts, and cancellation

The client methods accept a `context.Context`, so you can control deadlines
and cancellation per call. By default, if you don't provide a custom
`HTTPClient` in `Config`, the client uses an `http.Client` with a 60 second
timeout; you can also apply shorter/longer timeouts via `context`:

```go
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
```

You can reuse the same client with different contexts depending on the
requirements of each call.

### Request & Response Types

```go
type DetectRequest struct {
    Text           string   `json:"text"`
    RID            string   `json:"rid,omitempty"`
    ExpectedFormat string   `json:"expected_format,omitempty"`
    Guardrails     []string `json:"guardrails,omitempty"`
}

type DetectResponse struct {
    RedactedText      string            `json:"redacted_text,omitempty"`
    Detections        []DetectionResult `json:"detections,omitempty"`
    ValidatorResults  []ValidatorResult `json:"validator_results,omitempty"`
    Breakdown         map[string]int    `json:"breakdown"`
    Blocked           bool              `json:"blocked"`
    ContainsPII       bool              `json:"contains_pii"`
    OverallConfidence string            `json:"overall_confidence"`
    Message           string            `json:"message,omitempty"`
}
```

### Example (basic Detect)

```go
package main

import (
    "context"
    "log"
    "time"

    tszclient "thyris-sz/pkg/tszclient-go"
)

func main() {
    // Per-call timeout via context
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    client, err := tszclient.New(tszclient.Config{
        BaseURL: "http://localhost:8080",
    })
    if err != nil {
        log.Fatalf("failed to create tsz client: %v", err)
    }

    resp, err := client.Detect(ctx, tszclient.DetectRequest{
        Text:       "Contact me at john@example.com",
        RID:        "RID-GO-001",
        Guardrails: []string{"TOXIC_LANGUAGE"},
    })
    if err != nil {
        log.Fatalf("detect failed: %v", err)
    }

    if resp.Blocked {
        log.Printf("request blocked by TSZ: %s", resp.Message)
        return
    }

    log.Printf("Redacted: %s", resp.RedactedText)
}
```

### Optional convenience helpers

For common detect flows, you can use small helper functions to keep your
call sites concise. The client exposes a `DetectText` wrapper and
functional options such as `WithGuardrails`, `WithRID`, and
`WithExpectedFormat`:

```go
resp, err := client.DetectText(
    ctx,
    "Contact me at john@example.com",
    tszclient.WithRID("RID-GO-002"),
    tszclient.WithGuardrails("TOXIC_LANGUAGE", "FINANCIAL_DATA"),
)
if err != nil {
    log.Fatalf("detect failed: %v", err)
}
```

These helpers are completely optional; you can always construct a full
`DetectRequest` and call `Detect` directly if you prefer explicit
request structs.

---

## Using the LLM Gateway (/v1/chat/completions)

The client also makes it easy to call the OpenAI‑compatible TSZ LLM gateway.

### Types

```go
type ChatCompletionRequest struct {
    Model    string                   `json:"model"`
    Messages []map[string]interface{} `json:"messages"`
    Stream   bool                     `json:"stream,omitempty"`
    Extra    map[string]interface{}   `json:"-"`
}

type ChatCompletionResponse map[string]interface{}
```

### Example (non‑streaming)

```go
package main

import (
    "context"
    "fmt"
    "log"

    tszclient "thyris-sz/pkg/tszclient-go"
)

func main() {
    ctx := context.Background()

    client, err := tszclient.New(tszclient.Config{
        BaseURL: "http://localhost:8080",
    })
    if err != nil {
        log.Fatalf("failed to create tsz client: %v", err)
    }

    resp, err := client.ChatCompletions(ctx, tszclient.ChatCompletionRequest{
        Model: "llama3.1:8b",
        Messages: []map[string]interface{}{
            {"role": "user", "content": "Hello via TSZ gateway"},
        },
        Stream: false,
    }, map[string]string{
        "X-TSZ-RID":        "RID-GW-GO-001",
        "X-TSZ-Guardrails": "TOXIC_LANGUAGE",
    })
    if err != nil {
        log.Fatalf("chat completions failed: %v", err)
    }

    choices, ok := resp["choices"].([]interface{})
    if !ok || len(choices) == 0 {
        log.Println("no choices in response")
        return
    }

    first, _ := choices[0].(map[string]interface{})
    msg, _ := first["message"].(map[string]interface{})
    content, _ := msg["content"].(string)

    fmt.Println("LLM response via TSZ:")
    fmt.Println(content)
}
```

In this call:
- TSZ first scans the user message with `/detect` (PII + guardrails) and masks or blocks according to your policies.
- If the input is safe, TSZ forwards only the redacted prompt to the upstream LLM.
- When the LLM responds, TSZ applies the same guardrail set on the assistant output before returning it to the client.

---

## Error handling

When TSZ returns a non‑2xx HTTP response, the client returns an `APIError`:

```go
type APIError struct {
    StatusCode int
    Body       []byte
}
```

This allows you to inspect the raw JSON error body, including TSZ‑specific
error codes such as `tsz_content_blocked` or `tsz_output_blocked`.

---
