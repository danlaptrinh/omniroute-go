FROM golang:1.23-alpine AS builder
WORKDIR /src
RUN apk add --no-cache ca-certificates tzdata
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o /out/omni-gateway ./cmd/omni-gateway

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata curl
WORKDIR /app
COPY --from=builder /out/omni-gateway /usr/local/bin/omni-gateway
RUN mkdir -p /data
ENV GATEWAY_LISTEN_ADDR="0.0.0.0:8787"
ENV GATEWAY_OAUTH_CALLBACK_ADDR="0.0.0.0:20128"
ENV GATEWAY_CONFIG_FILE="/data/gateway_config.json"
EXPOSE 8787 20128
ENTRYPOINT ["/usr/local/bin/omni-gateway"]

