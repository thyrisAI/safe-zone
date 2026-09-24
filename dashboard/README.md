# Safe Zone Dashboard

A lightweight, read-only web dashboard for Thyris Safe Zone, built with
React, Vite, and TypeScript. See [issue #16](https://github.com/thyrisAI/safe-zone/issues/16)
for the original feature request.

For a general overview of what this dashboard shows and how it fits into
the rest of the project, see the "Dashboard (Web UI)" section in the
[repository root README](../README.md).

## Prerequisites

- Node.js 20.19+ or 22.12+ (required by the Vite tooling used here)
- A running Safe Zone backend, reachable at `http://localhost:8080`
  (see the root [Quick Start guide](../docs/QUICK_START.md))

## Development

```bash
npm install
npm run dev
```

Open `http://localhost:5173`. Requests to the backend are proxied during
development — see `vite.config.ts` for the proxy configuration. No backend
URL is hardcoded anywhere in the frontend code.

## Testing

```bash
npm run test
```

## Type Checking

```bash
npx tsc --noEmit
```

## Building

```bash
npm run build
```

Note: no production serving strategy has been decided yet for the built
output. See `../docs/DASHBOARD_PRODUCTION_NOTES.md` for the open options
under discussion.

## Project Structure

```text
src/
  api/          HTTP client and per-resource fetch functions
  components/   Reusable UI pieces (status badges, pills)
  layouts/      App shell (sidebar, header)
  pages/        One component per route (Overview, Patterns, ...)
  types/        TypeScript types mirroring backend response shapes
```

## Environment Variables

| Variable | Default | Purpose |
|---|---|---|
| `VITE_API_BASE_URL` | `/api` | Base path used by the API client; resolved through the Vite dev proxy to the backend. |

## Known Limitations

- Request counters and recent events are stored in memory on the backend
  and reset on every backend restart (see `internal/metrics/store.go`).
  This is a deliberate trade-off for the initial version, not a bug.
- Enable/disable actions for patterns and guardrails are not implemented,
  as the backend does not currently expose a reliable update endpoint for
  them.
- Allowlist and blocklist management are out of scope for this initial
  version.