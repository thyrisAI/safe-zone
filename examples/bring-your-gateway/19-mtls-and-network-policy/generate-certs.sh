#!/usr/bin/env bash
# Generates throwaway credentials for the local Kind mTLS example. The output
# is deliberately ignored by Git and must never be used outside this example.
set -euo pipefail

output_dir="${1:-$(dirname "$0")/certs}"
rm -rf "$output_dir"
mkdir -p "$output_dir"

openssl ecparam -name prime256v1 -genkey -noout -out "$output_dir/ca.key"
openssl req -x509 -new -sha256 -days 30 -key "$output_dir/ca.key" \
  -out "$output_dir/ca.crt" -subj '/O=TSZ BYG example/CN=tsz-byg-demo-ca'

cat >"$output_dir/server.ext" <<'EOF'
subjectAltName=DNS:tsz-ext-proc.tsz-byg-demo.svc,DNS:tsz-ext-proc.tsz-byg-demo.svc.cluster.local
extendedKeyUsage=serverAuth
keyUsage=critical,digitalSignature,keyEncipherment
EOF
openssl ecparam -name prime256v1 -genkey -noout -out "$output_dir/server.key"
openssl req -new -key "$output_dir/server.key" -out "$output_dir/server.csr" \
  -subj '/O=TSZ BYG example/CN=tsz-ext-proc.tsz-byg-demo.svc'
openssl x509 -req -sha256 -days 30 -in "$output_dir/server.csr" \
  -CA "$output_dir/ca.crt" -CAkey "$output_dir/ca.key" -CAcreateserial \
  -out "$output_dir/server.crt" -extfile "$output_dir/server.ext"

cat >"$output_dir/client.ext" <<'EOF'
extendedKeyUsage=clientAuth
keyUsage=critical,digitalSignature,keyEncipherment
EOF
openssl ecparam -name prime256v1 -genkey -noout -out "$output_dir/client.key"
openssl req -new -key "$output_dir/client.key" -out "$output_dir/client.csr" \
  -subj '/O=TSZ BYG example/CN=envoy-gateway'
openssl x509 -req -sha256 -days 30 -in "$output_dir/client.csr" \
  -CA "$output_dir/ca.crt" -CAkey "$output_dir/ca.key" -CAcreateserial \
  -out "$output_dir/client.crt" -extfile "$output_dir/client.ext"

rm -f "$output_dir"/*.csr "$output_dir"/*.ext "$output_dir"/*.srl "$output_dir/ca.key"
printf 'Generated local-only certificates in %s\n' "$output_dir"
