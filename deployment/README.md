# Deployment

All deployable packaging lives in this directory.

- `docker/` contains the production Dockerfile and the local Docker Compose
  stack.
- `helm/thyris-sz/` contains the Kubernetes chart. Envoy Gateway support is
  controlled by `envoyGateway.enabled` in the chart values.

Run the local stack from the repository root:

```bash
docker compose -f deployment/docker/docker-compose.yml up --build
```

Render the Kubernetes chart:

```bash
helm template thyris-sz deployment/helm/thyris-sz
```
