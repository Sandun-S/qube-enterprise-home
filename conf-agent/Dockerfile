# conf-agent v2 Dockerfile
# Combines v1 conf-agent-master structure (scripts, html, network)
# with enterprise stack (WebSocket sync, SQLite, Docker Swarm deploy).
#
# Runtime requirements on Qube:
#   - /boot/mit.txt mounted read-only
#   - /opt/qube/data/ volume for SQLite
#   - /opt/qube/ volume for docker-compose.yml and scripts
#   - Docker socket bind-mounted (/var/run/docker.sock)
#   - Host network mode or appropriate port bindings for web UI
#   - Privileged mode (or specific capabilities) for network config scripts

FROM golang:1.22-alpine AS builder
WORKDIR /app

RUN apk add --no-cache git bash

COPY go.mod go.sum* ./
RUN go mod download 2>/dev/null || true

COPY . .
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -o conf-agent .

# ─── Runtime image ────────────────────────────────────────────────────────────
FROM alpine:3.19

RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    bash \
    docker-cli \
    docker-cli-compose \
    iproute2 \
    iputils \
    cifs-utils \
    rsync \
    util-linux

WORKDIR /app

# Binary
COPY --from=builder /app/conf-agent .

# v1 assets: web UI, maintenance scripts, network templates
COPY html/       ./html/
COPY scripts/    ./scripts/
COPY network/    ./network/

# Default config
COPY config.yml .

# Make scripts executable
RUN chmod +x scripts/*.sh

ENTRYPOINT ["./conf-agent"]
