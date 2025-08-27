## Multi-stage build for Fly.io deployment

# Builder with CGO + SQLite3 headers on Chainguard (lower CVEs)
FROM cgr.dev/chainguard/go:1.25 AS builder

RUN apk add --no-cache build-base sqlite-dev ca-certificates

WORKDIR /src

# Cache deps first
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build with embedded time zone data to avoid tzdata at runtime
ENV CGO_ENABLED=1
RUN go build -tags timetzdata -ldflags "-s -w" -o /out/fight-night-bot ./cmd/fight-night-bot
RUN mkdir -p /out/data


# Runtime image with just what we need
# Chainguard glibc-dynamic minimizes known CVEs versus generic Debian base
FROM cgr.dev/chainguard/glibc-dynamic:latest

WORKDIR /app

# Default DB path; mount a Fly volume to /data for persistence
ENV DB_FILE=/data/bot.db

# Pre-create a writable /data directory owned by nonroot user
COPY --from=builder --chown=nonroot:nonroot /out/data /data

COPY --from=builder /out/fight-night-bot /app/fight-night-bot

USER nonroot

ENTRYPOINT ["/app/fight-night-bot"]
