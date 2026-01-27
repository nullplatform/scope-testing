#!/bin/bash
# Generate self-signed certificates for integration test TLS proxy
# These certificates are used by nginx to proxy requests to mock services

CERT_DIR="$(dirname "$0")/certs"
mkdir -p "$CERT_DIR"

# Generate private key
openssl genrsa -out "$CERT_DIR/key.pem" 2048 2>/dev/null

# Generate self-signed certificate with all required SANs
# These hostnames match the nginx proxy configuration
openssl req -new -x509 \
  -key "$CERT_DIR/key.pem" \
  -out "$CERT_DIR/cert.pem" \
  -days 365 \
  -subj "/CN=integration-test-proxy" \
  -addext "subjectAltName=DNS:api.nullplatform.com,DNS:management.azure.com,DNS:login.microsoftonline.com,DNS:devstoreaccount1.blob.core.windows.net,DNS:localhost" \
  2>/dev/null

echo "Certificates generated in $CERT_DIR"
