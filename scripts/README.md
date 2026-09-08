# Repository scripts

Operational helpers are grouped here instead of living in separate top-level
directories.

- `database/init.sql` is the canonical local PostgreSQL bootstrap schema.
- `release/` contains the product-specific release scripts invoked by GitHub
  Actions.
- `codegen/boilerplate.go.txt` is the header template used by `make generate`.

The Helm chart carries a packaged copy of the database bootstrap schema because
Helm cannot read files outside its chart directory. Release tests keep that copy
synchronized with `database/init.sql`.
