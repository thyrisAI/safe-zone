# Contributing to TSZ (Thyris Safe Zone)

First of all, thank you for your interest in contributing to TSZ. This project aims to be a production‑ready, security‑sensitive gateway, so we hold code quality, tests and documentation to a high standard.

This document explains how to set up a local development environment, how to run tests, and how we generally work with issues and pull requests.

---

## Code of Conduct

By participating in this project, you agree to abide by our [Code of Conduct](CODE_OF_CONDUCT.md).

If you witness or experience unacceptable behavior, please report it as described in the Code of Conduct.

---

## Project Overview

TSZ is a Go‑based PII Detection and Guardrails Gateway. The main components are:

- **HTTP gateway & APIs** (root: `main.go`, handlers under `internal/handlers/`)
- **Detection & guardrails engine** (under `internal/guardrails/` and `internal/ai/`)
- **Persistence & cache** (PostgreSQL + Redis, under `internal/database/` and `internal/cache/`)
- **Public client SDKs and examples** (under `pkg/` and `examples/`)
- **Automated tests** (under `tests/` with `unit`, `integration` and `e2e` packages)

Before working on larger features, we recommend reading:

- `docs/WHAT_IS_TSZ.md`
- `docs/ARCHITECTURE_SECURITY.md`
- `docs/API_REFERENCE.md`
- `ROADMAP.md`

---

## Setting Up a Development Environment

### Prerequisites

- Go (matching the version in `go.mod`)
- Docker and Docker Compose (for PostgreSQL and Redis)

### Running TSZ Locally

You can start the full stack (PostgreSQL + Redis + TSZ) using Docker Compose:

```bash
docker-compose up --build
```

This will:
- Build the TSZ binary using the Dockerfile
- Start PostgreSQL and Redis using the configuration in `docker-compose.yml`
- Apply the initial schema from `init.sql`

Once started, the TSZ HTTP API will be available on the port configured in the Dockerfile / environment (see `docs/QUICK_START.md` for details).

### Running the Server Directly with Go

You can also run the server directly using Go, assuming you have PostgreSQL and Redis available:

```bash
go run ./...
```

Before doing so, make sure you have configured the necessary environment variables as documented in `docs/QUICK_START.md` and `docs/ARCHITECTURE_SECURITY.md`.

---

## Running Tests

TSZ aims to have strong test coverage across unit, integration and end‑to‑end (e2e) tests.

### All Tests

From the repository root:

```bash
go test ./...
```

This should run quickly and is expected to pass before opening a pull request.

### Focused Test Suites

- **Unit tests:**
  - Location: `tests/unit/`
  - Command: `go test ./tests/unit/...`

- **Integration tests (HTTP + DB/Redis + AI boundaries):**
  - Location: `tests/integration/`
  - Command:
    ```bash
    # Make sure PostgreSQL and Redis are running, e.g. via docker-compose
    docker-compose up -d
    go test ./tests/integration/...
    ```

- **End‑to‑End tests:**
  - Location: `tests/e2e/`
  - Command:
    ```bash
    docker-compose up -d
    go test ./tests/e2e/...
    ```

Some tests may rely on environment variables or mock AI providers. Refer to comments inside the tests and `docs/QUICK_START.md` for details.

---

## Coding Style and Conventions

This project follows the standard Go style and idioms.

### Formatting

- Use `gofmt` (or your editor’s auto‑formatting) on all Go files.
- CI may reject PRs that contain unformatted code.

### Linting and Static Checks

Where possible, run:

```bash
go vet ./...
```

Please fix warnings that are relevant to your changes.

### Project Structure

- Production code lives under `internal/...` and `pkg/...`.
- Tests live under `tests/...` (unit, integration, e2e).
- Example/demo code lives under `examples/...`.
- Public documentation lives under `docs/...`.

Try to follow existing patterns in the directory you are working in. If you need to introduce a new package or pattern, consider mentioning it in your pull request description.

### API and Backward Compatibility

TSZ is intended for production use, so we try to avoid breaking changes to public APIs lightly.

- For HTTP APIs, changes should be reflected in `docs/API_REFERENCE.md`.
- For public Go APIs (e.g. under `pkg/tszclient-go`), changes should be backwards compatible when possible.

When a breaking change is unavoidable, call it out clearly in your PR description.

---

## Working with Issues

- **Bug reports:** Please include clear steps to reproduce, expected vs actual behavior, and any relevant logs or configuration (with secrets removed).
- **Feature requests:** Explain the use case, not just the desired implementation. This helps us maintain a coherent design.
- **Security issues:** Please **do not** open a public issue. Follow the process described in [`SECURITY.md`](SECURITY.md).

Before filing a new issue, please search existing issues to avoid duplicates.

---

## Pull Requests

1. **Fork** the repository and create a branch from `main`.
2. **Make your changes** in small, focused commits.
3. **Add or update tests** to cover your changes.
4. **Run `go test ./...`** and ensure everything passes.
5. **Update documentation** (`docs/`, `README.md`, etc.) if your change impacts public behavior.
6. **Open a Pull Request** with:
   - A clear title
   - A description of what changed and why
   - Any relevant screenshots or logs

We prefer:

- Small, reviewable PRs over large “mega‑PRs”
- Clear commit messages that explain the intent
- Early drafts / WIP PRs for larger refactors, so we can discuss design

---

## License

By contributing to this repository, you agree that your contributions will be licensed under the terms of the repository’s main license, which is [Apache License 2.0](LICENSE).
