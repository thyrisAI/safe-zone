# Go SDK Demo – using TSZ client from GitHub

This example shows how to consume the TSZ Go SDK from the GitHub repository:

```go
import tszclient "github.com/thyrisAI/safe-zone/pkg/tszclient-go"
```

It demonstrates **both**:

- Calling the core `/detect` API for PII detection and guardrails
- Calling the OpenAI-compatible LLM gateway `/v1/chat/completions`

The example lives alongside the TSZ source code in this repo, but it is
structured as if it were an external project with its own `go.mod`.

---

## Files

- `main.go`
  - Creates a TSZ client with `BaseURL = http://localhost:8080`
  - Sends a `/detect` request using `DetectText` and prints the redacted text
  - Sends a `/v1/chat/completions` request using `ChatCompletions` and prints the LLM reply

- `go.mod`
  - Declares a separate module name (`example.com/tsz-go-sdk-external-demo`)
  - Depends on `github.com/thyrisAI/safe-zone`
  - Uses a `replace` directive so that **inside this repository** the SDK is
    resolved from the local checkout instead of fetching it from GitHub

In your own project, you can drop the `replace` line and use a normal
`go get` with a tagged version or commit.

---

## How to run this example (inside this repo)

From the repository root:

```bash
cd examples/go-sdk-demo

# Optional, but recommended to ensure dependencies are in place
go mod tidy

# Run the demo
go run .
```

You should see log output similar to:

- `/detect` response:
  - Redacted text (e.g. email addresses masked)
  - A breakdown map with detection counts
- `/v1/chat/completions` response:
  - A short reply from the LLM, returned via the TSZ gateway

**Prerequisites:**

- TSZ is running locally and accessible at `http://localhost:8080`
  (for example via `docker-compose up` as described in the main README/QUICK_START).
- The upstream LLM configured in `.env` / `AI_MODEL_URL` / `AI_MODEL`
  is reachable and supports the chosen model (e.g. `llama3.1:8b`).

---

## Using this pattern in your own project

In a separate project, you can follow the same pattern without the `replace`:

```bash
mkdir my-tsz-demo
cd my-tsz-demo

go mod init my-tsz-demo

go get github.com/thyrisAI/safe-zone@<version>
```

Then in your `main.go`:

```go
import tszclient "github.com/thyrisAI/safe-zone/pkg/tszclient-go"
```

You can copy/paste the logic from `main.go` in this directory and adjust:

- `BaseURL` to point at your TSZ deployment
- The model name to match your configured upstream LLM
- Guardrail headers / RIDs according to your needs

For more details on the TSZ Go client, see:

- `pkg/tszclient-go/README.md` in this repository
