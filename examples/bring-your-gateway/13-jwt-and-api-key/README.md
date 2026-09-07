# JWT authentication and request masking

This Kind integration test attaches an Envoy Gateway `SecurityPolicy` and TSZ
guardrails to the same `HTTPRoute`. A missing or invalid JWT can invoke
`ext_proc` only on the response path. With the default global
`TSZ_FAIL_MODE=closed`, TSZ therefore replaces Envoy's local `401` with its
safe `403` response; a valid JWT reaches TSZ and has synthetic
email PII masked before the mock provider receives it.

The JWKS and JWT are public Envoy Gateway v1.8.3 test fixtures only. They are
not credentials and must not be used outside this local test.

Run it with:

```bash
examples/bring-your-gateway/shared/run.sh examples/bring-your-gateway/13-jwt-and-api-key
examples/bring-your-gateway/shared/cleanup.sh examples/bring-your-gateway/13-jwt-and-api-key
```
