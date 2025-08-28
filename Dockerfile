## Multi-stage build for Fly.io deployment

# Builder: Go on Alpine (musl) to match Alpine runtime for CGO sqlite3
FROM golang:1.25.0-alpine AS builder

RUN apk add --no-cache build-base sqlite-dev ca-certificates tzdata

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


############################
# Runtime image (Alpine 3.22)
############################
FROM alpine:3.22

WORKDIR /app

# Runtime deps: CA certs for TLS and sqlite-libs for CGO sqlite3
RUN apk add --no-cache ca-certificates sqlite-libs tzdata \
    && adduser -D -u 10001 app \
    && mkdir -p /data \
    && chown -R app:app /data

# Default DB path; mount a Fly volume to /data for persistence
ENV DB_FILE=/data/bot.db

COPY --from=builder /out/fight-night-bot /app/fight-night-bot
COPY --from=builder /out/data /data

USER app

ENTRYPOINT ["/app/fight-night-bot"]
