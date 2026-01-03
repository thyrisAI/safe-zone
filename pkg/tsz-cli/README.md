# TSZ CLI

The **TSZ CLI** (`tsz`) is a command-line tool for interacting with your Safe Zone Gateway. It provides easy access to detection APIs and full management capabilities for patterns, lists, and guardrails.

## Installation

You can install the CLI directly from the source:

```bash
go install github.com/thyrisAI/safe-zone/pkg/tsz-cli@latest
```

Or build it locally:

```bash
cd pkg/tsz-cli
go build -o tsz
```

## Configuration

By default, the CLI connects to `http://localhost:8080`.

You can configure the connection via flags:

- `--url`: The URL of your TSZ instance.
- `--key`: The Admin API Key (required for management commands like `add`, `remove`, `import`).

Example:

```bash
tsz scan --text "hello" --url "https://tsz.example.com"
```

## Commands

### Scan for PII

Scan plain text or files for sensitive data.

```bash
# Scan a string
tsz scan --text "My email is user@example.com"

# Scan a file
tsz scan --file ./document.txt

# Specify a Request ID (RID) for audit logs
tsz scan --text "test" --rid "CLI-TEST-001"
```

### Manage Patterns

View and modify regex detection patterns.

```bash
# List all patterns
tsz patterns list

# Add a new pattern
tsz patterns add --name "PROJECT_CODE" --regex "PROJ-\d{4}" --category "SECRET" --desc "Internal Project Codes"

# Remove a pattern
tsz patterns remove <ID>
```

### Manage Lists

Manage Allowlist (ignored items) and Blocklist (forbidden items).

```bash
# Allowlist
tsz allowlist list
tsz allowlist add --value "support@company.com" --desc "Support Email"
tsz allowlist remove <ID>

# Blocklist
tsz blocklist list
tsz blocklist add --value "CONFIDENTIAL" --desc "Restricted keyword"
tsz blocklist remove <ID>
```

### Manage Validators

Configure AI Guardrails and custom validators.

```bash
# List validators
tsz validators list

# Add a new AI Guardrail
tsz validators add --name "TOXICITY" --type "AI_PROMPT" --rule "Is this text toxic? YES/NO" --expected "NO"

# Remove a validator
tsz validators remove <ID>
```

### Import Templates

Import full policy packs (JSON) to setup multiple rules at once.

```bash
tsz templates import --file ./policy_pack.json
```
