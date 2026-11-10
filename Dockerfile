# Stage 1: Build pure Go standalone binary
FROM golang:1.22-alpine AS builder

WORKDIR /app
RUN apk add --no-cache git ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /omni-gateway ./cmd/omni-gateway

# Stage 2: Minimal hardened Alpine runtime
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata su-exec && \
    addgroup -S omni && adduser -S omni -G omni && \
    mkdir -p /data && chown -R omni:omni /data

WORKDIR /app
COPY --from=builder /omni-gateway /usr/local/bin/omni-gateway

ENV GATEWAY_LISTEN_ADDR="0.0.0.0:8787"
ENV GATEWAY_OAUTH_CALLBACK_ADDR="0.0.0.0:20128"
ENV GATEWAY_CONFIG_FILE="/data/gateway_config.json"

VOLUME ["/data"]
EXPOSE 8787 20128

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8787/health || exit 1

ENTRYPOINT ["/bin/sh", "-c", "chown -R omni:omni /data 2>/dev/null || true; exec su-exec omni /usr/local/bin/omni-gateway \"$@\"", "--"]
