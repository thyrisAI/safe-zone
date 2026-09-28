# Safe Zone Dashboard

A lightweight web dashboard for Thyris Safe Zone, built with
React, Vite, and TypeScript. See [issue #16](https://github.com/thyrisAI/safe-zone/issues/16)
for the original feature request.

For a general overview of what this dashboard shows and how it fits into
the rest of the project, see the "Dashboard (Web UI)" section in the
[repository root README](../README.md).

## Prerequisites

- Node.js 20.19+ or 22.12+ (required by the Vite tooling used here)
- A running Safe Zone backend, reachable at `http://localhost:8080`
  (see the root [Quick Start guide](../docs/QUICK_START.md))

### Local development and CORS

In development, the Vite proxy removes the `Origin` header before
forwarding requests, so no CORS configuration is needed in the backend's
`.env`. This applies to the dev server only; a production deployment must
configure `CORS_ALLOWED_ORIGINS` itself.

## Development

    npm install
    npm run dev

Open `http://localhost:5173`. Requests to the backend are proxied during
development — see `vite.config.ts` for the proxy configuration. No backend
URL is hardcoded anywhere in the frontend code.

## Testing

    npm run test

## Type Checking

    npx tsc --noEmit

## Building

    npm run build

Note: no production serving strategy has been decided yet for the built
output. See `../docs/DASHBOARD_PRODUCTION_NOTES.md` for the open options
under discussion.

## Project Structure

    src/
      api/          HTTP client and per-resource fetch functions
      components/   Reusable UI pieces (status badges, pills, modals, dialogs)
      layouts/      App shell (sidebar, header)
      pages/        One component per route (Overview, Patterns, Lists, ...)
      types/        TypeScript types mirroring backend response shapes

## Environment Variables

| Variable | Default | Purpose |
|---|---|---|
| `VITE_API_BASE_URL` | `/api` | Base path used by the API client; resolved through the Vite dev proxy to the backend. |

## Known Limitations

- Request counters and recent events are stored in memory on the backend
  and reset on every backend restart (see `internal/metrics/store.go`).
  This is a deliberate trade-off for the initial version, not a bug.
- Patterns can be enabled or disabled from the Patterns screen (backed by
  the new `PATCH /patterns/{id}` endpoint). Guardrails cannot: validators
  have no active/inactive field in the data model, so a toggle would need
  a schema change and engine support, which is left as an open question
  for maintainers.
- Allowlist and blocklist entries can be viewed, added, and deleted from
  the Lists screen. Editing an existing entry is not supported (the
  backend has no update endpoint for these); an entry must be deleted
  and re-added instead. There is no maximum length or format validation
  on the `Value` field beyond requiring it to be non-empty.
