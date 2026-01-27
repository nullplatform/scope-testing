# Azure Mock API Server
#
# Lightweight mock server that implements Azure REST API endpoints
# for integration testing without requiring real Azure resources.
#
# Build:
#   docker build -t azure-mock .
#
# Run:
#   docker run -p 8080:8080 azure-mock

FROM golang:1.21-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod ./

# Copy source code
COPY main.go ./

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -o azure-mock .

# Final stage - minimal image
FROM alpine:3.19

# Add ca-certificates for HTTPS (if needed) and curl for healthcheck
RUN apk --no-cache add ca-certificates curl

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/azure-mock .

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=5s --timeout=3s --retries=10 \
  CMD curl -f http://localhost:8080/health || exit 1

# Run the server
CMD ["./azure-mock"]
