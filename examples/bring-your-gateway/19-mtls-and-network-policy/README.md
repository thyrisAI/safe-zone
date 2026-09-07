# mTLS and NetworkPolicy

This production-oriented Kind example secures the Envoy Gateway to TSZ
external-processor gRPC hop with mutual TLS. Envoy verifies the TSZ server
certificate, and TSZ requires a client certificate signed by the same local
example CA. The base deployment's NetworkPolicy remains in force, so only the
Envoy data-plane pods may connect to TCP/9002.

The certificates are local test fixtures only: they use a new ECDSA P-256 CA
on every run, expire after 30 days, and are never committed. In production,
replace the generated Secret/ConfigMap with certificates issued and rotated by
the organization's PKI through cert-manager, Vault, or an equivalent service.

## Prerequisites

- Docker, Kind, kubectl, OpenSSL, and jq
- The repository's pinned Envoy Gateway `v1.8.3` bootstrap dependencies

## Run and verify

```bash
examples/bring-your-gateway/19-mtls-and-network-policy/run.sh
```

The script verifies the mTLS `Backend` and `EnvoyExtensionPolicy` resources are
accepted and that TSZ is healthy after loading its server certificate and
client CA. It leaves the route ready for the repository's existing safe
request smoke test.

To demonstrate that the client identity is required, remove the Envoy client
certificate Secret, restart the Envoy deployment, and repeat the request. With
`failOpen: false`, the request must fail rather than bypass TSZ. Restore the
Secret before continuing the demo.

## Configuration fields

`TSZ_GRPC_TLS_CERT_FILE` and `TSZ_GRPC_TLS_KEY_FILE` identify TSZ's server
certificate and private key. `TSZ_GRPC_TLS_CLIENT_CA_FILE` is the CA bundle
used to verify Envoy's client certificate. TSZ requires all three fields
together and refuses to start if any are absent or unreadable. TLS 1.2 is the
minimum supported version and client certificates are mandatory.

The `Backend` configuration provides the complementary client side: `sni`
matches the TSZ certificate SAN, `caCertificateRefs` trusts its issuing CA,
and `clientCertificateRef` gives Envoy its client identity. These resources
are namespaced deliberately: private material stays in `tsz-byg-demo`.

## Cleanup

```bash
examples/bring-your-gateway/19-mtls-and-network-policy/cleanup.sh
```

Cleanup deletes only the mTLS resources and restores the reusable insecure
development deployment. Delete the Kind cluster separately when it is no
longer needed.
