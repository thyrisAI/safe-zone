# BYG policy-health dashboards and alerts

Apply `prometheus-rules.yaml` only in clusters with Prometheus Operator. It
alerts on replica policy-version skew, pending activation delivery, and
reconciliation failures while a last-known-good snapshot remains loaded.

Grafana panels should graph `tsz_policy_cache_snapshot_info` by `pod`,
`policy`, and `version`; policy-cache reconciliation success/error; controller
activation outcomes; and active snapshots per pod. Policy/version identify
immutable governed objects; never add RID, tenant, request content, or raw PII
to a dashboard variable or metric label. See `docs/operations/BYG_OBSERVABILITY.md`
and `docs/operations/BYG_TROUBLESHOOTING.md` for triage.
