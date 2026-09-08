# TSZ Deployment Guide

This guide covers Kubernetes deployment with the Helm chart under
`deployment/helm/thyris-sz`.

For local Docker Compose setup, see `docs/QUICK_START.md`.

---

## Prerequisites

- Kubernetes 1.23+
- Helm 3
- A container image for the API, built with `deployment/docker/Dockerfile`
- Optional: existing PostgreSQL and Redis services for production

---

## Build and Push the API Image

Build the image from the repository root:

```bash
docker build -f deployment/docker/Dockerfile -t ghcr.io/thyrisai/thyris-sz:0.1.0 .
docker push ghcr.io/thyrisai/thyris-sz:0.1.0
```

For local clusters such as kind or minikube, load or build the image into the
cluster runtime instead of pushing to a registry.

---

## Install with Bundled PostgreSQL and Redis

The chart includes simple PostgreSQL and Redis deployments for development,
POC and single-cluster evaluation environments.

```bash
helm upgrade --install thyris-sz deployment/helm/thyris-sz \
  --namespace thyris-sz \
  --create-namespace \
  --set image.repository=ghcr.io/thyrisai/thyris-sz \
  --set image.tag=0.1.0 \
  --set secrets.aiApiKey=ollama \
  --set secrets.adminApiKey=change-me-in-production
```

The default API service is `ClusterIP` on port `8080`.

Check status:

```bash
kubectl -n thyris-sz get pods
kubectl -n thyris-sz get svc
```

Run the chart smoke test:

```bash
helm test thyris-sz -n thyris-sz
```

Port-forward the API:

```bash
kubectl -n thyris-sz port-forward svc/thyris-sz 8080:8080
curl http://localhost:8080/healthz
curl http://localhost:8080/ready
```

---

## Production-Style Install with External Dependencies

For production, use managed PostgreSQL and Redis where possible. Disable the
bundled services and pass connection strings through an existing Kubernetes
Secret.

Create a secret:

```bash
kubectl -n thyris-sz create secret generic thyris-sz-runtime \
  --from-literal=DB_DSN='postgres://USER:PASSWORD@postgres.example.com:5432/thyris?sslmode=require&TimeZone=Europe/Istanbul' \
  --from-literal=REDIS_URL='redis://:PASSWORD@redis.example.com:6379/0' \
  --from-literal=AI_API_KEY='replace-me' \
  --from-literal=ADMIN_API_KEY='replace-me'
```

Install:

```bash
helm upgrade --install thyris-sz deployment/helm/thyris-sz \
  --namespace thyris-sz \
  --create-namespace \
  --set image.repository=ghcr.io/thyrisai/thyris-sz \
  --set image.tag=0.1.0 \
  --set postgresql.enabled=false \
  --set redis.enabled=false \
  --set secrets.create=false \
  --set secrets.existingSecret=thyris-sz-runtime \
  --set config.appMode=PROD \
  --set config.authEnabled=true
```

The existing secret must contain:

- `DB_DSN`
- `REDIS_URL`
- `AI_API_KEY`
- `ADMIN_API_KEY`

---

## Optional Envoy Gateway / BYG Components

The same chart owns the API and Envoy integration. Existing installations are
unchanged because `envoyGateway.enabled` defaults to `false`.

Enable the native controller-managed profile:

```bash
kubectl apply -f config/crd/bases/security.thyris.ai_tszguardrailpolicies.yaml
helm upgrade --install thyris-sz deployment/helm/thyris-sz \
  --namespace tsz-system \
  --create-namespace \
  --set image.repository=ghcr.io/thyrisai/thyris-sz \
  --set image.tag=0.1.0 \
  --set envoyGateway.enabled=true \
  --set envoyGateway.mode=native
```

Use `envoyGateway.mode=manual` for header-resolved policy selection. A manual
`EnvoyExtensionPolicy` can also be rendered by setting
`envoyGateway.attachment.enabled=true` and providing
`envoyGateway.attachment.targetRef.name`.

The chart can additionally manage the ext-proc HPA, PDB, NetworkPolicy,
database migrations, controller RBAC, and Prometheus rules through the nested
`envoyGateway` values. Envoy Gateway and its own CRDs remain platform
prerequisites. Local Kind fixtures are under
`examples/bring-your-gateway/cluster` and are not a production deployment
package.

---

## Common Values

Override values in `deployment/helm/thyris-sz/values.yaml` or pass them with
`--set`.

```yaml
replicaCount: 2

image:
  repository: ghcr.io/thyrisai/thyris-sz
  tag: 0.1.0

config:
  appMode: PROD
  authEnabled: "true"
  corsAllowedOrigins: "https://app.example.com"
  aiProvider: OPENAI_COMPATIBLE
  aiModelUrl: https://api.openai.com/v1
  aiModel: gpt-4o-mini

secrets:
  aiApiKey: replace-me
  adminApiKey: replace-me
```

Enable ingress:

```yaml
ingress:
  enabled: true
  className: nginx
  hosts:
    - host: tsz.example.com
      paths:
        - path: /
          pathType: Prefix
  tls:
    - secretName: tsz-example-com-tls
      hosts:
        - tsz.example.com
```

---

## Notes

- `/healthz` is used for liveness checks.
- `/ready` verifies PostgreSQL and Redis connectivity.
- The bundled PostgreSQL deployment loads `deployment/helm/thyris-sz/files/init.sql`
  only on first database initialization.
- Store production secrets outside Git and prefer `secrets.existingSecret`.
- Enable `config.authEnabled=true` before exposing TSZ outside a trusted network.
