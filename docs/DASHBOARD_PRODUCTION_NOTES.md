# Dashboard — Production Deployment Notes

This document accompanies the dashboard added in #16. It is intentionally
**not** a finished deployment guide — it lays out the open decisions that
need a maintainer call before the dashboard is wired into production
Docker/CI configuration.

## Current State

The dashboard currently runs only via `npm run dev` (Vite dev server,
port 5173) against a Safe Zone backend on port 8080, connected through
Vite's dev-time proxy (`vite.config.ts`). No production build path has
been wired into `docker-compose.yml` or the `Dockerfile` yet.

## Open Decision 1: How Should the Dashboard Be Served in Production?

Three options were considered, none implemented:

| Option | Description | Trade-offs |
|---|---|---|
| **A. Separate frontend container** | Add a second Dockerfile (e.g. nginx serving the Vite build output) and a new service in `docker-compose.yml`. | Clean separation, independent scaling/deploys. Adds a new container, a new port, and CORS becomes relevant again unless fronted by a shared reverse proxy. |
| **B. Embed into the Go binary** | Run `npm run build`, embed the resulting `dist/` via Go's `embed` package, and serve it from the existing `api` binary on the same port. | Single binary, same-origin (no CORS), simplest ops story. Requires changing the multi-stage `Dockerfile` to run an `npm run build` step before the Go build, and adding a static-file route to `main.go`. |
| **C. Reverse proxy (nginx/Caddy)** | A proxy container serves the built static files and forwards `/api/*` (or similar) to the Go backend, giving same-origin access without embedding. | Same-origin benefits without touching the Go binary. Adds one more moving part (proxy config) to operate and keep in sync with route changes. |

**No option has been implemented in this PR.** Implementing the wrong one
means non-trivial rework later (e.g. moving from embed to a separate
container changes the build pipeline significantly), so this is left as
an open question for the maintainers rather than a unilateral choice.

## Open Decision 2: Should the Dashboard Be Enabled by Default?

Per the issue's own "Open Questions" section, whether the dashboard ships
enabled-by-default or behind a feature flag was left unresolved.

Given that Safe Zone is a security product, the recommendation from this
PR's author is to default the dashboard to **off**, gated by an
environment variable (e.g. `ENABLE_DASHBOARD=true`), so that:

- Existing deployments do not silently gain a new HTTP surface on upgrade.
- The dashboard can be adopted opt-in, consistent with the issue's
  "initial foundation" framing.

This has **not** been implemented — it is a recommendation pending
maintainer confirmation, not a default assumed by the code in this PR.

## What This PR Does Ship

- A fully functional dashboard reachable via `npm run dev`, connecting to
  a locally running Safe Zone instance (see `web/README.md` — TODO in
  FAZ 25 — for setup steps).
- Backend endpoints (`/dashboard/summary`, `/dashboard/events`,
  `/dashboard/config`) that are always available once the Go binary is
  running, regardless of the frontend's deployment story. These are
  read-only and permission-gated (`dashboard:read`) like the rest of the
  API.

## Suggested Next Steps (Not in Scope of This PR)

1. Maintainers pick one of Options A/B/C above.
2. A follow-up PR wires the chosen option into `Dockerfile` /
   `docker-compose.yml`.
3. The `ENABLE_DASHBOARD` flag (if adopted) is implemented in `main.go`,
   likely wrapping the `/dashboard/*` route registrations.