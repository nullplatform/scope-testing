# GCP Mock API Server
#
# Lightweight mock server that implements the subset of Google Cloud REST APIs
# used by the gcp-functions scope (Cloud Functions gen2, Cloud Run IAM, Compute
# load balancer, Cloud DNS, IAM service accounts), for integration testing
# without real GCP resources.
#
# Build: docker build -t gcp-mock .
# Run:   docker run -p 8080:8080 gcp-mock

FROM golang:1.21-alpine AS builder

WORKDIR /app
COPY go.mod ./
COPY main.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -o gcp-mock .

FROM alpine:3.19
RUN apk --no-cache add ca-certificates curl
WORKDIR /app
COPY --from=builder /app/gcp-mock .
EXPOSE 8080
HEALTHCHECK --interval=5s --timeout=3s --retries=10 \
  CMD curl -f http://localhost:8080/health || exit 1
CMD ["./gcp-mock"]
