#!/usr/bin/env bash
set -euo pipefail

mkdir -p certs
cd certs

# 1. Root CA (3650 днів)
openssl req -x509 -newkey rsa:4096 -nodes \
  -keyout ca.key -out ca.crt -days 3650 \
  -subj "/C=UA/O=ZeroTrust-Corp/CN=ZeroTrust-Root-CA"

# 2. Server CSR + SAN extension
cat > server.ext << 'EOF'
authorityKeyIdentifier=keyid,issuer
basicConstraints=CA:FALSE
keyUsage = digitalSignature, nonRepudiation, keyEncipherment, dataEncipherment
subjectAltName = @alt_names

[alt_names]
DNS.1 = localhost
IP.1 = 127.0.0.1
EOF

openssl req -newkey rsa:2048 -nodes \
  -keyout server.key -out server.csr \
  -subj "/C=UA/O=ZeroTrust-Corp/CN=alert-gateway-server"

openssl x509 -req -in server.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out server.crt -days 825 -sha256 -extfile server.ext

# 3. Client CSR (CN=dispatcher-agent-01)
openssl req -newkey rsa:2048 -nodes \
  -keyout client.key -out client.csr \
  -subj "/C=UA/O=ZeroTrust-Corp/CN=dispatcher-agent-01"

openssl x509 -req -in client.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out client.crt -days 825 -sha256

# Прибираємо тимчасові файли запитів та .ext
rm -f server.csr client.csr server.ext ca.srl

echo "PKI згенеровано у каталозі certs/:"
ls -la