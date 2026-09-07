# Shared processor scaling

This example sends a safe OpenAI-compatible request through Envoy Gateway to a
shared, two-replica `tsz-ext-proc` Service. It verifies that the request
reaches the mock upstream and that the deployment, HPA, and PDB backing the
shared processor are present and healthy.

It demonstrates a shared processor, not a capacity benchmark. The HPA is not
artificially driven by synthetic CPU/memory load; select HPA targets and
maximum replicas from measured production traffic.

## Prerequisites

- Docker, Kind, kubectl and jq
- Envoy Gateway v1.8.3 and Gateway API v1.5.1, installed by the repository
  Kind bootstrap

## Run and verify

```bash
examples/bring-your-gateway/shared/run.sh examples/bring-your-gateway/17-shared-processor
```

The smoke test verifies two ready processor replicas, an HPA with a minimum of
two replicas, and a PDB that allows only one voluntary disruption. The local
mock provider receives the safe request. No customer data or credentials are
used.

## Security and limitations

The shared Service still needs trusted route identity, NetworkPolicy, and
TLS/mTLS in production. HPA/PDB improve availability during normal scaling and
voluntary disruption; they do not make PostgreSQL, Redis, Envoy, or a single
cluster failure highly available.

## Troubleshooting and cleanup

Check `kubectl -n tsz-byg-demo get deploy,hpa,pdb,pods` if the replica check
fails. The common shared example cleanup removes the temporary policy Job and
ConfigMap; remove the Kind cluster separately when finished.
